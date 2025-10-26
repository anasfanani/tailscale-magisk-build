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
	if len(cfg.Nameservers) > 0 && !m.hijacked {
		if err := addDNSNATRule(false); err != nil {
			m.logf("dns: failed to setup hijack: %v", err)
			return err
		}
		addDNSNATRule(true) // IPv6 is optional
		m.hijacked = true
		m.logf("dns: hijack enabled")
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
		delDNSNATRule(false)
		delDNSNATRule(true)
		m.hijacked = false
		m.logf("dns: hijack disabled")
	}
	return nil
}

func addDNSNATRule(ipv6 bool) error {
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

	// OUTPUT chain: local DNS queries
	ipt.Append("nat", "OUTPUT", "!", "-d", dnsIP, "-p", "udp", "--dport", "53", "-j", "DNAT", "--to-destination", dnsIP+":53")
	
	// PREROUTING chain: hotspot clients DNS - use REDIRECT to keep it local
	ipt.Append("nat", "PREROUTING", "-p", "udp", "--dport", "53", "-j", "REDIRECT", "--to-ports", "53")

	return nil
}

func delDNSNATRule(ipv6 bool) {
	proto := iptables.ProtocolIPv4
	dnsIP := tsaddr.TailscaleServiceIPString
	if ipv6 {
		proto = iptables.ProtocolIPv6
		dnsIP = tsaddr.TailscaleServiceIPv6String
	}

	ipt, err := iptables.NewWithProtocol(proto)
	if err != nil {
		return
	}

	ipt.Delete("nat", "OUTPUT", "!", "-d", dnsIP, "-p", "udp", "--dport", "53", "-j", "DNAT", "--to-destination", dnsIP+":53")
	ipt.Delete("nat", "PREROUTING", "-p", "udp", "--dport", "53", "-j", "REDIRECT", "--to-ports", "53")
}
