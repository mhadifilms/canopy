package api

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"server.go", "go"},
		{"app.ts", "typescript"},
		{"App.tsx", "typescriptreact"},
		{"main.py", "python"},
		{"styles.css", "css"},
		{"data.json", "json"},
		{"config.yaml", "yaml"},
		{"README.md", "markdown"},
		{"Dockerfile", "dockerfile"},
		{"Makefile", "makefile"},
		{"unknown.xyz", "plaintext"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := DetectLanguage(tt.path)
			if got != tt.want {
				t.Errorf("DetectLanguage(%q): got %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestReadFileRestricted(t *testing.T) {
	dir := t.TempDir()

	// Create a test file.
	filePath := filepath.Join(dir, "test.txt")
	os.WriteFile(filePath, []byte("hello world"), 0644)

	// Read within root.
	data, err := ReadFileRestricted(filePath, dir, 1024)
	if err != nil {
		t.Fatalf("ReadFileRestricted: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("content: got %q", data)
	}
}

func TestReadFileRestrictedOutsideRoot(t *testing.T) {
	dir := t.TempDir()
	otherDir := t.TempDir()

	otherFile := filepath.Join(otherDir, "secret.txt")
	os.WriteFile(otherFile, []byte("secret"), 0644)

	_, err := ReadFileRestricted(otherFile, dir, 1024)
	if err == nil {
		t.Fatal("expected error for file outside root")
	}
}

func TestReadFileRestrictedTooLarge(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "big.txt")
	os.WriteFile(filePath, make([]byte, 2048), 0644)

	_, err := ReadFileRestricted(filePath, dir, 1024)
	if err == nil {
		t.Fatal("expected error for file too large")
	}
}

func TestReadFileRestrictedNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadFileRestricted(filepath.Join(dir, "nope.txt"), dir, 1024)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}
