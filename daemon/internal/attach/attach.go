// Package attach implements the canopyd attach command: a transparent PTY proxy
// that captures all terminal I/O and forwards it to the daemon.
package attach

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/canopy-dev/canopyd/internal/config"
	"github.com/canopy-dev/canopyd/internal/protocol"
	"github.com/creack/pty"
	"go.uber.org/zap"
)

// Options configures the attach command.
type Options struct {
	SessionID string
	Command   string
	Args      []string
}

// Run starts the PTY proxy. It creates an inner PTY running the given command,
// copies all I/O bidirectionally, and forwards copies to the daemon socket.
func Run(ctx context.Context, opts Options, logger *zap.Logger) error {
	if opts.SessionID == "" {
		return fmt.Errorf("session-id is required")
	}
	if opts.Command == "" {
		return fmt.Errorf("command is required")
	}

	logger.Debug("attach starting",
		zap.String("session_id", opts.SessionID),
		zap.String("command", opts.Command),
		zap.Strings("args", opts.Args),
	)

	// Set stdin to raw mode so we get all keystrokes.
	oldState, err := makeRaw(os.Stdin.Fd())
	if err != nil {
		return fmt.Errorf("set raw mode: %w", err)
	}
	defer restoreTerminal(os.Stdin.Fd(), oldState)

	// Start the child process with a PTY.
	cmd := exec.CommandContext(ctx, opts.Command, opts.Args...)
	cmd.Env = os.Environ()

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("start pty: %w", err)
	}
	defer ptmx.Close()

	// Match inner PTY size to outer terminal.
	if sz, err := pty.GetsizeFull(os.Stdin); err == nil {
		pty.Setsize(ptmx, sz)
	}

	// Connect to daemon socket (non-blocking, with retry).
	dc := newDaemonClient(opts.SessionID, logger)
	dc.connect()
	defer dc.close()

	// Send session registration.
	dc.sendRegister(opts, ptmx)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	// Goroutine 1 — User Input: stdin → inner PTY + copy to daemon.
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				data := buf[:n]
				ptmx.Write(data)
				dc.sendFrame(protocol.FrameInputData, data)
			}
			if err != nil {
				return
			}
		}
	}()

	// Goroutine 2 — Terminal Output: inner PTY → stdout + copy to daemon.
	wg.Add(1)
	go func() {
		defer wg.Done()
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				data := buf[:n]
				os.Stdout.Write(data)
				dc.sendFrame(protocol.FrameOutputData, data)
			}
			if err != nil {
				return
			}
		}
	}()

	// Goroutine 3 — Remote Input: daemon socket → inner PTY.
	wg.Add(1)
	go func() {
		defer wg.Done()
		dc.readRemoteInput(ctx, ptmx)
	}()

	// Goroutine 4 — Signals: SIGWINCH, SIGTERM, SIGINT.
	sigCh := make(chan os.Signal, 4)
	signal.Notify(sigCh, syscall.SIGWINCH, syscall.SIGTERM, syscall.SIGINT)
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case sig := <-sigCh:
				switch sig {
				case syscall.SIGWINCH:
					if sz, err := pty.GetsizeFull(os.Stdin); err == nil {
						pty.Setsize(ptmx, sz)
						dc.sendResize(sz.Rows, sz.Cols)
					}
				case syscall.SIGTERM, syscall.SIGINT:
					if cmd.Process != nil {
						cmd.Process.Signal(sig)
					}
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	// Heartbeat goroutine: send heartbeat every 5s.
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				dc.sendFrame(protocol.FrameHeartbeat, nil)
			case <-ctx.Done():
				return
			}
		}
	}()

	// Wait for child to exit.
	exitCode := 0
	if err := cmd.Wait(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	cancel()

	// Send session end.
	dc.sendSessionEnd(exitCode)

	logger.Debug("attach finished",
		zap.String("session_id", opts.SessionID),
		zap.Int("exit_code", exitCode),
	)

	// Wait for goroutines to drain (with timeout).
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	os.Exit(exitCode)
	return nil // unreachable
}

// daemonClient manages the non-blocking connection to the daemon's Unix socket.
type daemonClient struct {
	sessionID string
	logger    *zap.Logger
	mu        sync.Mutex
	conn      net.Conn
	buf       *frameBuf
}

// frameBuf is a bounded buffer for non-blocking socket writes.
type frameBuf struct {
	mu      sync.Mutex
	pending []protocol.Frame
	maxSize int // max total payload bytes
	curSize int
}

func newFrameBuf(maxSize int) *frameBuf {
	return &frameBuf{
		maxSize: maxSize,
	}
}

func (fb *frameBuf) push(f protocol.Frame) bool {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	newSize := fb.curSize + len(f.Payload) + 5 // 4 len + 1 type
	if newSize > fb.maxSize {
		return false // drop frame
	}
	fb.pending = append(fb.pending, f)
	fb.curSize = newSize
	return true
}

func (fb *frameBuf) drain() []protocol.Frame {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	frames := fb.pending
	fb.pending = nil
	fb.curSize = 0
	return frames
}

func newDaemonClient(sessionID string, logger *zap.Logger) *daemonClient {
	return &daemonClient{
		sessionID: sessionID,
		logger:    logger,
		buf:       newFrameBuf(1 << 20), // 1MB buffer
	}
}

func (dc *daemonClient) connect() {
	sockPath := config.SocketPath()
	conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
	if err != nil {
		dc.logger.Debug("daemon socket unavailable, will retry", zap.Error(err))
		go dc.retryConnect()
		return
	}
	dc.mu.Lock()
	dc.conn = conn
	dc.mu.Unlock()

	// Flush any buffered frames.
	dc.flushBuffer()
}

func (dc *daemonClient) retryConnect() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		sockPath := config.SocketPath()
		conn, err := net.DialTimeout("unix", sockPath, 2*time.Second)
		if err != nil {
			continue
		}
		dc.mu.Lock()
		dc.conn = conn
		dc.mu.Unlock()
		dc.flushBuffer()
		dc.logger.Debug("reconnected to daemon socket")
		return
	}
}

func (dc *daemonClient) flushBuffer() {
	frames := dc.buf.drain()
	for _, f := range frames {
		dc.writeFrame(f)
	}
}

func (dc *daemonClient) close() {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if dc.conn != nil {
		dc.conn.Close()
	}
}

func (dc *daemonClient) sendFrame(frameType byte, payload []byte) {
	f := protocol.Frame{Type: frameType, Payload: payload}
	dc.mu.Lock()
	conn := dc.conn
	dc.mu.Unlock()

	if conn == nil {
		dc.buf.push(f)
		return
	}
	if err := dc.writeFrame(f); err != nil {
		dc.buf.push(f)
	}
}

func (dc *daemonClient) writeFrame(f protocol.Frame) error {
	dc.mu.Lock()
	conn := dc.conn
	dc.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("not connected")
	}
	conn.SetWriteDeadline(time.Now().Add(100 * time.Millisecond))
	return protocol.WriteFrame(conn, f)
}

func (dc *daemonClient) sendRegister(opts Options, ptmx *os.File) {
	hostname, _ := os.Hostname()
	cwd, _ := os.Getwd()

	rows, cols := 24, 80
	if sz, err := pty.GetsizeFull(os.Stdin); err == nil {
		rows = int(sz.Rows)
		cols = int(sz.Cols)
	}

	reg := protocol.SessionRegister{
		SessionID:    opts.SessionID,
		ShellPID:     os.Getpid(),
		TTYName:      ptmx.Name(),
		CWD:          cwd,
		Rows:         rows,
		Cols:         cols,
		Hostname:     hostname,
		RegisteredAt: time.Now().UTC(),
	}

	f, err := protocol.MarshalJSONFrame(protocol.FrameSessionRegister, reg)
	if err != nil {
		dc.logger.Error("marshal register frame", zap.Error(err))
		return
	}
	dc.sendFrame(f.Type, f.Payload)
}

func (dc *daemonClient) sendResize(rows, cols uint16) {
	r := protocol.Resize{Rows: int(rows), Cols: int(cols)}
	f, err := protocol.MarshalJSONFrame(protocol.FrameResize, r)
	if err != nil {
		return
	}
	dc.sendFrame(f.Type, f.Payload)
}

func (dc *daemonClient) sendSessionEnd(exitCode int) {
	end := protocol.SessionEnd{
		ExitCode: exitCode,
		EndedAt:  time.Now().UTC(),
	}
	f, err := protocol.MarshalJSONFrame(protocol.FrameSessionEnd, end)
	if err != nil {
		return
	}
	dc.sendFrame(f.Type, f.Payload)
}

// readRemoteInput reads remote_input frames from the daemon and writes to the PTY.
func (dc *daemonClient) readRemoteInput(ctx context.Context, ptmx io.Writer) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		dc.mu.Lock()
		conn := dc.conn
		dc.mu.Unlock()
		if conn == nil {
			time.Sleep(time.Second)
			continue
		}

		conn.SetReadDeadline(time.Now().Add(5 * time.Second))
		f, err := protocol.ReadFrame(conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			time.Sleep(time.Second)
			continue
		}

		if f.Type == protocol.FrameRemoteInput {
			ptmx.Write(f.Payload)
		}
	}
}
