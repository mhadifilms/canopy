package parser

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ClaudeParser detects Claude Code-specific patterns in terminal output
// and emits ai_response, ai_action, ai_approval, and ai_usage events.
// It supplements (never replaces) the base conversation parser events.
type ClaudeParser struct {
	callback EventCallback

	// State tracking
	state          claudeState
	responseBuf    strings.Builder // accumulates ai_response text
	currentAction  *pendingAction  // in-progress tool action
	diffBuf        strings.Builder // accumulates diff lines
	inDiff         bool            // currently collecting diff lines
	lastEmitTime   time.Time
	pendingApproval bool // an action is pending user approval
}

type claudeState int

const (
	claudeIdle       claudeState = iota // waiting for Claude output
	claudeResponding                    // Claude is generating text
	claudeAction                        // Claude is performing a tool action
	claudeApproval                      // Claude is waiting for approval
)

type pendingAction struct {
	action      string // read_file, edit_file, run_command, search, write_file
	description string
	startTime   time.Time
}

// Patterns for Claude Code output detection.
// The filled circle (⏺) prefix is the primary indicator for Claude Code tool actions.
var (
	// Tool action patterns: "⏺ Read filepath", "⏺ Edit filepath", etc.
	// Claude Code uses the filled circle Unicode character U+23FA or the emoji variant.
	// In terminal output after ANSI stripping, it appears as the literal character.
	patActionRead   = regexp.MustCompile(`^[[:space:]]*(?:⏺|●)[[:space:]]+Read[[:space:]]+(.+)$`)
	patActionEdit   = regexp.MustCompile(`^[[:space:]]*(?:⏺|●)[[:space:]]+Edit[[:space:]]+(.+)$`)
	patActionWrite  = regexp.MustCompile(`^[[:space:]]*(?:⏺|●)[[:space:]]+Write[[:space:]]+(.+)$`)
	patActionRun    = regexp.MustCompile(`^[[:space:]]*(?:⏺|●)[[:space:]]+(?:Run|Bash|Execute)[[:space:]]+(.+)$`)
	patActionSearch = regexp.MustCompile(`^[[:space:]]*(?:⏺|●)[[:space:]]+(?:Search|Grep|Glob|TodoRead|WebFetch|WebSearch|Grep|Task|ToolSearch)`)

	// Additional tool patterns that don't have arguments on the same line
	patActionGeneric = regexp.MustCompile(`^[[:space:]]*(?:⏺|●)[[:space:]]+(\w+)`)

	// Approval patterns
	patApprovalProceed = regexp.MustCompile(`(?i)do you want to (?:proceed|continue|allow|approve)`)
	patApprovalYN      = regexp.MustCompile(`\([yY]/[nN]\)|\[[yY]/[nN]\]`)
	patApprovalAllow   = regexp.MustCompile(`(?i)(?:allow|approve|reject|deny)\s+(?:this|the)?\s*(?:edit|change|write|run|command|action)`)

	// Usage/cost patterns
	patTokens = regexp.MustCompile(`(\d+(?:[.,]\d+)?)\s*[kKmM]?\s*tokens?`)
	patCost   = regexp.MustCompile(`\$\s*(\d+(?:\.\d+)?)`)

	// Diff line patterns (unified diff format)
	patDiffAdd    = regexp.MustCompile(`^\+[^+]`)
	patDiffRemove = regexp.MustCompile(`^-[^-]`)
	patDiffHeader = regexp.MustCompile(`^(?:@@|---|\+\+\+)`)

	// Claude Code prompt return pattern - indicates Claude finished and returned to prompt
	patClaudePrompt = regexp.MustCompile(`^[[:space:]]*(?:>|❯|\$)[[:space:]]*$`)

	// Cost summary line: "Total cost: $1.23" or "Cost: $0.45 | Tokens: 1.5k in, 800 out"
	patCostSummary = regexp.MustCompile(`(?i)(?:total\s+)?cost:?\s*\$\s*(\d+(?:\.\d+)?)`)
	patTokenIn     = regexp.MustCompile(`(\d+(?:[.,]\d+)?\s*[kKmM]?)\s*(?:tokens?\s+)?(?:input|in)\b`)
	patTokenOut    = regexp.MustCompile(`(\d+(?:[.,]\d+)?\s*[kKmM]?)\s*(?:tokens?\s+)?(?:output|out)\b`)
)

// NewClaudeParser creates a parser for Claude Code terminal output.
func NewClaudeParser(cb EventCallback) *ClaudeParser {
	return &ClaudeParser{
		callback: cb,
		state:    claudeIdle,
	}
}

// FeedLine processes a single line of stripped terminal output through Claude Code
// pattern matching. It emits ai_* events as supplements to the base parser events.
func (cp *ClaudeParser) FeedLine(line Line) {
	// Only process text lines (markers are handled by the base conversation parser)
	text := line.Text
	if text == "" {
		return
	}

	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		// Blank lines in a response are part of the response
		if cp.state == claudeResponding {
			cp.responseBuf.WriteByte('\n')
		}
		return
	}

	// Check patterns in priority order

	// 1. Tool actions (⏺ prefix)
	if cp.matchAction(trimmed) {
		return
	}

	// 2. Diff lines (+ or - prefix in context of an action)
	if cp.matchDiff(trimmed) {
		return
	}

	// 3. Approval prompts
	if cp.matchApproval(trimmed) {
		return
	}

	// 4. Usage/cost display
	if cp.matchUsage(trimmed) {
		return
	}

	// 5. Default: accumulate as AI response text
	cp.accumulateResponse(text)
}

// Flush emits any buffered response content.
func (cp *ClaudeParser) Flush() {
	cp.flushResponse(false)
	cp.flushDiff()
	cp.finishAction("done")
}

// Reset clears all parser state.
func (cp *ClaudeParser) Reset() {
	cp.flushResponse(false)
	cp.flushDiff()
	cp.finishAction("done")
	cp.state = claudeIdle
	cp.pendingApproval = false
}

// matchAction checks if the line is a Claude Code tool action (⏺ prefix).
func (cp *ClaudeParser) matchAction(trimmed string) bool {
	now := time.Now()

	if m := patActionRead.FindStringSubmatch(trimmed); m != nil {
		cp.startAction("read_file", "Read "+strings.TrimSpace(m[1]), now)
		return true
	}
	if m := patActionEdit.FindStringSubmatch(trimmed); m != nil {
		cp.startAction("edit_file", "Edit "+strings.TrimSpace(m[1]), now)
		return true
	}
	if m := patActionWrite.FindStringSubmatch(trimmed); m != nil {
		cp.startAction("write_file", "Write "+strings.TrimSpace(m[1]), now)
		return true
	}
	if m := patActionRun.FindStringSubmatch(trimmed); m != nil {
		cp.startAction("run_command", "Run "+strings.TrimSpace(m[1]), now)
		return true
	}
	if patActionSearch.MatchString(trimmed) {
		// Extract the tool name for the description
		m := patActionGeneric.FindStringSubmatch(trimmed)
		desc := "Search"
		if m != nil {
			desc = m[1]
		}
		cp.startAction("search", desc, now)
		return true
	}

	// Check for generic ⏺ actions that don't match specific patterns above
	if m := patActionGeneric.FindStringSubmatch(trimmed); m != nil {
		toolName := m[1]
		// Map known tool names to actions
		action := mapToolNameToAction(toolName)
		if action != "" {
			cp.startAction(action, toolName+" "+strings.TrimSpace(strings.TrimPrefix(trimmed, m[0])), now)
			return true
		}
	}

	return false
}

// mapToolNameToAction maps Claude Code tool names to action types.
func mapToolNameToAction(name string) string {
	switch strings.ToLower(name) {
	case "read":
		return "read_file"
	case "edit":
		return "edit_file"
	case "write":
		return "write_file"
	case "run", "bash", "execute":
		return "run_command"
	case "search", "grep", "glob", "todoread", "webfetch", "websearch", "toolsearch", "task":
		return "search"
	default:
		return ""
	}
}

// startAction handles the beginning of a new tool action.
func (cp *ClaudeParser) startAction(action, description string, now time.Time) {
	// Flush any pending response
	cp.flushResponse(false)
	// Finish any prior action
	cp.flushDiff()
	cp.finishAction("done")

	cp.currentAction = &pendingAction{
		action:      action,
		description: description,
		startTime:   now,
	}
	cp.state = claudeAction
	cp.inDiff = false

	cp.callback(Event{
		Type:        EventAIAction,
		Timestamp:   now,
		Tool:        "claude_code",
		Action:      action,
		Description: description,
		Status:      "running",
	})
}

// finishAction completes the current action with the given status.
func (cp *ClaudeParser) finishAction(status string) {
	if cp.currentAction == nil {
		return
	}

	now := time.Now()
	detail := ""
	if cp.diffBuf.Len() > 0 {
		detail = strings.TrimSpace(cp.diffBuf.String())
		cp.diffBuf.Reset()
	}

	cp.callback(Event{
		Type:        EventAIAction,
		Timestamp:   now,
		Tool:        "claude_code",
		Action:      cp.currentAction.action,
		Description: cp.currentAction.description,
		Detail:      detail,
		Status:      status,
	})

	cp.currentAction = nil
	cp.inDiff = false
}

// matchDiff checks if the line is a diff line and accumulates it.
func (cp *ClaudeParser) matchDiff(trimmed string) bool {
	if cp.currentAction == nil && !cp.pendingApproval {
		return false
	}

	isDiff := patDiffAdd.MatchString(trimmed) ||
		patDiffRemove.MatchString(trimmed) ||
		patDiffHeader.MatchString(trimmed)

	if isDiff {
		if !cp.inDiff {
			cp.inDiff = true
		}
		cp.diffBuf.WriteString(trimmed)
		cp.diffBuf.WriteByte('\n')
		return true
	}

	// If we were in a diff and hit a non-diff line, the diff is done
	if cp.inDiff {
		cp.inDiff = false
		// Don't consume this line - let it be processed as something else
		return false
	}

	return false
}

// matchApproval checks if the line is an approval prompt.
func (cp *ClaudeParser) matchApproval(trimmed string) bool {
	isApproval := patApprovalProceed.MatchString(trimmed) ||
		patApprovalYN.MatchString(trimmed) ||
		patApprovalAllow.MatchString(trimmed)

	if !isApproval {
		return false
	}

	now := time.Now()

	// Flush any pending response
	cp.flushResponse(false)

	// Collect any accumulated diff for the approval event
	diff := ""
	if cp.diffBuf.Len() > 0 {
		diff = strings.TrimSpace(cp.diffBuf.String())
		cp.diffBuf.Reset()
	}

	action := ""
	description := trimmed
	if cp.currentAction != nil {
		action = cp.currentAction.action
		description = cp.currentAction.description
		cp.currentAction = nil
	}

	cp.state = claudeApproval
	cp.pendingApproval = true

	cp.callback(Event{
		Type:        EventAIApproval,
		Timestamp:   now,
		Tool:        "claude_code",
		Description: description,
		Action:      action,
		Diff:        diff,
	})

	return true
}

// matchUsage checks if the line contains token/cost information.
func (cp *ClaudeParser) matchUsage(trimmed string) bool {
	// Look for cost summary lines
	costMatch := patCostSummary.FindStringSubmatch(trimmed)
	if costMatch == nil && !patTokens.MatchString(trimmed) {
		return false
	}

	now := time.Now()
	var costUSD float64
	var tokensIn, tokensOut int

	// Parse cost
	if costMatch != nil {
		if v, err := strconv.ParseFloat(costMatch[1], 64); err == nil {
			costUSD = v
		}
	} else if m := patCost.FindStringSubmatch(trimmed); m != nil {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			costUSD = v
		}
	}

	// Parse token counts
	if m := patTokenIn.FindStringSubmatch(trimmed); m != nil {
		tokensIn = parseTokenCount(m[1])
	}
	if m := patTokenOut.FindStringSubmatch(trimmed); m != nil {
		tokensOut = parseTokenCount(m[1])
	}

	// If we got only a generic token match without in/out distinction
	if tokensIn == 0 && tokensOut == 0 {
		if m := patTokens.FindStringSubmatch(trimmed); m != nil {
			tokensIn = parseTokenCount(m[1])
		}
	}

	// Only emit if we actually parsed something useful
	if costUSD > 0 || tokensIn > 0 || tokensOut > 0 {
		cp.callback(Event{
			Type:      EventAIUsage,
			Timestamp: now,
			Tool:      "claude_code",
			TokensIn:  tokensIn,
			TokensOut: tokensOut,
			CostUSD:   costUSD,
		})
		return true
	}

	return false
}

// accumulateResponse adds text to the response buffer.
func (cp *ClaudeParser) accumulateResponse(text string) {
	// If we were in action state and get non-action text, the action might be done
	if cp.state == claudeAction && cp.currentAction != nil {
		// Non-diff, non-action text after an action means the action detail
		// or the action is complete and Claude is responding again
		if cp.inDiff {
			cp.inDiff = false
		}
		cp.flushDiff()
		cp.finishAction("done")
	}

	if cp.pendingApproval {
		// Text after an approval might be the user's response or Claude continuing
		cp.pendingApproval = false
	}

	if cp.state != claudeResponding {
		cp.state = claudeResponding
		// Emit streaming ai_response
	}

	cp.responseBuf.WriteString(text)
	cp.responseBuf.WriteByte('\n')
}

// flushResponse emits the accumulated response text as an ai_response event.
func (cp *ClaudeParser) flushResponse(streaming bool) {
	content := strings.TrimSpace(cp.responseBuf.String())
	if content == "" {
		return
	}

	now := time.Now()
	cp.callback(Event{
		Type:      EventAIResponse,
		Timestamp: now,
		Content:   content,
		Tool:      "claude_code",
		Streaming: streaming,
	})

	cp.responseBuf.Reset()
	cp.lastEmitTime = now
}

// flushDiff pushes any accumulated diff into the current action's detail.
// This is called before finishAction to ensure diff content is captured.
func (cp *ClaudeParser) flushDiff() {
	cp.inDiff = false
	// diffBuf content will be consumed by finishAction or matchApproval
}

// parseTokenCount parses a token count string like "1.5k", "800", "1.2M", "2k".
// The input may contain whitespace between the number and suffix.
func parseTokenCount(s string) int {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, ",", "")

	if s == "" {
		return 0
	}

	multiplier := 1.0
	cleaned := s
	last := s[len(s)-1]
	if last == 'k' || last == 'K' {
		multiplier = 1000
		cleaned = strings.TrimSpace(s[:len(s)-1])
	} else if last == 'm' || last == 'M' {
		multiplier = 1000000
		cleaned = strings.TrimSpace(s[:len(s)-1])
	}

	if v, err := strconv.ParseFloat(cleaned, 64); err == nil {
		return int(v * multiplier)
	}
	return 0
}
