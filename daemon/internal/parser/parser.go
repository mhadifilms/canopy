// Package parser implements the terminal output parsing pipeline:
// ANSI stripping, line accumulation, conversation parsing, and AI enhancement.
package parser

import "time"

// EventType identifies the type of parsed event.
type EventType string

const (
	EventUserInput     EventType = "user_input"
	EventSystemOutput  EventType = "system_output"
	EventCompleted     EventType = "completed"
	EventInputRequest  EventType = "input_request"
	EventIdle          EventType = "idle"
	EventAIResponse    EventType = "ai_response"
	EventAIAction      EventType = "ai_action"
	EventAIApproval    EventType = "ai_approval"
	EventAIUsage       EventType = "ai_usage"
	EventProcessChange EventType = "process_change"
	EventStatusChange  EventType = "status_change"
	EventRemoteInput   EventType = "remote_input"
)

// InputType classifies user input.
type InputType string

const (
	InputCommand   InputType = "command"
	InputResponse  InputType = "response"
	InputAIMessage InputType = "ai_message"
)

// Event is a single parsed event, stored as a line in events.jsonl.
type Event struct {
	Type      EventType `json:"type"`
	Timestamp time.Time `json:"ts"`

	// user_input fields
	Text      string    `json:"text,omitempty"`
	CWD       string    `json:"cwd,omitempty"`
	InputType InputType `json:"input_type,omitempty"`

	// system_output / ai_response fields
	Content   string `json:"content,omitempty"`
	Streaming bool   `json:"streaming,omitempty"`

	// completed fields
	ExitCode   *int  `json:"exit_code,omitempty"`
	DurationMS int64 `json:"duration_ms,omitempty"`

	// input_request fields
	PromptText   string   `json:"prompt_text,omitempty"`
	QuickActions []string `json:"quick_actions,omitempty"`
	Process      string   `json:"process,omitempty"`

	// ai_action fields
	Tool        string `json:"tool,omitempty"`
	Action      string `json:"action,omitempty"`
	Description string `json:"description,omitempty"`
	Detail      string `json:"detail,omitempty"`
	Status      string `json:"status,omitempty"`
	Diff        string `json:"diff,omitempty"`

	// ai_usage fields
	TokensIn  int     `json:"tokens_in,omitempty"`
	TokensOut int     `json:"tokens_out,omitempty"`
	CostUSD   float64 `json:"cost_usd,omitempty"`

	// process_change fields
	ProcessName string `json:"process_name,omitempty"`
	ToolType    string `json:"tool_type,omitempty"`
	PID         int    `json:"pid,omitempty"`

	// status_change fields
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`

	// remote_input fields
	FromDevice string `json:"from_device,omitempty"`
}
