package apns

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/canopy-dev/coord/internal/apns/jwt"
)

func generateTestKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return key
}

func TestJWTGeneration(t *testing.T) {
	key := generateTestKey(t)
	teamID := "TEAM123456"
	keyID := "KEY1234567"

	token, err := jwt.Generate(teamID, keyID, key)
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}

	// Verify the signature.
	if err := jwt.Verify(token, &key.PublicKey); err != nil {
		t.Fatalf("verify jwt: %v", err)
	}

	// Check header.
	header, err := jwt.DecodeHeader(token)
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	if header["alg"] != "ES256" {
		t.Errorf("alg: got %v, want ES256", header["alg"])
	}
	if header["kid"] != keyID {
		t.Errorf("kid: got %v, want %s", header["kid"], keyID)
	}

	// Check claims.
	claims, err := jwt.DecodeClaims(token)
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	if claims["iss"] != teamID {
		t.Errorf("iss: got %v, want %s", claims["iss"], teamID)
	}
	if _, ok := claims["iat"]; !ok {
		t.Error("missing iat claim")
	}
}

func TestJWTVerifyWrongKey(t *testing.T) {
	key := generateTestKey(t)
	otherKey := generateTestKey(t)

	token, err := jwt.Generate("team", "key", key)
	if err != nil {
		t.Fatalf("generate jwt: %v", err)
	}

	if err := jwt.Verify(token, &otherKey.PublicKey); err == nil {
		t.Fatal("expected verification to fail with wrong key")
	}
}

func TestPayloadFormatting(t *testing.T) {
	badge := 3
	p := &Payload{
		Alert: Alert{
			Title:    "Claude Code needs approval",
			Subtitle: "test-mac",
			Body:     "Edit server.ts lines 40-45",
		},
		Sound:    "default",
		Badge:    &badge,
		Category: "APPROVAL_REQUEST",
		ThreadID: "session-123",
		Data: map[string]any{
			"session_id": "abc123",
		},
	}

	body, err := buildBody(p)
	if err != nil {
		t.Fatalf("build body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Check aps structure.
	aps, ok := result["aps"].(map[string]any)
	if !ok {
		t.Fatal("missing aps field")
	}

	alert, ok := aps["alert"].(map[string]any)
	if !ok {
		t.Fatal("missing alert in aps")
	}
	if alert["title"] != "Claude Code needs approval" {
		t.Errorf("title: got %v", alert["title"])
	}
	if alert["subtitle"] != "test-mac" {
		t.Errorf("subtitle: got %v", alert["subtitle"])
	}
	if alert["body"] != "Edit server.ts lines 40-45" {
		t.Errorf("body: got %v", alert["body"])
	}
	if aps["sound"] != "default" {
		t.Errorf("sound: got %v", aps["sound"])
	}
	if aps["badge"] != float64(3) {
		t.Errorf("badge: got %v", aps["badge"])
	}
	if aps["category"] != "APPROVAL_REQUEST" {
		t.Errorf("category: got %v", aps["category"])
	}
	if aps["thread-id"] != "session-123" {
		t.Errorf("thread-id: got %v", aps["thread-id"])
	}

	// Check extra data at top level.
	if result["session_id"] != "abc123" {
		t.Errorf("session_id: got %v", result["session_id"])
	}
}

func TestPayloadMinimal(t *testing.T) {
	p := &Payload{
		Alert: Alert{Title: "Hello"},
	}
	body, err := buildBody(p)
	if err != nil {
		t.Fatalf("build body: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	aps := result["aps"].(map[string]any)

	// Badge should be absent when nil.
	if _, ok := aps["badge"]; ok {
		t.Error("badge should be omitted when nil")
	}
}

func TestSendWithMockAPNs(t *testing.T) {
	key := generateTestKey(t)
	var requestCount atomic.Int32

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)

		// Verify request format.
		if r.Method != http.MethodPost {
			t.Errorf("method: got %s, want POST", r.Method)
		}
		if r.Header.Get("apns-topic") != "dev.canopy.app" {
			t.Errorf("apns-topic: got %s", r.Header.Get("apns-topic"))
		}
		if r.Header.Get("apns-push-type") != "alert" {
			t.Errorf("apns-push-type: got %s", r.Header.Get("apns-push-type"))
		}
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" || len(authHeader) < 8 {
			t.Error("missing or short authorization header")
		}

		// Verify body.
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("unmarshal body: %v", err)
		}
		if _, ok := payload["aps"]; !ok {
			t.Error("missing aps in request body")
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer mock.Close()

	client := newClientInternal("TEAM123", "KEY123", key, mock.URL, mock.Client())

	err := client.Send(context.Background(), "device-token-abc123", &Payload{
		Alert:    Alert{Title: "Test", Body: "Body"},
		Sound:    "default",
		Category: "TEST",
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if requestCount.Load() != 1 {
		t.Errorf("expected 1 request, got %d", requestCount.Load())
	}
}

func TestSendAPNsError(t *testing.T) {
	key := generateTestKey(t)

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"reason":"BadDeviceToken"}`))
	}))
	defer mock.Close()

	client := newClientInternal("TEAM123", "KEY123", key, mock.URL, mock.Client())

	err := client.Send(context.Background(), "bad-token", &Payload{
		Alert: Alert{Title: "Test"},
	})
	if err == nil {
		t.Fatal("expected error for bad token")
	}
	apnsErr, ok := err.(*APNsError)
	if !ok {
		t.Fatalf("expected APNsError, got %T", err)
	}
	if apnsErr.StatusCode != http.StatusBadRequest {
		t.Errorf("status: got %d, want %d", apnsErr.StatusCode, http.StatusBadRequest)
	}
}

func TestSendRetryTransient(t *testing.T) {
	key := generateTestKey(t)
	var requestCount atomic.Int32

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := requestCount.Add(1)
		if n <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"reason":"TooManyRequests"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer mock.Close()

	client := newClientInternal("TEAM123", "KEY123", key, mock.URL, mock.Client())

	err := client.Send(context.Background(), "token123", &Payload{
		Alert: Alert{Title: "Retry test"},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if requestCount.Load() != 3 {
		t.Errorf("expected 3 requests (2 retries + 1 success), got %d", requestCount.Load())
	}
}

func TestSendNoRetryOnClientError(t *testing.T) {
	key := generateTestKey(t)
	var requestCount atomic.Int32

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"reason":"InvalidProviderToken"}`))
	}))
	defer mock.Close()

	client := newClientInternal("TEAM123", "KEY123", key, mock.URL, mock.Client())

	err := client.Send(context.Background(), "token123", &Payload{
		Alert: Alert{Title: "No retry"},
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if requestCount.Load() != 1 {
		t.Errorf("expected 1 request (no retry on 403), got %d", requestCount.Load())
	}
}

func TestNewClientNilWhenEnvNotSet(t *testing.T) {
	t.Setenv("APNS_TEAM_ID", "")
	t.Setenv("APNS_KEY_ID", "")
	t.Setenv("APNS_KEY_PATH", "")

	client := NewClient()
	if client != nil {
		t.Fatal("expected nil client when env vars not set")
	}
}

func TestNewClientNilWhenKeyPathInvalid(t *testing.T) {
	t.Setenv("APNS_TEAM_ID", "TEAM123")
	t.Setenv("APNS_KEY_ID", "KEY123")
	t.Setenv("APNS_KEY_PATH", "/nonexistent/path/key.p8")

	client := NewClient()
	if client != nil {
		t.Fatal("expected nil client when key path invalid")
	}
}

func TestNewClientWithValidEnv(t *testing.T) {
	key := generateTestKey(t)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "AuthKey.p8")
	writeTestP8Key(t, keyPath, key)

	t.Setenv("APNS_TEAM_ID", "TEAM123")
	t.Setenv("APNS_KEY_ID", "KEY123")
	t.Setenv("APNS_KEY_PATH", keyPath)
	t.Setenv("APNS_ENVIRONMENT", "sandbox")

	client := NewClient()
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.baseURL != sandboxURL {
		t.Errorf("baseURL: got %s, want %s", client.baseURL, sandboxURL)
	}
}

func TestNewClientProduction(t *testing.T) {
	key := generateTestKey(t)

	dir := t.TempDir()
	keyPath := filepath.Join(dir, "AuthKey.p8")
	writeTestP8Key(t, keyPath, key)

	t.Setenv("APNS_TEAM_ID", "TEAM123")
	t.Setenv("APNS_KEY_ID", "KEY123")
	t.Setenv("APNS_KEY_PATH", keyPath)
	t.Setenv("APNS_ENVIRONMENT", "production")

	client := NewClient()
	if client == nil {
		t.Fatal("expected non-nil client")
	}
	if client.baseURL != productionURL {
		t.Errorf("baseURL: got %s, want %s", client.baseURL, productionURL)
	}
}

func TestTokenCaching(t *testing.T) {
	key := generateTestKey(t)
	var requestCount atomic.Int32

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer mock.Close()

	client := newClientInternal("TEAM123", "KEY123", key, mock.URL, mock.Client())

	// Send two requests; the JWT should be cached.
	for i := 0; i < 2; i++ {
		err := client.Send(context.Background(), "token123", &Payload{
			Alert: Alert{Title: "Cache test"},
		})
		if err != nil {
			t.Fatalf("send %d: %v", i, err)
		}
	}

	if client.cachedToken == "" {
		t.Error("expected cached token to be set")
	}
}

func writeTestP8Key(t *testing.T, path string, key *ecdsa.PrivateKey) {
	t.Helper()
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})
	if err := os.WriteFile(path, pemBytes, 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
}
