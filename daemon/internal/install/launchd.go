package install

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

const plistLabel = "dev.canopy.daemon"

// PlistPath returns the full path to the launchd plist.
func PlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", plistLabel+".plist"), nil
}

// PlistContent returns the launchd plist XML for the given canopyd binary path.
func PlistContent(binaryPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>daemon</string>
    <string>start</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <dict>
    <key>SuccessfulExit</key>
    <false/>
  </dict>
  <key>StandardOutPath</key>
  <string>/tmp/canopyd.stdout.log</string>
  <key>StandardErrorPath</key>
  <string>/tmp/canopyd.stderr.log</string>
  <key>ProcessType</key>
  <string>Background</string>
  <key>LowPriorityIO</key>
  <true/>
</dict>
</plist>
`, plistLabel, binaryPath)
}

// InstallPlist writes the launchd plist to disk.
func InstallPlist(binaryPath string) error {
	path, err := PlistPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	content := PlistContent(binaryPath)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	return nil
}

// LoadPlist loads the launchd plist via launchctl.
func LoadPlist() error {
	path, err := PlistPath()
	if err != nil {
		return err
	}
	cmd := exec.Command("launchctl", "load", path)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl load: %s: %w", string(output), err)
	}
	return nil
}

// UnloadPlist unloads the launchd plist via launchctl.
func UnloadPlist() error {
	path, err := PlistPath()
	if err != nil {
		return err
	}
	// Unload may fail if not loaded; that's fine.
	cmd := exec.Command("launchctl", "unload", path)
	cmd.Run() // Ignore errors.
	return nil
}

// RemovePlist removes the launchd plist from disk.
func RemovePlist() error {
	path, err := PlistPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}
	return nil
}

// IsDaemonLoaded checks if the launchd job is currently loaded.
func IsDaemonLoaded() bool {
	cmd := exec.Command("launchctl", "list", plistLabel)
	return cmd.Run() == nil
}
