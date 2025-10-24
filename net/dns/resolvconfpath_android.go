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
			if fi, err := os.Stat("/data/adb/tailscale"); err == nil && fi.IsDir() {
				prefix = "/data/adb/tailscale"
			} else if wd, err := os.Getwd(); err == nil {
				prefix = wd
			} else {
				prefix = os.TempDir()
			}
		} else {
			prefix = os.TempDir()
		}
	}
	dnsDir := filepath.Join(prefix, "etc")
	os.MkdirAll(dnsDir, 0755)
	resolvConf = filepath.Join(dnsDir, "resolv.conf")
	backupConf = filepath.Join(dnsDir, "resolv.pre-tailscale-backup.conf")
}
