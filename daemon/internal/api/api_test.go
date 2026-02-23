package api

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/canopy-dev/canopyd/internal/config"
	"github.com/canopy-dev/canopyd/internal/parser"
	"github.com/canopy-dev/canopyd/internal/session"
	"github.com/canopy-dev/canopyd/internal/storage"
	"go.uber.org/zap"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

func setupTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	logger := zap.NewNop()
	reg := session.NewRegistry()
	store := storage.New(t.TempDir())
	cfg := config.Default()

	srv := New(":0", reg, store, cfg, logger)
	httpSrv := httptest.NewServer(srv.mux)
	t.Cleanup(func() { httpSrv.Close() })

	return srv, httpSrv
}

func connectWS(t *testing.T, httpSrv *httptest.Server) (*websocket.Conn, context.Context) {
	t.Helper()
	ctx := context.Background()
	url := "ws" + httpSrv.URL[4:] + "/ws"
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("websocket dial: %v", err)
	}
	t.Cleanup(func() { conn.Close(websocket.StatusNormalClosure, "") })
	return conn, ctx
}

func TestPingPong(t *testing.T) {
	_, httpSrv := setupTestServer(t)
	conn, ctx := connectWS(t, httpSrv)

	// Send ping.
	if err := wsjson.Write(ctx, conn, IncomingMessage{Type: "ping"}); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	// Expect pong.
	var resp OutgoingMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if resp.Type != "pong" {
		t.Errorf("expected pong, got %q", resp.Type)
	}
}

func TestGetInfo(t *testing.T) {
	_, httpSrv := setupTestServer(t)
	conn, ctx := connectWS(t, httpSrv)

	if err := wsjson.Write(ctx, conn, IncomingMessage{Type: "get_info"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp OutgoingMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "info" {
		t.Errorf("expected info, got %q", resp.Type)
	}
	if resp.Version != config.Version {
		t.Errorf("version: got %q, want %q", resp.Version, config.Version)
	}
}

func TestListSessions(t *testing.T) {
	srv, httpSrv := setupTestServer(t)

	// Register a session.
	meta := &session.Meta{
		SessionID: "test-session",
		Status:    session.StatusActive,
		StartedAt: time.Now(),
		Hostname:  "test-mac",
		Title:     "npm build",
	}
	srv.registry.Register(session.NewSession(meta))

	conn, ctx := connectWS(t, httpSrv)

	if err := wsjson.Write(ctx, conn, IncomingMessage{Type: "list_sessions"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp OutgoingMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "session_list" {
		t.Errorf("expected session_list, got %q", resp.Type)
	}
	if resp.Total != 1 {
		t.Errorf("total: got %d, want 1", resp.Total)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("sessions: got %d, want 1", len(resp.Sessions))
	}
	if resp.Sessions[0].Title != "npm build" {
		t.Errorf("title: got %q", resp.Sessions[0].Title)
	}
}

func TestSubscribeAndReceiveEvents(t *testing.T) {
	srv, httpSrv := setupTestServer(t)

	meta := &session.Meta{
		SessionID: "sub-session",
		Status:    session.StatusActive,
		StartedAt: time.Now(),
	}
	sess := session.NewSession(meta)
	srv.registry.Register(sess)

	conn, ctx := connectWS(t, httpSrv)

	// Subscribe.
	if err := wsjson.Write(ctx, conn, IncomingMessage{Type: "subscribe", SessionID: "sub-session"}); err != nil {
		t.Fatalf("write subscribe: %v", err)
	}

	// Give the subscription goroutine time to set up.
	time.Sleep(100 * time.Millisecond)

	// Broadcast an event.
	sess.Broadcast(parser.Event{
		Type:      parser.EventSystemOutput,
		Timestamp: time.Now(),
		Content:   "hello from test",
	})

	// Read the event.
	var resp OutgoingMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("read event: %v", err)
	}
	if resp.Type != "event" {
		t.Errorf("expected event, got %q", resp.Type)
	}
	if resp.SessionID != "sub-session" {
		t.Errorf("session_id: got %q", resp.SessionID)
	}
	if resp.Event == nil || resp.Event.Content != "hello from test" {
		t.Error("event content mismatch")
	}
}

func TestSubscribeNonexistentSession(t *testing.T) {
	_, httpSrv := setupTestServer(t)
	conn, ctx := connectWS(t, httpSrv)

	if err := wsjson.Write(ctx, conn, IncomingMessage{Type: "subscribe", SessionID: "does-not-exist"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp OutgoingMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("expected error, got %q", resp.Type)
	}
	if resp.Code != "session_not_found" {
		t.Errorf("error code: got %q", resp.Code)
	}
}

func TestGetHistory(t *testing.T) {
	srv, httpSrv := setupTestServer(t)

	// Create a session with some events on disk.
	meta := &session.Meta{SessionID: "hist-session", Status: session.StatusActive}
	srv.store.CreateSession(meta)

	for i := 0; i < 5; i++ {
		srv.store.AppendEvent("hist-session", parser.Event{
			Type:      parser.EventUserInput,
			Timestamp: time.Now(),
			Text:      "command",
		})
	}

	conn, ctx := connectWS(t, httpSrv)

	if err := wsjson.Write(ctx, conn, IncomingMessage{
		Type:      "get_history",
		SessionID: "hist-session",
		Limit:     3,
	}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp OutgoingMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "history" {
		t.Errorf("expected history, got %q", resp.Type)
	}
	if len(resp.Events) != 3 {
		t.Errorf("events: got %d, want 3", len(resp.Events))
	}
	if !resp.HasMore {
		t.Error("HasMore should be true")
	}
}

func TestUnknownMessageType(t *testing.T) {
	_, httpSrv := setupTestServer(t)
	conn, ctx := connectWS(t, httpSrv)

	if err := wsjson.Write(ctx, conn, IncomingMessage{Type: "bogus"}); err != nil {
		t.Fatalf("write: %v", err)
	}

	var resp OutgoingMessage
	if err := wsjson.Read(ctx, conn, &resp); err != nil {
		t.Fatalf("read: %v", err)
	}
	if resp.Type != "error" {
		t.Errorf("expected error, got %q", resp.Type)
	}
	if resp.Code != "unknown_type" {
		t.Errorf("code: got %q", resp.Code)
	}
}

func TestClientCount(t *testing.T) {
	srv, httpSrv := setupTestServer(t)

	if srv.ClientCount() != 0 {
		t.Errorf("initial client count: got %d", srv.ClientCount())
	}

	conn, ctx := connectWS(t, httpSrv)

	// Send a message to ensure the connection is fully established.
	wsjson.Write(ctx, conn, IncomingMessage{Type: "ping"})
	var resp OutgoingMessage
	wsjson.Read(ctx, conn, &resp)

	if srv.ClientCount() != 1 {
		t.Errorf("after connect: got %d", srv.ClientCount())
	}

	conn.Close(websocket.StatusNormalClosure, "")
	time.Sleep(100 * time.Millisecond)

	if srv.ClientCount() != 0 {
		t.Errorf("after disconnect: got %d", srv.ClientCount())
	}
}
