package caffeinate

import (
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestStartStop(t *testing.T) {
	logger := zap.NewNop()
	m := NewManager(logger)

	// Start should make it running.
	m.Start()
	if !m.IsRunning() {
		t.Fatal("expected IsRunning=true after Start")
	}

	// Start is idempotent — calling again should not error.
	m.Start()
	if !m.IsRunning() {
		t.Fatal("expected IsRunning=true after second Start")
	}

	// Stop should make it not running.
	m.Stop()
	// Give the goroutine a moment to reap.
	time.Sleep(50 * time.Millisecond)
	if m.IsRunning() {
		t.Fatal("expected IsRunning=false after Stop")
	}

	// Stop is idempotent.
	m.Stop()
	if m.IsRunning() {
		t.Fatal("expected IsRunning=false after second Stop")
	}
}

func TestUpdateForSessionCount(t *testing.T) {
	logger := zap.NewNop()
	m := NewManager(logger)

	// With active sessions, caffeinate should start.
	m.UpdateForSessionCount(3)
	if !m.IsRunning() {
		t.Fatal("expected running when session count > 0")
	}

	// Still running with 1 session.
	m.UpdateForSessionCount(1)
	if !m.IsRunning() {
		t.Fatal("expected running when session count = 1")
	}

	// Should stop when session count drops to 0.
	m.UpdateForSessionCount(0)
	time.Sleep(50 * time.Millisecond)
	if m.IsRunning() {
		t.Fatal("expected not running when session count = 0")
	}
}

func TestIsRunning(t *testing.T) {
	logger := zap.NewNop()
	m := NewManager(logger)

	if m.IsRunning() {
		t.Fatal("new manager should not be running")
	}

	m.Start()
	if !m.IsRunning() {
		t.Fatal("should be running after Start")
	}

	m.Stop()
	time.Sleep(50 * time.Millisecond)
	if m.IsRunning() {
		t.Fatal("should not be running after Stop")
	}
}
