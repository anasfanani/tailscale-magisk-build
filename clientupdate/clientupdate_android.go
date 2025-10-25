//go:build android

package clientupdate

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"tailscale.com/util/cmpver"
)

const androidGitHubRepoURL = "https://api.github.com/repos/anasfanani/tailscale-magisk-build/releases"

// updateAndroid handles Tailscale updates on Android (Magisk/system installation).
// Downloads release from GitHub and extracts binaries to the configured directory.
func (up *Updater) updateAndroid() error {
	// Get the download and extract directories
	downloadDir, dirExtract, err := androidDirectories()
	if err != nil {
		return err
	}

	// Fetch the release information
	repoURL := androidGitHubRepoURL
	if up.Version != "" {
		repoURL = fmt.Sprintf(repoURL+"/tags/v%s-android", up.Version)
	} else if up.Track == UnstableTrack {
		// For unstable track, fetch all releases to get the latest (including pre-releases)
		repoURL = androidGitHubRepoURL
	} else {
		repoURL = repoURL + "/latest"
	}

	release, ver, err := fetchAndroidRelease(repoURL, up.Track)
	if err != nil {
		return err
	}

	// Confirm the update with the user (allow downgrades when specific version is requested)
	if !confirmAndroidUpdate(up, ver) {
		return nil
	}

	// Find the matching asset for the current architecture
	assetURL, err := findAndroidAsset(release, ver)
	if err != nil {
		return err
	}

	// Download the asset
	downloadPath, err := downloadAndroidAsset(assetURL, downloadDir, ver)
	if err != nil {
		return err
	}

	// Extract and install
	if err := extractAndInstallAndroid(downloadPath, dirExtract, up.Logf); err != nil {
		return err
	}

	// Cleanup
	_ = os.RemoveAll(downloadDir)

	up.Logf("Please restart the tailscaled service to apply the update.")
	return nil
}

// confirmAndroidUpdate confirms the update with the user, allowing downgrades when a specific version is requested
func confirmAndroidUpdate(up *Updater, ver string) bool {
	// Allow downgrades when a specific version is requested
	if up.Version != "" {
		if up.Confirm != nil {
			return up.Confirm(ver)
		}
		return true
	}

	// Only check version when we're not switching tracks.
	if up.Track == "" || up.Track == CurrentTrack {
		switch c := cmpver.Compare(up.currentVersion, ver); {
		case c == 0:
			up.Logf("already running %v version %v; no update needed", up.Track, ver)
			return false
		case c > 0:
			up.Logf("installed %v version %v is newer than the latest available version %v; no update needed", up.Track, up.currentVersion, ver)
			return false
		}
	}
	if up.Confirm != nil {
		return up.Confirm(ver)
	}
	return true
}

// androidDirectories returns the download and extract directories for Android updates
// downloadDir: uses TempDir, fallback to current working directory
// extractDir: uses the directory of the current executable
func androidDirectories() (downloadDir, extractDir string, err error) {
	// Determine download directory: TempDir -> cwd -> /tmp
	downloadDir = os.TempDir()
	if downloadDir == "" {
		wd, err := os.Getwd()
		if err != nil || wd == "" {
			downloadDir = "."
		} else {
			downloadDir = wd
		}
	}
	downloadDir = filepath.Join(downloadDir, "tailscale-android-update")

	// Determine extract directory: symlink -> executable -> cwd -> "."
	executable, err := os.Executable()
	if err == nil {
		// Try to resolve symlinks first
		realPath, err := filepath.EvalSymlinks(executable)
		if err == nil && realPath != "" {
			extractDir = filepath.Dir(realPath)
		} else if executable != "" {
			// Fallback to executable directory
			extractDir = filepath.Dir(executable)
		}
	}

	// If still empty, try current working directory
	if extractDir == "" {
		wd, err := os.Getwd()
		if err == nil && wd != "" {
			extractDir = wd
		}
	}

	// Last resort: current directory
	if extractDir == "" {
		extractDir = "."
	}

	return downloadDir, extractDir, nil
}

// fetchAndroidRelease fetches release information from GitHub API
// If track is "unstable", it fetches from all releases including pre-releases
// Otherwise, it only fetches stable releases
func fetchAndroidRelease(repoURL, track string) (release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
	Prerelease bool `json:"prerelease"`
}, ver string, err error) {
	resp, err := http.Get(repoURL)
	if err != nil {
		return release, "", fmt.Errorf("failed to fetch releases: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return release, "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Check if we're fetching all releases (for unstable track)
	if strings.HasSuffix(repoURL, "/releases") {
		// Fetch all releases and find the latest one (including pre-releases)
		var releases []struct {
			TagName string `json:"tag_name"`
			Assets  []struct {
				Name string `json:"name"`
				URL  string `json:"browser_download_url"`
			} `json:"assets"`
			Prerelease bool `json:"prerelease"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
			return release, "", fmt.Errorf("failed to decode releases metadata: %w", err)
		}

		if len(releases) == 0 {
			return release, "", fmt.Errorf("no releases found")
		}

		// Get the first release (latest)
		release.TagName = releases[0].TagName
		release.Assets = releases[0].Assets
		release.Prerelease = releases[0].Prerelease
	} else {
		// Single release (latest or specific tag)
		if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
			return release, "", fmt.Errorf("failed to decode release metadata: %w", err)
		}
	}

	// If fetching latest and it's a pre-release, skip it for stable track
	if track != UnstableTrack && release.Prerelease {
		return release, "", fmt.Errorf("latest release is a pre-release; use --track=unstable to update to pre-releases")
	}

	ver = strings.TrimPrefix(release.TagName, "v")
	ver = strings.TrimSuffix(ver, "-android")

	return release, ver, nil
}

// findAndroidAsset finds the correct asset for the current architecture
func findAndroidAsset(release struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
	Prerelease bool `json:"prerelease"`
}, ver string) (assetURL string, err error) {
	if runtime.GOARCH != "arm64" && runtime.GOARCH != "arm" {
		return "", fmt.Errorf("unsupported architecture: %s", runtime.GOARCH)
	}

	assetsName := fmt.Sprintf(`tailscale_%s_%s.tgz`, ver, runtime.GOARCH)
	for _, asset := range release.Assets {
		matched, err := regexp.MatchString(`^`+assetsName+`$`, asset.Name)
		if err != nil {
			return "", fmt.Errorf("failed to match asset name: %w", err)
		}
		if matched {
			return asset.URL, nil
		}
	}

	return "", fmt.Errorf("error while fetch release for arch %q, asset name %q, download manually on %q", runtime.GOARCH, assetsName, androidGitHubRepoURL)
}

// downloadAndroidAsset downloads the release asset
func downloadAndroidAsset(assetURL, downloadDir, ver string) (downloadPath string, err error) {
	resp, err := http.Get(assetURL)
	if err != nil {
		return "", fmt.Errorf("failed to download asset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code while downloading asset: %d", resp.StatusCode)
	}

	if err := os.MkdirAll(downloadDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create download directory: %w", err)
	}

	assetsName := fmt.Sprintf(`tailscale_%s_%s.tgz`, ver, runtime.GOARCH)
	downloadPath = filepath.Join(downloadDir, assetsName)
	out, err := os.Create(downloadPath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		return "", fmt.Errorf("failed to save downloaded file: %w", err)
	}

	return downloadPath, nil
}

// extractAndInstallAndroid extracts and installs the downloaded tarball
func extractAndInstallAndroid(downloadPath, dirExtract string, logf func(string, ...interface{})) error {
	file, err := os.Open(downloadPath)
	if err != nil {
		return fmt.Errorf("failed to open downloaded file: %w", err)
	}
	defer file.Close()

	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("failed to create gzip reader: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		if header.Typeflag != tar.TypeReg {
			continue
		}

		// Only extract tailscaled binary
		if header.Name != "tailscaled" {
			continue
		}

		// Install as "tailscaled" in the extract directory
		destPath := filepath.Join(dirExtract, "tailscaled")
		destFile, err := os.Create(destPath + ".new")
		if err != nil {
			return fmt.Errorf("failed to create destination file: %w", err)
		}
		defer destFile.Close()

		if _, err := io.Copy(destFile, tarReader); err != nil {
			return fmt.Errorf("failed to extract file: %w", err)
		}

		if err := os.Chmod(destPath+".new", os.FileMode(0755)|os.ModePerm); err != nil {
			return fmt.Errorf("failed to set executable permissions: %w", err)
		}

		if err := os.Rename(destPath+".new", destPath); err != nil {
			return fmt.Errorf("failed to rename file: %w", err)
		}

		logf("Extracted %s to %s", header.Name, destPath)
	}

	return nil
}
