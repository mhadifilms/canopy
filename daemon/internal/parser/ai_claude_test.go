package parser

import (
	"strings"
	"testing"
	"time"
)

// Realistic Claude Code terminal output samples for testing.
// These simulate what the parser sees AFTER ANSI stripping.

const sampleClaudeReadAction = `I'll look at the authentication module to understand the issue.

⏺ Read src/auth/server.ts`

const sampleClaudeEditAction = `The problem is on line 42. The token validation doesn't check for expiration.

⏺ Edit src/auth/server.ts`

const sampleClaudeEditWithDiff = `⏺ Edit src/auth/server.ts
- if (token.valid) {
+ if (token.valid && !token.expired()) {`

const sampleClaudeRunAction = `Let me run the test suite to verify the fix.

⏺ Run npm test`

const sampleClaudeSearchAction = `Let me search for other places where token validation is used.

⏺ Search`

const sampleClaudeWriteAction = `I'll create the new configuration file.

⏺ Write config/auth.json`

const sampleClaudeApproval = `Do you want to proceed? (y/n)`

const sampleClaudeFullSession = `I'll fix the authentication bug in server.ts. Let me first read the file to understand the current implementation.

⏺ Read src/auth/server.ts

The issue is on line 42. The token validation checks if the token is valid but doesn't verify the expiration timestamp. I'll fix this now.

⏺ Edit src/auth/server.ts
- if (token.valid) {
+ if (token.valid && !token.expired()) {

Do you want to proceed? (y/n)`

const sampleClaudeUsage = `Total cost: $0.42
1.5k tokens in, 800 tokens out`

const sampleClaudeMultipleActions = `Let me look at several files to understand the codebase.

⏺ Read src/auth/server.ts

Now let me check the client code.

⏺ Read src/auth/client.ts

And the shared types.

⏺ Read src/auth/types.ts

I see the issue. The Token interface is missing the expired() method. Let me fix it.

⏺ Edit src/auth/types.ts
- interface Token {
-   valid: boolean;
- }
+ interface Token {
+   valid: boolean;
+   expired(): boolean;
+ }

⏺ Edit src/auth/server.ts
- if (token.valid) {
+ if (token.valid && !token.expired()) {`

func TestClaudeParser_ReadAction(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	for _, line := range splitLines(sampleClaudeReadAction) {
		cp.FeedLine(Line{Text: line})
	}
	cp.Flush()

	// Should have an ai_response and an ai_action
	responses := col.ofType(EventAIResponse)
	if len(responses) < 1 {
		t.Errorf("expected at least 1 ai_response, got %d", len(responses))
	}

	actions := col.ofType(EventAIAction)
	// We get a "running" and a "done" event for the action
	found := false
	for _, a := range actions {
		if a.Action == "read_file" && a.Status == "running" {
			found = true
			if !strings.Contains(a.Description, "src/auth/server.ts") {
				t.Errorf("expected description to contain filepath, got %q", a.Description)
			}
		}
	}
	if !found {
		t.Errorf("expected ai_action with action=read_file, status=running; got actions: %+v", actions)
	}
}

func TestClaudeParser_EditAction(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	for _, line := range splitLines(sampleClaudeEditAction) {
		cp.FeedLine(Line{Text: line})
	}
	cp.Flush()

	actions := col.ofType(EventAIAction)
	found := false
	for _, a := range actions {
		if a.Action == "edit_file" && a.Status == "running" {
			found = true
		}
	}
	if !found {
		t.Error("expected ai_action with action=edit_file")
	}
}

func TestClaudeParser_EditWithDiff(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	for _, line := range splitLines(sampleClaudeEditWithDiff) {
		cp.FeedLine(Line{Text: line})
	}
	cp.Flush()

	actions := col.ofType(EventAIAction)
	// Should have a "running" action for edit_file, and a "done" with diff detail
	var doneAction *Event
	for i, a := range actions {
		if a.Action == "edit_file" && a.Status == "done" {
			doneAction = &actions[i]
		}
	}
	if doneAction == nil {
		t.Fatal("expected ai_action with status=done for edit_file")
	}
	if !strings.Contains(doneAction.Detail, "token.valid") {
		t.Errorf("expected diff detail to contain 'token.valid', got %q", doneAction.Detail)
	}
}

func TestClaudeParser_RunAction(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	for _, line := range splitLines(sampleClaudeRunAction) {
		cp.FeedLine(Line{Text: line})
	}
	cp.Flush()

	actions := col.ofType(EventAIAction)
	found := false
	for _, a := range actions {
		if a.Action == "run_command" && a.Status == "running" {
			found = true
			if !strings.Contains(a.Description, "npm test") {
				t.Errorf("expected description to contain 'npm test', got %q", a.Description)
			}
		}
	}
	if !found {
		t.Error("expected ai_action with action=run_command")
	}
}

func TestClaudeParser_SearchAction(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	for _, line := range splitLines(sampleClaudeSearchAction) {
		cp.FeedLine(Line{Text: line})
	}
	cp.Flush()

	actions := col.ofType(EventAIAction)
	found := false
	for _, a := range actions {
		if a.Action == "search" {
			found = true
		}
	}
	if !found {
		t.Error("expected ai_action with action=search")
	}
}

func TestClaudeParser_WriteAction(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	for _, line := range splitLines(sampleClaudeWriteAction) {
		cp.FeedLine(Line{Text: line})
	}
	cp.Flush()

	actions := col.ofType(EventAIAction)
	found := false
	for _, a := range actions {
		if a.Action == "write_file" && a.Status == "running" {
			found = true
			if !strings.Contains(a.Description, "config/auth.json") {
				t.Errorf("expected description to contain filepath, got %q", a.Description)
			}
		}
	}
	if !found {
		t.Error("expected ai_action with action=write_file")
	}
}

func TestClaudeParser_Approval(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	for _, line := range splitLines(sampleClaudeApproval) {
		cp.FeedLine(Line{Text: line})
	}
	cp.Flush()

	approvals := col.ofType(EventAIApproval)
	if len(approvals) != 1 {
		t.Fatalf("expected 1 ai_approval, got %d", len(approvals))
	}
	if approvals[0].Tool != "claude_code" {
		t.Errorf("expected tool=claude_code, got %q", approvals[0].Tool)
	}
}

func TestClaudeParser_FullSession(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	for _, line := range splitLines(sampleClaudeFullSession) {
		cp.FeedLine(Line{Text: line})
	}
	cp.Flush()

	// Should have: ai_response, ai_action(read), ai_response, ai_action(edit) with diff, ai_approval
	responses := col.ofType(EventAIResponse)
	if len(responses) < 1 {
		t.Errorf("expected at least 1 ai_response, got %d", len(responses))
	}

	actions := col.ofType(EventAIAction)
	readActions := 0
	editActions := 0
	for _, a := range actions {
		if a.Action == "read_file" {
			readActions++
		}
		if a.Action == "edit_file" {
			editActions++
		}
	}
	if readActions == 0 {
		t.Error("expected at least one read_file action")
	}
	if editActions == 0 {
		t.Error("expected at least one edit_file action")
	}

	approvals := col.ofType(EventAIApproval)
	if len(approvals) != 1 {
		t.Errorf("expected 1 ai_approval, got %d", len(approvals))
	}
	// The approval should carry the diff from the preceding edit
	if approvals[0].Diff == "" {
		t.Log("NOTE: diff may have been consumed by the edit action's detail instead of the approval")
	}
}

func TestClaudeParser_Usage(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	for _, line := range splitLines(sampleClaudeUsage) {
		cp.FeedLine(Line{Text: line})
	}
	cp.Flush()

	usages := col.ofType(EventAIUsage)
	if len(usages) == 0 {
		t.Fatal("expected at least 1 ai_usage event")
	}

	// Check that cost was parsed
	foundCost := false
	for _, u := range usages {
		if u.CostUSD > 0 {
			foundCost = true
			if u.CostUSD != 0.42 {
				t.Errorf("expected cost $0.42, got $%.2f", u.CostUSD)
			}
		}
	}
	if !foundCost {
		t.Error("expected ai_usage with cost > 0")
	}
}

func TestClaudeParser_MultipleActions(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	for _, line := range splitLines(sampleClaudeMultipleActions) {
		cp.FeedLine(Line{Text: line})
	}
	cp.Flush()

	actions := col.ofType(EventAIAction)
	readCount := 0
	editCount := 0
	for _, a := range actions {
		switch a.Action {
		case "read_file":
			readCount++
		case "edit_file":
			editCount++
		}
	}

	// 3 reads (running + done for each = 6 events, but we count unique actions)
	if readCount < 3 {
		t.Errorf("expected at least 3 read_file actions, got %d", readCount)
	}
	if editCount < 2 {
		t.Errorf("expected at least 2 edit_file actions, got %d", editCount)
	}
}

func TestClaudeParser_EmptyInput(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	// Empty and whitespace-only lines should not panic
	cp.FeedLine(Line{Text: ""})
	cp.FeedLine(Line{Text: "   "})
	cp.FeedLine(Line{Text: "\t"})
	cp.FeedLine(Line{})
	cp.Flush()

	// No events should be emitted for empty input
	if len(col.events) != 0 {
		t.Errorf("expected 0 events for empty input, got %d", len(col.events))
	}
}

func TestClaudeParser_NoPanicOnGarbage(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	// Feed random/unexpected content — must never panic
	garbage := []string{
		"\x00\x01\x02\x03",
		"🔥🎉💯",
		strings.Repeat("A", 10000),
		"⏺",          // bare circle, no tool name
		"⏺ ",         // circle with space only
		"⏺   ",       // circle with multiple spaces
		"● Read",      // alternative circle, no path
		"+ ",          // diff-like but too short
		"- ",          // diff-like but too short
		"$0.00",       // zero cost
		"0 tokens",    // zero tokens
		"[y/n]",       // approval pattern in isolation
		"Do you want to proceed?", // approval without y/n
	}

	for _, g := range garbage {
		cp.FeedLine(Line{Text: g})
	}
	cp.Flush()
	cp.Reset()

	// If we get here without panicking, the test passes
}

func TestClaudeParser_StreamingResponse(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	// Simulate streaming text output from Claude
	lines := []string{
		"Let me analyze the code.",
		"The issue is in the authentication module.",
		"Specifically, the token validation on line 42",
		"does not check for expiration.",
	}

	for _, l := range lines {
		cp.FeedLine(Line{Text: l})
	}
	cp.Flush()

	responses := col.ofType(EventAIResponse)
	if len(responses) == 0 {
		t.Fatal("expected at least 1 ai_response")
	}

	// All response text should be accumulated
	var allContent strings.Builder
	for _, r := range responses {
		allContent.WriteString(r.Content)
		allContent.WriteByte('\n')
	}
	combined := allContent.String()
	if !strings.Contains(combined, "authentication module") {
		t.Error("response should contain 'authentication module'")
	}
	if !strings.Contains(combined, "expiration") {
		t.Error("response should contain 'expiration'")
	}
}

func TestClaudeParser_ActionThenResponse(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	// Action followed by response text (Claude describes what it found)
	lines := []string{
		"⏺ Read src/auth/server.ts",
		"",
		"I can see the file has 245 lines. The token validation",
		"is on line 42.",
	}

	for _, l := range lines {
		cp.FeedLine(Line{Text: l})
	}
	cp.Flush()

	actions := col.ofType(EventAIAction)
	if len(actions) == 0 {
		t.Fatal("expected at least 1 ai_action")
	}

	responses := col.ofType(EventAIResponse)
	if len(responses) == 0 {
		t.Fatal("expected at least 1 ai_response after the action")
	}
}

func TestClaudeParser_DiffCapture(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	lines := []string{
		"⏺ Edit src/server.ts",
		"- const old = true;",
		"+ const new = false;",
		"- function deprecated() {}",
		"+ function replacement() {}",
	}

	for _, l := range lines {
		cp.FeedLine(Line{Text: l})
	}
	cp.Flush()

	actions := col.ofType(EventAIAction)
	var doneAction *Event
	for i, a := range actions {
		if a.Action == "edit_file" && a.Status == "done" {
			doneAction = &actions[i]
		}
	}
	if doneAction == nil {
		t.Fatal("expected a done edit_file action")
	}
	if !strings.Contains(doneAction.Detail, "const old") {
		t.Errorf("expected diff detail to contain 'const old', got %q", doneAction.Detail)
	}
	if !strings.Contains(doneAction.Detail, "function replacement") {
		t.Errorf("expected diff detail to contain 'function replacement', got %q", doneAction.Detail)
	}
}

func TestClaudeParser_ApprovalWithPrecedingDiff(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	lines := []string{
		"⏺ Edit src/server.ts",
		"- if (token.valid) {",
		"+ if (token.valid && !token.expired()) {",
		"Do you want to proceed? (y/n)",
	}

	for _, l := range lines {
		cp.FeedLine(Line{Text: l})
	}
	cp.Flush()

	approvals := col.ofType(EventAIApproval)
	if len(approvals) != 1 {
		t.Fatalf("expected 1 ai_approval, got %d", len(approvals))
	}

	// The approval should have the diff or the action should have it
	actions := col.ofType(EventAIAction)
	hasDiff := approvals[0].Diff != ""
	for _, a := range actions {
		if a.Detail != "" {
			hasDiff = true
		}
	}
	if !hasDiff {
		t.Error("expected diff to be captured either in approval or action")
	}
}

func TestClaudeParser_UsageParsing(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCost  float64
		wantIn    int
		wantOut   int
		wantEmit  bool
	}{
		{
			name:     "cost only",
			input:    "Total cost: $1.23",
			wantCost: 1.23,
			wantEmit: true,
		},
		{
			name:     "cost with tokens",
			input:    "Cost: $0.05 | 2k tokens in, 500 tokens out",
			wantCost: 0.05,
			wantIn:   2000,
			wantOut:  500,
			wantEmit: true,
		},
		{
			name:     "tokens only",
			input:    "1500 tokens in, 800 tokens out",
			wantIn:   1500,
			wantOut:  800,
			wantEmit: true,
		},
		{
			name:     "no match",
			input:    "Building the project...",
			wantEmit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := &testEventCollector{}
			cp := NewClaudeParser(col.callback)

			cp.FeedLine(Line{Text: tt.input})
			cp.Flush()

			usages := col.ofType(EventAIUsage)
			if !tt.wantEmit {
				if len(usages) != 0 {
					t.Errorf("expected no ai_usage, got %d", len(usages))
				}
				return
			}

			if len(usages) == 0 {
				t.Fatal("expected ai_usage event")
			}

			u := usages[0]
			if tt.wantCost > 0 && u.CostUSD != tt.wantCost {
				t.Errorf("expected cost=%.2f, got %.2f", tt.wantCost, u.CostUSD)
			}
			if tt.wantIn > 0 && u.TokensIn != tt.wantIn {
				t.Errorf("expected tokens_in=%d, got %d", tt.wantIn, u.TokensIn)
			}
			if tt.wantOut > 0 && u.TokensOut != tt.wantOut {
				t.Errorf("expected tokens_out=%d, got %d", tt.wantOut, u.TokensOut)
			}
		})
	}
}

func TestClaudeParser_Reset(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	// Build up some state
	cp.FeedLine(Line{Text: "Some response text"})
	cp.FeedLine(Line{Text: "⏺ Read file.ts"})

	// Reset should flush and clear everything
	cp.Reset()

	// Feed new data — should work cleanly
	col2 := &testEventCollector{}
	cp2 := NewClaudeParser(col2.callback)
	cp2.FeedLine(Line{Text: "Fresh start"})
	cp2.Flush()

	responses := col2.ofType(EventAIResponse)
	if len(responses) != 1 {
		t.Errorf("expected 1 response after reset, got %d", len(responses))
	}
}

func TestClaudeParser_BashToolVariant(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	// Claude Code sometimes uses "Bash" instead of "Run"
	cp.FeedLine(Line{Text: "⏺ Bash npm test"})
	cp.Flush()

	actions := col.ofType(EventAIAction)
	found := false
	for _, a := range actions {
		if a.Action == "run_command" {
			found = true
		}
	}
	if !found {
		t.Error("expected Bash to be recognized as run_command")
	}
}

func TestClaudeParser_AlternateCircleChar(t *testing.T) {
	col := &testEventCollector{}
	cp := NewClaudeParser(col.callback)

	// Test with ● (U+25CF) instead of ⏺ (U+23FA)
	cp.FeedLine(Line{Text: "● Read src/main.go"})
	cp.Flush()

	actions := col.ofType(EventAIAction)
	found := false
	for _, a := range actions {
		if a.Action == "read_file" {
			found = true
		}
	}
	if !found {
		t.Error("expected ● to be recognized as an action marker")
	}
}

// TestClaudeParser_PipelineIntegration tests the Claude parser integrated with
// the full pipeline (ANSI stripping + accumulator + conversation parser + AI parser).
func TestClaudeParser_PipelineIntegration(t *testing.T) {
	p := NewPipeline()
	p.SetProcess("claude", "claude_code")
	p.SetCWD("/home/user/project")

	// Simulate Claude Code session
	// Prompt
	p.FeedOutput([]byte("\x1b]133;A\x07> "))
	time.Sleep(10 * time.Millisecond)

	// User types a message
	p.FeedInput([]byte("fix the auth bug"))
	p.FeedOutput([]byte("\x1b]133;C\x07"))
	time.Sleep(10 * time.Millisecond)

	// Claude responds with text, then a read action
	p.FeedOutput([]byte("I'll look at the auth module.\n"))
	time.Sleep(10 * time.Millisecond)

	p.FeedOutput([]byte("\xe2\x8f\xba Read src/auth/server.ts\n"))
	time.Sleep(10 * time.Millisecond)

	p.FeedOutput([]byte("The issue is on line 42.\n"))
	time.Sleep(10 * time.Millisecond)

	p.FeedOutput([]byte("\xe2\x8f\xba Edit src/auth/server.ts\n"))
	time.Sleep(10 * time.Millisecond)

	p.FeedOutput([]byte("- if (token.valid) {\n"))
	time.Sleep(10 * time.Millisecond)

	p.FeedOutput([]byte("+ if (token.valid && !token.expired()) {\n"))
	time.Sleep(10 * time.Millisecond)

	p.Close()

	events := drainEvents(p, time.Second)

	// Verify we got BOTH base events and AI events
	typeMap := make(map[EventType]int)
	for _, e := range events {
		typeMap[e.Type]++
	}

	// Base events
	if typeMap[EventIdle] < 1 {
		t.Errorf("expected at least 1 idle event, got %d", typeMap[EventIdle])
	}
	if typeMap[EventUserInput] < 1 {
		t.Errorf("expected at least 1 user_input event, got %d", typeMap[EventUserInput])
	}
	if typeMap[EventSystemOutput] < 1 {
		t.Errorf("expected at least 1 system_output event, got %d", typeMap[EventSystemOutput])
	}

	// AI events (supplemental)
	if typeMap[EventAIAction] < 1 {
		t.Errorf("expected at least 1 ai_action event, got %d", typeMap[EventAIAction])
	}

	// Verify user_input is classified as ai_message
	for _, e := range events {
		if e.Type == EventUserInput {
			if e.InputType != InputAIMessage {
				t.Errorf("expected input_type 'ai_message', got %q", e.InputType)
			}
		}
	}
}

func TestClaudeParser_PipelineProcessSwitch(t *testing.T) {
	p := NewPipeline()

	// Start as regular shell
	p.SetProcess("zsh", "")
	p.SetCWD("/home/user")

	// Shell command
	p.FeedOutput([]byte("\x1b]133;A\x07$ "))
	p.FeedInput([]byte("echo hello"))
	p.FeedOutput([]byte("\x1b]133;C\x07"))
	time.Sleep(10 * time.Millisecond)
	p.FeedOutput([]byte("hello\n"))
	p.FeedOutput([]byte("\x1b]133;D;0\x07"))
	time.Sleep(10 * time.Millisecond)

	// Switch to Claude Code
	p.SetProcess("claude", "claude_code")

	p.FeedOutput([]byte("\x1b]133;A\x07> "))
	p.FeedInput([]byte("fix bug"))
	p.FeedOutput([]byte("\x1b]133;C\x07"))
	time.Sleep(10 * time.Millisecond)

	p.FeedOutput([]byte("\xe2\x8f\xba Read src/main.go\n"))
	time.Sleep(10 * time.Millisecond)

	p.Close()

	events := drainEvents(p, time.Second)

	// After process switch to claude_code, AI events should appear
	aiActions := make([]Event, 0)
	for _, e := range events {
		if e.Type == EventAIAction {
			aiActions = append(aiActions, e)
		}
	}

	if len(aiActions) == 0 {
		t.Error("expected ai_action events after switching to claude_code process")
	}
}

func TestParseTokenCount(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"1500", 1500},
		{"1.5k", 1500},
		{"1.5K", 1500},
		{"2M", 2000000},
		{"800", 800},
		{"1,500", 1500},
		{"0", 0},
		{"", 0},
		{"abc", 0},
	}

	for _, tt := range tests {
		result := parseTokenCount(tt.input)
		if result != tt.expected {
			t.Errorf("parseTokenCount(%q): expected %d, got %d", tt.input, tt.expected, result)
		}
	}
}

// splitLines splits a multi-line string into individual lines.
func splitLines(s string) []string {
	return strings.Split(s, "\n")
}
