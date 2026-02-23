package parser

import (
	"strings"
	"testing"
	"time"
)

const sampleAiderEditBlock = `I'll fix the authentication issue.

<<<<<<< SEARCH
if (token.valid) {
=======
if (token.valid && !token.expired()) {
>>>>>>> REPLACE`

const sampleAiderFileEdit = `Applied edit to src/auth/server.ts`

const sampleAiderGitCommit = `Commit abc1234 Fix token expiration check in auth module`

const sampleAiderCost = `Tokens: 1.5k tokens, Cost: $0.05`

func TestAiderParser_EditBlock(t *testing.T) {
	col := &testEventCollector{}
	ap := NewAiderParser(col.callback)

	for _, line := range splitLines(sampleAiderEditBlock) {
		ap.FeedLine(Line{Text: line})
	}
	ap.Flush()

	// Should have response text and edit actions
	responses := col.ofType(EventAIResponse)
	if len(responses) == 0 {
		t.Error("expected at least 1 ai_response")
	}

	actions := col.ofType(EventAIAction)
	editFound := false
	for _, a := range actions {
		if a.Action == "edit_file" {
			editFound = true
		}
	}
	if !editFound {
		t.Error("expected ai_action with action=edit_file for search/replace block")
	}
}

func TestAiderParser_FileEdit(t *testing.T) {
	col := &testEventCollector{}
	ap := NewAiderParser(col.callback)

	ap.FeedLine(Line{Text: sampleAiderFileEdit})
	ap.Flush()

	actions := col.ofType(EventAIAction)
	found := false
	for _, a := range actions {
		if a.Action == "edit_file" && strings.Contains(a.Description, "src/auth/server.ts") {
			found = true
		}
	}
	if !found {
		t.Error("expected ai_action for file edit")
	}
}

func TestAiderParser_GitCommit(t *testing.T) {
	col := &testEventCollector{}
	ap := NewAiderParser(col.callback)

	ap.FeedLine(Line{Text: sampleAiderGitCommit})
	ap.Flush()

	actions := col.ofType(EventAIAction)
	found := false
	for _, a := range actions {
		if a.Action == "run_command" && strings.Contains(a.Description, "git commit") {
			found = true
		}
	}
	if !found {
		t.Error("expected ai_action for git commit")
	}
}

func TestAiderParser_Cost(t *testing.T) {
	col := &testEventCollector{}
	ap := NewAiderParser(col.callback)

	ap.FeedLine(Line{Text: sampleAiderCost})
	ap.Flush()

	usages := col.ofType(EventAIUsage)
	if len(usages) == 0 {
		t.Fatal("expected at least 1 ai_usage")
	}

	if usages[0].Tool != "aider" {
		t.Errorf("expected tool=aider, got %q", usages[0].Tool)
	}
	if usages[0].CostUSD != 0.05 {
		t.Errorf("expected cost=0.05, got %.2f", usages[0].CostUSD)
	}
}

func TestAiderParser_EmptyInput(t *testing.T) {
	col := &testEventCollector{}
	ap := NewAiderParser(col.callback)

	ap.FeedLine(Line{Text: ""})
	ap.FeedLine(Line{Text: "   "})
	ap.FeedLine(Line{})
	ap.Flush()

	if len(col.events) != 0 {
		t.Errorf("expected 0 events for empty input, got %d", len(col.events))
	}
}

func TestAiderParser_NoPanicOnGarbage(t *testing.T) {
	col := &testEventCollector{}
	ap := NewAiderParser(col.callback)

	garbage := []string{
		"\x00\x01\x02",
		strings.Repeat("X", 10000),
		"<<<<<<< NOT_SEARCH",
		">>>>>>> NOT_REPLACE",
		"$0.00",
	}

	for _, g := range garbage {
		ap.FeedLine(Line{Text: g})
	}
	ap.Flush()
	ap.Reset()
	// No panic = pass
}

func TestAiderParser_Reset(t *testing.T) {
	col := &testEventCollector{}
	ap := NewAiderParser(col.callback)

	ap.FeedLine(Line{Text: "Some response"})
	ap.Reset()

	// After reset, state should be clean
	ap.FeedLine(Line{Text: "New response"})
	ap.Flush()

	responses := col.ofType(EventAIResponse)
	// We should have events from both before reset (flushed by Reset) and after
	if len(responses) < 1 {
		t.Errorf("expected at least 1 response, got %d", len(responses))
	}
}

func TestAiderParser_ResponseAccumulation(t *testing.T) {
	col := &testEventCollector{}
	ap := NewAiderParser(col.callback)

	lines := []string{
		"I see the issue in the code.",
		"The token validation is missing",
		"an expiration check.",
	}

	for _, l := range lines {
		ap.FeedLine(Line{Text: l})
	}
	ap.Flush()

	responses := col.ofType(EventAIResponse)
	if len(responses) == 0 {
		t.Fatal("expected at least 1 ai_response")
	}

	var combined strings.Builder
	for _, r := range responses {
		combined.WriteString(r.Content)
	}
	content := combined.String()
	if !strings.Contains(content, "expiration check") {
		t.Error("response should contain accumulated text")
	}
}

// TestAiderParser_PipelineIntegration tests the Aider parser with the full pipeline.
func TestAiderParser_PipelineIntegration(t *testing.T) {
	p := NewPipeline()
	p.SetProcess("aider", "aider")
	p.SetCWD("/home/user/project")

	// Simulate Aider output
	p.FeedOutput([]byte("\x1b]133;A\x07> "))
	p.FeedInput([]byte("fix the bug"))
	p.FeedOutput([]byte("\x1b]133;C\x07"))

	p.FeedOutput([]byte("I'll fix the issue.\n"))
	p.FeedOutput([]byte("Applied edit to src/main.go\n"))

	p.Close()

	events := drainEvents(p, 500*time.Millisecond)

	// Should have both base events and AI events
	hasBase := false
	hasAI := false
	for _, e := range events {
		if e.Type == EventUserInput || e.Type == EventSystemOutput {
			hasBase = true
		}
		if e.Type == EventAIResponse || e.Type == EventAIAction {
			hasAI = true
		}
	}
	if !hasBase {
		t.Error("expected base parser events (user_input or system_output)")
	}
	if !hasAI {
		t.Error("expected AI parser events (ai_response or ai_action)")
	}
}
