// Package update handles checking for and applying canopyd updates.
package update

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/canopy-dev/canopyd/internal/config"
)

const (
	releasesBaseURL = "https://releases.canopy.dev/latest"
	checkTimeout    = 10 * time.Second
	downloadTimeout = 5 * time.Minute
)

// Release describes an available update.
type Release struct {
	Version  string
	URL      string
	Checksum string
}

// CheckResult is the result of an update check.
type CheckResult struct {
	CurrentVersion string
	LatestVersion  string
	UpdateAvail    bool
	DownloadURL    string
}

// Check queries the releases server for the latest version.
func Check() (*CheckResult, error) {
	client := &http.Client{Timeout: checkTimeout}

	versionURL := releasesBaseURL + "/version.txt"
	resp, err := client.Get(versionURL)
	if err != nil {
		return nil, fmt.Errorf("check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("check for updates: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return nil, fmt.Errorf("read version: %w", err)
	}

	latest := strings.TrimSpace(string(body))
	current := config.Version

	arch := runtime.GOARCH
	binaryName := fmt.Sprintf("canopyd-darwin-%s", arch)
	downloadURL := fmt.Sprintf("%s/%s", releasesBaseURL, binaryName)

	return &CheckResult{
		CurrentVersion: current,
		LatestVersion:  latest,
		UpdateAvail:    latest != current && current != "dev",
		DownloadURL:    downloadURL,
	}, nil
}

// Apply downloads and installs the latest version.
func Apply() error {
	result, err := Check()
	if err != nil {
		return err
	}
	if !result.UpdateAvail {
		return fmt.Errorf("already up to date (version %s)", result.CurrentVersion)
	}

	client := &http.Client{Timeout: downloadTimeout}

	// Download checksums.
	checksumURL := releasesBaseURL + "/checksums.txt"
	checksums, err := fetchText(client, checksumURL)
	if err != nil {
		return fmt.Errorf("fetch checksums: %w", err)
	}

	// Find expected checksum for our binary.
	arch := runtime.GOARCH
	binaryName := fmt.Sprintf("canopyd-darwin-%s", arch)
	expectedHash, err := findChecksum(checksums, binaryName)
	if err != nil {
		return err
	}

	// Download binary.
	resp, err := client.Get(result.DownloadURL)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download binary: HTTP %d", resp.StatusCode)
	}

	// Write to temp file.
	tmpFile, err := os.CreateTemp("", "canopyd-update-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)

	hash := sha256.New()
	writer := io.MultiWriter(tmpFile, hash)

	if _, err := io.Copy(writer, resp.Body); err != nil {
		tmpFile.Close()
		return fmt.Errorf("download: %w", err)
	}
	tmpFile.Close()

	// Verify checksum.
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if actualHash != expectedHash {
		return fmt.Errorf("checksum mismatch: got %s, expected %s", actualHash, expectedHash)
	}

	// Make executable.
	if err := os.Chmod(tmpPath, 0755); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}

	// Replace the current binary.
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find executable: %w", err)
	}

	// Atomic replace: rename new over old.
	if err := os.Rename(tmpPath, execPath); err != nil {
		// Rename may fail across filesystems; fall back to copy.
		if err := copyFile(tmpPath, execPath); err != nil {
			return fmt.Errorf("install binary: %w", err)
		}
	}

	return nil
}

func fetchText(client *http.Client, url string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// findChecksum parses a sha256sum-style checksums file and returns the hex
// checksum whose filename column matches binaryName exactly. Substring matches
// are rejected so that entries like "canopyd-darwin-arm64.backup" do not
// shadow the real binary checksum.
func findChecksum(checksums, binaryName string) (string, error) {
	for _, line := range strings.Split(checksums, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		// sha256sum format: "<hex>  <filename>". The filename column may be
		// prefixed with "*" for binary mode; strip it and the optional
		// leading "./" for comparison.
		name := strings.TrimPrefix(parts[1], "*")
		name = strings.TrimPrefix(name, "./")
		if name == binaryName {
			return parts[0], nil
		}
	}
	return "", fmt.Errorf("checksum not found for %s", binaryName)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
