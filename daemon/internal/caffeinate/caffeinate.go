// Package caffeinate prevents macOS from sleeping while terminal sessions are active.
// It wraps the system's caffeinate command, which asserts power management assertions
// to keep the system awake.
package caffeinate

import (
	"os/exec"
	"sync"

	"go.uber.org/zap"
)

// Manager controls caffeinate process lifecycle.
type Manager struct {
	logger  *zap.Logger
	mu      sync.Mutex
	cmd     *exec.Cmd
	running bool
}

// NewManager creates a new caffeinate manager.
func NewManager(logger *zap.Logger) *Manager {
	return &Manager{logger: logger}
}

// Start begins preventing sleep. Idempotent — does nothing if already running.
// Uses caffeinate -i (prevent idle sleep) -s (prevent system sleep on AC).
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return
	}

	cmd := exec.Command("caffeinate", "-i", "-s")
	if err := cmd.Start(); err != nil {
		m.logger.Warn("failed to start caffeinate", zap.Error(err))
		return
	}

	m.cmd = cmd
	m.running = true
	m.logger.Debug("caffeinate started, preventing system sleep")

	// Reap the process when it exits.
	go func() {
		cmd.Wait()
		m.mu.Lock()
		if m.cmd == cmd {
			m.running = false
			m.cmd = nil
		}
		m.mu.Unlock()
	}()
}

// Stop allows the system to sleep again. Idempotent.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running || m.cmd == nil || m.cmd.Process == nil {
		m.running = false
		return
	}

	m.cmd.Process.Kill()
	m.cmd = nil
	m.running = false
	m.logger.Debug("caffeinate stopped, system can sleep")
}

// IsRunning returns whether caffeinate is currently active.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.running
}

// UpdateForSessionCount starts or stops caffeinate based on active session count.
// When activeSessions > 0, caffeinate is started. When 0, it is stopped.
func (m *Manager) UpdateForSessionCount(activeSessions int) {
	if activeSessions > 0 {
		m.Start()
	} else {
		m.Stop()
	}
}
