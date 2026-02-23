// Package apns implements an Apple Push Notification service HTTP/2 client.
package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/canopy-dev/coord/internal/apns/jwt"
)

const (
	productionURL = "https://api.push.apple.com"
	sandboxURL    = "https://api.sandbox.push.apple.com"
	apnsTopic     = "dev.canopy.app"
	maxRetries    = 3
)

// Client sends push notifications to Apple Push Notification service.
type Client struct {
	teamID     string
	keyID      string
	privateKey *ecdsa.PrivateKey
	baseURL    string
	httpClient *http.Client

	mu          sync.Mutex
	cachedToken string
	tokenExpiry time.Time
}

// NewClient creates an APNs client from environment variables.
// Returns nil if APNS_TEAM_ID, APNS_KEY_ID, or APNS_KEY_PATH are not set.
func NewClient() *Client {
	teamID := os.Getenv("APNS_TEAM_ID")
	keyID := os.Getenv("APNS_KEY_ID")
	keyPath := os.Getenv("APNS_KEY_PATH")

	if teamID == "" || keyID == "" || keyPath == "" {
		return nil
	}

	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil
	}

	key, err := parseP8Key(keyData)
	if err != nil {
		return nil
	}

	env := os.Getenv("APNS_ENVIRONMENT")
	baseURL := sandboxURL
	if env == "production" {
		baseURL = productionURL
	}

	return &Client{
		teamID:     teamID,
		keyID:      keyID,
		privateKey: key,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// newClientInternal creates a client for testing with explicit parameters.
func newClientInternal(teamID, keyID string, key *ecdsa.PrivateKey, baseURL string, httpClient *http.Client) *Client {
	return &Client{
		teamID:     teamID,
		keyID:      keyID,
		privateKey: key,
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

// Payload is the push notification payload.
type Payload struct {
	Alert    Alert          `json:"alert,omitempty"`
	Sound    string         `json:"sound,omitempty"`
	Badge    *int           `json:"badge,omitempty"`
	Category string         `json:"category,omitempty"`
	ThreadID string         `json:"thread-id,omitempty"`
	Data     map[string]any `json:"data,omitempty"`
}

// Alert is the alert portion of the push notification.
type Alert struct {
	Title    string `json:"title,omitempty"`
	Subtitle string `json:"subtitle,omitempty"`
	Body     string `json:"body,omitempty"`
}

// apnsPayload is the wire format sent to APNs.
type apnsPayload struct {
	APS apsFields      `json:"aps"`
	Extra map[string]any `json:"-"`
}

type apsFields struct {
	Alert    *Alert `json:"alert,omitempty"`
	Sound    string `json:"sound,omitempty"`
	Badge    *int   `json:"badge,omitempty"`
	Category string `json:"category,omitempty"`
	ThreadID string `json:"thread-id,omitempty"`
}

// MarshalJSON produces the APNs JSON with aps and extra data fields at the top level.
func (p apnsPayload) MarshalJSON() ([]byte, error) {
	m := map[string]any{
		"aps": p.APS,
	}
	for k, v := range p.Extra {
		m[k] = v
	}
	return json.Marshal(m)
}

// Send sends a push notification to the given device token.
func (c *Client) Send(ctx context.Context, deviceToken string, payload *Payload) error {
	body, err := buildBody(payload)
	if err != nil {
		return fmt.Errorf("apns: marshal payload: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(math.Pow(2, float64(attempt-1))) * 500 * time.Millisecond
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}

		lastErr = c.sendOnce(ctx, deviceToken, body)
		if lastErr == nil {
			return nil
		}
		// Only retry on transient errors.
		if !isTransient(lastErr) {
			return lastErr
		}
	}
	return lastErr
}

func (c *Client) sendOnce(ctx context.Context, deviceToken string, body []byte) error {
	token, err := c.getToken()
	if err != nil {
		return fmt.Errorf("apns: generate jwt: %w", err)
	}

	url := c.baseURL + "/3/device/" + deviceToken
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(string(body)))
	if err != nil {
		return fmt.Errorf("apns: create request: %w", err)
	}

	req.Header.Set("Authorization", "bearer "+token)
	req.Header.Set("apns-topic", apnsTopic)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("apns: http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}

	respBody, _ := io.ReadAll(resp.Body)
	return &APNsError{
		StatusCode: resp.StatusCode,
		Body:       string(respBody),
	}
}

func (c *Client) getToken() (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Cache JWT for 50 minutes (APNs JWTs valid for 60 min).
	if c.cachedToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.cachedToken, nil
	}

	token, err := jwt.Generate(c.teamID, c.keyID, c.privateKey)
	if err != nil {
		return "", err
	}

	c.cachedToken = token
	c.tokenExpiry = time.Now().Add(50 * time.Minute)
	return token, nil
}

// APNsError represents an error response from APNs.
type APNsError struct {
	StatusCode int
	Body       string
}

func (e *APNsError) Error() string {
	return fmt.Sprintf("apns: status %d: %s", e.StatusCode, e.Body)
}

func isTransient(err error) bool {
	apnsErr, ok := err.(*APNsError)
	if !ok {
		return false
	}
	switch apnsErr.StatusCode {
	case http.StatusTooManyRequests, http.StatusInternalServerError, http.StatusServiceUnavailable:
		return true
	}
	return false
}

func buildBody(p *Payload) ([]byte, error) {
	aps := apsFields{
		Sound:    p.Sound,
		Badge:    p.Badge,
		Category: p.Category,
		ThreadID: p.ThreadID,
	}
	if p.Alert.Title != "" || p.Alert.Subtitle != "" || p.Alert.Body != "" {
		aps.Alert = &p.Alert
	}
	payload := apnsPayload{
		APS:   aps,
		Extra: p.Data,
	}
	return payload.MarshalJSON()
}

func parseP8Key(data []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("apns: no PEM block found in key file")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("apns: parse private key: %w", err)
	}
	ecKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("apns: key is not ECDSA")
	}
	return ecKey, nil
}
