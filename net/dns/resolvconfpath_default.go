// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

//go:build !gokrazy && !android

package dns

const (
	resolvConf = "/etc/resolv.conf"
	backupConf = "/etc/resolv.pre-tailscale-backup.conf"
)
