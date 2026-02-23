// Package auth provides Ed25519 signature verification for the coordination server.
// Keys are raw 32-byte Ed25519 public keys, matching the daemon's key format
// (see daemon/internal/install/keys.go and daemon/internal/wireguard/keys.go).
package auth

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidSignature = errors.New("invalid signature")
	ErrInvalidKey       = errors.New("invalid public key")
	ErrTimestampExpired = errors.New("timestamp too old")
)

// MaxTimestampAge is the maximum age of a request timestamp before rejection.
const MaxTimestampAge = 5 * time.Minute

// VerifySignature verifies an Ed25519 signature over the given message.
// The public key is base64-encoded raw 32-byte Ed25519 public key.
// The signature is base64-encoded.
func VerifySignature(pubKeyB64, signatureB64 string, message []byte) error {
	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil {
		return fmt.Errorf("%w: decode public key: %v", ErrInvalidKey, err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidKey, ed25519.PublicKeySize, len(pubKeyBytes))
	}

	sig, err := base64.StdEncoding.DecodeString(signatureB64)
	if err != nil {
		return fmt.Errorf("%w: decode signature: %v", ErrInvalidSignature, err)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: expected %d bytes, got %d", ErrInvalidSignature, ed25519.SignatureSize, len(sig))
	}

	pubKey := ed25519.PublicKey(pubKeyBytes)
	if !ed25519.Verify(pubKey, message, sig) {
		return ErrInvalidSignature
	}
	return nil
}

// DeviceIDFromPublicKey derives a human-readable device ID from an Ed25519 public key.
// Compatible with daemon's install.DeviceIDFromPublicKey and wireguard.DeviceID.
// Uses first 8 bytes of SHA256(pubkey), hex-encoded, truncated to 8 chars.
func DeviceIDFromPublicKey(pubKeyB64 string) (string, error) {
	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil {
		return "", fmt.Errorf("decode public key: %w", err)
	}
	if len(pubKeyBytes) != ed25519.PublicKeySize {
		return "", fmt.Errorf("invalid key size: %d", len(pubKeyBytes))
	}
	hash := sha256.Sum256(pubKeyBytes)
	return hex.EncodeToString(hash[:8])[:8], nil
}

// ValidateTimestamp checks that a timestamp string (RFC3339) is within MaxTimestampAge.
func ValidateTimestamp(ts string) error {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		// Also try RFC3339Nano.
		t, err = time.Parse(time.RFC3339Nano, ts)
		if err != nil {
			return fmt.Errorf("parse timestamp: %w", err)
		}
	}

	age := time.Since(t)
	if age < 0 {
		age = -age
	}
	if age > MaxTimestampAge {
		return fmt.Errorf("%w: age %v exceeds %v", ErrTimestampExpired, age, MaxTimestampAge)
	}
	return nil
}

// VerifyBearerToken validates a signed bearer token for endpoint lookups.
// Token format: base64(pubkey):base64(signature_of_pubkey)
func VerifyBearerToken(token string) (pubKeyB64 string, err error) {
	parts := strings.SplitN(token, ":", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("%w: invalid token format", ErrInvalidSignature)
	}

	pubKeyB64 = parts[0]
	sigB64 := parts[1]

	pubKeyBytes, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil {
		return "", fmt.Errorf("%w: decode key: %v", ErrInvalidKey, err)
	}

	if err := VerifySignature(pubKeyB64, sigB64, pubKeyBytes); err != nil {
		return "", err
	}

	return pubKeyB64, nil
}
