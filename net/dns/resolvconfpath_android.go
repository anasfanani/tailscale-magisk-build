//go:build android

package dns

import (
	"os"
	"path/filepath"
)

var (
	resolvConf string
	backupConf string
)

func init() {
	prefix := os.Getenv("PREFIX")
	if prefix == "" {
		if os.Geteuid() == 0 {
			prefix = "/data/adb/tailscale"
		} else {
			prefix = filepath.Join(os.TempDir(), "tailscale")
		}
	}
	dnsDir := filepath.Join(prefix, "etc")
	resolvConf = filepath.Join(dnsDir, "resolv.conf")
	backupConf = filepath.Join(dnsDir, "resolv.pre-tailscale-backup.conf")
}
