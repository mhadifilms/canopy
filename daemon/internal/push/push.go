// Package push implements push notification triggers for the daemon.
// When certain events occur (ai_approval, errors, long-running completions),
// the daemon sends push notifications to paired phones via the coordination server.
package push

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/canopy-dev/canopyd/internal/coord"
	"github.com/canopy-dev/canopyd/internal/parser"
)

// TriggerType identifies push notification triggers.
type TriggerType string

const (
	TriggerAIApproval      TriggerType = "ai_approval"
	TriggerError           TriggerType = "error"
	TriggerLongCompletion  TriggerType = "long_completion"
	TriggerCustomKeyword   TriggerType = "custom_keyword"
)

// TriggerConfig controls which events trigger push notifications.
type TriggerConfig struct {
	AIApproval     bool  `json:"ai_approval"`      // AI tool waiting for approval (default: ON)
	Error          bool  `json:"error"`             // Session error status (default: ON)
	LongCompletion bool  `json:"long_completion"`   // Command completed after >60s (default: OFF)
	LongThresholdS int64 `json:"long_threshold_s"`  // Seconds threshold for long completion
	CustomKeywords []string `json:"custom_keywords"` // User-defined keyword alerts (default: OFF)
}

// DefaultTriggerConfig returns the default trigger configuration per §3.9.1.
func DefaultTriggerConfig() TriggerConfig {
	return TriggerConfig{
		AIApproval:     true,
		Error:          true,
		LongCompletion: false,
		LongThresholdS: 60,
		CustomKeywords: nil,
	}
}

// Service manages push notification delivery for the daemon.
type Service struct {
	coordClient *coord.Client
	config      TriggerConfig
	logger      *zap.Logger
	hostname    string

	// APNs tokens from paired devices. Updated when devices check in.
	mu         sync.RWMutex
	apnsTokens []string
}

// NewService creates a push notification service.
func NewService(coordClient *coord.Client, cfg TriggerConfig, hostname string, logger *zap.Logger) *Service {
	return &Service{
		coordClient: coordClient,
		config:      cfg,
		logger:      logger,
		hostname:    hostname,
	}
}

// SetAPNSTokens updates the APNs tokens for paired devices.
func (s *Service) SetAPNSTokens(tokens []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apnsTokens = make([]string, len(tokens))
	copy(s.apnsTokens, tokens)
}

// HandleEvent evaluates an event against the trigger configuration and sends
// push notifications if appropriate. This is called from consumePipelineEvents.
func (s *Service) HandleEvent(ctx context.Context, sessionID string, event parser.Event) {
	switch event.Type {
	case parser.EventAIApproval:
		if s.config.AIApproval {
			s.sendApprovalPush(ctx, sessionID, event)
		}

	case parser.EventCompleted:
		// Long-running completion trigger.
		if s.config.LongCompletion && event.DurationMS > s.config.LongThresholdS*1000 {
			s.sendCompletionPush(ctx, sessionID, event)
		}
		// Failed command trigger (non-zero exit).
		if s.config.Error && event.ExitCode != nil && *event.ExitCode != 0 {
			s.sendErrorPush(ctx, sessionID, event)
		}

	case parser.EventStatusChange:
		if s.config.Error && event.To == "error" {
			s.sendStatusErrorPush(ctx, sessionID, event)
		}

	case parser.EventSystemOutput:
		if len(s.config.CustomKeywords) > 0 {
			s.checkKeywords(ctx, sessionID, event)
		}
	}
}

func (s *Service) sendApprovalPush(ctx context.Context, sessionID string, event parser.Event) {
	description := event.Description
	if description == "" {
		description = "Action requires approval"
	}

	notification := coord.PushNotification{
		Title:    "Claude Code needs approval",
		Subtitle: s.hostname,
		Body:     description,
		Category: "APPROVAL_REQUEST",
		ThreadID: sessionID,
		Data: map[string]string{
			"session_id":    sessionID,
			"event_type":    "ai_approval",
		},
	}

	s.sendToAll(ctx, notification)
}

func (s *Service) sendCompletionPush(ctx context.Context, sessionID string, event parser.Event) {
	durationSec := event.DurationMS / 1000

	notification := coord.PushNotification{
		Title:    "Command completed",
		Subtitle: s.hostname,
		Body:     fmt.Sprintf("Finished after %ds", durationSec),
		Category: "SESSION_ALERT",
		ThreadID: sessionID,
		Data: map[string]string{
			"session_id":  sessionID,
			"event_type":  "completed",
			"duration_ms": fmt.Sprintf("%d", event.DurationMS),
		},
	}

	s.sendToAll(ctx, notification)
}

func (s *Service) sendErrorPush(ctx context.Context, sessionID string, event parser.Event) {
	exitCode := 1
	if event.ExitCode != nil {
		exitCode = *event.ExitCode
	}

	notification := coord.PushNotification{
		Title:    "Command failed",
		Subtitle: s.hostname,
		Body:     fmt.Sprintf("Exit code %d", exitCode),
		Category: "SESSION_ALERT",
		ThreadID: sessionID,
		Data: map[string]string{
			"session_id": sessionID,
			"event_type": "error",
			"exit_code":  fmt.Sprintf("%d", exitCode),
		},
	}

	s.sendToAll(ctx, notification)
}

func (s *Service) sendStatusErrorPush(ctx context.Context, sessionID string, event parser.Event) {
	notification := coord.PushNotification{
		Title:    "Session error",
		Subtitle: s.hostname,
		Body:     "Session encountered an error",
		Category: "SESSION_ALERT",
		ThreadID: sessionID,
		Data: map[string]string{
			"session_id": sessionID,
			"event_type": "status_change",
		},
	}

	s.sendToAll(ctx, notification)
}

func (s *Service) checkKeywords(ctx context.Context, sessionID string, event parser.Event) {
	for _, kw := range s.config.CustomKeywords {
		if containsKeyword(event.Content, kw) {
			notification := coord.PushNotification{
				Title:    fmt.Sprintf("Keyword matched: %s", kw),
				Subtitle: s.hostname,
				Body:     truncate(event.Content, 100),
				Category: "SESSION_ALERT",
				ThreadID: sessionID,
				Data: map[string]string{
					"session_id": sessionID,
					"event_type": "keyword",
					"keyword":    kw,
				},
			}
			s.sendToAll(ctx, notification)
			return // Only one notification per event even if multiple keywords match.
		}
	}
}

// sendToAll sends a push notification to all paired devices with APNs tokens.
func (s *Service) sendToAll(ctx context.Context, notification coord.PushNotification) {
	s.mu.RLock()
	tokens := make([]string, len(s.apnsTokens))
	copy(tokens, s.apnsTokens)
	s.mu.RUnlock()

	if len(tokens) == 0 {
		s.logger.Debug("no apns tokens, skipping push notification",
			zap.String("title", notification.Title),
		)
		return
	}

	targets := make([]coord.PushTarget, 0, len(tokens))
	for _, token := range tokens {
		if token == "" {
			continue
		}
		targets = append(targets, coord.PushTarget{
			APNSToken:    token,
			Notification: notification,
		})
	}

	if len(targets) == 0 {
		return
	}

	if err := s.coordClient.SendPush(ctx, targets); err != nil {
		s.logger.Warn("failed to send push notification",
			zap.String("title", notification.Title),
			zap.Error(err),
		)
	} else {
		s.logger.Info("push notification sent",
			zap.String("title", notification.Title),
			zap.Int("targets", len(targets)),
		)
	}
}

// containsKeyword checks if content contains the keyword (case-insensitive).
func containsKeyword(content, keyword string) bool {
	if keyword == "" || content == "" {
		return false
	}
	// Simple case-insensitive substring match.
	lowerContent := toLower(content)
	lowerKeyword := toLower(keyword)
	return contains(lowerContent, lowerKeyword)
}

// toLower is a simple ASCII lowercase helper to avoid importing strings.
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

// contains checks if s contains substr.
func contains(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// truncate shortens a string to maxLen, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
