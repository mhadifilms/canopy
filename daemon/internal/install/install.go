package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/canopy-dev/canopyd/internal/config"
)

// Result holds the outcome of an installation.
type Result struct {
	ConfigDir string
	DeviceID  string
	Hostname  string
	Shells    []string
}

// Run performs a full canopyd installation.
//
// Steps per §3.2:
// 1. Create config directory and subdirectories
// 2. Generate Ed25519 identity keypair + WireGuard keypair
// 3. Derive device ID from identity public key
// 4. Write default config.json
// 5. Initialize devices.json
// 6. Inject shell hooks into detected shells
// 7. Install + load launchd plist
func Run(binaryPath string) (*Result, error) {
	configDir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}

	// 1. Create directories.
	for _, sub := range []string{"sessions", "parsers"} {
		dir := filepath.Join(configDir, sub)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create %s: %w", dir, err)
		}
	}

	// 2. Generate keys (skip if they already exist).
	var deviceID string
	if KeysExist(configDir) {
		pub, err := LoadIdentityPublicKey(configDir)
		if err != nil {
			return nil, fmt.Errorf("load existing keys: %w", err)
		}
		deviceID = DeviceIDFromPublicKey(pub)
	} else {
		pub, err := GenerateIdentityKeypair(configDir)
		if err != nil {
			return nil, err
		}
		if err := GenerateWireGuardKeypair(configDir); err != nil {
			return nil, err
		}
		deviceID = DeviceIDFromPublicKey(pub)
	}

	// 3. Write default config if it doesn't exist.
	cfgPath := filepath.Join(configDir, "config.json")
	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		cfg := config.Default()
		if err := config.Save(cfg); err != nil {
			return nil, fmt.Errorf("write default config: %w", err)
		}
	}

	// 4. Initialize devices.json if it doesn't exist.
	devicesPath := filepath.Join(configDir, "devices.json")
	if _, err := os.Stat(devicesPath); os.IsNotExist(err) {
		if err := os.WriteFile(devicesPath, []byte("[]"), 0600); err != nil {
			return nil, fmt.Errorf("write devices.json: %w", err)
		}
	}

	// 5. Detect shells and inject hooks.
	shells := DetectShells()
	for _, shell := range shells {
		if err := InjectHook(shell); err != nil {
			return nil, fmt.Errorf("inject %s hook: %w", shell, err)
		}
	}

	// 6. Install launchd plist and load it.
	if err := InstallPlist(binaryPath); err != nil {
		return nil, fmt.Errorf("install plist: %w", err)
	}
	if err := LoadPlist(); err != nil {
		return nil, fmt.Errorf("load plist: %w", err)
	}

	hostname, _ := os.Hostname()

	return &Result{
		ConfigDir: configDir,
		DeviceID:  deviceID,
		Hostname:  hostname,
		Shells:    shells,
	}, nil
}

// Uninstall removes canopyd from the system.
// If removeData is true, also deletes ~/.config/canopy/.
func Uninstall(removeData bool) error {
	// 1. Stop daemon.
	UnloadPlist()

	// 2. Remove launchd plist.
	if err := RemovePlist(); err != nil {
		return fmt.Errorf("remove plist: %w", err)
	}

	// 3. Remove shell hooks from all detected shells.
	for _, shell := range []string{"zsh", "bash", "fish"} {
		_ = RemoveHook(shell) // Best-effort.
	}

	// 4. Remove binary.
	binaryPath := "/usr/local/bin/canopyd"
	if err := os.Remove(binaryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove binary: %w", err)
	}

	// 5. Optionally remove config/session data.
	if removeData {
		configDir, err := config.ConfigDir()
		if err != nil {
			return err
		}
		if err := os.RemoveAll(configDir); err != nil {
			return fmt.Errorf("remove config dir: %w", err)
		}
	}

	return nil
}

// PairedDevice represents a paired iPhone/device stored in devices.json.
type PairedDevice struct {
	DeviceID         string `json:"device_id"`
	Name             string `json:"name"`
	WGPublicKey      string `json:"wg_public_key"`
	IdentityPubKey   string `json:"identity_public_key"`
	PairedAt         string `json:"paired_at"`
	APNSToken        string `json:"apns_token,omitempty"`
}

// LoadPairedDevices reads devices.json.
func LoadPairedDevices() ([]PairedDevice, error) {
	configDir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(configDir, "devices.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read devices.json: %w", err)
	}
	var devices []PairedDevice
	if err := json.Unmarshal(data, &devices); err != nil {
		return nil, fmt.Errorf("parse devices.json: %w", err)
	}
	return devices, nil
}

// SavePairedDevices writes devices.json.
func SavePairedDevices(devices []PairedDevice) error {
	configDir, err := config.ConfigDir()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(devices, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal devices: %w", err)
	}
	return os.WriteFile(filepath.Join(configDir, "devices.json"), data, 0600)
}

// AddPairedDevice adds a new device to devices.json.
func AddPairedDevice(device PairedDevice) error {
	devices, err := LoadPairedDevices()
	if err != nil {
		return err
	}

	// Replace if device ID already exists.
	found := false
	for i, d := range devices {
		if d.DeviceID == device.DeviceID {
			devices[i] = device
			found = true
			break
		}
	}
	if !found {
		devices = append(devices, device)
	}

	return SavePairedDevices(devices)
}

// RemovePairedDevice removes a device by ID from devices.json.
func RemovePairedDevice(deviceID string) error {
	devices, err := LoadPairedDevices()
	if err != nil {
		return err
	}

	filtered := make([]PairedDevice, 0, len(devices))
	for _, d := range devices {
		if d.DeviceID != deviceID {
			filtered = append(filtered, d)
		}
	}

	return SavePairedDevices(filtered)
}
