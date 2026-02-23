package parser

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// AiderParser detects Aider-specific patterns in terminal output
// and emits ai_response, ai_action, ai_approval, and ai_usage events.
// It supplements (never replaces) the base conversation parser events.
type AiderParser struct {
	callback    EventCallback
	state       aiderState
	responseBuf strings.Builder
}

type aiderState int

const (
	aiderIdle       aiderState = iota
	aiderResponding
)

// Aider output patterns
var (
	// User prompt: "> " at the start of a line (Aider's input prompt)
	patAiderPrompt = regexp.MustCompile(`^>\s*$`)

	// Edit block markers in search/replace format
	patAiderEditStart = regexp.MustCompile(`^<<<<<<< SEARCH$`)
	patAiderEditMid   = regexp.MustCompile(`^=======$`)
	patAiderEditEnd   = regexp.MustCompile(`^>>>>>>> REPLACE$`)

	// File being edited: "filename" in the header
	patAiderFileHeader = regexp.MustCompile(`^(?:Editing|Applied edit to|Add|Added)\s+(.+?)(?:\s|$)`)

	// Git operations
	patAiderGitCommit = regexp.MustCompile(`^(?:Commit|commit)\s+([0-9a-f]+)\s+(.+)`)
	patAiderGitDiff   = regexp.MustCompile(`^(?:Applied|Applying)\s+(?:edit|diff)`)

	// Cost display
	patAiderCost      = regexp.MustCompile(`(?i)(?:cost|tokens?).*\$\s*(\d+(?:\.\d+)?)`)
	patAiderTokens    = regexp.MustCompile(`(?i)(\d+(?:[.,]\d+)?)\s*[kK]?\s*tokens?`)
)

// NewAiderParser creates a parser for Aider terminal output.
func NewAiderParser(cb EventCallback) *AiderParser {
	return &AiderParser{
		callback: cb,
		state:    aiderIdle,
	}
}

// FeedLine processes a single line of stripped terminal output through Aider
// pattern matching.
func (ap *AiderParser) FeedLine(line Line) {
	text := line.Text
	if text == "" {
		return
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		if ap.state == aiderResponding {
			ap.responseBuf.WriteByte('\n')
		}
		return
	}

	// Check for edit blocks
	if patAiderEditStart.MatchString(trimmed) {
		ap.flushResponse()
		// Start of search/replace block - emit as action
		ap.callback(Event{
			Type:        EventAIAction,
			Timestamp:   time.Now(),
			Tool:        "aider",
			Action:      "edit_file",
			Description: "Search/Replace edit",
			Status:      "running",
		})
		return
	}

	if patAiderEditEnd.MatchString(trimmed) {
		ap.callback(Event{
			Type:        EventAIAction,
			Timestamp:   time.Now(),
			Tool:        "aider",
			Action:      "edit_file",
			Description: "Search/Replace edit",
			Status:      "done",
		})
		return
	}

	if patAiderEditMid.MatchString(trimmed) {
		return // Separator within edit block, skip
	}

	// Check for file header
	if m := patAiderFileHeader.FindStringSubmatch(trimmed); m != nil {
		ap.flushResponse()
		ap.callback(Event{
			Type:        EventAIAction,
			Timestamp:   time.Now(),
			Tool:        "aider",
			Action:      "edit_file",
			Description: "Edit " + strings.TrimSpace(m[1]),
			Status:      "done",
		})
		return
	}

	// Check for git operations
	if m := patAiderGitCommit.FindStringSubmatch(trimmed); m != nil {
		ap.flushResponse()
		ap.callback(Event{
			Type:        EventAIAction,
			Timestamp:   time.Now(),
			Tool:        "aider",
			Action:      "run_command",
			Description: "git commit " + m[1][:7] + ": " + m[2],
			Status:      "done",
		})
		return
	}

	// Check for cost/token display
	if ap.matchUsage(trimmed) {
		return
	}

	// Default: accumulate as response
	ap.state = aiderResponding
	ap.responseBuf.WriteString(text)
	ap.responseBuf.WriteByte('\n')
}

func (ap *AiderParser) matchUsage(trimmed string) bool {
	costMatch := patAiderCost.FindStringSubmatch(trimmed)
	tokMatch := patAiderTokens.FindStringSubmatch(trimmed)

	if costMatch == nil && tokMatch == nil {
		return false
	}

	var costUSD float64
	var tokensIn int

	if costMatch != nil {
		if v, err := strconv.ParseFloat(costMatch[1], 64); err == nil {
			costUSD = v
		}
	}
	if tokMatch != nil {
		tokensIn = parseTokenCount(tokMatch[1])
	}

	if costUSD > 0 || tokensIn > 0 {
		ap.flushResponse()
		ap.callback(Event{
			Type:      EventAIUsage,
			Timestamp: time.Now(),
			Tool:      "aider",
			TokensIn:  tokensIn,
			CostUSD:   costUSD,
		})
		return true
	}
	return false
}

// Flush emits any buffered response content.
func (ap *AiderParser) Flush() {
	ap.flushResponse()
}

// Reset clears all parser state.
func (ap *AiderParser) Reset() {
	ap.responseBuf.Reset()
	ap.state = aiderIdle
}

func (ap *AiderParser) flushResponse() {
	content := strings.TrimSpace(ap.responseBuf.String())
	if content == "" {
		return
	}

	ap.callback(Event{
		Type:      EventAIResponse,
		Timestamp: time.Now(),
		Content:   content,
		Tool:      "aider",
		Streaming: false,
	})

	ap.responseBuf.Reset()
}
