package wireguard

import (
	"crypto/ed25519"
	"testing"

	"golang.org/x/crypto/curve25519"
)

func TestGenerateWireGuardKeyPair(t *testing.T) {
	priv, pub, err := GenerateWireGuardKeyPair()
	if err != nil {
		t.Fatalf("GenerateWireGuardKeyPair: %v", err)
	}

	// Keys should not be zero.
	var zero [32]byte
	if priv == zero {
		t.Error("private key is zero")
	}
	if pub == zero {
		t.Error("public key is zero")
	}

	// Verify public key derivation.
	derivedPub, err := curve25519.X25519(priv[:], curve25519.Basepoint)
	if err != nil {
		t.Fatalf("derive public key: %v", err)
	}
	var derivedPubArr [32]byte
	copy(derivedPubArr[:], derivedPub)
	if pub != derivedPubArr {
		t.Error("public key doesn't match derived")
	}
}

func TestGenerateWireGuardKeyPairUnique(t *testing.T) {
	_, pub1, _ := GenerateWireGuardKeyPair()
	_, pub2, _ := GenerateWireGuardKeyPair()
	if pub1 == pub2 {
		t.Error("two generated keypairs should not be identical")
	}
}

func TestGenerateIdentityKeyPair(t *testing.T) {
	pub, priv, err := GenerateIdentityKeyPair()
	if err != nil {
		t.Fatalf("GenerateIdentityKeyPair: %v", err)
	}

	if len(pub) != ed25519.PublicKeySize {
		t.Errorf("public key size: got %d, want %d", len(pub), ed25519.PublicKeySize)
	}
	if len(priv) != ed25519.PrivateKeySize {
		t.Errorf("private key size: got %d, want %d", len(priv), ed25519.PrivateKeySize)
	}

	// Verify signing works.
	msg := []byte("test message")
	sig := ed25519.Sign(priv, msg)
	if !ed25519.Verify(pub, msg, sig) {
		t.Error("signature verification failed")
	}
}

func TestDeviceID(t *testing.T) {
	pub, _, _ := GenerateIdentityKeyPair()
	id := DeviceID(pub)

	if len(id) != 8 { // first 8 hex chars of SHA256
		t.Errorf("device ID length: got %d, want 8", len(id))
	}

	// Same key should produce same ID.
	id2 := DeviceID(pub)
	if id != id2 {
		t.Error("device ID should be deterministic")
	}
}

func TestDeviceIDDifferentKeys(t *testing.T) {
	pub1, _, _ := GenerateIdentityKeyPair()
	pub2, _, _ := GenerateIdentityKeyPair()

	id1 := DeviceID(pub1)
	id2 := DeviceID(pub2)

	if id1 == id2 {
		t.Error("different keys should produce different device IDs")
	}
}

func TestAssignIP(t *testing.T) {
	ip := AssignIP("a3f1c9b2")

	// Should be in 100.100.x.x range.
	if ip[:8] != "100.100." {
		t.Errorf("IP should start with 100.100., got %q", ip)
	}

	// Should be deterministic.
	ip2 := AssignIP("a3f1c9b2")
	if ip != ip2 {
		t.Error("IP should be deterministic")
	}

	// Different device IDs should (very likely) get different IPs.
	ip3 := AssignIP("different-id")
	if ip == ip3 {
		t.Error("different device IDs should get different IPs")
	}
}

func TestWireGuardKeyClamping(t *testing.T) {
	priv, _, err := GenerateWireGuardKeyPair()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Check WireGuard clamping: low 3 bits of first byte should be 0.
	if priv[0]&7 != 0 {
		t.Error("private key first byte low 3 bits should be cleared")
	}
	// High bit of last byte should be 0, second-to-high should be 1.
	if priv[31]&128 != 0 {
		t.Error("private key last byte high bit should be cleared")
	}
	if priv[31]&64 == 0 {
		t.Error("private key last byte second-to-high bit should be set")
	}
}
