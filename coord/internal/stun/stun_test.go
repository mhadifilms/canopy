package stun

import (
	"crypto/rand"
	"net"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestSTUNServer(t *testing.T) {
	logger := zap.NewNop()
	srv := New(logger)

	if err := srv.ListenAndServe("127.0.0.1:0"); err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer srv.Close()

	srvAddr := srv.Addr().(*net.UDPAddr)

	// Create a client UDP socket.
	conn, err := net.DialUDP("udp", nil, srvAddr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Build and send a binding request.
	var txnID [12]byte
	if _, err := rand.Read(txnID[:]); err != nil {
		t.Fatalf("rand: %v", err)
	}
	req := BuildBindingRequest(txnID)

	if _, err := conn.Write(req); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read response.
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// Parse response.
	ip, port, err := ParseBindingResponse(buf[:n])
	if err != nil {
		t.Fatalf("parse response: %v", err)
	}

	// The response should reflect our local address.
	localAddr := conn.LocalAddr().(*net.UDPAddr)

	if !ip.Equal(localAddr.IP) {
		t.Fatalf("ip: got %v, want %v", ip, localAddr.IP)
	}
	if port != localAddr.Port {
		t.Fatalf("port: got %d, want %d", port, localAddr.Port)
	}
}

func TestBuildBindingRequest(t *testing.T) {
	var txnID [12]byte
	for i := range txnID {
		txnID[i] = byte(i)
	}

	req := BuildBindingRequest(txnID)
	if len(req) != headerSize {
		t.Fatalf("request size: got %d, want %d", len(req), headerSize)
	}

	// Verify type is binding request.
	msgType := uint16(req[0])<<8 | uint16(req[1])
	if msgType != bindingRequest {
		t.Fatalf("type: got 0x%04x, want 0x%04x", msgType, bindingRequest)
	}

	// Verify magic cookie.
	magic := uint32(req[4])<<24 | uint32(req[5])<<16 | uint32(req[6])<<8 | uint32(req[7])
	if magic != magicCookie {
		t.Fatalf("magic: got 0x%08x, want 0x%08x", magic, magicCookie)
	}

	// Verify transaction ID.
	for i := 0; i < 12; i++ {
		if req[8+i] != txnID[i] {
			t.Fatalf("txn id byte %d: got %d, want %d", i, req[8+i], txnID[i])
		}
	}
}

func TestParseBindingResponseErrors(t *testing.T) {
	// Too short.
	_, _, err := ParseBindingResponse([]byte{0, 0})
	if err == nil {
		t.Fatal("expected error for short response")
	}

	// Wrong message type.
	bad := make([]byte, headerSize)
	bad[0] = 0x00
	bad[1] = 0x01 // binding request, not response
	_, _, err = ParseBindingResponse(bad)
	if err == nil {
		t.Fatal("expected error for wrong message type")
	}
}
