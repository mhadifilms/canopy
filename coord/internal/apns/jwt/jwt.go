// Package jwt generates ES256-signed JWTs for Apple Push Notification service authentication.
package jwt

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"time"
)

// Generate creates a signed JWT for APNs authentication.
func Generate(teamID, keyID string, key *ecdsa.PrivateKey) (string, error) {
	header := map[string]string{
		"alg": "ES256",
		"kid": keyID,
	}
	claims := map[string]any{
		"iss": teamID,
		"iat": time.Now().Unix(),
	}

	headerJSON, err := json.Marshal(header)
	if err != nil {
		return "", fmt.Errorf("jwt: marshal header: %w", err)
	}
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("jwt: marshal claims: %w", err)
	}

	headerB64 := base64.RawURLEncoding.EncodeToString(headerJSON)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsJSON)
	signingInput := headerB64 + "." + claimsB64

	hash := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, hash[:])
	if err != nil {
		return "", fmt.Errorf("jwt: sign: %w", err)
	}

	// Encode r and s as fixed-size 32-byte big-endian values.
	curveBits := key.Curve.Params().BitSize
	keyBytes := curveBits / 8
	rBytes := r.Bytes()
	sBytes := s.Bytes()

	sigBytes := make([]byte, 2*keyBytes)
	copy(sigBytes[keyBytes-len(rBytes):keyBytes], rBytes)
	copy(sigBytes[2*keyBytes-len(sBytes):], sBytes)

	sigB64 := base64.RawURLEncoding.EncodeToString(sigBytes)
	return signingInput + "." + sigB64, nil
}

// Verify verifies a JWT signature (used for testing).
func Verify(token string, key *ecdsa.PublicKey) error {
	parts := splitToken(token)
	if parts == nil {
		return fmt.Errorf("jwt: invalid token format")
	}

	signingInput := parts[0] + "." + parts[1]
	sigBytes, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("jwt: decode signature: %w", err)
	}

	curveBits := key.Curve.Params().BitSize
	keyBytes := curveBits / 8
	if len(sigBytes) != 2*keyBytes {
		return fmt.Errorf("jwt: invalid signature length")
	}

	r := new(big.Int).SetBytes(sigBytes[:keyBytes])
	s := new(big.Int).SetBytes(sigBytes[keyBytes:])

	hash := sha256.Sum256([]byte(signingInput))
	if !ecdsa.Verify(key, hash[:], r, s) {
		return fmt.Errorf("jwt: signature verification failed")
	}
	return nil
}

// DecodeClaims decodes the claims from a JWT (used for testing).
func DecodeClaims(token string) (map[string]any, error) {
	parts := splitToken(token)
	if parts == nil {
		return nil, fmt.Errorf("jwt: invalid token format")
	}
	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("jwt: decode claims: %w", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		return nil, fmt.Errorf("jwt: unmarshal claims: %w", err)
	}
	return claims, nil
}

// DecodeHeader decodes the header from a JWT (used for testing).
func DecodeHeader(token string) (map[string]any, error) {
	parts := splitToken(token)
	if parts == nil {
		return nil, fmt.Errorf("jwt: invalid token format")
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("jwt: decode header: %w", err)
	}
	var header map[string]any
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, fmt.Errorf("jwt: unmarshal header: %w", err)
	}
	return header, nil
}

func splitToken(token string) []string {
	// Split into exactly 3 parts.
	var parts []string
	start := 0
	count := 0
	for i := 0; i < len(token); i++ {
		if token[i] == '.' {
			parts = append(parts, token[start:i])
			start = i + 1
			count++
		}
	}
	parts = append(parts, token[start:])
	if len(parts) != 3 {
		return nil
	}
	return parts
}
