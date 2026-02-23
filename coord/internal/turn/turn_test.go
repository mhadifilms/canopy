package turn

import (
	"net"
	"testing"
	"time"

	"go.uber.org/zap"
)

// mockChecker always allows relaying between any devices.
type mockChecker struct {
	allowed map[string]map[string]bool
}

func newMockChecker() *mockChecker {
	return &mockChecker{allowed: make(map[string]map[string]bool)}
}

func (m *mockChecker) Allow(deviceKey, peerKey string) {
	if m.allowed[deviceKey] == nil {
		m.allowed[deviceKey] = make(map[string]bool)
	}
	m.allowed[deviceKey][peerKey] = true
}

func (m *mockChecker) CanRelay(deviceKey, peerKey string) bool {
	if m.allowed[deviceKey] != nil && m.allowed[deviceKey][peerKey] {
		return true
	}
	return false
}

func startRelay(t *testing.T, checker PairingChecker) *Relay {
	t.Helper()
	logger := zap.NewNop()
	r := New(checker, logger)
	if err := r.ListenAndServe("127.0.0.1:0"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func dialRelay(t *testing.T, relayAddr net.Addr) *net.UDPConn {
	t.Helper()
	udpAddr := relayAddr.(*net.UDPAddr)
	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestAllocateAndRelease(t *testing.T) {
	checker := newMockChecker()
	checker.Allow("device-a", "peer-b")
	relay := startRelay(t, checker)

	client := dialRelay(t, relay.Addr())

	// Send allocate request.
	msg := AllocateRequest("device-a", "peer-b")
	if _, err := client.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read response.
	buf := make([]byte, 1500)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	allocID, expiresUnix, err := ParseAllocateResponse(buf[:n])
	if err != nil {
		t.Fatalf("parse allocate response: %v", err)
	}
	if allocID == 0 {
		t.Fatal("expected non-zero allocation ID")
	}
	if expiresUnix == 0 {
		t.Fatal("expected non-zero expiry")
	}

	// Verify stats.
	stats := relay.Stats()
	if stats.ActiveAllocations != 1 {
		t.Fatalf("active allocations: got %d, want 1", stats.ActiveAllocations)
	}

	// Release the allocation.
	releaseMsg := ReleaseRequest(allocID)
	if _, err := client.Write(releaseMsg); err != nil {
		t.Fatalf("write release: %v", err)
	}

	n, err = client.Read(buf)
	if err != nil {
		t.Fatalf("read release response: %v", err)
	}
	if n < 1 || buf[0] != RespOK {
		t.Fatalf("expected OK response, got %v", buf[:n])
	}

	// Verify allocation was removed.
	time.Sleep(10 * time.Millisecond)
	stats = relay.Stats()
	if stats.ActiveAllocations != 0 {
		t.Fatalf("active allocations after release: got %d, want 0", stats.ActiveAllocations)
	}
}

func TestAllocateUnpaired(t *testing.T) {
	checker := newMockChecker()
	// No pairing registered.
	relay := startRelay(t, checker)
	client := dialRelay(t, relay.Addr())

	msg := AllocateRequest("device-a", "peer-b")
	client.Write(msg)

	buf := make([]byte, 1500)
	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if buf[0] != RespError {
		t.Fatalf("expected error response, got %d", buf[0])
	}
	errMsg := string(buf[1:n])
	if errMsg != "not_paired" {
		t.Fatalf("error: got %q, want %q", errMsg, "not_paired")
	}
}

func TestAllocateMaxLimit(t *testing.T) {
	checker := newMockChecker()
	deviceKey := "device-a"
	peerKey := "peer-b"
	checker.Allow(deviceKey, peerKey)

	relay := startRelay(t, checker)

	// Create MaxAllocationsPerDevice allocations.
	clients := make([]*net.UDPConn, 0)
	for i := 0; i < MaxAllocationsPerDevice; i++ {
		c := dialRelay(t, relay.Addr())
		clients = append(clients, c)

		msg := AllocateRequest(deviceKey, peerKey)
		c.Write(msg)

		buf := make([]byte, 1500)
		n, err := c.Read(buf)
		if err != nil {
			t.Fatalf("alloc %d read: %v", i, err)
		}
		if buf[0] != RespOK {
			t.Fatalf("alloc %d: expected OK, got %d (%s)", i, buf[0], string(buf[1:n]))
		}
	}

	// Next allocation should fail.
	extraClient := dialRelay(t, relay.Addr())
	msg := AllocateRequest(deviceKey, peerKey)
	extraClient.Write(msg)

	buf := make([]byte, 1500)
	n, err := extraClient.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if buf[0] != RespError {
		t.Fatalf("expected error, got %d", buf[0])
	}
	if string(buf[1:n]) != "max_allocations" {
		t.Fatalf("error: got %q, want %q", string(buf[1:n]), "max_allocations")
	}
}

func TestRefresh(t *testing.T) {
	checker := newMockChecker()
	checker.Allow("device-a", "peer-b")
	relay := startRelay(t, checker)
	client := dialRelay(t, relay.Addr())

	// Allocate.
	msg := AllocateRequest("device-a", "peer-b")
	client.Write(msg)

	buf := make([]byte, 1500)
	n, _ := client.Read(buf)
	allocID, _, _ := ParseAllocateResponse(buf[:n])

	// Refresh.
	refreshMsg := RefreshRequest(allocID)
	client.Write(refreshMsg)

	n, err := client.Read(buf)
	if err != nil {
		t.Fatalf("read refresh: %v", err)
	}

	newAllocID, newExpiry, err := ParseAllocateResponse(buf[:n])
	if err != nil {
		t.Fatalf("parse refresh: %v", err)
	}
	if newAllocID != allocID {
		t.Fatalf("allocation ID changed: got %d, want %d", newAllocID, allocID)
	}
	if newExpiry == 0 {
		t.Fatal("expected non-zero new expiry")
	}
}

func TestDataRelay(t *testing.T) {
	checker := newMockChecker()
	deviceKey := "device-a"
	peerKey := "peer-b"
	checker.Allow(deviceKey, peerKey)

	relay := startRelay(t, checker)

	// Device A allocates.
	deviceConn := dialRelay(t, relay.Addr())
	msg := AllocateRequest(deviceKey, peerKey)
	deviceConn.Write(msg)

	buf := make([]byte, 1500)
	n, _ := deviceConn.Read(buf)
	allocID, _, err := ParseAllocateResponse(buf[:n])
	if err != nil {
		t.Fatalf("parse alloc: %v", err)
	}

	// Peer B connects and sends data (this registers the peer address).
	peerConn := dialRelay(t, relay.Addr())
	peerData := DataMessage(allocID, []byte("hello from peer"))
	peerConn.Write(peerData)

	// Give a moment for the relay to process.
	time.Sleep(50 * time.Millisecond)

	// Device A sends data to peer B through the relay.
	deviceData := DataMessage(allocID, []byte("hello from device"))
	deviceConn.Write(deviceData)

	// Peer B should receive the relayed data.
	peerConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err = peerConn.Read(buf)
	if err != nil {
		t.Fatalf("peer read: %v", err)
	}

	relayedAllocID, payload, err := ParseChannelData(buf[:n])
	if err != nil {
		t.Fatalf("parse channel data: %v", err)
	}
	if relayedAllocID != allocID {
		t.Fatalf("allocation ID: got %d, want %d", relayedAllocID, allocID)
	}
	if string(payload) != "hello from device" {
		t.Fatalf("payload: got %q, want %q", string(payload), "hello from device")
	}

	// Device should have received the first peer message ("hello from peer") relayed to it.
	deviceConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err = deviceConn.Read(buf)
	if err != nil {
		t.Fatalf("device read first peer msg: %v", err)
	}
	relayedAllocID, payload, err = ParseChannelData(buf[:n])
	if err != nil {
		t.Fatalf("parse first peer msg: %v", err)
	}
	if string(payload) != "hello from peer" {
		t.Fatalf("first peer payload: got %q, want %q", string(payload), "hello from peer")
	}

	// Peer sends another message.
	peerData2 := DataMessage(allocID, []byte("more from peer"))
	peerConn.Write(peerData2)

	deviceConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	n, err = deviceConn.Read(buf)
	if err != nil {
		t.Fatalf("device read second peer msg: %v", err)
	}

	relayedAllocID, payload, err = ParseChannelData(buf[:n])
	if err != nil {
		t.Fatalf("parse channel data from peer: %v", err)
	}
	if string(payload) != "more from peer" {
		t.Fatalf("payload: got %q, want %q", string(payload), "more from peer")
	}

	// Verify bytes relayed.
	stats := relay.Stats()
	if stats.TotalBytesRelayed == 0 {
		t.Fatal("expected non-zero bytes relayed")
	}
}

func TestExpiry(t *testing.T) {
	checker := newMockChecker()
	checker.Allow("device-a", "peer-b")

	logger := zap.NewNop()
	relay := New(checker, logger)
	if err := relay.ListenAndServe("127.0.0.1:0"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer relay.Close()

	client := dialRelay(t, relay.Addr())

	// Allocate.
	msg := AllocateRequest("device-a", "peer-b")
	client.Write(msg)

	buf := make([]byte, 1500)
	n, _ := client.Read(buf)
	allocID, _, _ := ParseAllocateResponse(buf[:n])

	// Manually expire the allocation.
	relay.mu.Lock()
	alloc := relay.allocations[allocID]
	if alloc != nil {
		alloc.ExpiresAt = time.Now().Add(-1 * time.Second)
	}
	relay.mu.Unlock()

	// Trigger expiry.
	relay.expireAllocations()

	stats := relay.Stats()
	if stats.ActiveAllocations != 0 {
		t.Fatalf("expected 0 active allocations after expiry, got %d", stats.ActiveAllocations)
	}
}

func TestParseHelpers(t *testing.T) {
	// ParseAllocateResponse with empty data.
	_, _, err := ParseAllocateResponse(nil)
	if err == nil {
		t.Fatal("expected error for nil")
	}

	// ParseAllocateResponse with error.
	errData := append([]byte{RespError}, []byte("test_error")...)
	_, _, err = ParseAllocateResponse(errData)
	if err == nil {
		t.Fatal("expected error for error response")
	}

	// ParseChannelData too short.
	_, _, err = ParseChannelData([]byte{0x05, 0x00})
	if err == nil {
		t.Fatal("expected error for short channel data")
	}

	// ParseChannelData wrong type.
	_, _, err = ParseChannelData([]byte{0xFF, 0x00, 0x00, 0x00, 0x01})
	if err == nil {
		t.Fatal("expected error for wrong type")
	}
}
