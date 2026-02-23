package parser

import (
	"testing"
	"time"
)

// testEventCollector collects events from the conversation parser.
type testEventCollector struct {
	events []Event
}

func (c *testEventCollector) callback(e Event) {
	c.events = append(c.events, e)
}

func (c *testEventCollector) ofType(t EventType) []Event {
	var out []Event
	for _, e := range c.events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

func TestConversationParser_OSC133_IdleEvent(t *testing.T) {
	col := &testEventCollector{}
	p := NewConversationParser(col.callback)
	p.SetCWD("/home/user")

	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'A'}}})

	idles := col.ofType(EventIdle)
	if len(idles) != 1 {
		t.Fatalf("expected 1 idle event, got %d", len(idles))
	}
	if idles[0].CWD != "/home/user" {
		t.Errorf("expected CWD '/home/user', got %q", idles[0].CWD)
	}
}

func TestConversationParser_OSC133_CommandCycle(t *testing.T) {
	col := &testEventCollector{}
	p := NewConversationParser(col.callback)
	p.SetCWD("/home/user")

	// Prompt displayed
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'A'}}})

	// User types "ls -la" and presses Enter
	p.FeedInput([]byte("ls -la"))
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'C'}}})

	// Command output
	p.FeedLine(Line{Text: "total 42"})
	p.FeedLine(Line{Text: "drwxr-xr-x  2 user user 4096 file.txt"})

	// Command completes
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'D', Param: "0"}}})

	// New prompt
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'A'}}})

	// Check events
	inputs := col.ofType(EventUserInput)
	if len(inputs) != 1 {
		t.Fatalf("expected 1 user_input, got %d", len(inputs))
	}
	if inputs[0].Text != "ls -la" {
		t.Errorf("expected text 'ls -la', got %q", inputs[0].Text)
	}
	if inputs[0].InputType != InputCommand {
		t.Errorf("expected input_type 'command', got %q", inputs[0].InputType)
	}

	completed := col.ofType(EventCompleted)
	if len(completed) != 1 {
		t.Fatalf("expected 1 completed, got %d", len(completed))
	}
	if completed[0].ExitCode == nil || *completed[0].ExitCode != 0 {
		t.Errorf("expected exit code 0, got %v", completed[0].ExitCode)
	}

	idles := col.ofType(EventIdle)
	if len(idles) != 2 {
		t.Errorf("expected 2 idle events, got %d", len(idles))
	}
}

func TestConversationParser_OSC133_NonZeroExit(t *testing.T) {
	col := &testEventCollector{}
	p := NewConversationParser(col.callback)

	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'A'}}})
	p.FeedInput([]byte("false"))
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'C'}}})
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'D', Param: "1"}}})

	completed := col.ofType(EventCompleted)
	if len(completed) != 1 {
		t.Fatalf("expected 1 completed, got %d", len(completed))
	}
	if completed[0].ExitCode == nil || *completed[0].ExitCode != 1 {
		t.Errorf("expected exit code 1, got %v", completed[0].ExitCode)
	}
}

func TestConversationParser_SystemOutput(t *testing.T) {
	col := &testEventCollector{}
	p := NewConversationParser(col.callback)

	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'A'}}})
	p.FeedInput([]byte("echo hello"))
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'C'}}})
	p.FeedLine(Line{Text: "hello"})
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'D', Param: "0"}}})

	outputs := col.ofType(EventSystemOutput)
	if len(outputs) != 1 {
		t.Fatalf("expected 1 system_output, got %d", len(outputs))
	}
	if outputs[0].Content != "hello" {
		t.Errorf("expected content 'hello', got %q", outputs[0].Content)
	}
}

func TestConversationParser_InputClassification_Response(t *testing.T) {
	col := &testEventCollector{}
	p := NewConversationParser(col.callback)

	// Simulate: command runs, asks for input
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'A'}}})
	p.FeedInput([]byte("npm install"))
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'C'}}})

	// Interactive prompt detected
	p.FeedLine(Line{Text: "Are you sure? [y/N]"})

	// User responds
	p.FeedInput([]byte("y\n"))

	// Allow time for processing
	time.Sleep(50 * time.Millisecond)

	inputs := col.ofType(EventUserInput)
	if len(inputs) < 2 {
		t.Fatalf("expected at least 2 user_input events, got %d", len(inputs))
	}

	// Second input should be a response
	lastInput := inputs[len(inputs)-1]
	if lastInput.InputType != InputResponse {
		t.Errorf("expected input_type 'response', got %q", lastInput.InputType)
	}
	if lastInput.Text != "y" {
		t.Errorf("expected text 'y', got %q", lastInput.Text)
	}
}

func TestConversationParser_InputClassification_AIMessage(t *testing.T) {
	col := &testEventCollector{}
	p := NewConversationParser(col.callback)
	p.SetProcess("claude", "claude_code")

	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'A'}}})
	p.FeedInput([]byte("fix the auth bug"))
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'C'}}})

	inputs := col.ofType(EventUserInput)
	if len(inputs) != 1 {
		t.Fatalf("expected 1 user_input, got %d", len(inputs))
	}
	if inputs[0].InputType != InputAIMessage {
		t.Errorf("expected input_type 'ai_message', got %q", inputs[0].InputType)
	}
}

func TestConversationParser_InputRequest_YN(t *testing.T) {
	col := &testEventCollector{}
	p := NewConversationParser(col.callback)

	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'A'}}})
	p.FeedInput([]byte("rm -rf /tmp/test"))
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'C'}}})

	// Process asks for confirmation
	p.FeedLine(Line{Text: "Are you sure you want to continue? [y/N]"})

	requests := col.ofType(EventInputRequest)
	if len(requests) != 1 {
		t.Fatalf("expected 1 input_request, got %d", len(requests))
	}
	if requests[0].PromptText != "Are you sure you want to continue? [y/N]" {
		t.Errorf("unexpected prompt text: %q", requests[0].PromptText)
	}
	if len(requests[0].QuickActions) != 2 {
		t.Fatalf("expected 2 quick actions, got %d", len(requests[0].QuickActions))
	}
	if requests[0].QuickActions[0] != "y" || requests[0].QuickActions[1] != "N" {
		t.Errorf("expected ['y','N'], got %v", requests[0].QuickActions)
	}
}

func TestConversationParser_InputRequest_Password(t *testing.T) {
	col := &testEventCollector{}
	p := NewConversationParser(col.callback)

	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'A'}}})
	p.FeedInput([]byte("sudo ls"))
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'C'}}})
	p.FeedLine(Line{Text: "Password: "})

	requests := col.ofType(EventInputRequest)
	if len(requests) != 1 {
		t.Fatalf("expected 1 input_request, got %d", len(requests))
	}
	if requests[0].QuickActions != nil {
		t.Errorf("expected nil quick_actions for password, got %v", requests[0].QuickActions)
	}
}

func TestConversationParser_StateTransitions(t *testing.T) {
	col := &testEventCollector{}
	p := NewConversationParser(col.callback)

	if p.State() != StateIdle {
		t.Errorf("expected initial state IDLE, got %d", p.State())
	}

	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'A'}}})
	if p.State() != StateIdle {
		t.Errorf("expected IDLE after 133;A, got %d", p.State())
	}

	p.FeedInput([]byte("ls"))
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'C'}}})
	if p.State() != StateRunning {
		t.Errorf("expected RUNNING after 133;C, got %d", p.State())
	}

	p.FeedLine(Line{Text: "Continue? [y/N]"})
	if p.State() != StateInputWait {
		t.Errorf("expected INPUT_WAIT after prompt, got %d", p.State())
	}
}

func TestConversationParser_DurationMS(t *testing.T) {
	col := &testEventCollector{}
	p := NewConversationParser(col.callback)

	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'A'}}})
	p.FeedInput([]byte("sleep 0.1"))
	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'C'}}})

	time.Sleep(100 * time.Millisecond)

	p.FeedLine(Line{Markers: []OSCMarker{{Code: 'D', Param: "0"}}})

	completed := col.ofType(EventCompleted)
	if len(completed) != 1 {
		t.Fatalf("expected 1 completed, got %d", len(completed))
	}
	if completed[0].DurationMS < 50 {
		t.Errorf("expected duration >= 50ms, got %d", completed[0].DurationMS)
	}
}

func TestParseQuickActions(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"[y/N]", []string{"y", "N"}},
		{"[Y/n]", []string{"Y", "n"}},
		{"Continue? (yes/no)", []string{"yes", "no"}},
		{"(Y/n)", []string{"Y", "n"}},
		{"[1/2/3]", []string{"1", "2", "3"}},
		{"Choose an option [a/b/c]", []string{"a", "b", "c"}},
		{"Password:", nil},
		{"just regular text", nil},
		{"", nil},
	}

	for _, tt := range tests {
		result := parseQuickActions(tt.input)
		if tt.expected == nil {
			if result != nil {
				t.Errorf("parseQuickActions(%q): expected nil, got %v", tt.input, result)
			}
			continue
		}
		if len(result) != len(tt.expected) {
			t.Errorf("parseQuickActions(%q): expected %v, got %v", tt.input, tt.expected, result)
			continue
		}
		for i, v := range tt.expected {
			if result[i] != v {
				t.Errorf("parseQuickActions(%q)[%d]: expected %q, got %q", tt.input, i, v, result[i])
			}
		}
	}
}

func TestIsInteractivePrompt(t *testing.T) {
	prompts := []string{
		"Continue? [y/N]",
		"Are you sure? [Y/n]",
		"Proceed? (yes/no)",
		"Enter password:",
		"Password: ",
		"Enter passphrase: ",
		"Press any key to continue",
		"Press Enter to continue",
		"Continue?",
		"more: ",
	}
	for _, p := range prompts {
		if !isInteractivePrompt(p) {
			t.Errorf("expected %q to be detected as interactive prompt", p)
		}
	}

	nonPrompts := []string{
		"Building project...",
		"Compiling 42 files",
		"error: something failed",
		"total 8",
	}
	for _, p := range nonPrompts {
		if isInteractivePrompt(p) {
			t.Errorf("expected %q to NOT be detected as interactive prompt", p)
		}
	}
}
