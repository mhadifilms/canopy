// Package install handles canopyd installation: key generation, shell hook injection,
// launchd plist management, and configuration setup.
package install

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/curve25519"
)

// GenerateIdentityKeypair creates an Ed25519 identity keypair and writes it to disk.
// Returns the public key bytes.
func GenerateIdentityKeypair(configDir string) (ed25519.PublicKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate ed25519 key: %w", err)
	}

	privPath := filepath.Join(configDir, "identity.key")
	pubPath := filepath.Join(configDir, "identity.pub")

	if err := os.WriteFile(privPath, priv, 0600); err != nil {
		return nil, fmt.Errorf("write identity private key: %w", err)
	}
	if err := os.WriteFile(pubPath, pub, 0644); err != nil {
		return nil, fmt.Errorf("write identity public key: %w", err)
	}

	return pub, nil
}

// GenerateWireGuardKeypair creates a Curve25519 WireGuard keypair and writes it to disk.
func GenerateWireGuardKeypair(configDir string) error {
	var privateKey [32]byte
	if _, err := rand.Read(privateKey[:]); err != nil {
		return fmt.Errorf("generate random bytes: %w", err)
	}

	// Clamp the private key per Curve25519 convention.
	privateKey[0] &= 248
	privateKey[31] &= 127
	privateKey[31] |= 64

	publicKey, err := curve25519.X25519(privateKey[:], curve25519.Basepoint)
	if err != nil {
		return fmt.Errorf("derive WireGuard public key: %w", err)
	}

	privPath := filepath.Join(configDir, "wg_private.key")
	pubPath := filepath.Join(configDir, "wg_public.key")

	if err := os.WriteFile(privPath, privateKey[:], 0600); err != nil {
		return fmt.Errorf("write WireGuard private key: %w", err)
	}
	if err := os.WriteFile(pubPath, publicKey, 0644); err != nil {
		return fmt.Errorf("write WireGuard public key: %w", err)
	}

	return nil
}

// DeviceIDFromPublicKey derives a human-readable device ID from an Ed25519 public key.
// Uses the first 8 bytes of SHA256(pubkey), hex-encoded → 16 hex chars, truncated to 8.
func DeviceIDFromPublicKey(pub ed25519.PublicKey) string {
	hash := sha256.Sum256(pub)
	return hex.EncodeToString(hash[:8])[:8]
}

// LoadIdentityPublicKey reads the identity public key from disk.
func LoadIdentityPublicKey(configDir string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(filepath.Join(configDir, "identity.pub"))
	if err != nil {
		return nil, fmt.Errorf("read identity public key: %w", err)
	}
	return ed25519.PublicKey(data), nil
}

// KeysExist returns true if the identity keypair already exists.
func KeysExist(configDir string) bool {
	_, err := os.Stat(filepath.Join(configDir, "identity.key"))
	return err == nil
}
