// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

//go:build android

package dns

import (
	"github.com/coreos/go-iptables/iptables"
	"tailscale.com/control/controlknobs"
	"tailscale.com/health"
	"tailscale.com/net/tsaddr"
	"tailscale.com/types/logger"
	"tailscale.com/util/syspolicy/policyclient"
)

type androidManager struct {
	logf     logger.Logf
	hijacked bool
}

func NewOSConfigurator(logf logger.Logf, _ *health.Tracker, _ policyclient.Client, _ *controlknobs.Knobs, _ string) (OSConfigurator, error) {
	return &androidManager{logf: logf}, nil
}

func (m *androidManager) SetDNS(cfg OSConfig) error {
	shouldHijack := len(cfg.Nameservers) > 0
	
	if shouldHijack && !m.hijacked {
		// Enable DNS hijacking
		if err := setDNSRules(false, true); err != nil {
			m.logf("dns: failed to setup rules: %v", err)
			return err
		}
		if err := setDNSRules(true, true); err != nil {
			m.logf("dns: ipv6 rules failed (non-fatal): %v", err)
		}
		m.hijacked = true
		m.logf("dns: hijack enabled")
	} else if !shouldHijack {
		// Always clean up rules when DNS is disabled, regardless of tracked state
		if err := setDNSRules(false, false); err != nil {
			m.logf("dns: failed to remove rules: %v", err)
		}
		if err := setDNSRules(true, false); err != nil {
			m.logf("dns: ipv6 rules removal failed (non-fatal): %v", err)
		}
		if m.hijacked {
			m.hijacked = false
			m.logf("dns: hijack disabled")
		}
	}
	return nil
}

func (m *androidManager) SupportsSplitDNS() bool {
	return false
}

func (m *androidManager) GetBaseConfig() (OSConfig, error) {
	return OSConfig{}, nil
}

func (m *androidManager) Close() error {
	if m.hijacked {
		setDNSRules(false, false)
		setDNSRules(true, false)
		m.hijacked = false
		m.logf("dns: hijack disabled")
	}
	return nil
}

func setDNSRules(ipv6 bool, add bool) error {
	proto := iptables.ProtocolIPv4
	dnsIP := tsaddr.TailscaleServiceIPString
	if ipv6 {
		proto = iptables.ProtocolIPv6
		dnsIP = tsaddr.TailscaleServiceIPv6String
	}

	ipt, err := iptables.NewWithProtocol(proto)
	if err != nil {
		return err
	}

	rules := []struct {
		table string
		chain string
		args  []string
	}{
		// Redirect local DNS to Tailscale (exclude tun interfaces)
		{"nat", "OUTPUT", []string{"!", "-o", "tun+", "!", "-d", dnsIP, "-p", "udp", "--dport", "53", "-j", "DNAT", "--to-destination", dnsIP + ":53"}},
		// Redirect hotspot DNS to Tailscale (exclude tun interfaces)
		{"nat", "PREROUTING", []string{"!", "-i", "tun+", "-p", "udp", "--dport", "53", "-j", "DNAT", "--to-destination", dnsIP + ":53"}},
	}

	for _, rule := range rules {
		if add {
			ipt.Append(rule.table, rule.chain, rule.args...)
		} else {
			ipt.Delete(rule.table, rule.chain, rule.args...)
		}
	}

	return nil
}
