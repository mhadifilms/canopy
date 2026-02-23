package coord

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.uber.org/zap"
)

func testKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func testWGKey(t *testing.T) []byte {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return key
}

func TestClient_Checkin(t *testing.T) {
	pub, priv := testKeys(t)
	wgKey := testWGKey(t)

	var gotRequest map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/checkin" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}

		json.NewDecoder(r.Body).Decode(&gotRequest)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "device_id": "test1234"})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		CoordURL:     server.URL,
		IdentityPub:  pub,
		IdentityPriv: priv,
		WGPubKey:     wgKey,
		WGPort:       51820,
		PairedWGKeys: []string{"peer1_b64"},
	}, zap.NewNop())

	endpoints := []Endpoint{
		{IP: "203.0.113.42", Port: 51820, Type: "public"},
		{IP: "192.168.1.100", Port: 51820, Type: "local"},
	}

	err := client.checkin(context.Background(), endpoints)
	if err != nil {
		t.Fatalf("checkin failed: %v", err)
	}

	// Verify request fields.
	if gotRequest["device_key"] != base64.StdEncoding.EncodeToString(pub) {
		t.Error("device_key mismatch")
	}
	if gotRequest["wg_public_key"] != base64.StdEncoding.EncodeToString(wgKey) {
		t.Error("wg_public_key mismatch")
	}
	if gotRequest["sig"] == nil || gotRequest["sig"] == "" {
		t.Error("sig should not be empty")
	}
	if gotRequest["timestamp"] == nil || gotRequest["timestamp"] == "" {
		t.Error("timestamp should not be empty")
	}
}

func TestClient_CheckinFailure(t *testing.T) {
	pub, priv := testKeys(t)
	wgKey := testWGKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		json.NewEncoder(w).Encode(map[string]string{"error": "rate_limited"})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		CoordURL:     server.URL,
		IdentityPub:  pub,
		IdentityPriv: priv,
		WGPubKey:     wgKey,
		WGPort:       51820,
	}, zap.NewNop())

	err := client.checkin(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for rate-limited checkin")
	}
}

func TestClient_LookupEndpoints(t *testing.T) {
	pub, priv := testKeys(t)
	wgKey := testWGKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/endpoints" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != "GET" {
			t.Errorf("expected GET, got %s", r.Method)
		}

		// Verify bearer token is present.
		auth := r.Header.Get("Authorization")
		if auth == "" {
			t.Error("missing Authorization header")
		}

		peerKey := r.URL.Query().Get("peer_wg_key")
		if peerKey == "" {
			t.Error("missing peer_wg_key query param")
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"endpoints": []Endpoint{
				{IP: "203.0.113.42", Port: 51820, Type: "public"},
			},
			"online": true,
		})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		CoordURL:     server.URL,
		IdentityPub:  pub,
		IdentityPriv: priv,
		WGPubKey:     wgKey,
		WGPort:       51820,
	}, zap.NewNop())

	endpoints, online, err := client.LookupEndpoints(context.Background(), "peer_wg_key_b64")
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if !online {
		t.Error("expected online=true")
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected 1 endpoint, got %d", len(endpoints))
	}
	if endpoints[0].IP != "203.0.113.42" {
		t.Errorf("unexpected IP: %s", endpoints[0].IP)
	}
}

func TestClient_LookupEndpoints_Forbidden(t *testing.T) {
	pub, priv := testKeys(t)
	wgKey := testWGKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "not_paired"})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		CoordURL:     server.URL,
		IdentityPub:  pub,
		IdentityPriv: priv,
		WGPubKey:     wgKey,
		WGPort:       51820,
	}, zap.NewNop())

	_, _, err := client.LookupEndpoints(context.Background(), "unknown_peer")
	if err == nil {
		t.Fatal("expected error for forbidden lookup")
	}
}

func TestClient_RegisterPairing(t *testing.T) {
	pub, priv := testKeys(t)
	wgKey := testWGKey(t)

	var gotRequest map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/register_pairing" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotRequest)
		json.NewEncoder(w).Encode(map[string]bool{"ok": true})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		CoordURL:     server.URL,
		IdentityPub:  pub,
		IdentityPriv: priv,
		WGPubKey:     wgKey,
		WGPort:       51820,
	}, zap.NewNop())

	err := client.RegisterPairing(context.Background(), "peer_wg_key_b64")
	if err != nil {
		t.Fatalf("register pairing failed: %v", err)
	}

	if gotRequest["device_key"] != base64.StdEncoding.EncodeToString(pub) {
		t.Error("device_key mismatch")
	}
	if gotRequest["peer_wg_key"] != "peer_wg_key_b64" {
		t.Error("peer_wg_key mismatch")
	}
	if gotRequest["sig"] == "" {
		t.Error("sig should not be empty")
	}
}

func TestClient_SendPush(t *testing.T) {
	pub, priv := testKeys(t)
	wgKey := testWGKey(t)

	var gotRequest map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/push" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewDecoder(r.Body).Decode(&gotRequest)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "sent": 1})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		CoordURL:     server.URL,
		IdentityPub:  pub,
		IdentityPriv: priv,
		WGPubKey:     wgKey,
		WGPort:       51820,
	}, zap.NewNop())

	targets := []PushTarget{
		{
			APNSToken: "abc123def456",
			Notification: PushNotification{
				Title:    "Test notification",
				Subtitle: "test-mac",
				Body:     "This is a test",
				Category: "APPROVAL_REQUEST",
				ThreadID: "session-123",
				Data:     map[string]string{"session_id": "session-123"},
			},
		},
	}

	err := client.SendPush(context.Background(), targets)
	if err != nil {
		t.Fatalf("send push failed: %v", err)
	}

	if gotRequest["device_key"] != base64.StdEncoding.EncodeToString(pub) {
		t.Error("device_key mismatch")
	}
	if gotRequest["sig"] == "" {
		t.Error("sig should not be empty")
	}
}

func TestClient_SendPushEmpty(t *testing.T) {
	pub, priv := testKeys(t)
	wgKey := testWGKey(t)

	client := NewClient(ClientConfig{
		CoordURL:     "http://unused",
		IdentityPub:  pub,
		IdentityPriv: priv,
		WGPubKey:     wgKey,
		WGPort:       51820,
	}, zap.NewNop())

	// Sending with no targets should be a no-op.
	err := client.SendPush(context.Background(), nil)
	if err != nil {
		t.Fatalf("send push with nil targets should succeed: %v", err)
	}
}

func TestClient_StartStop(t *testing.T) {
	pub, priv := testKeys(t)
	wgKey := testWGKey(t)

	checkinCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkinCount++
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true})
	}))
	defer server.Close()

	client := NewClient(ClientConfig{
		CoordURL:     server.URL,
		IdentityPub:  pub,
		IdentityPriv: priv,
		WGPubKey:     wgKey,
		WGPort:       51820,
	}, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	client.Start(ctx)

	// Wait for initial check-in.
	time.Sleep(200 * time.Millisecond)

	client.Stop()
	cancel()

	// Should have done at least 1 check-in (the initial one).
	if checkinCount < 1 {
		t.Errorf("expected at least 1 checkin, got %d", checkinCount)
	}
}

func TestClient_SetPairedWGKeys(t *testing.T) {
	pub, priv := testKeys(t)
	wgKey := testWGKey(t)

	client := NewClient(ClientConfig{
		CoordURL:     "http://unused",
		IdentityPub:  pub,
		IdentityPriv: priv,
		WGPubKey:     wgKey,
		WGPort:       51820,
	}, zap.NewNop())

	client.SetPairedWGKeys([]string{"key1", "key2"})

	client.mu.RLock()
	defer client.mu.RUnlock()
	if len(client.pairedWGKeys) != 2 {
		t.Errorf("expected 2 paired keys, got %d", len(client.pairedWGKeys))
	}
}

func TestClient_BuildEndpoints(t *testing.T) {
	pub, priv := testKeys(t)
	wgKey := testWGKey(t)

	client := NewClient(ClientConfig{
		CoordURL:     "http://unused",
		IdentityPub:  pub,
		IdentityPriv: priv,
		WGPubKey:     wgKey,
		WGPort:       51820,
	}, zap.NewNop())

	pubEP := &Endpoint{IP: "203.0.113.1", Port: 51820, Type: "public"}
	endpoints := client.buildEndpoints(pubEP)

	// Should have at least the public endpoint.
	if len(endpoints) < 1 {
		t.Fatal("expected at least 1 endpoint")
	}
	if endpoints[0].Type != "public" {
		t.Errorf("first endpoint should be public, got %s", endpoints[0].Type)
	}
}

func TestClient_BuildEndpointsNoPublic(t *testing.T) {
	pub, priv := testKeys(t)
	wgKey := testWGKey(t)

	client := NewClient(ClientConfig{
		CoordURL:     "http://unused",
		IdentityPub:  pub,
		IdentityPriv: priv,
		WGPubKey:     wgKey,
		WGPort:       51820,
	}, zap.NewNop())

	// No public endpoint (STUN failed).
	endpoints := client.buildEndpoints(nil)

	// Should still have local endpoints (unless running in a CI container with no network).
	// Just verify it doesn't panic.
	_ = endpoints
}

func TestSTUNRoundtrip(t *testing.T) {
	// Start a minimal STUN server.
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	serverAddr := conn.LocalAddr().(*net.UDPAddr)

	// Server goroutine: read request, send response.
	go func() {
		buf := make([]byte, 1500)
		n, addr, err := conn.ReadFrom(buf)
		if err != nil {
			return
		}
		if n < stunHeaderSize {
			return
		}

		// Extract txn ID.
		var txnID [12]byte
		copy(txnID[:], buf[8:20])

		// Build response with the client's address.
		resp := buildTestSTUNResponse(txnID, addr.(*net.UDPAddr))
		conn.WriteTo(resp, addr)
	}()

	// Client: send STUN request and parse response.
	clientConn, err := net.DialUDP("udp4", nil, serverAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer clientConn.Close()
	clientConn.SetDeadline(time.Now().Add(2 * time.Second))

	var txnID [12]byte
	rand.Read(txnID[:])
	reqData := buildSTUNBindingRequest(txnID)

	if _, err := clientConn.Write(reqData); err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 1500)
	n, err := clientConn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}

	ip, port, err := parseSTUNBindingResponse(buf[:n])
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}

	if ip == nil {
		t.Fatal("expected non-nil IP")
	}
	if port == 0 {
		t.Fatal("expected non-zero port")
	}
}

// buildTestSTUNResponse constructs a STUN Binding Response for testing.
func buildTestSTUNResponse(txnID [12]byte, addr *net.UDPAddr) []byte {
	ip4 := addr.IP.To4()
	if ip4 == nil {
		return nil
	}

	xorPort := uint16(addr.Port) ^ uint16(stunMagicCookie>>16)
	var xorIP [4]byte
	var magicBytes [4]byte
	binary.BigEndian.PutUint32(magicBytes[:], stunMagicCookie)
	for i := 0; i < 4; i++ {
		xorIP[i] = ip4[i] ^ magicBytes[i]
	}

	attr := make([]byte, 12)
	binary.BigEndian.PutUint16(attr[0:2], stunAttrXORMapped)
	binary.BigEndian.PutUint16(attr[2:4], 8)
	attr[4] = 0x00
	attr[5] = 0x01
	binary.BigEndian.PutUint16(attr[6:8], xorPort)
	copy(attr[8:12], xorIP[:])

	resp := make([]byte, stunHeaderSize+len(attr))
	binary.BigEndian.PutUint16(resp[0:2], stunBindingResponse)
	binary.BigEndian.PutUint16(resp[2:4], uint16(len(attr)))
	binary.BigEndian.PutUint32(resp[4:8], stunMagicCookie)
	copy(resp[8:20], txnID[:])
	copy(resp[20:], attr)

	return resp
}

func TestGetLocalIPs(t *testing.T) {
	ips := getLocalIPs()
	// We should get at least the loopback... actually loopback is excluded.
	// In CI we may or may not have network interfaces. Just verify no panic.
	t.Logf("found %d local IPs: %v", len(ips), ips)
}

func TestParseSTUNBindingResponse_Invalid(t *testing.T) {
	// Too short.
	_, _, err := parseSTUNBindingResponse([]byte{0x01})
	if err == nil {
		t.Error("expected error for short data")
	}

	// Wrong message type.
	data := make([]byte, stunHeaderSize)
	binary.BigEndian.PutUint16(data[0:2], 0x0001) // request, not response
	_, _, err = parseSTUNBindingResponse(data)
	if err == nil {
		t.Error("expected error for wrong message type")
	}

	// Valid header but no attributes.
	data2 := make([]byte, stunHeaderSize)
	binary.BigEndian.PutUint16(data2[0:2], stunBindingResponse)
	binary.BigEndian.PutUint16(data2[2:4], 0)
	binary.BigEndian.PutUint32(data2[4:8], stunMagicCookie)
	_, _, err = parseSTUNBindingResponse(data2)
	if err == nil {
		t.Error("expected error for missing XOR-MAPPED-ADDRESS")
	}
}
