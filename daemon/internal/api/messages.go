package api

import (
	"time"

	"github.com/canopy-dev/canopyd/internal/parser"
)

// Client → Daemon messages.

// IncomingMessage is the envelope for all client-to-daemon messages.
type IncomingMessage struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id,omitempty"`

	// list_sessions fields
	Filter *SessionFilter `json:"filter,omitempty"`
	Limit  int            `json:"limit,omitempty"`
	Offset int            `json:"offset,omitempty"`

	// get_history fields
	Since *time.Time `json:"since,omitempty"`

	// input fields
	Text    string `json:"text,omitempty"`
	BytesB64 string `json:"bytes_b64,omitempty"`

	// signal fields
	Signal string `json:"signal,omitempty"`

	// read_file fields
	Path     string `json:"path,omitempty"`
	MaxBytes int    `json:"max_bytes,omitempty"`

	// search_sessions fields
	Query     string     `json:"query,omitempty"`
	DateRange *DateRange `json:"date_range,omitempty"`
}

// SessionFilter configures session listing.
type SessionFilter struct {
	Status       []string   `json:"status,omitempty"`
	IncludeEnded bool       `json:"include_ended,omitempty"`
	Since        *time.Time `json:"since,omitempty"`
}

// DateRange for search.
type DateRange struct {
	From *time.Time `json:"from,omitempty"`
	To   *time.Time `json:"to,omitempty"`
}

// Daemon → Client messages.

// OutgoingMessage is the envelope for all daemon-to-client messages.
type OutgoingMessage struct {
	Type string `json:"type"`

	// session_list
	Sessions []SessionInfo `json:"sessions,omitempty"`
	Total    int           `json:"total,omitempty"`

	// event
	SessionID string        `json:"session_id,omitempty"`
	Event     *parser.Event `json:"event,omitempty"`

	// session_status
	Status         string `json:"status,omitempty"`
	PreviousStatus string `json:"previous_status,omitempty"`
	Detail         string `json:"detail,omitempty"`

	// session_started / session_ended
	Session      *SessionInfo `json:"session,omitempty"`
	EndedAt      string       `json:"ended_at,omitempty"`
	LastExitCode *int         `json:"last_exit_code,omitempty"`

	// history
	Events     []parser.Event `json:"events,omitempty"`
	HasMore    bool           `json:"has_more,omitempty"`
	NextCursor string         `json:"next_cursor,omitempty"`

	// file_contents
	Path      string `json:"path,omitempty"`
	Content   string `json:"content,omitempty"`
	Language  string `json:"language,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`

	// search_results
	Query   string         `json:"query,omitempty"`
	Results []SearchResult `json:"results,omitempty"`

	// info
	Hostname       string `json:"hostname,omitempty"`
	DeviceID       string `json:"device_id,omitempty"`
	Version        string `json:"version,omitempty"`
	ActiveSessions int    `json:"active_sessions,omitempty"`

	// error
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// SessionInfo is a summary of a session for the session list.
type SessionInfo struct {
	SessionID       string `json:"session_id"`
	Status          string `json:"status"`
	ToolType        string `json:"tool_type,omitempty"`
	CurrentProcess  string `json:"current_process,omitempty"`
	Title           string `json:"title"`
	CWD             string `json:"cwd"`
	StartedAt       string `json:"started_at"`
	LastActivityAt  string `json:"last_activity_at"`
	Hostname        string `json:"hostname"`
	Preview         string `json:"preview,omitempty"`
	TotalCommands   int    `json:"total_commands"`
	ConnectedClients int   `json:"connected_clients"`
}

// SearchResult is a session match from a search.
type SearchResult struct {
	SessionID string        `json:"session_id"`
	Title     string        `json:"title"`
	StartedAt string        `json:"started_at"`
	Matches   []SearchMatch `json:"matches"`
}

// SearchMatch is a single match within a session.
type SearchMatch struct {
	EventType string `json:"event_type"`
	Timestamp string `json:"ts"`
	Snippet   string `json:"snippet"`
}
