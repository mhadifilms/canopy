// Package pairing implements Phase 1 manual-code pairing between canopyd and the Canopy iOS app.
package pairing

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
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

// StartPairing initiates a pairing session. It listens on the daemon socket for
// a pairing request, validates the code, and stores the paired device.
//
// This is Phase 1: manual code entry. No QR code yet.
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
		errResp := json.NewEncoder(conn)
		errResp.Encode(map[string]string{"error": "invalid_code"})
		return fmt.Errorf("invalid pairing code (attempt %d/%d)", session.Attempts, maxAttempts)
	}

	// Store the paired device.
	device := install.PairedDevice{
		DeviceID:       req.DeviceName,
		Name:           req.DeviceName,
		WGPublicKey:    req.PhoneWGPubKey,
		IdentityPubKey: req.PhoneIdentityPK,
		PairedAt:       time.Now().UTC().Format(time.RFC3339),
	}
	if err := install.AddPairedDevice(device); err != nil {
		return fmt.Errorf("store paired device: %w", err)
	}

	// Read our WireGuard public key for the confirmation.
	configDir, _ := config.ConfigDir()
	wgPubKey, _ := readFileString(configDir, "wg_public.key")
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
