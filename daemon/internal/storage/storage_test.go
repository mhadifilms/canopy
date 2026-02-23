package storage

import (
	"os"
	"testing"
	"time"

	"github.com/canopy-dev/canopyd/internal/parser"
	"github.com/canopy-dev/canopyd/internal/session"
)

func TestCreateSessionAndLoadMeta(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	meta := &session.Meta{
		SessionID:    "test-session-1",
		StartedAt:    time.Now().UTC().Truncate(time.Millisecond),
		Shell:        "/bin/zsh",
		InitialCWD:   "/Users/test",
		CurrentCWD:   "/Users/test",
		TerminalRows: 48,
		TerminalCols: 120,
		Hostname:     "test-mac",
		Status:       session.StatusActive,
	}

	if err := store.CreateSession(meta); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	// Directory should exist.
	if _, err := os.Stat(store.SessionDir("test-session-1")); err != nil {
		t.Fatalf("session dir not created: %v", err)
	}

	// Load meta back.
	loaded, err := store.LoadMeta("test-session-1")
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if loaded.SessionID != meta.SessionID {
		t.Errorf("SessionID: got %q, want %q", loaded.SessionID, meta.SessionID)
	}
	if loaded.Hostname != meta.Hostname {
		t.Errorf("Hostname: got %q, want %q", loaded.Hostname, meta.Hostname)
	}
	if loaded.TerminalRows != 48 || loaded.TerminalCols != 120 {
		t.Errorf("dims: got %dx%d", loaded.TerminalRows, loaded.TerminalCols)
	}
}

func TestAppendAndReadEvents(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	meta := &session.Meta{SessionID: "events-test", Status: session.StatusActive}
	store.CreateSession(meta)

	events := []parser.Event{
		{Type: parser.EventUserInput, Timestamp: time.Now().UTC(), Text: "ls -la"},
		{Type: parser.EventSystemOutput, Timestamp: time.Now().UTC(), Content: "total 42\n-rw-r--r-- ..."},
		{Type: parser.EventCompleted, Timestamp: time.Now().UTC(), ExitCode: intPtr(0)},
	}

	for _, e := range events {
		if err := store.AppendEvent("events-test", e); err != nil {
			t.Fatalf("AppendEvent: %v", err)
		}
	}

	// Read all events.
	read, err := store.ReadEvents("events-test", nil, 0)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(read) != 3 {
		t.Fatalf("ReadEvents: got %d events, want 3", len(read))
	}
	if read[0].Type != parser.EventUserInput {
		t.Errorf("event[0] type: got %q", read[0].Type)
	}
	if read[0].Text != "ls -la" {
		t.Errorf("event[0] text: got %q", read[0].Text)
	}
	if read[2].ExitCode == nil || *read[2].ExitCode != 0 {
		t.Error("event[2] exit code should be 0")
	}
}

func TestReadEventsWithLimit(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	store.CreateSession(&session.Meta{SessionID: "limit-test"})

	for i := 0; i < 10; i++ {
		store.AppendEvent("limit-test", parser.Event{
			Type:      parser.EventSystemOutput,
			Timestamp: time.Now().UTC(),
			Content:   "line",
		})
	}

	events, err := store.ReadEvents("limit-test", nil, 5)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 5 {
		t.Errorf("ReadEvents with limit 5: got %d events", len(events))
	}
}

func TestReadEventsWithSince(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	store.CreateSession(&session.Meta{SessionID: "since-test"})

	t1 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 1, 1, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	store.AppendEvent("since-test", parser.Event{Type: parser.EventUserInput, Timestamp: t1, Text: "first"})
	store.AppendEvent("since-test", parser.Event{Type: parser.EventUserInput, Timestamp: t2, Text: "second"})
	store.AppendEvent("since-test", parser.Event{Type: parser.EventUserInput, Timestamp: t3, Text: "third"})

	since := time.Date(2026, 1, 1, 10, 30, 0, 0, time.UTC)
	events, err := store.ReadEvents("since-test", &since, 0)
	if err != nil {
		t.Fatalf("ReadEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("ReadEvents since 10:30: got %d events, want 2", len(events))
	}
	if events[0].Text != "second" {
		t.Errorf("first event: got %q, want %q", events[0].Text, "second")
	}
}

func TestAppendRawOutput(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	store.CreateSession(&session.Meta{SessionID: "raw-test"})

	store.AppendRawOutput("raw-test", []byte("hello "))
	store.AppendRawOutput("raw-test", []byte("world"))

	data, err := os.ReadFile(store.SessionDir("raw-test") + "/raw.log")
	if err != nil {
		t.Fatalf("read raw.log: %v", err)
	}
	if string(data) != "hello world" {
		t.Errorf("raw.log: got %q", data)
	}
}

func TestListSessions(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)
	store.CreateSession(&session.Meta{SessionID: "a"})
	store.CreateSession(&session.Meta{SessionID: "b"})

	ids, err := store.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("ListSessions: got %d, want 2", len(ids))
	}
}

func TestUpdateMeta(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	meta := &session.Meta{SessionID: "update-test", Status: session.StatusActive, Title: ""}
	store.CreateSession(meta)

	meta.Title = "npm install && npm build"
	meta.Status = session.StatusEnded
	store.UpdateMeta(meta)

	loaded, err := store.LoadMeta("update-test")
	if err != nil {
		t.Fatalf("LoadMeta: %v", err)
	}
	if loaded.Title != "npm install && npm build" {
		t.Errorf("title: got %q", loaded.Title)
	}
	if loaded.Status != session.StatusEnded {
		t.Errorf("status: got %q", loaded.Status)
	}
}

func TestReadEventsMissingFile(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	events, err := store.ReadEvents("nonexistent", nil, 0)
	if err != nil {
		t.Fatalf("ReadEvents for missing session: %v", err)
	}
	if events != nil {
		t.Errorf("expected nil, got %d events", len(events))
	}
}

func intPtr(i int) *int { return &i }
