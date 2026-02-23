// Package storage handles reading/writing session data to disk:
// meta.json, raw.log, input.log, and events.jsonl per session.
package storage

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/canopy-dev/canopyd/internal/config"
	"github.com/canopy-dev/canopyd/internal/parser"
	"github.com/canopy-dev/canopyd/internal/session"
)

// Store manages on-disk session storage.
type Store struct {
	baseDir string
}

// New creates a new Store rooted at the given directory.
func New(baseDir string) *Store {
	return &Store{baseDir: baseDir}
}

// NewDefault creates a Store at the default sessions directory.
func NewDefault() (*Store, error) {
	dir, err := config.SessionsDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create sessions dir: %w", err)
	}
	return New(dir), nil
}

// SessionDir returns the directory for a given session ID.
func (s *Store) SessionDir(sessionID string) string {
	return filepath.Join(s.baseDir, sessionID)
}

// CreateSession initializes the on-disk structure for a new session.
func (s *Store) CreateSession(meta *session.Meta) error {
	dir := s.SessionDir(meta.SessionID)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}
	return s.UpdateMeta(meta)
}

// UpdateMeta writes updated metadata to meta.json.
func (s *Store) UpdateMeta(meta *session.Meta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal meta: %w", err)
	}
	path := filepath.Join(s.SessionDir(meta.SessionID), "meta.json")
	return os.WriteFile(path, data, 0644)
}

// LoadMeta reads a session's meta.json from disk.
func (s *Store) LoadMeta(sessionID string) (*session.Meta, error) {
	path := filepath.Join(s.SessionDir(sessionID), "meta.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read meta.json: %w", err)
	}
	var meta session.Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("parse meta.json: %w", err)
	}
	return &meta, nil
}

// AppendEvent appends a structured event to events.jsonl.
func (s *Store) AppendEvent(sessionID string, event parser.Event) error {
	path := filepath.Join(s.SessionDir(sessionID), "events.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open events file: %w", err)
	}
	defer f.Close()

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	data = append(data, '\n')

	_, err = f.Write(data)
	return err
}

// ReadEvents reads events from events.jsonl, optionally filtered by time.
func (s *Store) ReadEvents(sessionID string, since *time.Time, limit int) ([]parser.Event, error) {
	path := filepath.Join(s.SessionDir(sessionID), "events.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open events file: %w", err)
	}
	defer f.Close()

	var events []parser.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	for scanner.Scan() {
		var event parser.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue // skip malformed lines
		}
		if since != nil && event.Timestamp.Before(*since) {
			continue
		}
		events = append(events, event)
		if limit > 0 && len(events) >= limit {
			break
		}
	}
	return events, scanner.Err()
}

// AppendRawOutput appends raw PTY output bytes to raw.log.
func (s *Store) AppendRawOutput(sessionID string, data []byte) error {
	return s.appendToFile(sessionID, "raw.log", data)
}

// AppendRawInput appends raw user input bytes to input.log.
func (s *Store) AppendRawInput(sessionID string, data []byte) error {
	return s.appendToFile(sessionID, "input.log", data)
}

// ListSessions returns all session IDs on disk.
func (s *Store) ListSessions() ([]string, error) {
	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		return nil, fmt.Errorf("read sessions dir: %w", err)
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// SearchResult represents a session matching a search query.
type SearchResult struct {
	SessionID string
	Title     string
	StartedAt time.Time
	Matches   []SearchMatch
}

// SearchMatch represents a single event match within a session.
type SearchMatch struct {
	EventType string
	Timestamp time.Time
	Snippet   string
}

// SearchOptions configures a full-text search across sessions.
type SearchOptions struct {
	Query    string
	From     *time.Time
	To       *time.Time
	Limit    int // max sessions to return
	MaxPerSession int // max matches per session (0 = unlimited)
}

// SearchSessions performs full-text search across events.jsonl files for all sessions.
// It scans each session's events, matching the query against text and content fields,
// with optional date range filtering.
func (s *Store) SearchSessions(opts SearchOptions) ([]SearchResult, error) {
	if opts.Query == "" {
		return nil, nil
	}
	if opts.Limit <= 0 {
		opts.Limit = 20
	}
	if opts.MaxPerSession <= 0 {
		opts.MaxPerSession = 10
	}

	query := strings.ToLower(opts.Query)

	ids, err := s.ListSessions()
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	var results []SearchResult

	for _, sid := range ids {
		if len(results) >= opts.Limit {
			break
		}

		meta, err := s.LoadMeta(sid)
		if err != nil {
			continue
		}

		// Date range filter on session start time.
		if opts.From != nil && meta.StartedAt.Before(*opts.From) {
			// Session started before the range. It may still contain events in range,
			// but skip if the session ended before range start too.
			if meta.EndedAt != nil && meta.EndedAt.Before(*opts.From) {
				continue
			}
		}
		if opts.To != nil && meta.StartedAt.After(*opts.To) {
			continue
		}

		matches := s.searchEventsInSession(sid, query, opts.From, opts.To, opts.MaxPerSession)
		if len(matches) > 0 {
			results = append(results, SearchResult{
				SessionID: sid,
				Title:     meta.Title,
				StartedAt: meta.StartedAt,
				Matches:   matches,
			})
		}
	}

	return results, nil
}

// searchEventsInSession scans a single session's events.jsonl for matches.
func (s *Store) searchEventsInSession(sessionID, query string, from, to *time.Time, maxMatches int) []SearchMatch {
	path := filepath.Join(s.SessionDir(sessionID), "events.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var matches []SearchMatch
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	for scanner.Scan() {
		if maxMatches > 0 && len(matches) >= maxMatches {
			break
		}

		var event parser.Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			continue
		}

		// Date range filter on event timestamp.
		if from != nil && event.Timestamp.Before(*from) {
			continue
		}
		if to != nil && event.Timestamp.After(*to) {
			continue
		}

		// Search in text and content fields.
		text := strings.ToLower(event.Text + event.Content)
		if strings.Contains(text, query) {
			snippet := event.Text
			if snippet == "" {
				snippet = event.Content
			}
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			matches = append(matches, SearchMatch{
				EventType: string(event.Type),
				Timestamp: event.Timestamp,
				Snippet:   snippet,
			})
		}
	}

	return matches
}

// CompressSession gzip-compresses raw.log and input.log for a session.
// The original files are replaced with .gz versions. events.jsonl is left
// uncompressed so it remains searchable.
func (s *Store) CompressSession(sessionID string) error {
	dir := s.SessionDir(sessionID)
	for _, name := range []string{"raw.log", "input.log"} {
		path := filepath.Join(dir, name)
		gzPath := path + ".gz"

		// Skip if already compressed or doesn't exist.
		if _, err := os.Stat(gzPath); err == nil {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue // file doesn't exist, skip
		}
		if info.Size() == 0 {
			continue // empty file, skip
		}

		if err := gzipFile(path, gzPath); err != nil {
			return fmt.Errorf("compress %s/%s: %w", sessionID, name, err)
		}

		// Remove original after successful compression.
		os.Remove(path)
	}
	return nil
}

// gzipFile compresses src to dst using gzip.
func gzipFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	gz, err := gzip.NewWriterLevel(out, gzip.BestCompression)
	if err != nil {
		return err
	}
	defer gz.Close()

	if _, err := io.Copy(gz, in); err != nil {
		return err
	}
	return gz.Close()
}

// SessionDiskUsage returns the total bytes used by a session directory.
func (s *Store) SessionDiskUsage(sessionID string) (int64, error) {
	dir := s.SessionDir(sessionID)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	var total int64
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		total += info.Size()
	}
	return total, nil
}

// TotalDiskUsage returns the total bytes used by all sessions.
func (s *Store) TotalDiskUsage() (int64, error) {
	ids, err := s.ListSessions()
	if err != nil {
		return 0, err
	}
	var total int64
	for _, id := range ids {
		usage, err := s.SessionDiskUsage(id)
		if err != nil {
			continue
		}
		total += usage
	}
	return total, nil
}

// sessionAge is a helper for sorting sessions by start time.
type sessionAge struct {
	ID        string
	StartedAt time.Time
	EndedAt   *time.Time
}

// PruneOldSessions deletes sessions older than the retention period.
// Never deletes sessions less than 24h old.
func (s *Store) PruneOldSessions(retentionDays int) (int, error) {
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	minAge := time.Now().Add(-24 * time.Hour)

	ids, err := s.ListSessions()
	if err != nil {
		return 0, fmt.Errorf("list sessions: %w", err)
	}

	pruned := 0
	for _, id := range ids {
		meta, err := s.LoadMeta(id)
		if err != nil {
			continue
		}

		// Never delete sessions less than 24h old.
		if meta.StartedAt.After(minAge) {
			continue
		}

		// Delete if ended and older than retention period.
		if meta.EndedAt != nil && meta.EndedAt.Before(cutoff) {
			dir := s.SessionDir(id)
			if err := os.RemoveAll(dir); err == nil {
				pruned++
			}
		}
	}

	return pruned, nil
}

// CompressOldSessions compresses raw.log and input.log for sessions older
// than the given number of hours.
func (s *Store) CompressOldSessions(hoursOld int) (int, error) {
	cutoff := time.Now().Add(-time.Duration(hoursOld) * time.Hour)

	ids, err := s.ListSessions()
	if err != nil {
		return 0, fmt.Errorf("list sessions: %w", err)
	}

	compressed := 0
	for _, id := range ids {
		meta, err := s.LoadMeta(id)
		if err != nil {
			continue
		}

		// Only compress sessions that started before the cutoff.
		if meta.StartedAt.After(cutoff) {
			continue
		}

		// Check if already compressed by looking for .gz files.
		dir := s.SessionDir(id)
		rawGz := filepath.Join(dir, "raw.log.gz")
		if _, err := os.Stat(rawGz); err == nil {
			continue // already compressed
		}

		if err := s.CompressSession(id); err == nil {
			compressed++
		}
	}

	return compressed, nil
}

// EnforceDiskCap ensures total storage stays under maxGB.
// Strategy per spec:
// 1. Delete raw.log/input.log for oldest sessions (keep meta.json + events.jsonl)
// 2. If still over, delete entire oldest session directories
// 3. Never delete sessions < 24h old
func (s *Store) EnforceDiskCap(maxGB int) error {
	maxBytes := int64(maxGB) * 1024 * 1024 * 1024

	total, err := s.TotalDiskUsage()
	if err != nil {
		return fmt.Errorf("calculate disk usage: %w", err)
	}
	if total <= maxBytes {
		return nil
	}

	minAge := time.Now().Add(-24 * time.Hour)

	// Gather sessions sorted oldest first.
	ids, err := s.ListSessions()
	if err != nil {
		return fmt.Errorf("list sessions: %w", err)
	}

	var sessions []sessionAge
	for _, id := range ids {
		meta, err := s.LoadMeta(id)
		if err != nil {
			continue
		}
		sessions = append(sessions, sessionAge{
			ID:        id,
			StartedAt: meta.StartedAt,
			EndedAt:   meta.EndedAt,
		})
	}
	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].StartedAt.Before(sessions[j].StartedAt)
	})

	// Phase 1: Delete raw.log/input.log (and .gz variants) for oldest sessions.
	for _, sa := range sessions {
		if total <= maxBytes {
			return nil
		}
		if sa.StartedAt.After(minAge) {
			continue
		}

		dir := s.SessionDir(sa.ID)
		for _, name := range []string{"raw.log", "input.log", "raw.log.gz", "input.log.gz"} {
			path := filepath.Join(dir, name)
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			freed := info.Size()
			if os.Remove(path) == nil {
				total -= freed
			}
		}
	}

	if total <= maxBytes {
		return nil
	}

	// Phase 2: Delete entire oldest session directories.
	for _, sa := range sessions {
		if total <= maxBytes {
			return nil
		}
		if sa.StartedAt.After(minAge) {
			continue
		}

		usage, err := s.SessionDiskUsage(sa.ID)
		if err != nil {
			continue
		}
		dir := s.SessionDir(sa.ID)
		if os.RemoveAll(dir) == nil {
			total -= usage
		}
	}

	return nil
}

// DeleteSession removes a session's entire directory from disk.
func (s *Store) DeleteSession(sessionID string) error {
	dir := s.SessionDir(sessionID)
	return os.RemoveAll(dir)
}

func (s *Store) appendToFile(sessionID, filename string, data []byte) error {
	path := filepath.Join(s.SessionDir(sessionID), filename)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open %s: %w", filename, err)
	}
	defer f.Close()

	_, err = f.Write(data)
	return err
}
