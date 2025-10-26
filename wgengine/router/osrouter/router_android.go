// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

//go:build ignore

package osrouter

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/sagernet/netlink"
	"github.com/tailscale/wireguard-go/tun"
	"golang.org/x/sys/unix"
	"tailscale.com/health"
	"tailscale.com/net/netmon"
	"tailscale.com/tsconst"
	"tailscale.com/types/logger"
	"tailscale.com/util/eventbus"
	"tailscale.com/util/linuxfw"
	"tailscale.com/wgengine/router"
)

func init() {
	router.HookNewUserspaceRouter.Set(func(opts router.NewOpts) (router.Router, error) {
		return newUserspaceRouter(opts.Logf, opts.Tun, opts.NetMon, opts.Health, opts.Bus)
	})
	router.HookCleanUp.Set(func(logf logger.Logf, netMon *netmon.Monitor, ifName string) {
		cleanUp(logf, ifName)
	})
}

func newUserspaceRouter(logf logger.Logf, tundev tun.Device, netMon *netmon.Monitor, health *health.Tracker, bus *eventbus.Bus) (router.Router, error) {
	tunname, err := tundev.Name()
	if err != nil {
		return nil, err
	}
	
	nfr, err := linuxfw.New(logf, "")
	if err != nil {
		return nil, fmt.Errorf("creating linuxfw: %w", err)
	}
	
	return &androidRouter{
		logf:    logf,
		tunname: tunname,
		nfr:     nfr,
	}, nil
}

func cleanUp(logf logger.Logf, interfaceName string) {
	rule := netlink.NewRule()
	rule.Priority = tailscaleRulePriority
	rule.Invert = true
	rule.Mark = tsconst.LinuxBypassMarkNum
	rule.MarkSet = true
	rule.Mask = tsconst.LinuxFwmarkMaskNum
	rule.Table = tailscaleRouteTable
	rule.Family = unix.AF_INET
	netlink.RuleDel(rule)

	rule6 := netlink.NewRule()
	rule6.Priority = tailscaleRulePriority
	rule6.Invert = true
	rule6.Mark = tsconst.LinuxBypassMarkNum
	rule6.MarkSet = true
	rule6.Mask = tsconst.LinuxFwmarkMaskNum
	rule6.Table = tailscaleRouteTable
	rule6.Family = unix.AF_INET6
	netlink.RuleDel(rule6)
}

const (
	tailscaleRouteTable   = 52
	tailscaleRulePriority = 70
)

type androidRouter struct {
	logf       logger.Logf
	tunname    string
	linkIndex  int
	addrs      []netip.Prefix
	routes     []netip.Prefix
	ruleAdded  bool
	rule6Added bool
	nfr        linuxfw.NetfilterRunner
}

func (r *androidRouter) Up() error {
	link, err := netlink.LinkByName(r.tunname)
	if err != nil {
		return fmt.Errorf("get link: %w", err)
	}
	r.linkIndex = link.Attrs().Index

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("set link up: %w", err)
	}

	// Add inverted rule for IPv4: packets NOT marked with bypass mark go to tailscale table
	rule := netlink.NewRule()
	rule.Priority = tailscaleRulePriority
	rule.Invert = true
	rule.Mark = tsconst.LinuxBypassMarkNum
	rule.MarkSet = true
	rule.Mask = tsconst.LinuxFwmarkMaskNum
	rule.Table = tailscaleRouteTable
	rule.Family = unix.AF_INET
	if err := netlink.RuleAdd(rule); err != nil {
		r.logf("add ipv4 rule failed (may already exist): %v", err)
	} else {
		r.ruleAdded = true
	}

	// Setup linuxfw chains and hooks
	if err := r.nfr.AddChains(); err != nil {
		return fmt.Errorf("adding firewall chains: %w", err)
	}
	if err := r.nfr.AddHooks(); err != nil {
		return fmt.Errorf("adding firewall hooks: %w", err)
	}
	if err := r.nfr.AddBase(r.tunname); err != nil {
		return fmt.Errorf("adding firewall base rules: %w", err)
	}

	return nil
}

func (r *androidRouter) Set(cfg *router.Config) error {
	if cfg == nil {
		return nil
	}

	link, err := netlink.LinkByName(r.tunname)
	if err != nil {
		return err
	}

	// Check if we have IPv6 address
	hasIPv6Addr := false
	for _, addr := range cfg.LocalAddrs {
		if addr.Addr().Is6() {
			hasIPv6Addr = true
			break
		}
	}

	// Add addresses
	for _, addr := range cfg.LocalAddrs {
		if !contains(r.addrs, addr) {
			if err := r.addAddress(link, addr); err != nil {
				return err
			}
			r.addrs = append(r.addrs, addr)
		}
	}

	// Remove old addresses
	for i := len(r.addrs) - 1; i >= 0; i-- {
		if !contains(cfg.LocalAddrs, r.addrs[i]) {
			r.delAddress(link, r.addrs[i])
			r.addrs = append(r.addrs[:i], r.addrs[i+1:]...)
		}
	}

	// Add IPv6 rule only if we have IPv6 address
	if hasIPv6Addr && !r.rule6Added {
		rule6 := netlink.NewRule()
		rule6.Priority = tailscaleRulePriority
		rule6.Invert = true
		rule6.Mark = tsconst.LinuxBypassMarkNum
		rule6.MarkSet = true
		rule6.Mask = tsconst.LinuxFwmarkMaskNum
		rule6.Table = tailscaleRouteTable
		rule6.Family = unix.AF_INET6
		if err := netlink.RuleAdd(rule6); err != nil {
			r.logf("add ipv6 rule failed (may already exist): %v", err)
		} else {
			r.rule6Added = true
		}
	} else if !hasIPv6Addr && r.rule6Added {
		// Remove IPv6 rule if no IPv6 address
		rule6 := netlink.NewRule()
		rule6.Priority = tailscaleRulePriority
		rule6.Invert = true
		rule6.Mark = tsconst.LinuxBypassMarkNum
		rule6.MarkSet = true
		rule6.Mask = tsconst.LinuxFwmarkMaskNum
		rule6.Table = tailscaleRouteTable
		rule6.Family = unix.AF_INET6
		netlink.RuleDel(rule6)
		r.rule6Added = false
	}

	// Add routes
	for _, route := range cfg.Routes {
		if !contains(r.routes, route) {
			if err := r.addRoute(route); err != nil {
				return err
			}
			r.routes = append(r.routes, route)
		}
	}

	// Remove old routes
	for i := len(r.routes) - 1; i >= 0; i-- {
		if !contains(cfg.Routes, r.routes[i]) {
			r.delRoute(r.routes[i])
			r.routes = append(r.routes[:i], r.routes[i+1:]...)
		}
	}

	// Enable SNAT for subnet routes (hotspot support)
	if len(cfg.Routes) > 0 {
		if err := r.nfr.AddSNATRule(); err != nil {
			r.logf("failed to add SNAT rule: %v", err)
		}
	}

	return nil
}

func (r *androidRouter) Close() error {
	// Cleanup linuxfw
	if r.nfr != nil {
		r.nfr.DelSNATRule()
		r.nfr.DelBase()
		r.nfr.DelHooks(r.logf)
		r.nfr.DelChains()
	}

	if r.ruleAdded {
		rule := netlink.NewRule()
		rule.Priority = tailscaleRulePriority
		rule.Invert = true
		rule.Mark = tsconst.LinuxBypassMarkNum
		rule.MarkSet = true
		rule.Mask = tsconst.LinuxFwmarkMaskNum
		rule.Table = tailscaleRouteTable
		rule.Family = unix.AF_INET
		netlink.RuleDel(rule)
	}

	if r.rule6Added {
		rule6 := netlink.NewRule()
		rule6.Priority = tailscaleRulePriority
		rule6.Invert = true
		rule6.Mark = tsconst.LinuxBypassMarkNum
		rule6.MarkSet = true
		rule6.Mask = tsconst.LinuxFwmarkMaskNum
		rule6.Table = tailscaleRouteTable
		rule6.Family = unix.AF_INET6
		netlink.RuleDel(rule6)
	}

	link, err := netlink.LinkByName(r.tunname)
	if err != nil {
		return err
	}
	return netlink.LinkSetDown(link)
}

func (r *androidRouter) addAddress(link netlink.Link, addr netip.Prefix) error {
	nlAddr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   addr.Addr().AsSlice(),
			Mask: net.CIDRMask(addr.Bits(), addr.Addr().BitLen()),
		},
	}
	return netlink.AddrAdd(link, nlAddr)
}

func (r *androidRouter) delAddress(link netlink.Link, addr netip.Prefix) error {
	nlAddr := &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   addr.Addr().AsSlice(),
			Mask: net.CIDRMask(addr.Bits(), addr.Addr().BitLen()),
		},
	}
	return netlink.AddrDel(link, nlAddr)
}

func (r *androidRouter) addRoute(cidr netip.Prefix) error {
	route := &netlink.Route{
		LinkIndex: r.linkIndex,
		Dst: &net.IPNet{
			IP:   cidr.Addr().AsSlice(),
			Mask: net.CIDRMask(cidr.Bits(), cidr.Addr().BitLen()),
		},
		Table: tailscaleRouteTable,
	}
	return netlink.RouteAdd(route)
}

func (r *androidRouter) delRoute(cidr netip.Prefix) error {
	route := &netlink.Route{
		LinkIndex: r.linkIndex,
		Dst: &net.IPNet{
			IP:   cidr.Addr().AsSlice(),
			Mask: net.CIDRMask(cidr.Bits(), cidr.Addr().BitLen()),
		},
		Table: tailscaleRouteTable,
	}
	return netlink.RouteDel(route)
}

func contains(slice []netip.Prefix, item netip.Prefix) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
