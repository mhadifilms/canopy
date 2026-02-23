package api

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"go.uber.org/zap"

	"github.com/canopy-dev/coord/internal/store"
)

type testDevice struct {
	pub     ed25519.PublicKey
	priv    ed25519.PrivateKey
	pubB64  string
	wgPub   string // fake WG key (base64)
}

func newTestDevice(t *testing.T) testDevice {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	// Generate a fake WG key.
	wgKey := make([]byte, 32)
	rand.Read(wgKey)

	return testDevice{
		pub:    pub,
		priv:   priv,
		pubB64: base64.StdEncoding.EncodeToString(pub),
		wgPub:  base64.StdEncoding.EncodeToString(wgKey),
	}
}

func (d testDevice) sign(msg []byte) string {
	return base64.StdEncoding.EncodeToString(ed25519.Sign(d.priv, msg))
}

func (d testDevice) bearerToken() string {
	sig := ed25519.Sign(d.priv, d.pub)
	return d.pubB64 + ":" + base64.StdEncoding.EncodeToString(sig)
}

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()
	s := store.New()
	logger := zap.NewNop()
	return New(s, logger), s
}

func TestCheckin(t *testing.T) {
	srv, _ := newTestServer(t)
	dev := newTestDevice(t)

	ts := time.Now().UTC().Format(time.RFC3339)
	signedMsg := dev.pubB64 + dev.wgPub + ts

	body := CheckinRequest{
		DeviceKey:   dev.pubB64,
		WGPublicKey: dev.wgPub,
		Endpoints: []store.Endpoint{
			{IP: "203.0.113.42", Port: 51820, Type: "public"},
			{IP: "192.168.1.100", Port: 51820, Type: "local"},
		},
		PairedDevices: []string{},
		Timestamp:     ts,
		Sig:           dev.sign([]byte(signedMsg)),
	}

	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/checkin", bytes.NewReader(bodyJSON))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp CheckinResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !resp.OK {
		t.Fatal("expected ok: true")
	}
	if resp.DeviceID == "" {
		t.Fatal("expected non-empty device_id")
	}
}

func TestCheckinBadSignature(t *testing.T) {
	srv, _ := newTestServer(t)
	dev := newTestDevice(t)
	other := newTestDevice(t)

	ts := time.Now().UTC().Format(time.RFC3339)
	signedMsg := dev.pubB64 + dev.wgPub + ts

	body := CheckinRequest{
		DeviceKey:   dev.pubB64,
		WGPublicKey: dev.wgPub,
		Timestamp:   ts,
		Sig:         other.sign([]byte(signedMsg)), // Wrong signer
	}

	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/checkin", bytes.NewReader(bodyJSON))
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestCheckinMissingFields(t *testing.T) {
	srv, _ := newTestServer(t)

	body := CheckinRequest{DeviceKey: ""}
	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/checkin", bytes.NewReader(bodyJSON))
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestEndpoints(t *testing.T) {
	srv, st := newTestServer(t)
	devA := newTestDevice(t)
	devB := newTestDevice(t)

	// Register device B with some endpoints.
	st.Checkin(devB.pubB64, devB.wgPub, []store.Endpoint{
		{IP: "10.0.0.1", Port: 51820, Type: "public"},
	}, nil, nil)

	// Register device A with B as paired.
	st.Checkin(devA.pubB64, devA.wgPub, nil, []string{devB.wgPub}, nil)

	// A looks up B's endpoints.
	endpointURL := "/v1/endpoints?" + url.Values{"peer_wg_key": {devB.wgPub}}.Encode()
	req := httptest.NewRequest("GET", endpointURL, nil)
	req.Header.Set("Authorization", "Bearer "+devA.bearerToken())
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp EndpointsResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Endpoints) != 1 {
		t.Fatalf("endpoints: got %d, want 1", len(resp.Endpoints))
	}
	if !resp.Online {
		t.Fatal("expected online=true")
	}
}

func TestEndpointsNotPaired(t *testing.T) {
	srv, st := newTestServer(t)
	devA := newTestDevice(t)
	devB := newTestDevice(t)

	st.Checkin(devB.pubB64, devB.wgPub, nil, nil, nil)
	st.Checkin(devA.pubB64, devA.wgPub, nil, nil, nil) // A not paired with B

	req := httptest.NewRequest("GET", "/v1/endpoints?"+url.Values{"peer_wg_key": {devB.wgPub}}.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+devA.bearerToken())
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestEndpointsUnknownPeer(t *testing.T) {
	srv, st := newTestServer(t)
	devA := newTestDevice(t)

	fakeWG := base64.StdEncoding.EncodeToString(make([]byte, 32))

	// A claims to be paired.
	st.Checkin(devA.pubB64, devA.wgPub, nil, []string{fakeWG}, nil)

	req := httptest.NewRequest("GET", "/v1/endpoints?"+url.Values{"peer_wg_key": {fakeWG}}.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+devA.bearerToken())
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp EndpointsResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Endpoints) != 0 {
		t.Fatalf("expected 0 endpoints, got %d", len(resp.Endpoints))
	}
	if resp.Online {
		t.Fatal("expected online=false")
	}
}

func TestEndpointsMissingAuth(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/v1/endpoints?peer_wg_key=abc", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestRegisterPairing(t *testing.T) {
	srv, st := newTestServer(t)
	dev := newTestDevice(t)
	peerWG := base64.StdEncoding.EncodeToString(make([]byte, 32))

	signedMsg := dev.pubB64 + peerWG
	body := RegisterPairingRequest{
		DeviceKey: dev.pubB64,
		PeerWGKey: peerWG,
		Sig:       dev.sign([]byte(signedMsg)),
	}

	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/register_pairing", bytes.NewReader(bodyJSON))
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	// Verify it was stored.
	if !st.IsPaired(dev.pubB64, peerWG) {
		t.Fatal("pairing should be registered")
	}
}

func TestRegisterPairingBadSignature(t *testing.T) {
	srv, _ := newTestServer(t)
	dev := newTestDevice(t)
	other := newTestDevice(t)
	peerWG := base64.StdEncoding.EncodeToString(make([]byte, 32))

	signedMsg := dev.pubB64 + peerWG
	body := RegisterPairingRequest{
		DeviceKey: dev.pubB64,
		PeerWGKey: peerWG,
		Sig:       other.sign([]byte(signedMsg)), // wrong signer
	}

	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/register_pairing", bytes.NewReader(bodyJSON))
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestPush(t *testing.T) {
	srv, _ := newTestServer(t)
	dev := newTestDevice(t)

	body := PushRequest{
		DeviceKey: dev.pubB64,
		Sig:       dev.sign([]byte(dev.pubB64)),
		Targets: []PushTarget{
			{
				APNSToken: "abcdef1234567890abcdef1234567890",
				Notification: Notification{
					Title:    "Claude Code needs approval",
					Subtitle: "test-mac",
					Body:     "Edit server.ts lines 40-45",
					Category: "APPROVAL_REQUEST",
				},
			},
		},
	}

	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/push", bytes.NewReader(bodyJSON))
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["ok"] != true {
		t.Fatal("expected ok: true")
	}
}

func TestPushBadSignature(t *testing.T) {
	srv, _ := newTestServer(t)
	dev := newTestDevice(t)
	other := newTestDevice(t)

	body := PushRequest{
		DeviceKey: dev.pubB64,
		Sig:       other.sign([]byte(dev.pubB64)),
		Targets:   []PushTarget{{APNSToken: "token123456789012345678901234", Notification: Notification{Title: "t"}}},
	}

	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/push", bytes.NewReader(bodyJSON))
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestPushNoTargets(t *testing.T) {
	srv, _ := newTestServer(t)
	dev := newTestDevice(t)

	body := PushRequest{
		DeviceKey: dev.pubB64,
		Sig:       dev.sign([]byte(dev.pubB64)),
		Targets:   []PushTarget{},
	}

	bodyJSON, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/v1/push", bytes.NewReader(bodyJSON))
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHealth(t *testing.T) {
	srv, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusOK)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	if resp["status"] != "ok" {
		t.Fatalf("status: got %v, want ok", resp["status"])
	}
}

func TestEndpointsMissingParam(t *testing.T) {
	srv, _ := newTestServer(t)
	dev := newTestDevice(t)

	req := httptest.NewRequest("GET", "/v1/endpoints", nil)
	req.Header.Set("Authorization", "Bearer "+dev.bearerToken())
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status: got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// TestEndpointsWithRegisteredPairing tests that pairing registered via /v1/register_pairing works for lookups.
func TestEndpointsWithRegisteredPairing(t *testing.T) {
	srv, st := newTestServer(t)
	devA := newTestDevice(t)
	devB := newTestDevice(t)

	// B checks in.
	st.Checkin(devB.pubB64, devB.wgPub, []store.Endpoint{
		{IP: "10.0.0.1", Port: 51820, Type: "public"},
	}, nil, nil)

	// A checks in (no paired_devices list).
	st.Checkin(devA.pubB64, devA.wgPub, nil, nil, nil)

	// A registers pairing with B.
	st.RegisterPairing(devA.pubB64, devB.wgPub)

	// A looks up B.
	req := httptest.NewRequest("GET", "/v1/endpoints?"+url.Values{"peer_wg_key": {devB.wgPub}}.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+devA.bearerToken())
	w := httptest.NewRecorder()

	srv.Handler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status: got %d, want %d, body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp EndpointsResponse
	json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Endpoints) != 1 {
		t.Fatalf("endpoints: got %d, want 1", len(resp.Endpoints))
	}
}
