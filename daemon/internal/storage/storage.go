// Package storage handles reading/writing session data to disk:
// meta.json, raw.log, input.log, and events.jsonl per session.
package storage

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
