// Package wireguard manages the userspace WireGuard endpoint, key generation,
// peer management, and IP assignment.
package wireguard

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/curve25519"

	"github.com/canopy-dev/canopyd/internal/config"
)

// GenerateWireGuardKeyPair generates a Curve25519 keypair for WireGuard.
func GenerateWireGuardKeyPair() (privateKey, publicKey [32]byte, err error) {
	if _, err := rand.Read(privateKey[:]); err != nil {
		return privateKey, publicKey, fmt.Errorf("generate random key: %w", err)
	}
	// Clamp per WireGuard spec.
	privateKey[0] &= 248
	privateKey[31] &= 127
	privateKey[31] |= 64

	pub, err := curve25519.X25519(privateKey[:], curve25519.Basepoint)
	if err != nil {
		return privateKey, publicKey, fmt.Errorf("derive public key: %w", err)
	}
	copy(publicKey[:], pub)
	return
}

// GenerateIdentityKeyPair generates an Ed25519 identity keypair.
func GenerateIdentityKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate identity key: %w", err)
	}
	return pub, priv, nil
}

// DeviceID derives a human-readable device ID from an Ed25519 public key.
// Per the spec: first 8 bytes of SHA256(pubkey), hex encoded, yielding 8 hex chars.
// Example: "a3f1c9b2".
func DeviceID(pubKey ed25519.PublicKey) string {
	hash := sha256.Sum256(pubKey)
	return hex.EncodeToString(hash[:8])[:8]
}

// AssignIP derives a deterministic private IP from a device ID hash.
// Uses the 100.100.0.0/16 range. The third and fourth octets come from
// the first two bytes of SHA256(deviceID).
func AssignIP(deviceID string) string {
	hash := sha256.Sum256([]byte(deviceID))
	// Avoid .0 and .255 in the last octet.
	third := hash[0]
	fourth := hash[1]
	if fourth == 0 {
		fourth = 1
	}
	if fourth == 255 {
		fourth = 254
	}
	return fmt.Sprintf("100.100.%d.%d", third, fourth)
}

// EnsureKeys generates and saves keys if they don't exist yet.
// Returns the device ID.
func EnsureKeys() (string, error) {
	cfgDir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}

	// Identity keys.
	identityPubPath := filepath.Join(cfgDir, "identity.pub")
	identityKeyPath := filepath.Join(cfgDir, "identity.key")

	var identityPub ed25519.PublicKey

	if _, err := os.Stat(identityKeyPath); os.IsNotExist(err) {
		pub, priv, err := GenerateIdentityKeyPair()
		if err != nil {
			return "", err
		}
		identityPub = pub
		if err := os.WriteFile(identityKeyPath, []byte(priv), 0600); err != nil {
			return "", fmt.Errorf("write identity key: %w", err)
		}
		if err := os.WriteFile(identityPubPath, []byte(pub), 0644); err != nil {
			return "", fmt.Errorf("write identity pub: %w", err)
		}
	} else {
		data, err := os.ReadFile(identityPubPath)
		if err != nil {
			return "", fmt.Errorf("read identity pub: %w", err)
		}
		identityPub = ed25519.PublicKey(data)
	}

	// WireGuard keys.
	wgPrivPath := filepath.Join(cfgDir, "wg_private.key")
	wgPubPath := filepath.Join(cfgDir, "wg_public.key")

	if _, err := os.Stat(wgPrivPath); os.IsNotExist(err) {
		priv, pub, err := GenerateWireGuardKeyPair()
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(wgPrivPath, priv[:], 0600); err != nil {
			return "", fmt.Errorf("write wg private key: %w", err)
		}
		if err := os.WriteFile(wgPubPath, pub[:], 0644); err != nil {
			return "", fmt.Errorf("write wg public key: %w", err)
		}
	}

	return DeviceID(identityPub), nil
}

// LoadWireGuardPrivateKey reads the WireGuard private key from disk.
func LoadWireGuardPrivateKey() ([32]byte, error) {
	var key [32]byte
	cfgDir, err := config.ConfigDir()
	if err != nil {
		return key, err
	}

	data, err := os.ReadFile(filepath.Join(cfgDir, "wg_private.key"))
	if err != nil {
		return key, fmt.Errorf("read wg private key: %w", err)
	}

	if len(data) != 32 {
		return key, fmt.Errorf("invalid wg private key length: %d", len(data))
	}

	copy(key[:], data)
	return key, nil
}

// LoadWireGuardPublicKey reads the WireGuard public key from disk.
func LoadWireGuardPublicKey() ([32]byte, error) {
	var key [32]byte
	cfgDir, err := config.ConfigDir()
	if err != nil {
		return key, err
	}

	data, err := os.ReadFile(filepath.Join(cfgDir, "wg_public.key"))
	if err != nil {
		return key, fmt.Errorf("read wg public key: %w", err)
	}

	if len(data) != 32 {
		return key, fmt.Errorf("invalid wg public key length: %d", len(data))
	}

	copy(key[:], data)
	return key, nil
}
