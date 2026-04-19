// Package pairing implements manual-code pairing between canopyd and the Canopy iOS app.
//
// Two flows are supported:
//
//  1. Local-socket flow (same machine / same network). A Unix socket is opened
//     at {sockpath}.pair and accepts a JSON request from a phone/CLI test tool.
//  2. Coordination-server flow (WAN). The Mac POSTs its hostname, device ID,
//     WireGuard public key, and identity key to `/v1/pairing/initiate` on the
//     coord server. The phone polls `/v1/pairing/{code}/status` to retrieve
//     the Mac's info and then stores a pairing binding on its side.
//
// The iOS app currently only implements flow (2). Both flows are exposed so
// either can be used; they are mutually compatible and terminate the same way.
package pairing

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"time"

	"github.com/canopy-dev/canopyd/internal/config"
	"github.com/canopy-dev/canopyd/internal/install"
)

// PairingCode is a 6-digit numeric code used for manual pairing.
type PairingCode string

// Request is sent by the phone to pair with the Mac.
type Request struct {
	Code            string `json:"code"`
	PhoneWGPubKey   string `json:"wg_public_key"`
	PhoneIdentityPK string `json:"identity_public_key"`
	DeviceName      string `json:"device_name"`
}

// Confirmation is sent back to the phone after successful pairing.
type Confirmation struct {
	MacHostname  string `json:"hostname"`
	MacDeviceID  string `json:"device_id"`
	MacWGPubKey  string `json:"wg_public_key"`
	MacTunnelIP  string `json:"tunnel_ip"`
}

// Session manages a single pairing attempt.
type Session struct {
	Code      PairingCode
	ExpiresAt time.Time
	Attempts  int
}

const (
	maxAttempts = 3
	codeLength  = 6
)

// GenerateCode creates a cryptographically random 6-digit pairing code.
func GenerateCode() (PairingCode, error) {
	max := new(big.Int).Exp(big.NewInt(10), big.NewInt(codeLength), nil)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", fmt.Errorf("generate random code: %w", err)
	}
	code := fmt.Sprintf("%06d", n.Int64())
	return PairingCode(code), nil
}

// StartPairing initiates a pairing session. It publishes the Mac's info to the
// coordination server under a fresh 6-digit code (so a remote phone can
// discover it), and simultaneously listens on a local Unix socket for
// same-network clients.
func StartPairing(ctx context.Context, timeout time.Duration) error {
	code, err := GenerateCode()
	if err != nil {
		return err
	}

	session := &Session{
		Code:      code,
		ExpiresAt: time.Now().Add(timeout),
		Attempts:  0,
	}

	hostname, _ := config.Hostname()

	configDir, err := config.ConfigDir()
	if err != nil {
		return err
	}

	pub, err := install.LoadIdentityPublicKey(configDir)
	if err != nil {
		return fmt.Errorf("load identity key: %w", err)
	}
	deviceID := install.DeviceIDFromPublicKey(pub)

	// Publish to coord so the iOS app can poll /v1/pairing/{code}/status and
	// retrieve the Mac's identity + WireGuard keys. Best-effort: if the coord
	// server is unreachable, fall back to the local-socket flow.
	cfg, cfgErr := config.Load()
	if cfgErr == nil && cfg.CoordURL != "" {
		if err := publishToCoord(ctx, cfg.CoordURL, string(code), hostname, deviceID, configDir); err != nil {
			fmt.Printf("  Note: could not publish pairing code to coord server: %s\n", err)
		}
	}

	fmt.Println()
	fmt.Println("  Pairing mode active")
	fmt.Println()
	fmt.Printf("  Code: %s\n", code)
	fmt.Printf("  Device: %s (%s)\n", hostname, deviceID)
	fmt.Printf("  Expires in: %s\n", timeout.Round(time.Second))
	fmt.Println()
	fmt.Println("  Open the Canopy app on your iPhone and enter this code.")
	fmt.Println("  Waiting for connection...")
	fmt.Println()

	return waitForPairing(ctx, session, deviceID)
}

// publishToCoord posts the Mac's info under the given pairing code so a remote
// phone can retrieve it via /v1/pairing/{code}/status. The signature binds the
// identity key + WG key to the code, matching the coord server's canonical
// pairing message.
func publishToCoord(ctx context.Context, coordURL, code, hostname, deviceID, configDir string) error {
	priv, err := loadIdentityPrivateKey(configDir)
	if err != nil {
		return fmt.Errorf("load identity private key: %w", err)
	}
	pub, err := install.LoadIdentityPublicKey(configDir)
	if err != nil {
		return fmt.Errorf("load identity public key: %w", err)
	}
	wgPubKey, err := readFileString(configDir, "wg_public.key")
	if err != nil {
		return fmt.Errorf("read wg_public.key: %w", err)
	}
	wgPubB64 := base64.StdEncoding.EncodeToString([]byte(wgPubKey))
	identityB64 := base64.StdEncoding.EncodeToString(pub)

	// Canonical message: matches auth.CanonicalPairingMessage("initiate", ...)
	// on the coord server. Keep these strings in sync.
	canonical := []byte("canopy/pairing/initiate/v1\n" + code + "\n" + identityB64 + "\n" + wgPubB64)
	sig := ed25519.Sign(priv, canonical)

	body, err := json.Marshal(map[string]string{
		"code":      code,
		"hostname":  hostname,
		"device_id": deviceID,
		"wg_pub":    wgPubB64,
		"identity":  identityB64,
		"sig":       base64.StdEncoding.EncodeToString(sig),
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", coordURL+"/v1/pairing/initiate", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

// loadIdentityPrivateKey reads the raw Ed25519 private key from disk.
func loadIdentityPrivateKey(configDir string) (ed25519.PrivateKey, error) {
	data, err := config.ReadFileInDir(configDir, "identity.key")
	if err != nil {
		return nil, err
	}
	if len(data) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("unexpected private key length %d", len(data))
	}
	return ed25519.PrivateKey(data), nil
}

// waitForPairing listens on the daemon socket for a pairing request.
func waitForPairing(ctx context.Context, session *Session, macDeviceID string) error {
	sockPath := config.SocketPath()

	// Connect to the running daemon to register the pairing listener.
	// For Phase 1, we listen on a separate temporary socket for pairing.
	pairingPath := sockPath + ".pair"

	// Clean up stale socket.
	if c, err := net.Dial("unix", pairingPath); err == nil {
		c.Close()
	}
	listener, err := net.Listen("unix", pairingPath)
	if err != nil {
		return fmt.Errorf("listen for pairing: %w", err)
	}
	defer listener.Close()

	deadline, cancel := context.WithDeadline(ctx, session.ExpiresAt)
	defer cancel()

	// Close listener when context expires.
	go func() {
		<-deadline.Done()
		listener.Close()
	}()

	for {
		conn, err := listener.Accept()
		if err != nil {
			select {
			case <-deadline.Done():
				return fmt.Errorf("pairing timed out")
			default:
				return fmt.Errorf("accept pairing connection: %w", err)
			}
		}

		if err := handlePairingConnection(conn, session, macDeviceID); err != nil {
			fmt.Printf("  Pairing attempt failed: %s\n", err)
			conn.Close()
			if session.Attempts >= maxAttempts {
				return fmt.Errorf("maximum pairing attempts (%d) exceeded", maxAttempts)
			}
			continue
		}

		conn.Close()
		return nil
	}
}

func handlePairingConnection(conn net.Conn, session *Session, macDeviceID string) error {
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	session.Attempts++

	// Read the pairing request.
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("read pairing request: %w", err)
	}

	var req Request
	if err := json.Unmarshal(buf[:n], &req); err != nil {
		return fmt.Errorf("parse pairing request: %w", err)
	}

	// Validate code.
	if PairingCode(req.Code) != session.Code {
		if err := json.NewEncoder(conn).Encode(map[string]string{"error": "invalid_code"}); err != nil {
			return fmt.Errorf("write invalid_code response: %w", err)
		}
		return fmt.Errorf("invalid pairing code (attempt %d/%d)", session.Attempts, maxAttempts)
	}

	// Require the phone's identity public key so we can derive a stable device ID
	// and later verify signed traffic from this device. The display name is stored
	// separately and should never be used as an identifier.
	if req.PhoneIdentityPK == "" {
		if err := json.NewEncoder(conn).Encode(map[string]string{"error": "missing_identity"}); err != nil {
			return fmt.Errorf("write missing_identity response: %w", err)
		}
		return fmt.Errorf("missing phone identity public key")
	}
	phoneDeviceID, err := deriveDeviceIDFromBase64(req.PhoneIdentityPK)
	if err != nil {
		if encErr := json.NewEncoder(conn).Encode(map[string]string{"error": "invalid_identity"}); encErr != nil {
			return fmt.Errorf("write invalid_identity response: %w", encErr)
		}
		return fmt.Errorf("invalid phone identity public key: %w", err)
	}

	// Store the paired device keyed by identity-derived device ID.
	device := install.PairedDevice{
		DeviceID:       phoneDeviceID,
		Name:           req.DeviceName,
		WGPublicKey:    req.PhoneWGPubKey,
		IdentityPubKey: req.PhoneIdentityPK,
		PairedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if err := install.AddPairedDevice(device); err != nil {
		return fmt.Errorf("store paired device: %w", err)
	}

	// Read our WireGuard public key for the confirmation.
	configDir, err := config.ConfigDir()
	if err != nil {
		return fmt.Errorf("locate config dir: %w", err)
	}
	wgPubKey, err := readFileString(configDir, "wg_public.key")
	if err != nil {
		return fmt.Errorf("read wg_public.key: %w", err)
	}
	hostname, _ := config.Hostname()

	// Send confirmation.
	confirmation := Confirmation{
		MacHostname: hostname,
		MacDeviceID: macDeviceID,
		MacWGPubKey: wgPubKey,
	}
	if err := json.NewEncoder(conn).Encode(confirmation); err != nil {
		return fmt.Errorf("send confirmation: %w", err)
	}

	fmt.Printf("  Paired with %s\n", req.DeviceName)
	return nil
}

func readFileString(dir, name string) (string, error) {
	data, err := config.ReadFileInDir(dir, name)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// deriveDeviceIDFromBase64 parses a base64-encoded Ed25519 public key and derives
// the canonical 8-hex-char device ID used throughout the system.
func deriveDeviceIDFromBase64(pubB64 string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(pubB64)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}
	if len(raw) != ed25519.PublicKeySize {
		return "", fmt.Errorf("unexpected key length %d, want %d", len(raw), ed25519.PublicKeySize)
	}
	return install.DeviceIDFromPublicKey(ed25519.PublicKey(raw)), nil
}
