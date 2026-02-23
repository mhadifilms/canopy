// Package daemon manages the canopyd background daemon lifecycle:
// starting/stopping, the Unix domain socket server, and session coordination.
package daemon

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/canopy-dev/canopyd/internal/api"
	"github.com/canopy-dev/canopyd/internal/config"
	"github.com/canopy-dev/canopyd/internal/coord"
	"github.com/canopy-dev/canopyd/internal/install"
	"github.com/canopy-dev/canopyd/internal/parser"
	"github.com/canopy-dev/canopyd/internal/process"
	"github.com/canopy-dev/canopyd/internal/protocol"
	"github.com/canopy-dev/canopyd/internal/push"
	"github.com/canopy-dev/canopyd/internal/session"
	"github.com/canopy-dev/canopyd/internal/storage"
	"github.com/canopy-dev/canopyd/internal/wireguard"
	"go.uber.org/zap"
)

// Daemon is the main canopyd background process.
type Daemon struct {
	cfg         *config.Config
	logger      *zap.Logger
	registry    *session.Registry
	store       *storage.Store
	apiSrv      *api.Server
	wgEP        *wireguard.Endpoint
	coordClient *coord.Client
	pushSvc     *push.Service
	listener    net.Listener
	mu          sync.Mutex
	cancel      context.CancelFunc

	// Per-session parser pipelines. Protected by pipelineMu.
	pipelineMu sync.Mutex
	pipelines  map[string]*parser.Pipeline

	// Per-session Unix socket connections (for writing remote input back to PTY).
	connMu    sync.Mutex
	conns     map[string]net.Conn
	shellPIDs map[string]int // sessionID -> shell PID for signal forwarding
}

// New creates a new Daemon instance.
func New(cfg *config.Config, logger *zap.Logger) *Daemon {
	return &Daemon{
		cfg:       cfg,
		logger:    logger,
		registry:  session.NewRegistry(),
		pipelines: make(map[string]*parser.Pipeline),
		conns:     make(map[string]net.Conn),
		shellPIDs: make(map[string]int),
	}
}

// Registry returns the session registry.
func (d *Daemon) Registry() *session.Registry {
	return d.registry
}

// Store returns the storage instance.
func (d *Daemon) Store() *storage.Store {
	return d.store
}

// Start runs the daemon, listening on the Unix socket and serving until ctx is cancelled.
func (d *Daemon) Start(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	d.cancel = cancel
	defer cancel()

	// 1. Initialize storage.
	store, err := storage.NewDefault()
	if err != nil {
		return fmt.Errorf("init storage: %w", err)
	}
	d.store = store

	// 2. Ensure cryptographic keys exist and get device ID.
	deviceID, err := wireguard.EnsureKeys()
	if err != nil {
		return fmt.Errorf("ensure keys: %w", err)
	}

	// 3. Initialize WireGuard endpoint (Phase 1: port reservation + peer management).
	peers, err := wireguard.NewPeerStore()
	if err != nil {
		d.logger.Warn("init peer store failed, continuing without peers", zap.Error(err))
		peers = wireguard.NewPeerStoreFromPath("")
	}

	wgEP, err := wireguard.NewEndpoint(wireguard.EndpointConfig{
		ListenPort: d.cfg.WGListenPort,
		DeviceID:   deviceID,
	}, peers, d.logger)
	if err != nil {
		return fmt.Errorf("init wireguard endpoint: %w", err)
	}
	d.wgEP = wgEP

	if err := d.wgEP.Start(); err != nil {
		d.logger.Warn("wireguard endpoint start failed, API will still listen", zap.Error(err))
	} else {
		defer d.wgEP.Stop()
	}

	// 3b. Initialize coordination server client and push notification service.
	identityPub, identityPriv, err := wireguard.LoadIdentityKeyPair()
	if err != nil {
		d.logger.Warn("load identity keys for coord client failed, coordination disabled", zap.Error(err))
	} else {
		wgPubKey, err := wireguard.LoadWireGuardPublicKey()
		if err != nil {
			d.logger.Warn("load WG public key for coord client failed, coordination disabled", zap.Error(err))
		} else {
			// Collect paired device WG keys.
			var pairedWGKeys []string
			var apnsTokens []string
			devices, err := install.LoadPairedDevices()
			if err != nil {
				d.logger.Warn("load paired devices failed", zap.Error(err))
			} else {
				for _, dev := range devices {
					if dev.WGPublicKey != "" {
						pairedWGKeys = append(pairedWGKeys, dev.WGPublicKey)
					}
					if dev.APNSToken != "" {
						apnsTokens = append(apnsTokens, dev.APNSToken)
					}
				}
			}

			coordClient := coord.NewClient(coord.ClientConfig{
				CoordURL:     d.cfg.CoordURL,
				IdentityPub:  identityPub,
				IdentityPriv: identityPriv,
				WGPubKey:     wgPubKey[:],
				WGPort:       wgEP.Port(),
				PairedWGKeys: pairedWGKeys,
			}, d.logger)
			coordClient.Start(ctx)
			d.coordClient = coordClient
			defer coordClient.Stop()

			hostname, _ := config.Hostname()
			pushSvc := push.NewService(coordClient, push.DefaultTriggerConfig(), hostname, d.logger)
			if len(apnsTokens) > 0 {
				pushSvc.SetAPNSTokens(apnsTokens)
			}
			d.pushSvc = pushSvc

			d.logger.Info("coordination client started",
				zap.String("coord_url", d.cfg.CoordURL),
				zap.Int("paired_devices", len(pairedWGKeys)),
				zap.Int("apns_tokens", len(apnsTokens)),
			)
		}
	}

	// 4. Start the Unix domain socket server.
	sockPath := config.SocketPath()
	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	listener, err := net.Listen("unix", sockPath)
	if err != nil {
		return fmt.Errorf("listen on unix socket: %w", err)
	}
	d.mu.Lock()
	d.listener = listener
	d.mu.Unlock()
	defer listener.Close()
	defer os.Remove(sockPath)

	// 5. Start the WebSocket API server.
	apiAddr := wgEP.APIListenAddr(d.cfg.ListenPort)
	d.apiSrv = api.New(apiAddr, d.registry, d.store, d.cfg, d.logger)
	d.apiSrv.WriteToSession = d.writeToSession
	d.apiSrv.SignalSession = d.signalSession

	go func() {
		if err := d.apiSrv.Start(ctx); err != nil {
			d.logger.Error("api server error", zap.Error(err))
		}
	}()

	d.logger.Info("daemon started",
		zap.String("socket", sockPath),
		zap.String("api_addr", apiAddr),
		zap.String("device_id", deviceID),
		zap.String("version", config.Version),
	)

	// Handle shutdown signals.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go func() {
		select {
		case sig := <-sigCh:
			d.logger.Info("received signal, shutting down", zap.String("signal", sig.String()))
			cancel()
		case <-ctx.Done():
		}
	}()

	// Close listener when context is cancelled.
	go func() {
		<-ctx.Done()
		listener.Close()
	}()

	// 6. Accept connections until context is cancelled.
	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				d.shutdownPipelines()
				d.logger.Info("daemon stopped")
				return nil
			default:
				d.logger.Error("accept connection", zap.Error(err))
				continue
			}
		}
		go d.handleConnection(ctx, conn)
	}
}

// Stop signals the daemon to shut down.
func (d *Daemon) Stop() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cancel != nil {
		d.cancel()
	}
}

// writeToSession writes data to a session's PTY via the Unix socket connection.
// This is the callback used by the API server for remote input from iOS.
func (d *Daemon) writeToSession(sessionID string, data []byte) error {
	d.connMu.Lock()
	conn := d.conns[sessionID]
	d.connMu.Unlock()

	if conn == nil {
		return fmt.Errorf("session %s not connected", sessionID)
	}

	frame := protocol.Frame{
		Type:    protocol.FrameRemoteInput,
		Payload: data,
	}
	conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	return protocol.WriteFrame(conn, frame)
}

// signalSession sends a signal to the process group of a session's shell.
// This is the callback used by the API server for remote signal forwarding from iOS.
func (d *Daemon) signalSession(sessionID string, sig syscall.Signal) error {
	d.connMu.Lock()
	pid := d.shellPIDs[sessionID]
	d.connMu.Unlock()

	if pid == 0 {
		return fmt.Errorf("session %s has no tracked PID", sessionID)
	}

	// Send to the process group (negative PID).
	if err := syscall.Kill(-pid, sig); err != nil {
		return fmt.Errorf("kill process group %d: %w", pid, err)
	}
	d.logger.Info("signal sent to session",
		zap.String("session_id", sessionID),
		zap.Int("pid", pid),
		zap.String("signal", sig.String()),
	)
	return nil
}

func (d *Daemon) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	var sess *session.Session
	var sessionID string

	defer func() {
		if sess != nil {
			d.logger.Debug("session connection closed", zap.String("session_id", sessionID))
		}
		// Clean up connection tracking.
		if sessionID != "" {
			d.connMu.Lock()
			delete(d.conns, sessionID)
			delete(d.shellPIDs, sessionID)
			d.connMu.Unlock()

			d.closePipeline(sessionID)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn.SetReadDeadline(time.Now().Add(30 * time.Second))
		frame, err := protocol.ReadFrame(conn)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Timeout is expected if no heartbeat — check if session is still alive.
				if sess != nil && sess.GetStatus() == session.StatusEnded {
					return
				}
				continue
			}
			// Connection closed or error.
			if sess != nil {
				d.handleSessionDisconnect(sess)
			}
			return
		}

		switch frame.Type {
		case protocol.FrameSessionRegister:
			var reg protocol.SessionRegister
			if err := protocol.UnmarshalPayload(frame, &reg); err != nil {
				d.logger.Error("unmarshal session_register", zap.Error(err))
				return
			}
			sess, sessionID = d.handleRegister(reg, conn)

		case protocol.FrameOutputData:
			if sess != nil {
				d.handleOutput(sess, frame.Payload)
			}

		case protocol.FrameInputData:
			if sess != nil {
				d.handleInput(sess, frame.Payload)
			}

		case protocol.FrameResize:
			if sess != nil {
				var resize protocol.Resize
				if err := protocol.UnmarshalPayload(frame, &resize); err == nil {
					sess.UpdateMeta(func(m *session.Meta) {
						m.TerminalRows = resize.Rows
						m.TerminalCols = resize.Cols
					})
				}
			}

		case protocol.FrameSessionEnd:
			if sess != nil {
				var end protocol.SessionEnd
				if err := protocol.UnmarshalPayload(frame, &end); err == nil {
					d.handleSessionEnd(sess, end)
				}
				return
			}

		case protocol.FrameHeartbeat:
			// Keep-alive, nothing to do.

		default:
			d.logger.Warn("unknown frame type", zap.Uint8("type", frame.Type))
		}
	}
}

func (d *Daemon) handleRegister(reg protocol.SessionRegister, conn net.Conn) (*session.Session, string) {
	meta := &session.Meta{
		SessionID:      reg.SessionID,
		StartedAt:      reg.RegisteredAt,
		Shell:          reg.CWD, // Will be updated when we detect the shell
		InitialCWD:     reg.CWD,
		CurrentCWD:     reg.CWD,
		TerminalRows:   reg.Rows,
		TerminalCols:   reg.Cols,
		Hostname:       reg.Hostname,
		Status:         session.StatusIdle,
		LastActivityAt: reg.RegisteredAt,
	}

	sess := session.NewSession(meta)
	d.registry.Register(sess)

	if err := d.store.CreateSession(meta); err != nil {
		d.logger.Error("create session on disk", zap.Error(err), zap.String("session_id", reg.SessionID))
	}

	// Track the connection for remote input write-back and the shell PID for signal forwarding.
	d.connMu.Lock()
	d.conns[reg.SessionID] = conn
	if reg.ShellPID > 0 {
		d.shellPIDs[reg.SessionID] = reg.ShellPID
	}
	d.connMu.Unlock()

	// Create a parser pipeline for this session.
	pipeline := parser.NewPipeline()
	pipeline.SetCWD(reg.CWD)

	d.pipelineMu.Lock()
	d.pipelines[reg.SessionID] = pipeline
	d.pipelineMu.Unlock()

	// Start goroutine to consume pipeline events -> store + broadcast.
	go d.consumePipelineEvents(sess, pipeline)

	d.logger.Info("session registered",
		zap.String("session_id", reg.SessionID),
		zap.String("hostname", reg.Hostname),
		zap.String("cwd", reg.CWD),
	)

	// Notify API clients about the new session.
	if d.apiSrv != nil {
		d.apiSrv.BroadcastSessionStatus(reg.SessionID, string(session.StatusIdle), "", "session registered")
	}

	// Start process detection polling for this session.
	if reg.ShellPID > 0 {
		go d.pollForegroundProcess(sess, reg.ShellPID)
	}

	return sess, reg.SessionID
}

// consumePipelineEvents reads events from the pipeline and stores/broadcasts them.
func (d *Daemon) consumePipelineEvents(sess *session.Session, pipeline *parser.Pipeline) {
	for event := range pipeline.Events() {
		sess.IncrementEventsCount()
		sess.Broadcast(event)

		if err := d.store.AppendEvent(sess.Meta.SessionID, event); err != nil {
			d.logger.Error("append pipeline event", zap.Error(err),
				zap.String("session_id", sess.Meta.SessionID),
				zap.String("event_type", string(event.Type)),
			)
		}

		// Evaluate push notification triggers.
		if d.pushSvc != nil {
			d.pushSvc.HandleEvent(context.Background(), sess.Meta.SessionID, event)
		}

		// Update session metadata based on event type.
		switch event.Type {
		case parser.EventUserInput:
			sess.UpdateMeta(func(m *session.Meta) {
				m.LastActivityAt = event.Timestamp
			})
			if event.Text != "" {
				sess.RecordCommand(event.Text)
			}
		case parser.EventSystemOutput:
			sess.UpdateMeta(func(m *session.Meta) {
				m.LastActivityAt = event.Timestamp
				if m.Status == session.StatusIdle {
					m.Status = session.StatusActive
				}
			})
			if sess.GetStatus() == session.StatusActive {
				if d.apiSrv != nil {
					d.apiSrv.BroadcastSessionStatus(sess.Meta.SessionID, string(session.StatusActive), string(session.StatusIdle), "")
				}
			}
		case parser.EventIdle:
			var statusChanged bool
			sess.UpdateMeta(func(m *session.Meta) {
				if m.Status != session.StatusIdle {
					m.Status = session.StatusIdle
					statusChanged = true
				}
				if event.CWD != "" {
					m.CurrentCWD = event.CWD
				}
			})
			if statusChanged && d.apiSrv != nil {
				d.apiSrv.BroadcastSessionStatus(sess.Meta.SessionID, string(session.StatusIdle), string(session.StatusActive), "")
			}
		}
	}
}

// getPipeline retrieves the parser pipeline for a session.
func (d *Daemon) getPipeline(sessionID string) *parser.Pipeline {
	d.pipelineMu.Lock()
	defer d.pipelineMu.Unlock()
	return d.pipelines[sessionID]
}

// closePipeline closes and removes a session's parser pipeline.
func (d *Daemon) closePipeline(sessionID string) {
	d.pipelineMu.Lock()
	pipeline, ok := d.pipelines[sessionID]
	if ok {
		delete(d.pipelines, sessionID)
	}
	d.pipelineMu.Unlock()
	if ok {
		pipeline.Close()
	}
}

func (d *Daemon) handleOutput(sess *session.Session, data []byte) {
	sess.AddRawLogBytes(int64(len(data)))

	// Store raw output bytes for replay.
	if err := d.store.AppendRawOutput(sess.Meta.SessionID, data); err != nil {
		d.logger.Error("append raw output", zap.Error(err))
	}

	// Feed raw bytes into the parser pipeline.
	// The pipeline handles ANSI stripping, line accumulation, and conversation
	// parsing, emitting structured events via consumePipelineEvents.
	if pipeline := d.getPipeline(sess.Meta.SessionID); pipeline != nil {
		pipeline.FeedOutput(data)
	}
}

func (d *Daemon) handleInput(sess *session.Session, data []byte) {
	// Store raw input bytes for replay.
	if err := d.store.AppendRawInput(sess.Meta.SessionID, data); err != nil {
		d.logger.Error("append raw input", zap.Error(err))
	}

	// Feed raw input bytes into the parser pipeline.
	// The conversation parser correlates input with OSC 133;C markers
	// and emits properly classified user_input events.
	if pipeline := d.getPipeline(sess.Meta.SessionID); pipeline != nil {
		pipeline.FeedInput(data)
	}
}

func (d *Daemon) handleSessionEnd(sess *session.Session, end protocol.SessionEnd) {
	// Close the parser pipeline first to flush remaining data.
	d.closePipeline(sess.Meta.SessionID)

	var prevStatus string
	now := end.EndedAt
	sess.UpdateMeta(func(m *session.Meta) {
		prevStatus = string(m.Status)
		m.EndedAt = &now
		m.Status = session.StatusEnded
		m.LastActivityAt = now
	})

	// Emit session-level completed event.
	event := parser.Event{
		Type:      parser.EventCompleted,
		Timestamp: now,
		ExitCode:  &end.ExitCode,
	}
	sess.ReadMeta(func(m *session.Meta) {
		if !m.StartedAt.IsZero() {
			event.DurationMS = now.Sub(m.StartedAt).Milliseconds()
		}
	})
	sess.IncrementEventsCount()
	sess.Broadcast(event)

	if err := d.store.AppendEvent(sess.Meta.SessionID, event); err != nil {
		d.logger.Error("append event", zap.Error(err))
	}
	if err := d.store.UpdateMeta(sess.Meta); err != nil {
		d.logger.Error("update meta on end", zap.Error(err))
	}

	// Evaluate push notification triggers for session completion.
	if d.pushSvc != nil {
		d.pushSvc.HandleEvent(context.Background(), sess.Meta.SessionID, event)
	}

	// Notify API clients.
	if d.apiSrv != nil {
		d.apiSrv.BroadcastSessionStatus(sess.Meta.SessionID, string(session.StatusEnded), prevStatus, fmt.Sprintf("exit_code=%d", end.ExitCode))
	}

	d.logger.Info("session ended",
		zap.String("session_id", sess.Meta.SessionID),
		zap.Int("exit_code", end.ExitCode),
	)
}

func (d *Daemon) handleSessionDisconnect(sess *session.Session) {
	d.closePipeline(sess.Meta.SessionID)
	if sess.GetStatus() != session.StatusEnded {
		var prevStatus string
		now := time.Now().UTC()
		sess.UpdateMeta(func(m *session.Meta) {
			prevStatus = string(m.Status)
			m.EndedAt = &now
			m.Status = session.StatusEnded
		})
		d.store.UpdateMeta(sess.Meta)

		if d.apiSrv != nil {
			d.apiSrv.BroadcastSessionStatus(sess.Meta.SessionID, string(session.StatusEnded), prevStatus, "disconnected")
		}
	}
}

func (d *Daemon) pollForegroundProcess(sess *session.Session, shellPID int) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastProcess string

	for range ticker.C {
		if sess.GetStatus() == session.StatusEnded {
			return
		}

		info, err := process.DetectForeground(shellPID)
		if err != nil || info == nil {
			continue
		}

		if info.Name != lastProcess && info.Name != "" {
			lastProcess = info.Name
			toolType := process.ToolTypeForProcess(info.Name)

			sess.UpdateMeta(func(m *session.Meta) {
				m.CurrentProcess = info.Name
				m.ToolType = session.ToolType(string(toolType))
			})

			// Update the pipeline's AI tool context so the conversation
			// parser can classify input correctly (command vs AI message).
			if pipeline := d.getPipeline(sess.Meta.SessionID); pipeline != nil {
				pipeline.SetProcess(info.Name, string(toolType))
			}

			event := parser.Event{
				Type:        parser.EventProcessChange,
				Timestamp:   time.Now().UTC(),
				ProcessName: info.Name,
				ToolType:    string(toolType),
				PID:         info.PID,
			}
			sess.IncrementEventsCount()
			sess.Broadcast(event)

			if err := d.store.AppendEvent(sess.Meta.SessionID, event); err != nil {
				d.logger.Error("append process_change event", zap.Error(err))
			}
		}
	}
}

// shutdownPipelines closes all active parser pipelines during shutdown.
func (d *Daemon) shutdownPipelines() {
	d.pipelineMu.Lock()
	defer d.pipelineMu.Unlock()
	for id, p := range d.pipelines {
		p.Close()
		delete(d.pipelines, id)
	}
}

// Ping checks if the daemon is running by attempting to connect to the socket.
func Ping() error {
	sockPath := config.SocketPath()
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		return fmt.Errorf("daemon not running: %w", err)
	}
	conn.Close()
	return nil
}
