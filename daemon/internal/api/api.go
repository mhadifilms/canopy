// Package api implements the WebSocket API server that the iOS app connects to.
package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/canopy-dev/canopyd/internal/config"
	"github.com/canopy-dev/canopyd/internal/parser"
	"github.com/canopy-dev/canopyd/internal/session"
	"github.com/canopy-dev/canopyd/internal/storage"
	"go.uber.org/zap"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

const (
	maxClients       = 10
	maxMsgPerSec     = 100
	maxFileReadsMin  = 10
	maxHistoryReqMin = 30
	maxFileSize      = 1 << 20 // 1MB
	maxHistoryEvents = 1000
)

// Server is the WebSocket API server.
type Server struct {
	addr     string
	logger   *zap.Logger
	registry *session.Registry
	store    *storage.Store
	cfg      *config.Config
	mux      *http.ServeMux

	mu      sync.Mutex
	clients map[string]*Client
	nextID  atomic.Int64

	// writeToSession is called to write input to a session's PTY.
	// Set by the daemon when starting the API server.
	WriteToSession func(sessionID string, data []byte) error

	// SignalSession is called to send a signal to a session's PTY process group.
	// Set by the daemon when starting the API server.
	SignalSession func(sessionID string, sig syscall.Signal) error
}

// Client represents a connected WebSocket client.
type Client struct {
	ID            string
	conn          *websocket.Conn
	mu            sync.Mutex
	subscriptions map[string]context.CancelFunc
	fileReads     int
	historyReqs   int
	msgCount      int
	lastReset     time.Time
}

// New creates a new API server.
func New(addr string, registry *session.Registry, store *storage.Store, cfg *config.Config, logger *zap.Logger) *Server {
	s := &Server{
		addr:     addr,
		logger:   logger,
		registry: registry,
		store:    store,
		cfg:      cfg,
		mux:      http.NewServeMux(),
		clients:  make(map[string]*Client),
	}
	s.mux.HandleFunc("/ws", s.handleWebSocket)
	return s
}

// Start begins serving. Blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.addr,
		Handler: s.mux,
	}

	go func() {
		<-ctx.Done()
		srv.Close()
	}()

	s.logger.Info("api server starting", zap.String("addr", s.addr))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("api server: %w", err)
	}
	return nil
}

// ClientCount returns the number of connected clients.
func (s *Server) ClientCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.clients)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Check client limit.
	s.mu.Lock()
	if len(s.clients) >= maxClients {
		s.mu.Unlock()
		http.Error(w, "too many clients", http.StatusServiceUnavailable)
		return
	}
	s.mu.Unlock()

	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		InsecureSkipVerify: true, // Traffic is encrypted by WireGuard.
	})
	if err != nil {
		s.logger.Error("websocket accept", zap.Error(err))
		return
	}

	id := fmt.Sprintf("client-%d", s.nextID.Add(1))
	client := &Client{
		ID:            id,
		conn:          conn,
		subscriptions: make(map[string]context.CancelFunc),
		lastReset:     time.Now(),
	}

	s.mu.Lock()
	s.clients[id] = client
	s.mu.Unlock()

	s.logger.Info("client connected", zap.String("client_id", id))

	defer func() {
		// Clean up subscriptions.
		client.mu.Lock()
		for _, cancel := range client.subscriptions {
			cancel()
		}
		client.mu.Unlock()

		s.mu.Lock()
		delete(s.clients, id)
		s.mu.Unlock()

		conn.Close(websocket.StatusNormalClosure, "")
		s.logger.Info("client disconnected", zap.String("client_id", id))
	}()

	ctx := r.Context()
	for {
		var msg IncomingMessage
		if err := wsjson.Read(ctx, conn, &msg); err != nil {
			return
		}

		// Rate limit: reset counters every second for msg/s, every minute for file/history.
		now := time.Now()
		if now.Sub(client.lastReset) > time.Second {
			client.msgCount = 0
			if now.Sub(client.lastReset) > time.Minute {
				client.fileReads = 0
				client.historyReqs = 0
				client.lastReset = now
			}
		}
		client.msgCount++
		if client.msgCount > maxMsgPerSec {
			s.sendError(ctx, client, "rate_limited", "too many messages per second")
			continue
		}

		s.handleMessage(ctx, client, msg)
	}
}

func (s *Server) handleMessage(ctx context.Context, client *Client, msg IncomingMessage) {
	switch msg.Type {
	case "ping":
		s.send(ctx, client, OutgoingMessage{Type: "pong"})

	case "get_info":
		s.handleGetInfo(ctx, client)

	case "list_sessions":
		s.handleListSessions(ctx, client, msg)

	case "subscribe":
		s.handleSubscribe(ctx, client, msg.SessionID)

	case "unsubscribe":
		s.handleUnsubscribe(client, msg.SessionID)

	case "get_history":
		s.handleGetHistory(ctx, client, msg)

	case "input":
		s.handleInput(ctx, client, msg)

	case "input_raw":
		s.handleInputRaw(ctx, client, msg)

	case "signal":
		s.handleSignal(ctx, client, msg)

	case "read_file":
		s.handleReadFile(ctx, client, msg)

	case "search_sessions":
		s.handleSearchSessions(ctx, client, msg)

	default:
		s.sendError(ctx, client, "unknown_type", fmt.Sprintf("unknown message type: %s", msg.Type))
	}
}

func (s *Server) handleGetInfo(ctx context.Context, client *Client) {
	hostname, _ := os.Hostname()
	s.send(ctx, client, OutgoingMessage{
		Type:           "info",
		Hostname:       hostname,
		Version:        config.Version,
		ActiveSessions: s.registry.Count(),
	})
}

func (s *Server) handleListSessions(ctx context.Context, client *Client, msg IncomingMessage) {
	includeEnded := false
	if msg.Filter != nil {
		includeEnded = msg.Filter.IncludeEnded
	}

	metas := s.registry.List(includeEnded)

	infos := make([]SessionInfo, 0, len(metas))
	for _, m := range metas {
		sess := s.registry.Get(m.SessionID)
		connClients := 0
		if sess != nil {
			connClients = sess.SubscriberCount()
		}
		infos = append(infos, SessionInfo{
			SessionID:        m.SessionID,
			Status:           string(m.Status),
			ToolType:         string(m.ToolType),
			CurrentProcess:   m.CurrentProcess,
			Title:            m.Title,
			CWD:              m.CurrentCWD,
			StartedAt:        m.StartedAt.Format(time.RFC3339),
			LastActivityAt:   m.LastActivityAt.Format(time.RFC3339),
			Hostname:         m.Hostname,
			TotalCommands:    m.TotalCommands,
			ConnectedClients: connClients,
		})
	}

	// Apply pagination.
	total := len(infos)
	offset := msg.Offset
	if offset > len(infos) {
		offset = len(infos)
	}
	infos = infos[offset:]
	if msg.Limit > 0 && msg.Limit < len(infos) {
		infos = infos[:msg.Limit]
	}

	s.send(ctx, client, OutgoingMessage{
		Type:     "session_list",
		Sessions: infos,
		Total:    total,
	})
}

func (s *Server) handleSubscribe(ctx context.Context, client *Client, sessionID string) {
	if sessionID == "" {
		s.sendError(ctx, client, "missing_session_id", "session_id required")
		return
	}

	sess := s.registry.Get(sessionID)
	if sess == nil {
		s.sendError(ctx, client, "session_not_found", fmt.Sprintf("session %s not found", sessionID))
		return
	}

	client.mu.Lock()
	// If already subscribed, unsubscribe first.
	if cancel, ok := client.subscriptions[sessionID]; ok {
		cancel()
	}
	client.mu.Unlock()

	sub := sess.Subscribe(client.ID)
	subCtx, cancel := context.WithCancel(ctx)

	client.mu.Lock()
	client.subscriptions[sessionID] = cancel
	client.mu.Unlock()

	// Forward events to the client.
	go func() {
		defer sess.Unsubscribe(client.ID)
		for {
			select {
			case event, ok := <-sub.Events:
				if !ok {
					return
				}
				s.send(subCtx, client, OutgoingMessage{
					Type:      "event",
					SessionID: sessionID,
					Event:     &event,
				})
			case <-subCtx.Done():
				return
			}
		}
	}()
}

func (s *Server) handleUnsubscribe(client *Client, sessionID string) {
	client.mu.Lock()
	if cancel, ok := client.subscriptions[sessionID]; ok {
		cancel()
		delete(client.subscriptions, sessionID)
	}
	client.mu.Unlock()
}

func (s *Server) handleGetHistory(ctx context.Context, client *Client, msg IncomingMessage) {
	client.historyReqs++
	if client.historyReqs > maxHistoryReqMin {
		s.sendError(ctx, client, "rate_limited", "too many history requests")
		return
	}

	if msg.SessionID == "" {
		s.sendError(ctx, client, "missing_session_id", "session_id required")
		return
	}

	limit := msg.Limit
	if limit <= 0 || limit > maxHistoryEvents {
		limit = 200
	}

	events, err := s.store.ReadEvents(msg.SessionID, msg.Since, limit+1)
	if err != nil {
		s.sendError(ctx, client, "storage_error", err.Error())
		return
	}

	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}

	s.send(ctx, client, OutgoingMessage{
		Type:      "history",
		SessionID: msg.SessionID,
		Events:    events,
		HasMore:   hasMore,
	})
}

func (s *Server) handleInput(ctx context.Context, client *Client, msg IncomingMessage) {
	if msg.SessionID == "" || msg.Text == "" {
		s.sendError(ctx, client, "invalid_input", "session_id and text required")
		return
	}
	if s.WriteToSession == nil {
		s.sendError(ctx, client, "not_supported", "input not supported")
		return
	}
	data := []byte(msg.Text + "\n")
	if err := s.WriteToSession(msg.SessionID, data); err != nil {
		s.sendError(ctx, client, "input_error", err.Error())
		return
	}

	// Broadcast remote_input to other subscribers.
	s.broadcastRemoteInput(msg.SessionID, client.ID, msg.Text)
}

func (s *Server) handleInputRaw(ctx context.Context, client *Client, msg IncomingMessage) {
	if msg.SessionID == "" || msg.BytesB64 == "" {
		s.sendError(ctx, client, "invalid_input", "session_id and bytes_b64 required")
		return
	}
	if s.WriteToSession == nil {
		s.sendError(ctx, client, "not_supported", "input not supported")
		return
	}
	data, err := base64.StdEncoding.DecodeString(msg.BytesB64)
	if err != nil {
		s.sendError(ctx, client, "invalid_base64", err.Error())
		return
	}
	if err := s.WriteToSession(msg.SessionID, data); err != nil {
		s.sendError(ctx, client, "input_error", err.Error())
		return
	}
}

// signalNames maps signal name strings to syscall signals.
var signalNames = map[string]syscall.Signal{
	"SIGINT":  syscall.SIGINT,
	"SIGTERM": syscall.SIGTERM,
	"SIGHUP":  syscall.SIGHUP,
	"SIGQUIT": syscall.SIGQUIT,
	"INT":     syscall.SIGINT,
	"TERM":    syscall.SIGTERM,
	"HUP":     syscall.SIGHUP,
	"QUIT":    syscall.SIGQUIT,
}

func (s *Server) handleSignal(ctx context.Context, client *Client, msg IncomingMessage) {
	if msg.SessionID == "" || msg.Signal == "" {
		s.sendError(ctx, client, "invalid_signal", "session_id and signal required")
		return
	}
	if s.SignalSession == nil {
		s.sendError(ctx, client, "not_supported", "signal forwarding not supported")
		return
	}

	sig, ok := signalNames[strings.ToUpper(msg.Signal)]
	if !ok {
		s.sendError(ctx, client, "unknown_signal", fmt.Sprintf("unknown signal: %s", msg.Signal))
		return
	}

	if err := s.SignalSession(msg.SessionID, sig); err != nil {
		s.sendError(ctx, client, "signal_error", err.Error())
		return
	}

	s.send(ctx, client, OutgoingMessage{Type: "signal_sent", SessionID: msg.SessionID})
}

func (s *Server) handleReadFile(ctx context.Context, client *Client, msg IncomingMessage) {
	client.fileReads++
	if client.fileReads > maxFileReadsMin {
		s.sendError(ctx, client, "rate_limited", "too many file reads")
		return
	}

	if msg.Path == "" {
		s.sendError(ctx, client, "missing_path", "path required")
		return
	}

	maxSize := maxFileSize
	if msg.MaxBytes > 0 && msg.MaxBytes < maxSize {
		maxSize = msg.MaxBytes
	}

	accessRoot := ""
	if s.cfg.FileAccessRoot != nil {
		accessRoot = *s.cfg.FileAccessRoot
	} else {
		home, _ := os.UserHomeDir()
		accessRoot = home
	}

	data, err := ReadFileRestricted(msg.Path, accessRoot, maxSize)
	if err != nil {
		s.sendError(ctx, client, "file_error", err.Error())
		return
	}

	lang := DetectLanguage(msg.Path)

	s.send(ctx, client, OutgoingMessage{
		Type:      "file_contents",
		Path:      msg.Path,
		Content:   string(data),
		Language:  lang,
		SizeBytes: int64(len(data)),
	})
}

func (s *Server) handleSearchSessions(ctx context.Context, client *Client, msg IncomingMessage) {
	if msg.Query == "" {
		s.sendError(ctx, client, "missing_query", "query required")
		return
	}

	limit := msg.Limit
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	opts := storage.SearchOptions{
		Query: msg.Query,
		Limit: limit,
	}
	if msg.DateRange != nil {
		opts.From = msg.DateRange.From
		opts.To = msg.DateRange.To
	}

	storeResults, err := s.store.SearchSessions(opts)
	if err != nil {
		s.sendError(ctx, client, "storage_error", err.Error())
		return
	}

	results := make([]SearchResult, 0, len(storeResults))
	for _, sr := range storeResults {
		matches := make([]SearchMatch, 0, len(sr.Matches))
		for _, m := range sr.Matches {
			matches = append(matches, SearchMatch{
				EventType: m.EventType,
				Timestamp: m.Timestamp.Format(time.RFC3339),
				Snippet:   m.Snippet,
			})
		}
		results = append(results, SearchResult{
			SessionID: sr.SessionID,
			Title:     sr.Title,
			StartedAt: sr.StartedAt.Format(time.RFC3339),
			Matches:   matches,
		})
	}

	s.send(ctx, client, OutgoingMessage{
		Type:    "search_results",
		Query:   msg.Query,
		Results: results,
	})
}

func (s *Server) broadcastRemoteInput(sessionID, senderID, text string) {
	sess := s.registry.Get(sessionID)
	if sess == nil {
		return
	}
	event := parser.Event{
		Type:       parser.EventRemoteInput,
		Timestamp:  time.Now().UTC(),
		Text:       text,
		FromDevice: senderID,
	}
	// Send to all OTHER subscribers, not the sender.
	sess.BroadcastExcept(event, senderID)
}

// BroadcastSessionStatus sends a session_status message to ALL connected clients.
func (s *Server) BroadcastSessionStatus(sessionID, status, previousStatus, detail string) {
	msg := OutgoingMessage{
		Type:           "session_status",
		SessionID:      sessionID,
		Status:         status,
		PreviousStatus: previousStatus,
		Detail:         detail,
	}

	s.mu.Lock()
	clients := make([]*Client, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()

	for _, c := range clients {
		s.send(context.Background(), c, msg)
	}
}

func (s *Server) send(ctx context.Context, client *Client, msg OutgoingMessage) {
	client.mu.Lock()
	defer client.mu.Unlock()

	writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := wsjson.Write(writeCtx, client.conn, msg); err != nil {
		s.logger.Debug("send to client failed", zap.String("client", client.ID), zap.Error(err))
	}
}

func (s *Server) sendError(ctx context.Context, client *Client, code, message string) {
	s.send(ctx, client, OutgoingMessage{
		Type:    "error",
		Code:    code,
		Message: message,
	})
}

// SendToAllJSON is a helper to send a raw JSON message to all clients (for testing).
func (s *Server) SendToAllJSON(ctx context.Context, data json.RawMessage) {
	s.mu.Lock()
	clients := make([]*Client, 0, len(s.clients))
	for _, c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()

	for _, c := range clients {
		c.mu.Lock()
		writeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		c.conn.Write(writeCtx, websocket.MessageText, data)
		cancel()
		c.mu.Unlock()
	}
}
