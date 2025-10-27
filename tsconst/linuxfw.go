// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

package tsconst

// Linux firewall constants used by Tailscale.

// The following bits are added to packet marks for Tailscale use.
//
// We tried to pick bits sufficiently out of the way that it's
// unlikely to collide with existing uses. We have 4 bytes of mark
// bits to play with. We leave the lower byte alone on the assumption
// that sysadmins would use those. Kubernetes uses a few bits in the
// second byte, so we steer clear of that too.
//
// Ref: https://www.kxxt.dev/blog/full-tailscale-on-android-and-remote-unlocking/
// AOSP's code for Fwmark allocation is
// https://android.googlesource.com/platform/system/netd/+/master/include/Fwmark.h
//
// The lower 0-20 bits are already allocated by Android.
// Bit 21-28 is currently unused.
// Tailscale uses bits 25-28 and also sets bit 17 (ProtectedFromVPN).
//
// The constants are in the iptables/iproute2 string format for
// matching and setting the bits, so they can be directly embedded in
// commands.
const (
	// The mask for reading/writing the 'firewall mask' bits on a packet.
	// We claim bits 25:28 entirely.
	LinuxFwmarkMask    = "0x1e000000"
	LinuxFwmarkMaskNum = 0x1e000000

	// Packet is from Tailscale and to a subnet route destination, so
	// is allowed to be routed through this machine.
	LinuxSubnetRouteMark    = "0x8000000"
	LinuxSubnetRouteMarkNum = 0x8000000

	// Packet was originated by tailscaled itself, and must not be
	// routed over the Tailscale network.
	// Includes bit 17 (0x20000) ProtectedFromVPN to bypass Android VPN routing.
	LinuxBypassMark    = "0x10020000"
	LinuxBypassMarkNum = 0x10020000
)
