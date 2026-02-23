// Package session manages the session registry: tracking active terminal sessions,
// their metadata, and coordinating I/O between attach processes and subscribers.
package session

import (
	"strings"
	"sync"
	"time"

	"github.com/canopy-dev/canopyd/internal/parser"
)

// Status represents the current state of a session.
type Status string

const (
	StatusActive Status = "active"
	StatusIdle   Status = "idle"
	StatusEnded  Status = "ended"
	StatusError  Status = "error"
)

// ToolType identifies the foreground AI tool type.
type ToolType string

const (
	ToolNone       ToolType = ""
	ToolGeneric    ToolType = "generic"
	ToolClaudeCode ToolType = "claude_code"
	ToolAider      ToolType = "aider"
	ToolGoose      ToolType = "goose"
	ToolCodex      ToolType = "codex"
)

// Meta holds session metadata as stored in meta.json.
type Meta struct {
	SessionID      string     `json:"session_id"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at"`
	Shell          string     `json:"shell"`
	InitialCWD     string     `json:"initial_cwd"`
	CurrentCWD     string     `json:"current_cwd"`
	TerminalRows   int        `json:"terminal_rows"`
	TerminalCols   int        `json:"terminal_cols"`
	Hostname       string     `json:"hostname"`
	DeviceID       string     `json:"device_id"`
	CurrentProcess string     `json:"current_process"`
	ToolType       ToolType   `json:"tool_type"`
	Status         Status     `json:"status"`
	LastActivityAt time.Time  `json:"last_activity_at"`
	TotalCommands  int        `json:"total_commands"`
	Title          string     `json:"title"`
	RawLogBytes    int64      `json:"raw_log_bytes"`
	EventsCount    int        `json:"events_count"`
}

// Subscriber receives events for a session.
type Subscriber struct {
	Events chan parser.Event
	ID     string
}

// Session is a live in-memory session with metadata and event routing.
type Session struct {
	Meta *Meta

	mu          sync.RWMutex
	subscribers map[string]*Subscriber
	commands    []string // first few commands for title generation
}

// NewSession creates a new live session.
func NewSession(meta *Meta) *Session {
	return &Session{
		Meta:        meta,
		subscribers: make(map[string]*Subscriber),
	}
}

// Subscribe adds an event subscriber. Returns the subscriber for later removal.
func (s *Session) Subscribe(id string) *Subscriber {
	sub := &Subscriber{
		Events: make(chan parser.Event, 256),
		ID:     id,
	}
	s.mu.Lock()
	s.subscribers[id] = sub
	s.mu.Unlock()
	return sub
}

// Unsubscribe removes a subscriber.
func (s *Session) Unsubscribe(id string) {
	s.mu.Lock()
	if sub, ok := s.subscribers[id]; ok {
		close(sub.Events)
		delete(s.subscribers, id)
	}
	s.mu.Unlock()
}

// Broadcast sends an event to all subscribers (non-blocking: drops if full).
func (s *Session) Broadcast(event parser.Event) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, sub := range s.subscribers {
		select {
		case sub.Events <- event:
		default:
			// Drop if subscriber is slow.
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (s *Session) SubscriberCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subscribers)
}

// AddRawLogBytes atomically adds to the raw log byte counter.
func (s *Session) AddRawLogBytes(n int64) {
	s.mu.Lock()
	s.Meta.RawLogBytes += n
	s.mu.Unlock()
}

// GetRawLogBytes returns the raw log byte counter.
func (s *Session) GetRawLogBytes() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Meta.RawLogBytes
}

// SetStatus sets the session status.
func (s *Session) SetStatus(status Status) {
	s.mu.Lock()
	s.Meta.Status = status
	s.mu.Unlock()
}

// GetStatus returns the session status.
func (s *Session) GetStatus() Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Meta.Status
}

// UpdateMeta calls fn while holding the session lock for safe meta field updates.
func (s *Session) UpdateMeta(fn func(m *Meta)) {
	s.mu.Lock()
	fn(s.Meta)
	s.mu.Unlock()
}

// ReadMeta calls fn while holding a read lock for safe meta field reads.
func (s *Session) ReadMeta(fn func(m *Meta)) {
	s.mu.RLock()
	fn(s.Meta)
	s.mu.RUnlock()
}

// IncrementEventsCount atomically increments the events counter.
func (s *Session) IncrementEventsCount() {
	s.mu.Lock()
	s.Meta.EventsCount++
	s.mu.Unlock()
}

// RecordCommand records a command for title generation.
func (s *Session) RecordCommand(cmd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Meta.TotalCommands++
	s.Meta.LastActivityAt = time.Now()
	if len(s.commands) < 3 {
		s.commands = append(s.commands, cmd)
		s.Meta.Title = s.generateTitle()
	}
}

// SetAITitle sets the title from the first AI message.
func (s *Session) SetAITitle(tool string, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Only override if no AI title set yet.
	if !strings.Contains(s.Meta.Title, ":") || len(s.commands) <= 1 {
		if len(message) > 50 {
			message = message[:50]
		}
		s.Meta.Title = tool + ": " + message
	}
}

func (s *Session) generateTitle() string {
	if len(s.commands) == 0 {
		return ""
	}
	if len(s.commands) == 1 {
		cmd := s.commands[0]
		if len(cmd) > 60 {
			cmd = cmd[:60]
		}
		return cmd
	}
	parts := make([]string, len(s.commands))
	for i, cmd := range s.commands {
		if len(cmd) > 30 {
			cmd = cmd[:30]
		}
		parts[i] = cmd
	}
	return strings.Join(parts, " && ")
}

// Registry tracks all active sessions.
type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewRegistry creates a new session registry.
func NewRegistry() *Registry {
	return &Registry{
		sessions: make(map[string]*Session),
	}
}

// Register adds a new session to the registry.
func (r *Registry) Register(sess *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[sess.Meta.SessionID] = sess
}

// Get returns a live session by ID, or nil if not found.
func (r *Registry) Get(id string) *Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.sessions[id]
}

// Remove removes a session from the registry.
func (r *Registry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.sessions, id)
}

// List returns metadata for all sessions.
func (r *Registry) List(includeEnded bool) []*Meta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var result []*Meta
	for _, s := range r.sessions {
		if !includeEnded && s.Meta.Status == StatusEnded {
			continue
		}
		result = append(result, s.Meta)
	}
	return result
}

// Count returns the number of non-ended sessions.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, s := range r.sessions {
		if s.Meta.Status != StatusEnded {
			count++
		}
	}
	return count
}
