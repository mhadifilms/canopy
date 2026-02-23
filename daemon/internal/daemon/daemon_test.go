package daemon

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/canopy-dev/canopyd/internal/config"
	"github.com/canopy-dev/canopyd/internal/protocol"
	"github.com/canopy-dev/canopyd/internal/session"
	"github.com/canopy-dev/canopyd/internal/storage"
	"go.uber.org/zap"
)

func newTestDaemon(t *testing.T) (*Daemon, context.CancelFunc) {
	t.Helper()
	cfg := config.Default()
	logger, _ := zap.NewDevelopment()

	d := New(cfg, logger)

	// Use a temp dir for storage so tests don't pollute real storage.
	tmpDir := t.TempDir()
	d.store = newTestStore(t, tmpDir)

	_, cancel := context.WithCancel(context.Background())
	d.cancel = cancel

	return d, cancel
}

func newTestStore(t *testing.T, dir string) *storage.Store {
	t.Helper()
	return storage.New(dir)
}

func TestHandleRegisterCreatesSession(t *testing.T) {
	d, cancel := newTestDaemon(t)
	defer cancel()

	reg := protocol.SessionRegister{
		SessionID:    "test-session-1",
		ShellPID:     0, // Disable process polling in tests.
		CWD:          "/tmp",
		Rows:         24,
		Cols:         80,
		Hostname:     "testhost",
		RegisteredAt: time.Now().UTC(),
	}

	// Create a dummy conn for connection tracking.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	sess, sessionID := d.handleRegister(reg, server)

	if sessionID != "test-session-1" {
		t.Errorf("expected session ID test-session-1, got %s", sessionID)
	}
	if sess == nil {
		t.Fatal("expected non-nil session")
	}
	if sess.Meta.Status != session.StatusIdle {
		t.Errorf("expected status idle, got %s", sess.Meta.Status)
	}
	if sess.Meta.Hostname != "testhost" {
		t.Errorf("expected hostname testhost, got %s", sess.Meta.Hostname)
	}

	// Verify session is in the registry.
	got := d.registry.Get("test-session-1")
	if got == nil {
		t.Fatal("session not found in registry")
	}

	// Verify connection is tracked.
	d.connMu.Lock()
	conn := d.conns["test-session-1"]
	d.connMu.Unlock()
	if conn == nil {
		t.Fatal("connection not tracked for session")
	}

	// Verify pipeline was created.
	pipeline := d.getPipeline("test-session-1")
	if pipeline == nil {
		t.Fatal("pipeline not created for session")
	}

	// Clean up.
	d.closePipeline("test-session-1")
}

func TestWriteToSession(t *testing.T) {
	d, cancel := newTestDaemon(t)
	defer cancel()

	// Set up a pipe to simulate the attach connection.
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	sessionID := "write-test-session"
	d.connMu.Lock()
	d.conns[sessionID] = server
	d.connMu.Unlock()

	// Write to the session in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		errCh <- d.writeToSession(sessionID, []byte("hello from iOS"))
	}()

	// Read the frame from the client side.
	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	frame, err := protocol.ReadFrame(client)
	if err != nil {
		t.Fatalf("read frame: %v", err)
	}

	if frame.Type != protocol.FrameRemoteInput {
		t.Errorf("expected frame type %d, got %d", protocol.FrameRemoteInput, frame.Type)
	}
	if string(frame.Payload) != "hello from iOS" {
		t.Errorf("expected payload 'hello from iOS', got %q", string(frame.Payload))
	}

	if err := <-errCh; err != nil {
		t.Errorf("writeToSession error: %v", err)
	}
}

func TestWriteToSessionNotConnected(t *testing.T) {
	d, cancel := newTestDaemon(t)
	defer cancel()

	err := d.writeToSession("nonexistent", []byte("data"))
	if err == nil {
		t.Fatal("expected error for non-existent session")
	}
}

func TestPipelineEventsFlow(t *testing.T) {
	d, cancel := newTestDaemon(t)
	defer cancel()

	// Create a session and register it.
	reg := protocol.SessionRegister{
		SessionID:    "pipeline-test",
		ShellPID:     0,
		CWD:          "/tmp",
		Rows:         24,
		Cols:         80,
		Hostname:     "testhost",
		RegisteredAt: time.Now().UTC(),
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	sess, _ := d.handleRegister(reg, server)

	// Subscribe to session events.
	sub := sess.Subscribe("test-subscriber")

	// Feed output with OSC 133 shell integration markers to drive the
	// conversation parser through real state transitions:
	//   133;A = prompt start (emits idle event)
	//   133;C = command exec start (transitions to running)
	//   output text captured while running
	//   133;D;0 = command done (emits completed event)
	d.handleOutput(sess, []byte("\x1b]133;A\x07$ ls\x1b]133;C\x07"))
	d.handleOutput(sess, []byte("file1.txt\r\n"))
	d.handleOutput(sess, []byte("\x1b]133;D;0\x07"))

	// Close the pipeline to flush all pending events through the
	// accumulator and conversation parser.
	d.closePipeline("pipeline-test")

	// Wait for at least one event to arrive via the pipeline -> broadcast path.
	var received bool
	timeout := time.After(5 * time.Second)
	for !received {
		select {
		case event := <-sub.Events:
			if event.Type == "" {
				t.Error("received empty event type")
			}
			received = true
		case <-timeout:
			t.Fatal("timeout waiting for pipeline event")
		}
	}

	sess.Unsubscribe("test-subscriber")
}

func TestHandleSessionEndClosesPipeline(t *testing.T) {
	d, cancel := newTestDaemon(t)
	defer cancel()

	reg := protocol.SessionRegister{
		SessionID:    "end-test",
		ShellPID:     0,
		CWD:          "/tmp",
		Rows:         24,
		Cols:         80,
		Hostname:     "testhost",
		RegisteredAt: time.Now().UTC(),
	}

	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	sess, _ := d.handleRegister(reg, server)

	// Verify pipeline exists.
	if d.getPipeline("end-test") == nil {
		t.Fatal("pipeline should exist after register")
	}

	// End the session.
	exitCode := 0
	end := protocol.SessionEnd{
		ExitCode: exitCode,
		EndedAt:  time.Now().UTC(),
	}
	d.handleSessionEnd(sess, end)

	// Verify pipeline was closed.
	if d.getPipeline("end-test") != nil {
		t.Error("pipeline should be nil after session end")
	}

	// Verify session status.
	if sess.GetStatus() != session.StatusEnded {
		t.Errorf("expected status ended, got %s", sess.GetStatus())
	}
}

func TestFullSocketRoundtrip(t *testing.T) {
	cfg := config.Default()
	logger, _ := zap.NewDevelopment()
	d := New(cfg, logger)

	// Use temp storage.
	tmpDir := t.TempDir()
	d.store = storage.New(tmpDir)

	// Start daemon with a temp Unix socket.
	sockPath := t.TempDir() + "/test.sock"
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	d.cancel = cancel

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	d.mu.Lock()
	d.listener = listener
	d.mu.Unlock()

	// Channel to signal when handleConnection returns.
	connDone := make(chan struct{})

	// Accept one connection in background.
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		d.handleConnection(ctx, conn)
		close(connDone)
	}()

	// Connect as a client (simulating attach).
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Send registration.
	reg := protocol.SessionRegister{
		SessionID:    "roundtrip-test",
		ShellPID:     0,
		CWD:          "/tmp",
		Rows:         24,
		Cols:         80,
		Hostname:     "testhost",
		RegisteredAt: time.Now().UTC(),
	}
	regData, _ := json.Marshal(reg)
	protocol.WriteFrame(conn, protocol.Frame{
		Type:    protocol.FrameSessionRegister,
		Payload: regData,
	})

	// Give time for registration to be processed.
	time.Sleep(200 * time.Millisecond)

	// Verify session was registered.
	sess := d.registry.Get("roundtrip-test")
	if sess == nil {
		t.Fatal("session not found in registry after registration")
	}

	// Send output data.
	protocol.WriteFrame(conn, protocol.Frame{
		Type:    protocol.FrameOutputData,
		Payload: []byte("hello world\r\n"),
	})

	// Send session end.
	endData, _ := json.Marshal(protocol.SessionEnd{
		ExitCode: 0,
		EndedAt:  time.Now().UTC(),
	})
	protocol.WriteFrame(conn, protocol.Frame{
		Type:    protocol.FrameSessionEnd,
		Payload: endData,
	})

	// Wait for the connection handler to finish processing all frames.
	// This ensures all writes to sess.Meta are complete before we read them.
	select {
	case <-connDone:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for connection handler to finish")
	}

	// Now safe to read session metadata -- the handler goroutine has exited.
	if sess.GetRawLogBytes() == 0 {
		t.Error("expected non-zero raw log bytes")
	}
	if sess.GetStatus() != session.StatusEnded {
		t.Errorf("expected session status ended, got %s", sess.GetStatus())
	}
}
