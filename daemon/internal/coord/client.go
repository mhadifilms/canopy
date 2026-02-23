// Package coord implements the daemon's client for the Canopy coordination server.
// It handles periodic check-ins, endpoint discovery via STUN, endpoint lookups
// for peers, pairing registration, and push notification forwarding.
package coord

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	// checkinInterval is how often the daemon checks in with the coordination server.
	checkinInterval = 30 * time.Second

	// stunTimeout is how long to wait for a STUN response.
	stunTimeout = 5 * time.Second

	// httpTimeout is the HTTP request timeout for coordination server API calls.
	httpTimeout = 10 * time.Second

	// STUN constants (RFC 5389).
	stunBindingRequest  = 0x0001
	stunBindingResponse = 0x0101
	stunMagicCookie     = 0x2112A442
	stunHeaderSize      = 20
	stunAttrXORMapped   = 0x0020
)

// Endpoint represents a network endpoint (IP + port) for a device.
type Endpoint struct {
	IP   string `json:"ip"`
	Port int    `json:"port"`
	Type string `json:"type"` // "public" or "local"
}

// Client is the daemon's coordination server client.
type Client struct {
	coordURL   string
	httpClient *http.Client
	logger     *zap.Logger

	// Identity keys for signing requests.
	identityPub  ed25519.PublicKey
	identityPriv ed25519.PrivateKey
	pubKeyB64    string // base64-encoded identity public key

	// WireGuard public key (base64-encoded).
	wgPubKeyB64 string

	// Local endpoint information.
	wgPort int

	// STUN-discovered public endpoint (updated by check-in loop).
	mu             sync.RWMutex
	publicEndpoint *Endpoint

	// Paired device WG public keys (base64-encoded).
	pairedWGKeys []string

	// Background check-in loop control.
	cancel context.CancelFunc
}

// ClientConfig holds configuration for the coordination client.
type ClientConfig struct {
	CoordURL     string
	IdentityPub  ed25519.PublicKey
	IdentityPriv ed25519.PrivateKey
	WGPubKey     []byte // raw 32-byte WireGuard public key
	WGPort       int
	PairedWGKeys []string // base64-encoded WG public keys of paired devices
}

// NewClient creates a new coordination server client.
func NewClient(cfg ClientConfig, logger *zap.Logger) *Client {
	return &Client{
		coordURL: cfg.CoordURL,
		httpClient: &http.Client{
			Timeout: httpTimeout,
		},
		logger:       logger,
		identityPub:  cfg.IdentityPub,
		identityPriv: cfg.IdentityPriv,
		pubKeyB64:    base64.StdEncoding.EncodeToString(cfg.IdentityPub),
		wgPubKeyB64:  base64.StdEncoding.EncodeToString(cfg.WGPubKey),
		wgPort:       cfg.WGPort,
		pairedWGKeys: cfg.PairedWGKeys,
	}
}

// Start begins the background check-in loop. It runs until the context is cancelled.
func (c *Client) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	// Initial check-in immediately.
	c.doCheckin(ctx)

	go func() {
		ticker := time.NewTicker(checkinInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				c.doCheckin(ctx)
			}
		}
	}()
}

// Stop cancels the background check-in loop.
func (c *Client) Stop() {
	if c.cancel != nil {
		c.cancel()
	}
}

// SetPairedWGKeys updates the list of paired device WG public keys.
func (c *Client) SetPairedWGKeys(keys []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pairedWGKeys = keys
}

// PublicEndpoint returns the last STUN-discovered public endpoint, or nil.
func (c *Client) PublicEndpoint() *Endpoint {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.publicEndpoint == nil {
		return nil
	}
	ep := *c.publicEndpoint
	return &ep
}

// doCheckin performs a single check-in cycle: STUN discovery + HTTP check-in.
func (c *Client) doCheckin(ctx context.Context) {
	// 1. Discover public endpoint via STUN.
	pubEP, err := c.discoverPublicEndpoint(ctx)
	if err != nil {
		c.logger.Debug("stun discovery failed, continuing with local endpoint only", zap.Error(err))
	} else {
		c.mu.Lock()
		c.publicEndpoint = pubEP
		c.mu.Unlock()
	}

	// 2. Build endpoint list.
	endpoints := c.buildEndpoints(pubEP)

	// 3. POST check-in.
	if err := c.checkin(ctx, endpoints); err != nil {
		c.logger.Warn("coordination check-in failed", zap.Error(err))
	} else {
		c.logger.Debug("coordination check-in successful",
			zap.Int("endpoints", len(endpoints)),
		)
	}
}

// buildEndpoints assembles the endpoint list for check-in.
func (c *Client) buildEndpoints(publicEP *Endpoint) []Endpoint {
	var endpoints []Endpoint

	if publicEP != nil {
		endpoints = append(endpoints, *publicEP)
	}

	// Add local network endpoints.
	localIPs := getLocalIPs()
	for _, ip := range localIPs {
		endpoints = append(endpoints, Endpoint{
			IP:   ip,
			Port: c.wgPort,
			Type: "local",
		})
	}

	return endpoints
}

// checkin sends a POST /v1/checkin to the coordination server.
func (c *Client) checkin(ctx context.Context, endpoints []Endpoint) error {
	ts := time.Now().UTC().Format(time.RFC3339)

	c.mu.RLock()
	pairedKeys := make([]string, len(c.pairedWGKeys))
	copy(pairedKeys, c.pairedWGKeys)
	c.mu.RUnlock()

	// Sign: device_key + wg_public_key + timestamp
	signedMessage := c.pubKeyB64 + c.wgPubKeyB64 + ts
	sig := ed25519.Sign(c.identityPriv, []byte(signedMessage))
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	body := map[string]interface{}{
		"device_key":     c.pubKeyB64,
		"wg_public_key":  c.wgPubKeyB64,
		"endpoints":      endpoints,
		"paired_devices":  pairedKeys,
		"timestamp":      ts,
		"sig":            sigB64,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal checkin: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.coordURL+"/v1/checkin", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create checkin request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("checkin request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("checkin failed: status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// LookupEndpoints queries the coordination server for a peer's endpoints.
func (c *Client) LookupEndpoints(ctx context.Context, peerWGKeyB64 string) ([]Endpoint, bool, error) {
	// Build bearer token: base64(pubkey):base64(sig_of_pubkey_bytes)
	pubKeyBytes := []byte(c.identityPub)
	sig := ed25519.Sign(c.identityPriv, pubKeyBytes)
	sigB64 := base64.StdEncoding.EncodeToString(sig)
	token := c.pubKeyB64 + ":" + sigB64

	u := c.coordURL + "/v1/endpoints?peer_wg_key=" + url.QueryEscape(peerWGKeyB64)

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, false, fmt.Errorf("create endpoint lookup request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, false, fmt.Errorf("endpoint lookup request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		return nil, false, fmt.Errorf("not paired with peer")
	}
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, false, fmt.Errorf("endpoint lookup failed: status %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Endpoints []Endpoint `json:"endpoints"`
		Online    bool       `json:"online"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, false, fmt.Errorf("decode endpoint lookup: %w", err)
	}

	return result.Endpoints, result.Online, nil
}

// RegisterPairing tells the coordination server about a new pairing.
func (c *Client) RegisterPairing(ctx context.Context, peerWGKeyB64 string) error {
	signedMessage := c.pubKeyB64 + peerWGKeyB64
	sig := ed25519.Sign(c.identityPriv, []byte(signedMessage))
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	body := map[string]string{
		"device_key":  c.pubKeyB64,
		"peer_wg_key": peerWGKeyB64,
		"sig":         sigB64,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal register_pairing: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.coordURL+"/v1/register_pairing", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create register_pairing request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("register_pairing request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("register_pairing failed: status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// PushNotification represents a push notification to be sent via the coordination server.
type PushNotification struct {
	Title    string            `json:"title"`
	Subtitle string           `json:"subtitle"`
	Body     string            `json:"body"`
	Category string            `json:"category"`
	ThreadID string            `json:"thread_id"`
	Data     map[string]string `json:"data"`
}

// SendPush sends push notifications via the coordination server.
func (c *Client) SendPush(ctx context.Context, targets []PushTarget) error {
	if len(targets) == 0 {
		return nil
	}

	sig := ed25519.Sign(c.identityPriv, []byte(c.pubKeyB64))
	sigB64 := base64.StdEncoding.EncodeToString(sig)

	body := map[string]interface{}{
		"device_key": c.pubKeyB64,
		"sig":        sigB64,
		"targets":    targets,
	}

	data, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshal push: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.coordURL+"/v1/push", bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create push request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("push request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("push failed: status %d: %s", resp.StatusCode, string(respBody))
	}

	return nil
}

// PushTarget describes a single push notification target.
type PushTarget struct {
	APNSToken    string           `json:"apns_token"`
	Notification PushNotification `json:"notification"`
}

// discoverPublicEndpoint performs a STUN Binding Request to the coordination
// server's STUN endpoint and returns the reflexive public endpoint.
func (c *Client) discoverPublicEndpoint(ctx context.Context) (*Endpoint, error) {
	// Parse coord URL to extract the host for STUN.
	u, err := url.Parse(c.coordURL)
	if err != nil {
		return nil, fmt.Errorf("parse coord url: %w", err)
	}

	host := u.Hostname()
	stunAddr := net.JoinHostPort(host, "3478")

	// Resolve address.
	raddr, err := net.ResolveUDPAddr("udp4", stunAddr)
	if err != nil {
		return nil, fmt.Errorf("resolve stun addr %s: %w", stunAddr, err)
	}

	// Create UDP socket.
	conn, err := net.DialUDP("udp4", nil, raddr)
	if err != nil {
		return nil, fmt.Errorf("dial stun: %w", err)
	}
	defer conn.Close()

	// Set deadline.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(stunTimeout)
	}
	conn.SetDeadline(deadline)

	// Build and send STUN Binding Request.
	var txnID [12]byte
	if _, err := rand.Read(txnID[:]); err != nil {
		return nil, fmt.Errorf("generate stun txn id: %w", err)
	}

	reqData := buildSTUNBindingRequest(txnID)
	if _, err := conn.Write(reqData); err != nil {
		return nil, fmt.Errorf("send stun request: %w", err)
	}

	// Read response.
	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("read stun response: %w", err)
	}

	ip, port, err := parseSTUNBindingResponse(buf[:n])
	if err != nil {
		return nil, fmt.Errorf("parse stun response: %w", err)
	}

	return &Endpoint{
		IP:   ip.String(),
		Port: port,
		Type: "public",
	}, nil
}

// buildSTUNBindingRequest creates a minimal STUN Binding Request (RFC 5389).
func buildSTUNBindingRequest(txnID [12]byte) []byte {
	req := make([]byte, stunHeaderSize)
	binary.BigEndian.PutUint16(req[0:2], stunBindingRequest)
	binary.BigEndian.PutUint16(req[2:4], 0) // no attributes
	binary.BigEndian.PutUint32(req[4:8], stunMagicCookie)
	copy(req[8:20], txnID[:])
	return req
}

// parseSTUNBindingResponse extracts the XOR-MAPPED-ADDRESS from a STUN response.
func parseSTUNBindingResponse(data []byte) (net.IP, int, error) {
	if len(data) < stunHeaderSize {
		return nil, 0, fmt.Errorf("stun response too short: %d bytes", len(data))
	}

	msgType := binary.BigEndian.Uint16(data[0:2])
	if msgType != stunBindingResponse {
		return nil, 0, fmt.Errorf("not a stun binding response: 0x%04x", msgType)
	}

	attrLen := binary.BigEndian.Uint16(data[2:4])
	if int(attrLen)+stunHeaderSize > len(data) {
		return nil, 0, fmt.Errorf("stun response truncated")
	}

	attrs := data[stunHeaderSize : stunHeaderSize+int(attrLen)]

	for len(attrs) >= 4 {
		aType := binary.BigEndian.Uint16(attrs[0:2])
		aLen := binary.BigEndian.Uint16(attrs[2:4])

		if int(aLen)+4 > len(attrs) {
			break
		}

		if aType == stunAttrXORMapped && aLen >= 8 {
			family := attrs[5]
			if family != 0x01 { // IPv4 only
				attrs = attrs[4+aLen:]
				continue
			}

			xorPort := binary.BigEndian.Uint16(attrs[6:8])
			port := int(xorPort ^ uint16(stunMagicCookie>>16))

			var magicBytes [4]byte
			binary.BigEndian.PutUint32(magicBytes[:], stunMagicCookie)
			ip := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				ip[i] = attrs[8+i] ^ magicBytes[i]
			}

			return ip, port, nil
		}

		attrs = attrs[4+aLen:]
	}

	return nil, 0, fmt.Errorf("XOR-MAPPED-ADDRESS not found in stun response")
}

// getLocalIPs returns the machine's non-loopback IPv4 addresses.
func getLocalIPs() []string {
	var ips []string

	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() {
				continue
			}

			if ip4 := ip.To4(); ip4 != nil {
				ips = append(ips, ip4.String())
			}
		}
	}

	return ips
}
