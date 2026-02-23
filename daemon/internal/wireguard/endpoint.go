package wireguard

import (
	"fmt"
	"net"

	"go.uber.org/zap"
)

// Endpoint represents the userspace WireGuard endpoint.
// Phase 1: manages keys and peer config but does not run a full WG tunnel.
// For Phase 1, the WebSocket API listens on localhost + LAN directly.
// Phase 2 will integrate wireguard-go for the full encrypted tunnel.
type Endpoint struct {
	privateKey [32]byte
	publicKey  [32]byte
	deviceID   string
	ip         string
	port       int
	peers      *PeerStore
	logger     *zap.Logger

	// UDP listener to reserve the port (Phase 1). Phase 2 replaces with wireguard-go.
	udpConn net.PacketConn
}

// EndpointConfig holds configuration for the WireGuard endpoint.
type EndpointConfig struct {
	ListenPort int
	DeviceID   string
}

// NewEndpoint creates a new WireGuard endpoint.
func NewEndpoint(cfg EndpointConfig, peers *PeerStore, logger *zap.Logger) (*Endpoint, error) {
	privKey, err := LoadWireGuardPrivateKey()
	if err != nil {
		return nil, fmt.Errorf("load wg private key: %w", err)
	}

	pubKey, err := LoadWireGuardPublicKey()
	if err != nil {
		return nil, fmt.Errorf("load wg public key: %w", err)
	}

	ip := AssignIP(cfg.DeviceID)
	port := cfg.ListenPort
	if port == 0 {
		port = 51820
	}

	// Check if port is available.
	if !isPortAvailable(port) {
		// Find a random available port.
		port, err = findAvailablePort()
		if err != nil {
			return nil, fmt.Errorf("find available port: %w", err)
		}
		logger.Info("wireguard default port taken, using fallback",
			zap.Int("port", port),
		)
	}

	return &Endpoint{
		privateKey: privKey,
		publicKey:  pubKey,
		deviceID:   cfg.DeviceID,
		ip:         ip,
		port:       port,
		peers:      peers,
		logger:     logger,
	}, nil
}

// NewEndpointDirect creates a WireGuard endpoint with keys provided directly
// (no disk I/O). Useful for testing and for cases where keys are already loaded.
func NewEndpointDirect(privKey, pubKey [32]byte, deviceID string, port int, peers *PeerStore, logger *zap.Logger) *Endpoint {
	ip := AssignIP(deviceID)
	return &Endpoint{
		privateKey: privKey,
		publicKey:  pubKey,
		deviceID:   deviceID,
		ip:         ip,
		port:       port,
		peers:      peers,
		logger:     logger,
	}
}

// IP returns the assigned WireGuard private IP.
func (e *Endpoint) IP() string {
	return e.ip
}

// Port returns the UDP listen port.
func (e *Endpoint) Port() int {
	return e.port
}

// PublicKey returns the WireGuard public key.
func (e *Endpoint) PublicKey() [32]byte {
	return e.publicKey
}

// DeviceID returns the device ID.
func (e *Endpoint) DeviceID() string {
	return e.deviceID
}

// APIListenAddr returns the address the WebSocket API should listen on.
// Phase 1: listen on all interfaces for local network access.
// Phase 2: will listen only on the WireGuard interface IP.
func (e *Endpoint) APIListenAddr(apiPort int) string {
	// Phase 1: listen on 0.0.0.0 so clients on the LAN can connect.
	return fmt.Sprintf(":%d", apiPort)
}

// Start begins the WireGuard endpoint.
// Phase 1: binds the UDP port to reserve it and prove liveness. The API server
// listens directly on the network. Phase 2 will integrate wireguard-go here.
func (e *Endpoint) Start() error {
	addr := fmt.Sprintf(":%d", e.port)
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("bind udp %s: %w", addr, err)
	}
	e.udpConn = conn

	// Update port in case it was 0 (OS-assigned).
	e.port = conn.LocalAddr().(*net.UDPAddr).Port

	e.logger.Info("wireguard endpoint started (phase 1 — direct network)",
		zap.String("ip", e.ip),
		zap.Int("port", e.port),
		zap.String("device_id", e.deviceID),
	)
	return nil
}

// Stop shuts down the WireGuard endpoint.
func (e *Endpoint) Stop() {
	if e.udpConn != nil {
		e.udpConn.Close()
		e.udpConn = nil
	}
	e.logger.Info("wireguard endpoint stopped")
}

// Running returns true if the endpoint has been started and not stopped.
func (e *Endpoint) Running() bool {
	return e.udpConn != nil
}

// AddPeer adds a WireGuard peer dynamically.
func (e *Endpoint) AddPeer(peer *PeerConfig) error {
	if err := e.peers.Add(peer); err != nil {
		return err
	}
	e.logger.Info("peer added",
		zap.String("device_id", peer.DeviceID),
		zap.String("name", peer.Name),
	)
	// Phase 2: update the wireguard-go peer list.
	return nil
}

// RemovePeer removes a WireGuard peer.
func (e *Endpoint) RemovePeer(deviceID string) error {
	if err := e.peers.Remove(deviceID); err != nil {
		return err
	}
	e.logger.Info("peer removed", zap.String("device_id", deviceID))
	// Phase 2: update the wireguard-go peer list.
	return nil
}

func isPortAvailable(port int) bool {
	addr := fmt.Sprintf(":%d", port)
	ln, err := net.ListenPacket("udp", addr)
	if err != nil {
		return false
	}
	ln.Close()
	return true
}

func findAvailablePort() (int, error) {
	ln, err := net.ListenPacket("udp", ":0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	return ln.LocalAddr().(*net.UDPAddr).Port, nil
}
