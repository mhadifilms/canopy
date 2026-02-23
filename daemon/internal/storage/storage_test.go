package storage

import (
	"fmt"
	"os"
	"path/filepath"
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

func TestSearchSessions(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	now := time.Now().UTC()

	// Create 3 sessions with events containing different text.
	for i, text := range []string{"deploying the auth service", "fixing the database migration", "auth token refresh bug"} {
		sid := fmt.Sprintf("search-%d", i)
		store.CreateSession(&session.Meta{SessionID: sid, StartedAt: now, Status: session.StatusActive})
		store.AppendEvent(sid, parser.Event{
			Type: parser.EventSystemOutput, Timestamp: now, Content: text,
		})
	}

	results, err := store.SearchSessions(SearchOptions{Query: "auth"})
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results for 'auth', got %d", len(results))
	}
	// Both matching sessions should have at least one match.
	for _, r := range results {
		if len(r.Matches) == 0 {
			t.Errorf("session %s has 0 matches", r.SessionID)
		}
	}
}

func TestSearchSessionsDateFilter(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	jan := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 15, 12, 0, 0, 0, time.UTC)
	mar := time.Date(2026, 3, 15, 12, 0, 0, 0, time.UTC)

	store.CreateSession(&session.Meta{SessionID: "jan", StartedAt: jan, Status: session.StatusActive})
	store.AppendEvent("jan", parser.Event{Type: parser.EventUserInput, Timestamp: jan, Text: "deploy service"})

	store.CreateSession(&session.Meta{SessionID: "feb", StartedAt: feb, Status: session.StatusActive})
	store.AppendEvent("feb", parser.Event{Type: parser.EventUserInput, Timestamp: feb, Text: "deploy service"})

	store.CreateSession(&session.Meta{SessionID: "mar", StartedAt: mar, Status: session.StatusActive})
	store.AppendEvent("mar", parser.Event{Type: parser.EventUserInput, Timestamp: mar, Text: "deploy service"})

	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 28, 23, 59, 59, 0, time.UTC)

	results, err := store.SearchSessions(SearchOptions{
		Query: "deploy",
		From:  &from,
		To:    &to,
	})
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result in Feb range, got %d", len(results))
	}
	if results[0].SessionID != "feb" {
		t.Errorf("expected session 'feb', got %q", results[0].SessionID)
	}
}

func TestSearchSessionsEmptyQuery(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	store.CreateSession(&session.Meta{SessionID: "s1", StartedAt: time.Now().UTC()})
	store.AppendEvent("s1", parser.Event{Type: parser.EventUserInput, Timestamp: time.Now().UTC(), Text: "hello"})

	results, err := store.SearchSessions(SearchOptions{Query: ""})
	if err != nil {
		t.Fatalf("SearchSessions: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil for empty query, got %d results", len(results))
	}
}

func TestCompressSession(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	store.CreateSession(&session.Meta{SessionID: "compress-test"})
	store.AppendRawOutput("compress-test", []byte("hello world raw output data"))

	sessDir := store.SessionDir("compress-test")

	// raw.log should exist before compression.
	if _, err := os.Stat(filepath.Join(sessDir, "raw.log")); err != nil {
		t.Fatalf("raw.log should exist before compress: %v", err)
	}

	if err := store.CompressSession("compress-test"); err != nil {
		t.Fatalf("CompressSession: %v", err)
	}

	// raw.log.gz should exist after compression.
	if _, err := os.Stat(filepath.Join(sessDir, "raw.log.gz")); err != nil {
		t.Errorf("raw.log.gz should exist after compress: %v", err)
	}

	// Original raw.log should be removed.
	if _, err := os.Stat(filepath.Join(sessDir, "raw.log")); !os.IsNotExist(err) {
		t.Errorf("raw.log should be removed after compress")
	}
}

func TestCompressOldSessions(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	oldTime := time.Now().UTC().Add(-48 * time.Hour)
	newTime := time.Now().UTC()

	store.CreateSession(&session.Meta{SessionID: "old-sess", StartedAt: oldTime})
	store.AppendRawOutput("old-sess", []byte("old raw data that should be compressed"))

	store.CreateSession(&session.Meta{SessionID: "new-sess", StartedAt: newTime})
	store.AppendRawOutput("new-sess", []byte("new raw data that should stay"))

	compressed, err := store.CompressOldSessions(24)
	if err != nil {
		t.Fatalf("CompressOldSessions: %v", err)
	}
	if compressed != 1 {
		t.Errorf("expected 1 compressed, got %d", compressed)
	}

	// Old session should have .gz file.
	if _, err := os.Stat(filepath.Join(store.SessionDir("old-sess"), "raw.log.gz")); err != nil {
		t.Errorf("old session raw.log.gz should exist: %v", err)
	}

	// New session should still have raw.log (not compressed).
	if _, err := os.Stat(filepath.Join(store.SessionDir("new-sess"), "raw.log")); err != nil {
		t.Errorf("new session raw.log should still exist: %v", err)
	}
}

func TestPruneOldSessions(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	oldTime := time.Now().UTC().Add(-10 * 24 * time.Hour) // 10 days ago
	endedTime := oldTime.Add(time.Hour)

	store.CreateSession(&session.Meta{
		SessionID: "old-ended",
		StartedAt: oldTime,
		EndedAt:   &endedTime,
		Status:    session.StatusEnded,
	})

	pruned, err := store.PruneOldSessions(7)
	if err != nil {
		t.Fatalf("PruneOldSessions: %v", err)
	}
	if pruned != 1 {
		t.Errorf("expected 1 pruned, got %d", pruned)
	}

	// Session directory should be gone.
	if _, err := os.Stat(store.SessionDir("old-ended")); !os.IsNotExist(err) {
		t.Errorf("old-ended session dir should be removed")
	}
}

func TestPruneProtects24hSessions(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	// Create a session started 12h ago (within 24h protection).
	recentTime := time.Now().UTC().Add(-12 * time.Hour)
	endedTime := recentTime.Add(time.Hour)

	store.CreateSession(&session.Meta{
		SessionID: "recent-ended",
		StartedAt: recentTime,
		EndedAt:   &endedTime,
		Status:    session.StatusEnded,
	})

	pruned, err := store.PruneOldSessions(0) // retention 0 days = delete everything old
	if err != nil {
		t.Fatalf("PruneOldSessions: %v", err)
	}
	if pruned != 0 {
		t.Errorf("expected 0 pruned (24h protection), got %d", pruned)
	}

	// Session directory should still exist.
	if _, err := os.Stat(store.SessionDir("recent-ended")); err != nil {
		t.Errorf("recent session dir should still exist: %v", err)
	}
}

func TestEnforceDiskCap(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	oldTime := time.Now().UTC().Add(-48 * time.Hour)

	// Create sessions with large raw.log files.
	for i := 0; i < 3; i++ {
		sid := fmt.Sprintf("big-%d", i)
		store.CreateSession(&session.Meta{SessionID: sid, StartedAt: oldTime.Add(time.Duration(i) * time.Hour)})
		// Write ~1KB per session.
		store.AppendRawOutput(sid, make([]byte, 1024))
	}

	total, _ := store.TotalDiskUsage()
	if total == 0 {
		t.Fatal("total disk usage should be > 0")
	}

	// Set cap to 1 byte to force cleanup of everything eligible.
	// EnforceDiskCap uses GB, but we can test the logic with a 0 GB cap.
	// With 0 GB cap = 0 bytes, everything old should be cleaned.
	err := store.EnforceDiskCap(0)
	if err != nil {
		t.Fatalf("EnforceDiskCap: %v", err)
	}

	// After enforcement, total should be reduced. Sessions > 24h old should be cleaned.
	afterTotal, _ := store.TotalDiskUsage()
	if afterTotal >= total {
		t.Errorf("expected disk usage to decrease: before=%d, after=%d", total, afterTotal)
	}
}

func TestSessionDiskUsage(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	store.CreateSession(&session.Meta{SessionID: "disk-test"})
	store.AppendRawOutput("disk-test", []byte("some raw data"))
	store.AppendEvent("disk-test", parser.Event{
		Type: parser.EventUserInput, Timestamp: time.Now().UTC(), Text: "hello",
	})

	usage, err := store.SessionDiskUsage("disk-test")
	if err != nil {
		t.Fatalf("SessionDiskUsage: %v", err)
	}
	if usage <= 0 {
		t.Errorf("expected positive disk usage, got %d", usage)
	}
}

func TestDeleteSession(t *testing.T) {
	dir := t.TempDir()
	store := New(dir)

	store.CreateSession(&session.Meta{SessionID: "delete-me"})
	store.AppendRawOutput("delete-me", []byte("data"))

	if _, err := os.Stat(store.SessionDir("delete-me")); err != nil {
		t.Fatalf("session dir should exist before delete: %v", err)
	}

	if err := store.DeleteSession("delete-me"); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	if _, err := os.Stat(store.SessionDir("delete-me")); !os.IsNotExist(err) {
		t.Errorf("session dir should be removed after delete")
	}
}

func intPtr(i int) *int { return &i }
