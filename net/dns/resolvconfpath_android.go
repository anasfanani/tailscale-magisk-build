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
	if fi, err := os.Stat("/data/adb/tailscale"); err == nil && fi.IsDir() {
		prefix = "/data/adb/tailscale"
	} else if prefix == "" {
		prefix = filepath.Join(os.TempDir(), "tailscale")
	}
	dnsDir := filepath.Join(prefix, "etc")
	resolvConf = filepath.Join(dnsDir, "resolv.conf")
	backupConf = filepath.Join(dnsDir, "resolv.pre-tailscale-backup.conf")
}
