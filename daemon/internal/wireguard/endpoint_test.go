package wireguard

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestAssignIPNonZeroNon255(t *testing.T) {
	// Test many device IDs to verify the last octet is never 0 or 255.
	for i := 0; i < 100; i++ {
		id := DeviceID([]byte{byte(i), byte(i + 1), byte(i + 2)})
		ip := AssignIP(id)
		// Parse the last octet.
		var a, b, c, d int
		n, _ := fmt.Sscanf(ip, "%d.%d.%d.%d", &a, &b, &c, &d)
		if n != 4 {
			t.Fatalf("failed to parse IP %q", ip)
		}
		if d == 0 || d == 255 {
			t.Errorf("last octet should not be 0 or 255, got %d for ID %q", d, id)
		}
		if a != 100 || b != 100 {
			t.Errorf("IP should start with 100.100, got %s", ip)
		}
	}
}

func TestIsPortAvailable(t *testing.T) {
	// Port 0 should always be available (OS assigns random).
	// A very high port that's likely free.
	if !isPortAvailable(0) {
		t.Skip("port 0 not available, skip")
	}
}

func TestFindAvailablePort(t *testing.T) {
	port, err := findAvailablePort()
	if err != nil {
		t.Fatalf("findAvailablePort: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Errorf("invalid port: %d", port)
	}
}

func newTestEndpoint(t *testing.T) *Endpoint {
	t.Helper()
	priv, pub, err := GenerateWireGuardKeyPair()
	if err != nil {
		t.Fatalf("generate keys: %v", err)
	}
	peers := NewPeerStoreFromPath(filepath.Join(t.TempDir(), "devices.json"))
	logger := zap.NewNop()
	return NewEndpointDirect(priv, pub, "test-device", 0, peers, logger)
}

func TestNewEndpointDirect(t *testing.T) {
	ep := newTestEndpoint(t)

	if ep.DeviceID() != "test-device" {
		t.Errorf("DeviceID: got %q, want %q", ep.DeviceID(), "test-device")
	}

	// IP should be in 100.100.x.x range.
	if !strings.HasPrefix(ep.IP(), "100.100.") {
		t.Errorf("IP should start with 100.100., got %q", ep.IP())
	}

	// Public key should not be zero.
	var zero [32]byte
	if ep.PublicKey() == zero {
		t.Error("public key should not be zero")
	}
}

func TestEndpointStartStop(t *testing.T) {
	ep := newTestEndpoint(t)

	if ep.Running() {
		t.Error("endpoint should not be running before Start")
	}

	if err := ep.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	if !ep.Running() {
		t.Error("endpoint should be running after Start")
	}

	// Port should have been assigned by the OS (since we passed 0).
	if ep.Port() <= 0 || ep.Port() > 65535 {
		t.Errorf("invalid port after Start: %d", ep.Port())
	}

	ep.Stop()

	if ep.Running() {
		t.Error("endpoint should not be running after Stop")
	}
}

func TestEndpointStartBindsPort(t *testing.T) {
	ep := newTestEndpoint(t)
	if err := ep.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer ep.Stop()

	// The port should now be in use -- binding the same port should fail.
	if isPortAvailable(ep.Port()) {
		t.Error("port should be in use after Start")
	}
}

func TestEndpointStopReleasesPort(t *testing.T) {
	ep := newTestEndpoint(t)
	if err := ep.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	port := ep.Port()
	ep.Stop()

	// Port should be available again after Stop.
	if !isPortAvailable(port) {
		t.Error("port should be available after Stop")
	}
}

func TestEndpointDoubleStop(t *testing.T) {
	ep := newTestEndpoint(t)
	if err := ep.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ep.Stop()
	// Second stop should not panic.
	ep.Stop()
}

func TestEndpointAddPeer(t *testing.T) {
	ep := newTestEndpoint(t)
	peer := &PeerConfig{
		DeviceID:  "phone-1",
		Name:      "Test iPhone",
		WGPubKey:  "abc123",
		AllowedIP: "100.100.1.1/32",
	}

	if err := ep.AddPeer(peer); err != nil {
		t.Fatalf("AddPeer: %v", err)
	}

	// Verify the peer is in the store.
	got := ep.peers.Get("phone-1")
	if got == nil {
		t.Fatal("peer should exist after AddPeer")
	}
	if got.Name != "Test iPhone" {
		t.Errorf("peer name: got %q", got.Name)
	}
}

func TestEndpointRemovePeer(t *testing.T) {
	ep := newTestEndpoint(t)
	ep.AddPeer(&PeerConfig{DeviceID: "phone-1", Name: "Test"})

	if err := ep.RemovePeer("phone-1"); err != nil {
		t.Fatalf("RemovePeer: %v", err)
	}

	if ep.peers.Get("phone-1") != nil {
		t.Error("peer should not exist after RemovePeer")
	}
}

func TestEndpointRemovePeerNonexistent(t *testing.T) {
	ep := newTestEndpoint(t)
	// Removing a non-existent peer should not error (it's a no-op delete).
	if err := ep.RemovePeer("nonexistent"); err != nil {
		t.Fatalf("RemovePeer nonexistent: %v", err)
	}
}

func TestEndpointAPIListenAddr(t *testing.T) {
	ep := newTestEndpoint(t)
	addr := ep.APIListenAddr(8080)
	if addr != ":8080" {
		t.Errorf("APIListenAddr: got %q, want %q", addr, ":8080")
	}
}

func TestEndpointIPDeterministic(t *testing.T) {
	priv, pub, _ := GenerateWireGuardKeyPair()
	peers := NewPeerStoreFromPath(filepath.Join(t.TempDir(), "d.json"))
	logger := zap.NewNop()

	ep1 := NewEndpointDirect(priv, pub, "same-device", 0, peers, logger)
	ep2 := NewEndpointDirect(priv, pub, "same-device", 0, peers, logger)

	if ep1.IP() != ep2.IP() {
		t.Errorf("same device ID should produce same IP: %q vs %q", ep1.IP(), ep2.IP())
	}
}
