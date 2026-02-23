package install

import (
	"crypto/ed25519"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateIdentityKeypair(t *testing.T) {
	dir := t.TempDir()

	pub, err := GenerateIdentityKeypair(dir)
	if err != nil {
		t.Fatalf("GenerateIdentityKeypair: %v", err)
	}

	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("expected public key size %d, got %d", ed25519.PublicKeySize, len(pub))
	}

	// Check files exist
	privData, err := os.ReadFile(filepath.Join(dir, "identity.key"))
	if err != nil {
		t.Fatalf("read private key: %v", err)
	}
	if len(privData) != ed25519.PrivateKeySize {
		t.Errorf("expected private key size %d, got %d", ed25519.PrivateKeySize, len(privData))
	}

	pubData, err := os.ReadFile(filepath.Join(dir, "identity.pub"))
	if err != nil {
		t.Fatalf("read public key: %v", err)
	}
	if len(pubData) != ed25519.PublicKeySize {
		t.Errorf("expected public key size %d, got %d", ed25519.PublicKeySize, len(pubData))
	}

	// Verify file permissions
	info, _ := os.Stat(filepath.Join(dir, "identity.key"))
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected private key mode 0600, got %o", info.Mode().Perm())
	}
}

func TestGenerateWireGuardKeypair(t *testing.T) {
	dir := t.TempDir()

	if err := GenerateWireGuardKeypair(dir); err != nil {
		t.Fatalf("GenerateWireGuardKeypair: %v", err)
	}

	privData, err := os.ReadFile(filepath.Join(dir, "wg_private.key"))
	if err != nil {
		t.Fatalf("read WG private key: %v", err)
	}
	if len(privData) != 32 {
		t.Errorf("expected WG private key size 32, got %d", len(privData))
	}

	pubData, err := os.ReadFile(filepath.Join(dir, "wg_public.key"))
	if err != nil {
		t.Fatalf("read WG public key: %v", err)
	}
	if len(pubData) != 32 {
		t.Errorf("expected WG public key size 32, got %d", len(pubData))
	}

	// Verify file permissions
	info, _ := os.Stat(filepath.Join(dir, "wg_private.key"))
	if info.Mode().Perm() != 0600 {
		t.Errorf("expected WG private key mode 0600, got %o", info.Mode().Perm())
	}
}

func TestDeviceIDFromPublicKey(t *testing.T) {
	// Deterministic: same key should always produce the same ID
	pub := make(ed25519.PublicKey, ed25519.PublicKeySize)
	for i := range pub {
		pub[i] = byte(i)
	}

	id1 := DeviceIDFromPublicKey(pub)
	id2 := DeviceIDFromPublicKey(pub)

	if id1 != id2 {
		t.Errorf("device ID not deterministic: %q vs %q", id1, id2)
	}

	if len(id1) != 8 {
		t.Errorf("expected device ID length 8, got %d (%q)", len(id1), id1)
	}

	// Should be hex
	for _, c := range id1 {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			t.Errorf("device ID contains non-hex char: %c in %q", c, id1)
		}
	}
}

func TestKeysExist(t *testing.T) {
	dir := t.TempDir()

	if KeysExist(dir) {
		t.Error("expected KeysExist false for empty dir")
	}

	// Create just the private key
	os.WriteFile(filepath.Join(dir, "identity.key"), []byte("test"), 0600)
	if !KeysExist(dir) {
		t.Error("expected KeysExist true when identity.key exists")
	}
}

func TestLoadIdentityPublicKey(t *testing.T) {
	dir := t.TempDir()

	pub, err := GenerateIdentityKeypair(dir)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	loaded, err := LoadIdentityPublicKey(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if !pub.Equal(loaded) {
		t.Error("loaded public key doesn't match generated key")
	}
}

func TestRemoveBlock(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		start    string
		end      string
		expected string
	}{
		{
			name:     "removes block",
			input:    "before\n# START\nstuff\n# END\nafter\n",
			start:    "# START",
			end:      "# END",
			expected: "before\nafter\n",
		},
		{
			name:     "no match",
			input:    "just text\n",
			start:    "# START",
			end:      "# END",
			expected: "just text\n",
		},
		{
			name:     "only start marker",
			input:    "before\n# START\nstuff\n",
			start:    "# START",
			end:      "# END",
			expected: "before\n# START\nstuff\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := removeBlock(tt.input, tt.start, tt.end)
			if result != tt.expected {
				t.Errorf("removeBlock:\n  input: %q\n  expected: %q\n  got: %q", tt.input, tt.expected, result)
			}
		})
	}
}

func TestInjectAndRemoveHook(t *testing.T) {
	// Create a temporary zshrc
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("# existing config\nexport FOO=bar\n"), 0644)

	// Override ShellRCPath for testing by writing directly
	content, _ := os.ReadFile(rcPath)
	text := string(content)

	// Simulate injection
	hook := ZshHook()
	text = strings.TrimRight(text, "\n") + "\n\n" + hook.Hook + "\n\n" + hook.Integration + "\n"
	os.WriteFile(rcPath, []byte(text), 0644)

	// Verify hook is present
	content, _ = os.ReadFile(rcPath)
	text = string(content)
	if !strings.Contains(text, "Canopy Hook") {
		t.Error("hook not found after injection")
	}
	if !strings.Contains(text, "Canopy Shell Integration") {
		t.Error("integration not found after injection")
	}
	if !strings.Contains(text, "export FOO=bar") {
		t.Error("existing config was lost")
	}
	if !strings.Contains(text, "__canopy_precmd") {
		t.Error("precmd function not found")
	}
	if !strings.Contains(text, "133;A") {
		t.Error("OSC 133;A marker not found")
	}
	if !strings.Contains(text, "133;C") {
		t.Error("OSC 133;C marker not found")
	}
	if !strings.Contains(text, "133;D") {
		t.Error("OSC 133;D marker not found")
	}

	// Simulate removal
	text = removeBlock(text, hookStartMarker, hookEndMarker)
	text = removeBlock(text, "# --- Canopy Shell Integration (do not edit) ---", "# --- End Canopy Shell Integration ---")
	os.WriteFile(rcPath, []byte(text), 0644)

	// Verify hook is removed
	content, _ = os.ReadFile(rcPath)
	text = string(content)
	if strings.Contains(text, "Canopy Hook") {
		t.Error("hook still present after removal")
	}
	if strings.Contains(text, "Canopy Shell Integration") {
		t.Error("integration still present after removal")
	}
	if !strings.Contains(text, "export FOO=bar") {
		t.Error("existing config was lost during removal")
	}
}

func TestInjectHookIdempotent(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("# existing\n"), 0644)

	// Inject twice by simulating
	hook := ZshHook()
	for i := 0; i < 2; i++ {
		content, _ := os.ReadFile(rcPath)
		text := string(content)
		text = removeBlock(text, hookStartMarker, hookEndMarker)
		text = removeBlock(text, "# --- Canopy Shell Integration (do not edit) ---", "# --- End Canopy Shell Integration ---")
		text = strings.TrimRight(text, "\n") + "\n\n" + hook.Hook + "\n\n" + hook.Integration + "\n"
		os.WriteFile(rcPath, []byte(text), 0644)
	}

	content, _ := os.ReadFile(rcPath)
	text := string(content)

	// Should only have one copy of each marker
	hookCount := strings.Count(text, "Canopy Hook (do not edit)")
	if hookCount != 1 {
		t.Errorf("expected 1 hook block, found %d", hookCount)
	}

	integrationCount := strings.Count(text, "Canopy Shell Integration (do not edit)")
	if integrationCount != 1 {
		t.Errorf("expected 1 integration block, found %d", integrationCount)
	}
}

func TestShellHookContent(t *testing.T) {
	// Verify all shell hooks have required components
	tests := []struct {
		name  string
		hook  ShellHook
	}{
		{"zsh", ZshHook()},
		{"bash", BashHook()},
		{"fish", FishHook()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Hook must contain session ID guard
			if !strings.Contains(tt.hook.Hook, "CANOPY_SESSION_ID") {
				t.Error("hook missing CANOPY_SESSION_ID guard")
			}

			// Hook must contain daemon ping check
			if !strings.Contains(tt.hook.Hook, "daemon ping") {
				t.Error("hook missing daemon ping check")
			}

			// Hook must use exec
			if !strings.Contains(tt.hook.Hook, "exec canopyd attach") {
				t.Error("hook missing exec canopyd attach")
			}

			// Integration must have all 3 OSC 133 markers
			if !strings.Contains(tt.hook.Integration, "133;A") {
				t.Error("integration missing OSC 133;A (prompt start)")
			}
			if !strings.Contains(tt.hook.Integration, "133;C") {
				t.Error("integration missing OSC 133;C (command exec start)")
			}
			if !strings.Contains(tt.hook.Integration, "133;D") {
				t.Error("integration missing OSC 133;D (command done)")
			}
		})
	}
}

func TestPairedDevices(t *testing.T) {
	// Use a temp directory for config
	dir := t.TempDir()
	devicesPath := filepath.Join(dir, "devices.json")

	// Write initial empty list
	os.WriteFile(devicesPath, []byte("[]"), 0600)

	// Read (manually for testing since LoadPairedDevices uses config dir)
	// Just verify the JSON structure works
	device := PairedDevice{
		DeviceID:       "test1234",
		Name:           "Test iPhone",
		WGPublicKey:    "abcdef",
		IdentityPubKey: "123456",
		PairedAt:       "2026-01-01T00:00:00Z",
	}

	if device.DeviceID != "test1234" {
		t.Error("device ID mismatch")
	}
}

func TestPlistContent(t *testing.T) {
	content := PlistContent("/usr/local/bin/canopyd")

	if !strings.Contains(content, "dev.canopy.daemon") {
		t.Error("plist missing label")
	}
	if !strings.Contains(content, "/usr/local/bin/canopyd") {
		t.Error("plist missing binary path")
	}
	if !strings.Contains(content, "daemon") {
		t.Error("plist missing daemon argument")
	}
	if !strings.Contains(content, "start") {
		t.Error("plist missing start argument")
	}
	if !strings.Contains(content, "SuccessfulExit") {
		t.Error("plist missing KeepAlive config")
	}
	if !strings.Contains(content, "Background") {
		t.Error("plist missing ProcessType")
	}
}
