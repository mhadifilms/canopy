// Package config handles loading, saving, and providing defaults for canopyd configuration.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Version is set at build time via ldflags.
var Version = "dev"

const (
	DefaultListenPort        = 19876
	DefaultWGListenPort      = 51820
	DefaultCoordURL          = "https://coord.canopy.dev"
	DefaultRetentionDays     = 30
	DefaultMaxStorageGB      = 10
	DefaultCompressAfterHrs  = 24
	DefaultMaxPairedDevices  = 10
	DefaultFileAccessMaxMB   = 1
)

// Config represents the daemon configuration from ~/.config/canopy/config.json.
type Config struct {
	ListenPort              int      `json:"listen_port"`
	WGListenPort            int      `json:"wg_listen_port"`
	CoordURL                string   `json:"coord_url"`
	CaptureAllSessions      bool     `json:"capture_all_sessions"`
	CaptureExcludeProcesses []string `json:"capture_exclude_processes"`
	CaptureExcludeEnv       map[string]string `json:"capture_exclude_env"`
	ParsersEnabled          []string `json:"parsers_enabled"`
	ShellIntegrationMarkers bool     `json:"shell_integration_markers"`
	RetentionDays           int      `json:"retention_days"`
	MaxStorageGB            int      `json:"max_storage_gb"`
	CompressAfterHours      int      `json:"compress_after_hours"`
	PreventSleepWhileActive bool     `json:"prevent_sleep_while_active"`
	AutoUpdate              bool     `json:"auto_update"`
	FileAccessRoot          *string  `json:"file_access_root"`
	FileAccessMaxSizeMB     int      `json:"file_access_max_size_mb"`
	MaxPairedDevices        int      `json:"max_paired_devices"`
}

// Default returns a Config with all default values per the spec.
func Default() *Config {
	return &Config{
		ListenPort:              DefaultListenPort,
		WGListenPort:            DefaultWGListenPort,
		CoordURL:                DefaultCoordURL,
		CaptureAllSessions:      true,
		CaptureExcludeProcesses: []string{"ssh-agent", "gpg-agent"},
		CaptureExcludeEnv:       map[string]string{"CANOPY_DISABLE": "1"},
		ParsersEnabled:          []string{"generic", "claude_code", "aider", "goose", "codex"},
		ShellIntegrationMarkers: true,
		RetentionDays:           DefaultRetentionDays,
		MaxStorageGB:            DefaultMaxStorageGB,
		CompressAfterHours:      DefaultCompressAfterHrs,
		PreventSleepWhileActive: true,
		AutoUpdate:              true,
		FileAccessRoot:          nil,
		FileAccessMaxSizeMB:     DefaultFileAccessMaxMB,
		MaxPairedDevices:        DefaultMaxPairedDevices,
	}
}

// ConfigDir returns the path to ~/.config/canopy.
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".config", "canopy"), nil
}

// ConfigPath returns the full path to config.json.
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load reads config from disk, falling back to defaults for missing fields.
func Load() (*Config, error) {
	cfg := Default()

	path, err := ConfigPath()
	if err != nil {
		return cfg, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return cfg, nil
}

// Save writes the config to disk.
func Save(cfg *Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}

// SocketPath returns the path to the daemon Unix domain socket.
func SocketPath() string {
	tmpdir := os.TempDir()
	return filepath.Join(tmpdir, "canopyd.sock")
}

// SessionsDir returns the path to the sessions storage directory.
func SessionsDir() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions"), nil
}

// Hostname returns the machine's hostname.
func Hostname() (string, error) {
	return os.Hostname()
}

// ReadFileInDir reads a file from the given directory.
func ReadFileInDir(dir, name string) ([]byte, error) {
	return os.ReadFile(filepath.Join(dir, name))
}
