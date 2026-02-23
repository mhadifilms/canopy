package parser

import (
	"regexp"
	"strings"
	"strconv"
	"time"
)

// ConversationState represents the state of the conversation parser.
type ConversationState int

const (
	StateIdle      ConversationState = iota // Shell prompt visible, waiting for input
	StateRunning                            // A command is executing
	StateInputWait                          // Running process asking for interactive input
)

// inputWaitTimeout is how long output must be silent before we consider
// the process to be waiting for input.
const inputWaitTimeout = 2 * time.Second

// EventCallback is called when the conversation parser produces an event.
type EventCallback func(event Event)

// quickActionPatterns matches common interactive prompt patterns.
var quickActionPatterns = []*regexp.Regexp{
	// [y/N], [Y/n], [y/N/q], etc.
	regexp.MustCompile(`\[([a-zA-Z0-9](?:/[a-zA-Z0-9])*)\]\s*$`),
	// (yes/no), (y/n), etc.
	regexp.MustCompile(`\(([a-zA-Z0-9]+(?:/[a-zA-Z0-9]+)*)\)\s*$`),
}

// interactivePromptPatterns matches known interactive prompt endings.
var interactivePromptPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\[y/n\]\s*$`),
	regexp.MustCompile(`(?i)\[yes/no\]\s*$`),
	regexp.MustCompile(`(?i)\(y/n\)\s*$`),
	regexp.MustCompile(`(?i)\(yes/no\)\s*$`),
	regexp.MustCompile(`(?i)password:\s*$`),
	regexp.MustCompile(`(?i)passphrase:\s*$`),
	regexp.MustCompile(`(?i)press any key`),
	regexp.MustCompile(`(?i)press enter`),
	regexp.MustCompile(`(?i)continue\?`),
	regexp.MustCompile(`:\s*$`), // Generic colon prompt (less, man, etc.)
}

// ConversationParser implements the core conversation state machine.
// It receives lines from the accumulator and emits structured events.
type ConversationParser struct {
	state    ConversationState
	callback EventCallback

	// Current command tracking
	commandStartTime time.Time
	outputBuf        strings.Builder
	lastOutputTime   time.Time
	lastLineText     string

	// Input buffer: raw keystrokes from user, used to capture command text
	inputBuf strings.Builder

	// Timer for detecting input waits (output silence > 2s)
	silenceTimer *time.Timer

	// Current working directory (updated externally)
	cwd string

	// Current foreground process name (updated externally)
	processName string
	toolType    string
}

// NewConversationParser creates a new conversation parser.
func NewConversationParser(cb EventCallback) *ConversationParser {
	return &ConversationParser{
		state:    StateIdle,
		callback: cb,
	}
}

// SetCWD updates the current working directory.
func (c *ConversationParser) SetCWD(cwd string) {
	c.cwd = cwd
}

// SetProcess updates the current foreground process info.
func (c *ConversationParser) SetProcess(name, toolType string) {
	c.processName = name
	c.toolType = toolType
}

// State returns the current parser state.
func (c *ConversationParser) State() ConversationState {
	return c.state
}

// FeedLine processes a single line from the accumulator.
func (c *ConversationParser) FeedLine(line Line) {
	now := time.Now()

	// Process any OSC 133 markers first
	for _, m := range line.Markers {
		c.handleMarker(m, now)
	}

	// Process the line text if non-empty
	if line.Text != "" {
		c.handleText(line.Text, now)
	}
}

// FeedInput processes raw user input bytes.
func (c *ConversationParser) FeedInput(data []byte) {
	for _, b := range data {
		if b == '\n' || b == '\r' {
			// Newline in input — the user pressed Enter
			c.handleInputEnter()
		} else if b >= 0x20 { // Printable
			c.inputBuf.WriteByte(b)
		}
		// Non-printable control chars (except CR/LF) are ignored for input capture
	}
}

func (c *ConversationParser) handleMarker(m OSCMarker, now time.Time) {
	switch m.Code {
	case 'A': // Prompt start — shell is idle
		c.flushOutput(now, false)
		c.cancelSilenceTimer()
		c.state = StateIdle
		c.callback(Event{
			Type:      EventIdle,
			Timestamp: now,
			CWD:       c.cwd,
		})

	case 'C': // Command execution start
		// The user pressed Enter. Capture command from input buffer.
		cmdText := strings.TrimSpace(c.inputBuf.String())
		c.inputBuf.Reset()

		inputType := InputCommand
		if c.state == StateInputWait {
			inputType = InputResponse
		}
		if c.toolType != "" && c.toolType != "generic" {
			inputType = InputAIMessage
		}

		if cmdText != "" {
			c.callback(Event{
				Type:      EventUserInput,
				Timestamp: now,
				Text:      cmdText,
				CWD:       c.cwd,
				InputType: inputType,
			})
		}

		c.commandStartTime = now
		c.outputBuf.Reset()
		c.state = StateRunning
		c.startSilenceTimer()

	case 'D': // Command finished with exit code
		c.flushOutput(now, false)
		c.cancelSilenceTimer()

		exitCode := 0
		if m.Param != "" {
			if code, err := strconv.Atoi(m.Param); err == nil {
				exitCode = code
			}
		}

		durationMS := int64(0)
		if !c.commandStartTime.IsZero() {
			durationMS = now.Sub(c.commandStartTime).Milliseconds()
		}

		c.callback(Event{
			Type:       EventCompleted,
			Timestamp:  now,
			ExitCode:   &exitCode,
			DurationMS: durationMS,
		})

		c.commandStartTime = time.Time{}
		// State transitions to IDLE when 133;A follows
	}
}

func (c *ConversationParser) handleText(text string, now time.Time) {
	if c.state == StateRunning || c.state == StateInputWait {
		c.outputBuf.WriteString(text)
		c.outputBuf.WriteByte('\n')
		c.lastOutputTime = now
		c.lastLineText = text
		c.resetSilenceTimer()

		// Check for interactive prompt patterns immediately
		if c.state == StateRunning {
			if isInteractivePrompt(text) {
				c.emitInputRequest(text, now)
			}
		}
	}
}

func (c *ConversationParser) handleInputEnter() {
	now := time.Now()

	// If we're in a state where we don't have OSC 133 markers,
	// emit user_input based on the accumulated input buffer.
	if c.state == StateInputWait {
		cmdText := strings.TrimSpace(c.inputBuf.String())
		c.inputBuf.Reset()
		if cmdText != "" {
			inputType := InputResponse
			if c.toolType != "" && c.toolType != "generic" {
				inputType = InputAIMessage
			}
			c.callback(Event{
				Type:      EventUserInput,
				Timestamp: now,
				Text:      cmdText,
				CWD:       c.cwd,
				InputType: inputType,
			})
		}
		c.state = StateRunning
		c.startSilenceTimer()
	}
}

// flushOutput emits accumulated output as a system_output event.
func (c *ConversationParser) flushOutput(now time.Time, streaming bool) {
	content := strings.TrimRight(c.outputBuf.String(), "\n")
	if content == "" {
		return
	}

	c.callback(Event{
		Type:      EventSystemOutput,
		Timestamp: now,
		Content:   content,
		Streaming: streaming,
	})

	c.outputBuf.Reset()
}

// FlushPending flushes any accumulated output as a streaming event.
// Called periodically (e.g., every 200ms or 4KB) by the pipeline.
func (c *ConversationParser) FlushPending() {
	if c.outputBuf.Len() > 0 {
		c.flushOutput(time.Now(), true)
	}
}

func (c *ConversationParser) emitInputRequest(promptText string, now time.Time) {
	c.flushOutput(now, false)

	qa := parseQuickActions(promptText)

	c.callback(Event{
		Type:         EventInputRequest,
		Timestamp:    now,
		PromptText:   promptText,
		QuickActions: qa,
		Process:      c.processName,
	})

	c.state = StateInputWait
	c.cancelSilenceTimer()
}

func (c *ConversationParser) startSilenceTimer() {
	c.cancelSilenceTimer()
	c.silenceTimer = time.AfterFunc(inputWaitTimeout, func() {
		// Output has been silent for >2s while a command is running.
		// Check if the last line looks like a prompt.
		if c.state == StateRunning && c.lastLineText != "" {
			c.emitInputRequest(c.lastLineText, time.Now())
		}
	})
}

func (c *ConversationParser) resetSilenceTimer() {
	if c.silenceTimer != nil {
		c.silenceTimer.Stop()
		c.startSilenceTimer()
	}
}

func (c *ConversationParser) cancelSilenceTimer() {
	if c.silenceTimer != nil {
		c.silenceTimer.Stop()
		c.silenceTimer = nil
	}
}

// Close cleans up the conversation parser.
func (c *ConversationParser) Close() {
	c.cancelSilenceTimer()
	c.flushOutput(time.Now(), false)
}

// isInteractivePrompt checks if a line of text matches known interactive prompt patterns.
func isInteractivePrompt(text string) bool {
	trimmed := strings.TrimSpace(text)
	for _, pat := range interactivePromptPatterns {
		if pat.MatchString(trimmed) {
			return true
		}
	}
	return false
}

// parseQuickActions extracts actionable options from a prompt string.
// Examples:
//
//	"[y/N]" → ["y", "N"]
//	"(yes/no)" → ["yes", "no"]
//	"[1/2/3]" → ["1", "2", "3"]
//	"Continue? (Y/n)" → ["Y", "n"]
//	No match → nil
func parseQuickActions(text string) []string {
	for _, pat := range quickActionPatterns {
		matches := pat.FindStringSubmatch(text)
		if len(matches) >= 2 {
			options := strings.Split(matches[1], "/")
			if len(options) >= 2 {
				return options
			}
		}
	}
	return nil
}
