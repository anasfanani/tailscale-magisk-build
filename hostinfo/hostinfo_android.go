//go:build android

package hostinfo

import (
	"os/exec"
	"strings"

	"tailscale.com/tailcfg"
)

func init() {
	RegisterHostinfoNewHook(func(hi *tailcfg.Hostinfo) {
		// Replace generic "node" or "localhost" with meaningful Android hostname
		if hi.Hostname == "node" || hi.Hostname == "localhost" {
			hi.Hostname = sanitizeHostname(
				getSystemProperty("ro.product.manufacturer") + "-" +
					getSystemProperty("ro.product.model"),
			)
			if hi.Hostname == "" {
				hi.Hostname = "android"
			}
		}

		// Set OSVersion if not already set
		if hi.OSVersion == "" {
			hi.OSVersion = getSystemProperty("ro.build.version.release")
		}

		// Ensure Device Model is populated
		if hi.DeviceModel == "" {
			hi.DeviceModel = getSystemProperty("ro.product.model")
		}
	})
}

// getSystemProperty reads Android system properties via getprop command
func getSystemProperty(prop string) string {
	out, err := exec.Command("getprop", prop).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// sanitizeHostname removes invalid characters from a hostname.
// Valid hostnames contain only alphanumerics and hyphens.
func sanitizeHostname(s string) string {
	s = strings.ToLower(s)
	var result strings.Builder

	for _, ch := range s {
		switch {
		case (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9'):
			result.WriteRune(ch)
		case ch == '-' || ch == '_' || ch == ' ':
			// Convert spaces and underscores to hyphens
			if result.Len() > 0 && result.String()[result.Len()-1] != '-' {
				result.WriteRune('-')
			}
		}
	}

	hostname := result.String()
	// Remove leading/trailing hyphens
	hostname = strings.Trim(hostname, "-")
	// Collapse multiple hyphens
	for strings.Contains(hostname, "--") {
		hostname = strings.ReplaceAll(hostname, "--", "-")
	}

	// Limit to 63 characters (max label length in hostname)
	if len(hostname) > 63 {
		hostname = hostname[:63]
		hostname = strings.TrimRight(hostname, "-")
	}

	return hostname
}
