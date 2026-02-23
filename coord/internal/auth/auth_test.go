package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"testing"
	"time"
)

func generateTestKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

func TestVerifySignature(t *testing.T) {
	pub, priv := generateTestKeypair(t)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	message := []byte("hello world")
	sig := ed25519.Sign(priv, message)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	if err := VerifySignature(pubB64, sigB64, message); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}

	// Wrong message.
	if err := VerifySignature(pubB64, sigB64, []byte("wrong")); err == nil {
		t.Fatal("expected error for wrong message")
	}

	// Wrong key.
	pub2, _ := generateTestKeypair(t)
	pub2B64 := base64.StdEncoding.EncodeToString(pub2)
	if err := VerifySignature(pub2B64, sigB64, message); err == nil {
		t.Fatal("expected error for wrong key")
	}

	// Invalid base64.
	if err := VerifySignature("not-base64!", sigB64, message); err == nil {
		t.Fatal("expected error for invalid key encoding")
	}

	// Wrong key length.
	shortKey := base64.StdEncoding.EncodeToString([]byte("short"))
	if err := VerifySignature(shortKey, sigB64, message); err == nil {
		t.Fatal("expected error for short key")
	}
}

func TestDeviceIDFromPublicKey(t *testing.T) {
	pub, _ := generateTestKeypair(t)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	id, err := DeviceIDFromPublicKey(pubB64)
	if err != nil {
		t.Fatalf("device id: %v", err)
	}

	// Verify compatibility with daemon format.
	hash := sha256.Sum256(pub)
	expected := hex.EncodeToString(hash[:8])[:8]

	if id != expected {
		t.Fatalf("device id mismatch: got %q, want %q", id, expected)
	}

	if len(id) != 8 {
		t.Fatalf("device id length: got %d, want 8", len(id))
	}
}

func TestValidateTimestamp(t *testing.T) {
	// Valid timestamp.
	ts := time.Now().UTC().Format(time.RFC3339)
	if err := ValidateTimestamp(ts); err != nil {
		t.Fatalf("valid timestamp rejected: %v", err)
	}

	// RFC3339Nano also works.
	ts = time.Now().UTC().Format(time.RFC3339Nano)
	if err := ValidateTimestamp(ts); err != nil {
		t.Fatalf("valid nano timestamp rejected: %v", err)
	}

	// Old timestamp.
	old := time.Now().Add(-10 * time.Minute).UTC().Format(time.RFC3339)
	if err := ValidateTimestamp(old); err == nil {
		t.Fatal("expected error for old timestamp")
	}

	// Future timestamp (within tolerance).
	future := time.Now().Add(2 * time.Minute).UTC().Format(time.RFC3339)
	if err := ValidateTimestamp(future); err != nil {
		t.Fatalf("near-future timestamp rejected: %v", err)
	}

	// Far future timestamp.
	farFuture := time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339)
	if err := ValidateTimestamp(farFuture); err == nil {
		t.Fatal("expected error for far-future timestamp")
	}

	// Invalid format.
	if err := ValidateTimestamp("not-a-timestamp"); err == nil {
		t.Fatal("expected error for invalid format")
	}
}

func TestVerifyBearerToken(t *testing.T) {
	pub, priv := generateTestKeypair(t)
	pubB64 := base64.StdEncoding.EncodeToString(pub)

	sig := ed25519.Sign(priv, pub)
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	token := pubB64 + ":" + sigB64

	gotKey, err := VerifyBearerToken(token)
	if err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
	if gotKey != pubB64 {
		t.Fatalf("key mismatch: got %q, want %q", gotKey, pubB64)
	}

	// Invalid token format.
	if _, err := VerifyBearerToken("no-colon"); err == nil {
		t.Fatal("expected error for missing colon")
	}

	// Wrong signature.
	_, priv2 := generateTestKeypair(t)
	badSig := ed25519.Sign(priv2, pub)
	badSigB64 := base64.StdEncoding.EncodeToString(badSig)
	badToken := pubB64 + ":" + badSigB64
	if _, err := VerifyBearerToken(badToken); err == nil {
		t.Fatal("expected error for wrong signature")
	}
}
