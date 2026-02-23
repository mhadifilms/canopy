package process

import (
	"os"
	"testing"

	"github.com/canopy-dev/canopyd/internal/session"
)

func TestToolTypeForProcess(t *testing.T) {
	tests := []struct {
		name string
		want session.ToolType
	}{
		{"claude", session.ToolClaudeCode},
		{"aider", session.ToolAider},
		{"goose", session.ToolGoose},
		{"codex", session.ToolCodex},
		{"npm", session.ToolGeneric},
		{"zsh", session.ToolGeneric},
		{"unknown", session.ToolGeneric},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ToolTypeForProcess(tt.name)
			if got != tt.want {
				t.Errorf("ToolTypeForProcess(%q): got %q, want %q", tt.name, got, tt.want)
			}
		})
	}
}

func TestDetectForegroundSelf(t *testing.T) {
	// Detect our own process (the test runner).
	pid := os.Getpid()
	info, err := DetectForeground(pid)
	if err != nil {
		t.Fatalf("DetectForeground(%d): %v", pid, err)
	}
	if info == nil {
		t.Fatal("DetectForeground returned nil")
	}
	// The info should at least have a PID.
	if info.PID == 0 {
		t.Error("PID should not be 0")
	}
}

func TestGetChildPIDs(t *testing.T) {
	// PID 1 (launchd) should have children.
	children := GetChildPIDs(1)
	if len(children) == 0 {
		t.Skip("no children for PID 1, might not have permissions")
	}
	// Verify they're positive PIDs.
	for _, pid := range children {
		if pid <= 0 {
			t.Errorf("invalid child PID: %d", pid)
		}
	}
}
