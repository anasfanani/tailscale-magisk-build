//go:build android

package dns

const (
	resolvConf = "/data/adb/tailscale/tmp/resolv.conf"
	backupConf = "/data/adb/tailscale/tmp/resolv.pre-tailscale-backup.conf"
)
