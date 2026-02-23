package config

import "testing"

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if cfg.ListenPort != DefaultListenPort {
		t.Errorf("ListenPort: got %d, want %d", cfg.ListenPort, DefaultListenPort)
	}
	if cfg.WGListenPort != DefaultWGListenPort {
		t.Errorf("WGListenPort: got %d, want %d", cfg.WGListenPort, DefaultWGListenPort)
	}
	if cfg.CoordURL != DefaultCoordURL {
		t.Errorf("CoordURL: got %q, want %q", cfg.CoordURL, DefaultCoordURL)
	}
	if !cfg.CaptureAllSessions {
		t.Error("CaptureAllSessions should be true by default")
	}
	if !cfg.ShellIntegrationMarkers {
		t.Error("ShellIntegrationMarkers should be true by default")
	}
	if cfg.RetentionDays != DefaultRetentionDays {
		t.Errorf("RetentionDays: got %d, want %d", cfg.RetentionDays, DefaultRetentionDays)
	}
	if cfg.MaxStorageGB != DefaultMaxStorageGB {
		t.Errorf("MaxStorageGB: got %d, want %d", cfg.MaxStorageGB, DefaultMaxStorageGB)
	}
	if len(cfg.ParsersEnabled) != 5 {
		t.Errorf("ParsersEnabled: got %d entries, want 5", len(cfg.ParsersEnabled))
	}
	if cfg.FileAccessRoot != nil {
		t.Error("FileAccessRoot should be nil by default")
	}
}

func TestSocketPath(t *testing.T) {
	path := SocketPath()
	if path == "" {
		t.Error("SocketPath should not be empty")
	}
}

func TestLoadMissingFile(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with missing file should not error: %v", err)
	}
	if cfg == nil {
		t.Fatal("Load should return default config when file is missing")
	}
	if cfg.ListenPort != DefaultListenPort {
		t.Errorf("should return defaults: got port %d", cfg.ListenPort)
	}
}
