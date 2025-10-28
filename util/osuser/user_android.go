//go:build android

package osuser

import (
	"context"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"time"
)

func init() {
	overrideLookupFunc = androidLookup
}

// androidLookup handles user lookup on Android systems.
// On Android, there's no getent command, so we use shell commands (id, etc).
// androidLookup implements user lookup for Android systems
func androidLookup(usernameOrUID string, wantShell bool) (*user.User, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var shell string
	if wantShell {
		if out, err := exec.LookPath("bash"); err == nil {
			shell = out
		} else if out, err := exec.LookPath("sh"); err == nil {
			shell = out
		} else {
			shell = "/system/bin/sh"
		}
	}

	// Try to get info for requested user first
	uid := getAndroidCommandOutput(ctx, "id", "-u", usernameOrUID, "")
	if uid == "" {
		// User doesn't exist, fallback to current user
		uid = getAndroidCommandOutput(ctx, "id", "-u", "0")
		usernameOrUID = "root"
	}

	gid := getAndroidCommandOutput(ctx, "id", "-g", usernameOrUID, "0")
	username := getAndroidCommandOutput(ctx, "id", "-un", usernameOrUID, usernameOrUID)
	homeDir := getAndroidHomeDir("/")

	return &user.User{
		Uid:      uid,
		Gid:      gid,
		Username: username,
		Name:     "Android",
		HomeDir:  homeDir,
	}, shell, nil
}

// getAndroidCommandOutput executes a command and returns trimmed output,
// or returns the default value (last arg) if the command fails.
func getAndroidCommandOutput(ctx context.Context, cmd string, args ...string) string {
	if len(args) == 0 {
		return ""
	}

	// Last argument is the default value
	defaultValue := args[len(args)-1]
	cmdArgs := args[:len(args)-1]

	out, err := exec.CommandContext(ctx, cmd, cmdArgs...).Output()
	if err != nil {
		return defaultValue
	}

	return strings.TrimSpace(string(out))
}

// getAndroidHomeDir returns the home directory on Android.
// Falls back to defaultDir if unable to determine.
func getAndroidHomeDir(defaultDir string) string {
	if home, err := os.UserHomeDir(); err == nil {
		return strings.TrimSpace(home)
	}
	return defaultDir
}
