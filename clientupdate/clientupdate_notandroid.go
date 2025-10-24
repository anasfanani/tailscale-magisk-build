// Copyright (c) Tailscale Inc & AUTHORS
// SPDX-License-Identifier: BSD-3-Clause

//go:build !android

package clientupdate

func (up *Updater) updateAndroid() error {
	panic("unreachable")
}
