package push

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/canopy-dev/canopyd/internal/coord"
	"github.com/canopy-dev/canopyd/internal/parser"
)

func testCoordClient(t *testing.T, handler http.Handler) *coord.Client {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	wgKey := make([]byte, 32)
	rand.Read(wgKey)

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return coord.NewClient(coord.ClientConfig{
		CoordURL:     server.URL,
		IdentityPub:  pub,
		IdentityPriv: priv,
		WGPubKey:     wgKey,
		WGPort:       51820,
	}, zap.NewNop())
}

func TestService_AIApprovalTrigger(t *testing.T) {
	var pushCount int32
	client := testCoordClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/push" {
			atomic.AddInt32(&pushCount, 1)
			json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "sent": 1})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))

	svc := NewService(client, DefaultTriggerConfig(), "test-mac", zap.NewNop())
	svc.SetAPNSTokens([]string{"token123"})

	event := parser.Event{
		Type:        parser.EventAIApproval,
		Timestamp:   time.Now(),
		Tool:        "claude_code",
		Description: "Edit server.ts lines 40-45",
		Action:      "edit_file",
	}

	svc.HandleEvent(context.Background(), "session-1", event)

	if atomic.LoadInt32(&pushCount) != 1 {
		t.Errorf("expected 1 push, got %d", pushCount)
	}
}

func TestService_AIApprovalDisabled(t *testing.T) {
	var pushCount int32
	client := testCoordClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/push" {
			atomic.AddInt32(&pushCount, 1)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))

	cfg := DefaultTriggerConfig()
	cfg.AIApproval = false

	svc := NewService(client, cfg, "test-mac", zap.NewNop())
	svc.SetAPNSTokens([]string{"token123"})

	event := parser.Event{
		Type:        parser.EventAIApproval,
		Timestamp:   time.Now(),
		Tool:        "claude_code",
		Description: "Edit server.ts",
	}

	svc.HandleEvent(context.Background(), "session-1", event)

	if atomic.LoadInt32(&pushCount) != 0 {
		t.Errorf("expected 0 pushes when AI approval disabled, got %d", pushCount)
	}
}

func TestService_ErrorTrigger(t *testing.T) {
	var pushCount int32
	client := testCoordClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/push" {
			atomic.AddInt32(&pushCount, 1)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))

	svc := NewService(client, DefaultTriggerConfig(), "test-mac", zap.NewNop())
	svc.SetAPNSTokens([]string{"token123"})

	exitCode := 1
	event := parser.Event{
		Type:      parser.EventCompleted,
		Timestamp: time.Now(),
		ExitCode:  &exitCode,
	}

	svc.HandleEvent(context.Background(), "session-1", event)

	if atomic.LoadInt32(&pushCount) != 1 {
		t.Errorf("expected 1 push for error, got %d", pushCount)
	}
}

func TestService_ErrorTriggerDisabled(t *testing.T) {
	var pushCount int32
	client := testCoordClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/push" {
			atomic.AddInt32(&pushCount, 1)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))

	cfg := DefaultTriggerConfig()
	cfg.Error = false

	svc := NewService(client, cfg, "test-mac", zap.NewNop())
	svc.SetAPNSTokens([]string{"token123"})

	exitCode := 1
	event := parser.Event{
		Type:      parser.EventCompleted,
		Timestamp: time.Now(),
		ExitCode:  &exitCode,
	}

	svc.HandleEvent(context.Background(), "session-1", event)

	if atomic.LoadInt32(&pushCount) != 0 {
		t.Errorf("expected 0 pushes when error disabled, got %d", pushCount)
	}
}

func TestService_SuccessfulExitNoPush(t *testing.T) {
	var pushCount int32
	client := testCoordClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/push" {
			atomic.AddInt32(&pushCount, 1)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))

	svc := NewService(client, DefaultTriggerConfig(), "test-mac", zap.NewNop())
	svc.SetAPNSTokens([]string{"token123"})

	exitCode := 0
	event := parser.Event{
		Type:      parser.EventCompleted,
		Timestamp: time.Now(),
		ExitCode:  &exitCode,
	}

	svc.HandleEvent(context.Background(), "session-1", event)

	if atomic.LoadInt32(&pushCount) != 0 {
		t.Errorf("expected 0 pushes for exit code 0, got %d", pushCount)
	}
}

func TestService_LongCompletionTrigger(t *testing.T) {
	var pushCount int32
	client := testCoordClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/push" {
			atomic.AddInt32(&pushCount, 1)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))

	cfg := DefaultTriggerConfig()
	cfg.LongCompletion = true
	cfg.LongThresholdS = 60

	svc := NewService(client, cfg, "test-mac", zap.NewNop())
	svc.SetAPNSTokens([]string{"token123"})

	exitCode := 0
	event := parser.Event{
		Type:       parser.EventCompleted,
		Timestamp:  time.Now(),
		ExitCode:   &exitCode,
		DurationMS: 120000, // 120 seconds
	}

	svc.HandleEvent(context.Background(), "session-1", event)

	if atomic.LoadInt32(&pushCount) != 1 {
		t.Errorf("expected 1 push for long completion, got %d", pushCount)
	}
}

func TestService_LongCompletionShortDuration(t *testing.T) {
	var pushCount int32
	client := testCoordClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/push" {
			atomic.AddInt32(&pushCount, 1)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))

	cfg := DefaultTriggerConfig()
	cfg.LongCompletion = true
	cfg.LongThresholdS = 60

	svc := NewService(client, cfg, "test-mac", zap.NewNop())
	svc.SetAPNSTokens([]string{"token123"})

	exitCode := 0
	event := parser.Event{
		Type:       parser.EventCompleted,
		Timestamp:  time.Now(),
		ExitCode:   &exitCode,
		DurationMS: 5000, // 5 seconds, below threshold
	}

	svc.HandleEvent(context.Background(), "session-1", event)

	if atomic.LoadInt32(&pushCount) != 0 {
		t.Errorf("expected 0 pushes for short completion, got %d", pushCount)
	}
}

func TestService_StatusErrorTrigger(t *testing.T) {
	var pushCount int32
	client := testCoordClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/push" {
			atomic.AddInt32(&pushCount, 1)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))

	svc := NewService(client, DefaultTriggerConfig(), "test-mac", zap.NewNop())
	svc.SetAPNSTokens([]string{"token123"})

	event := parser.Event{
		Type: parser.EventStatusChange,
		To:   "error",
	}

	svc.HandleEvent(context.Background(), "session-1", event)

	if atomic.LoadInt32(&pushCount) != 1 {
		t.Errorf("expected 1 push for status error, got %d", pushCount)
	}
}

func TestService_NoAPNSTokens(t *testing.T) {
	var pushCount int32
	client := testCoordClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/push" {
			atomic.AddInt32(&pushCount, 1)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))

	svc := NewService(client, DefaultTriggerConfig(), "test-mac", zap.NewNop())
	// No SetAPNSTokens call — no tokens.

	event := parser.Event{
		Type:        parser.EventAIApproval,
		Timestamp:   time.Now(),
		Tool:        "claude_code",
		Description: "Edit server.ts",
	}

	svc.HandleEvent(context.Background(), "session-1", event)

	// Should not attempt to send push without tokens.
	if atomic.LoadInt32(&pushCount) != 0 {
		t.Errorf("expected 0 pushes without tokens, got %d", pushCount)
	}
}

func TestService_CustomKeyword(t *testing.T) {
	var pushCount int32
	client := testCoordClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/push" {
			atomic.AddInt32(&pushCount, 1)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))

	cfg := DefaultTriggerConfig()
	cfg.CustomKeywords = []string{"deploy", "error"}

	svc := NewService(client, cfg, "test-mac", zap.NewNop())
	svc.SetAPNSTokens([]string{"token123"})

	event := parser.Event{
		Type:    parser.EventSystemOutput,
		Content: "Starting deploy to production...",
	}

	svc.HandleEvent(context.Background(), "session-1", event)

	if atomic.LoadInt32(&pushCount) != 1 {
		t.Errorf("expected 1 push for keyword match, got %d", pushCount)
	}
}

func TestService_CustomKeywordNoMatch(t *testing.T) {
	var pushCount int32
	client := testCoordClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/push" {
			atomic.AddInt32(&pushCount, 1)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))

	cfg := DefaultTriggerConfig()
	cfg.CustomKeywords = []string{"deploy"}

	svc := NewService(client, cfg, "test-mac", zap.NewNop())
	svc.SetAPNSTokens([]string{"token123"})

	event := parser.Event{
		Type:    parser.EventSystemOutput,
		Content: "Building project...",
	}

	svc.HandleEvent(context.Background(), "session-1", event)

	if atomic.LoadInt32(&pushCount) != 0 {
		t.Errorf("expected 0 pushes for no keyword match, got %d", pushCount)
	}
}

func TestContainsKeyword(t *testing.T) {
	tests := []struct {
		content  string
		keyword  string
		expected bool
	}{
		{"Deploy to production started", "deploy", true},
		{"DEPLOY TO PRODUCTION", "deploy", true},
		{"building project", "deploy", false},
		{"", "deploy", false},
		{"some content", "", false},
		{"", "", false},
		{"error: something failed", "error", true},
	}

	for _, tt := range tests {
		result := containsKeyword(tt.content, tt.keyword)
		if result != tt.expected {
			t.Errorf("containsKeyword(%q, %q) = %v, want %v", tt.content, tt.keyword, result, tt.expected)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input    string
		maxLen   int
		expected string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is a longer string", 10, "this is..."},
		{"ab", 5, "ab"},
		{"abcde", 3, "abc"},
	}

	for _, tt := range tests {
		result := truncate(tt.input, tt.maxLen)
		if result != tt.expected {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.maxLen, result, tt.expected)
		}
	}
}

func TestDefaultTriggerConfig(t *testing.T) {
	cfg := DefaultTriggerConfig()
	if !cfg.AIApproval {
		t.Error("AIApproval should be true by default")
	}
	if !cfg.Error {
		t.Error("Error should be true by default")
	}
	if cfg.LongCompletion {
		t.Error("LongCompletion should be false by default")
	}
	if cfg.LongThresholdS != 60 {
		t.Errorf("LongThresholdS should be 60, got %d", cfg.LongThresholdS)
	}
}
