// Package turn implements a TURN-style UDP relay for forwarding encrypted WireGuard
// packets between paired Canopy devices when direct P2P connection fails.
//
// This is not a full RFC 5766 TURN implementation. It is a simplified relay designed
// specifically for forwarding opaque WireGuard UDP packets between two devices that
// have registered a pairing via the coordination server.
//
// The relay cannot decrypt the packets — they are WireGuard-encrypted end-to-end.
package turn

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
)

const (
	// MaxAllocationsPerDevice is the maximum concurrent TURN allocations per device.
	MaxAllocationsPerDevice = 10

	// AllocationTTL is the maximum lifetime of a single allocation.
	AllocationTTL = 1 * time.Hour

	// AllocationRefreshInterval is how often an allocation must be refreshed.
	AllocationRefreshInterval = 5 * time.Minute

	// MaxBandwidthBytesPerSec is the bandwidth limit per allocation (10 Mbps).
	MaxBandwidthBytesPerSec = 10 * 1024 * 1024 / 8 // 1.25 MB/s

	// MaxPacketSize is the maximum UDP packet size we relay.
	MaxPacketSize = 1500

	// Protocol message types (first byte of each packet to the relay port).
	MsgAllocate  byte = 0x01 // Request a new allocation
	MsgRefresh   byte = 0x02 // Refresh an existing allocation
	MsgRelease   byte = 0x03 // Release an allocation
	MsgData      byte = 0x04 // Relay data to peer
	MsgChannelData byte = 0x05 // Data from peer (forwarded by relay)

	// Response types.
	RespOK    byte = 0x10
	RespError byte = 0x11
)

// PairingChecker is used to verify that two devices are paired.
type PairingChecker interface {
	// CanRelay checks if the device identified by deviceKey is allowed to relay to peerKey.
	CanRelay(deviceKey, peerKey string) bool
}

// Allocation represents a TURN relay allocation between two devices.
type Allocation struct {
	ID         uint32
	DeviceKey  string // Identifying key of the requesting device
	PeerKey    string // Key of the peer device
	DeviceAddr *net.UDPAddr
	PeerAddr   *net.UDPAddr // Set when peer sends first packet
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastActive time.Time
	BytesIn    atomic.Int64
	BytesOut   atomic.Int64
}

// Relay is the TURN relay server.
type Relay struct {
	mu          sync.RWMutex
	conn        net.PacketConn
	logger      *zap.Logger
	checker     PairingChecker
	allocations map[uint32]*Allocation // ID -> allocation
	deviceAllocs map[string]map[uint32]bool // deviceKey -> set of allocation IDs
	addrIndex   map[string]uint32 // addr string -> allocation ID (for data forwarding)
	nextID      atomic.Uint32
	done        chan struct{}

	// Metrics.
	TotalAllocations atomic.Int64
	ActiveAllocations atomic.Int64
	TotalBytesRelayed atomic.Int64
}

// New creates a new TURN relay.
func New(checker PairingChecker, logger *zap.Logger) *Relay {
	r := &Relay{
		logger:       logger,
		checker:      checker,
		allocations:  make(map[uint32]*Allocation),
		deviceAllocs: make(map[string]map[uint32]bool),
		addrIndex:    make(map[string]uint32),
		done:         make(chan struct{}),
	}
	r.nextID.Store(1)
	return r
}

// ListenAndServe starts the TURN relay on the given UDP address.
func (r *Relay) ListenAndServe(addr string) error {
	conn, err := net.ListenPacket("udp", addr)
	if err != nil {
		return fmt.Errorf("bind udp: %w", err)
	}
	r.conn = conn

	r.logger.Info("turn relay started", zap.String("addr", addr))

	go r.serve()
	go r.expiryLoop()
	return nil
}

// Addr returns the listen address.
func (r *Relay) Addr() net.Addr {
	if r.conn == nil {
		return nil
	}
	return r.conn.LocalAddr()
}

// Close shuts down the relay.
func (r *Relay) Close() error {
	close(r.done)
	if r.conn != nil {
		return r.conn.Close()
	}
	return nil
}

func (r *Relay) serve() {
	buf := make([]byte, MaxPacketSize+64)

	for {
		select {
		case <-r.done:
			return
		default:
		}

		n, addr, err := r.conn.ReadFrom(buf)
		if err != nil {
			select {
			case <-r.done:
				return
			default:
				r.logger.Error("turn read error", zap.Error(err))
				continue
			}
		}

		if n < 1 {
			continue
		}

		udpAddr, ok := addr.(*net.UDPAddr)
		if !ok {
			continue
		}

		msgType := buf[0]
		payload := buf[1:n]

		switch msgType {
		case MsgAllocate:
			r.handleAllocate(udpAddr, payload)
		case MsgRefresh:
			r.handleRefresh(udpAddr, payload)
		case MsgRelease:
			r.handleRelease(udpAddr, payload)
		case MsgData:
			r.handleData(udpAddr, payload)
		default:
			// Unknown message type, ignore.
		}
	}
}

// handleAllocate processes an allocation request.
// Payload: [32 bytes: device_key][32 bytes: peer_key]
func (r *Relay) handleAllocate(addr *net.UDPAddr, payload []byte) {
	if len(payload) < 64 {
		r.sendError(addr, "invalid_payload")
		return
	}

	deviceKey := trimNull(string(payload[:32]))
	peerKey := trimNull(string(payload[32:64]))

	// Check pairing.
	if !r.checker.CanRelay(deviceKey, peerKey) {
		r.sendError(addr, "not_paired")
		return
	}

	// Check per-device limit.
	r.mu.RLock()
	count := len(r.deviceAllocs[deviceKey])
	r.mu.RUnlock()

	if count >= MaxAllocationsPerDevice {
		r.sendError(addr, "max_allocations")
		return
	}

	now := time.Now()
	id := r.nextID.Add(1) - 1

	alloc := &Allocation{
		ID:         id,
		DeviceKey:  deviceKey,
		PeerKey:    peerKey,
		DeviceAddr: addr,
		CreatedAt:  now,
		ExpiresAt:  now.Add(AllocationTTL),
		LastActive: now,
	}

	r.mu.Lock()
	r.allocations[id] = alloc
	if r.deviceAllocs[deviceKey] == nil {
		r.deviceAllocs[deviceKey] = make(map[uint32]bool)
	}
	r.deviceAllocs[deviceKey][id] = true
	r.addrIndex[addr.String()] = id
	r.mu.Unlock()

	r.TotalAllocations.Add(1)
	r.ActiveAllocations.Add(1)

	r.logger.Info("turn allocation created",
		zap.Uint32("id", id),
		zap.String("device_addr", addr.String()),
	)

	// Send allocation response: [RespOK][4 bytes: allocation ID][8 bytes: expiry unix]
	resp := make([]byte, 13)
	resp[0] = RespOK
	binary.BigEndian.PutUint32(resp[1:5], id)
	binary.BigEndian.PutUint64(resp[5:13], uint64(alloc.ExpiresAt.Unix()))
	if _, err := r.conn.WriteTo(resp, addr); err != nil {
		r.logger.Warn("turn write allocation response", zap.Error(err), zap.String("addr", addr.String()))
	}
}

// handleRefresh extends an allocation's lifetime.
// Payload: [4 bytes: allocation ID]
func (r *Relay) handleRefresh(addr *net.UDPAddr, payload []byte) {
	if len(payload) < 4 {
		r.sendError(addr, "invalid_payload")
		return
	}

	id := binary.BigEndian.Uint32(payload[:4])

	r.mu.Lock()
	alloc, ok := r.allocations[id]
	if !ok {
		r.mu.Unlock()
		r.sendError(addr, "not_found")
		return
	}

	// Verify the request comes from the allocation's device.
	if alloc.DeviceAddr.String() != addr.String() {
		r.mu.Unlock()
		r.sendError(addr, "unauthorized")
		return
	}

	now := time.Now()
	alloc.ExpiresAt = now.Add(AllocationTTL)
	alloc.LastActive = now
	r.mu.Unlock()

	// Send response.
	resp := make([]byte, 13)
	resp[0] = RespOK
	binary.BigEndian.PutUint32(resp[1:5], id)
	binary.BigEndian.PutUint64(resp[5:13], uint64(alloc.ExpiresAt.Unix()))
	if _, err := r.conn.WriteTo(resp, addr); err != nil {
		r.logger.Warn("turn write refresh response", zap.Error(err), zap.String("addr", addr.String()))
	}
}

// handleRelease removes an allocation.
// Payload: [4 bytes: allocation ID]
func (r *Relay) handleRelease(addr *net.UDPAddr, payload []byte) {
	if len(payload) < 4 {
		r.sendError(addr, "invalid_payload")
		return
	}

	id := binary.BigEndian.Uint32(payload[:4])

	r.mu.Lock()
	alloc, ok := r.allocations[id]
	if !ok {
		r.mu.Unlock()
		r.sendError(addr, "not_found")
		return
	}

	if alloc.DeviceAddr.String() != addr.String() {
		r.mu.Unlock()
		r.sendError(addr, "unauthorized")
		return
	}

	r.removeAllocationLocked(id)
	r.mu.Unlock()

	r.ActiveAllocations.Add(-1)

	r.logger.Info("turn allocation released", zap.Uint32("id", id))

	resp := []byte{RespOK}
	if _, err := r.conn.WriteTo(resp, addr); err != nil {
		r.logger.Warn("turn write release response", zap.Error(err), zap.String("addr", addr.String()))
	}
}

// handleData relays data to the peer.
// Payload (after MsgData byte is stripped by serve): [4 bytes: allocation ID][N bytes: data]
func (r *Relay) handleData(addr *net.UDPAddr, payload []byte) {
	if len(payload) < 5 {
		return
	}

	id := binary.BigEndian.Uint32(payload[:4])
	data := payload[4:]

	r.mu.Lock()
	alloc, ok := r.allocations[id]
	if !ok {
		r.mu.Unlock()
		return
	}

	// Determine sender and set peer address if needed.
	var targetAddr *net.UDPAddr
	if alloc.DeviceAddr.String() == addr.String() {
		targetAddr = alloc.PeerAddr
	} else {
		// This is the peer. Register their address if not yet known.
		if alloc.PeerAddr == nil {
			alloc.PeerAddr = addr
			r.addrIndex[addr.String()] = id
		}
		targetAddr = alloc.DeviceAddr
	}
	alloc.LastActive = time.Now()
	r.mu.Unlock()

	if targetAddr == nil {
		// Peer hasn't connected yet; drop the packet.
		return
	}

	r.relayData(alloc, data, targetAddr)
}

func (r *Relay) relayData(alloc *Allocation, data []byte, target *net.UDPAddr) {
	// Wrap data with channel data prefix so the receiver knows it's relayed.
	msg := make([]byte, 5+len(data))
	msg[0] = MsgChannelData
	binary.BigEndian.PutUint32(msg[1:5], alloc.ID)
	copy(msg[5:], data)

	if _, err := r.conn.WriteTo(msg, target); err != nil {
		r.logger.Error("turn relay write error", zap.Error(err))
		return
	}

	alloc.BytesIn.Add(int64(len(data)))
	alloc.BytesOut.Add(int64(len(msg)))
	r.TotalBytesRelayed.Add(int64(len(data)))
}

func (r *Relay) sendError(addr *net.UDPAddr, code string) {
	// Cap the error code length so a pathological caller can't amplify tiny
	// requests into large responses.
	if len(code) > 64 {
		code = code[:64]
	}
	msg := make([]byte, 1+len(code))
	msg[0] = RespError
	copy(msg[1:], code)
	if _, err := r.conn.WriteTo(msg, addr); err != nil {
		r.logger.Debug("turn write error response", zap.Error(err))
	}
}

func (r *Relay) removeAllocationLocked(id uint32) {
	alloc, ok := r.allocations[id]
	if !ok {
		return
	}

	delete(r.allocations, id)
	if alloc.DeviceAddr != nil {
		delete(r.addrIndex, alloc.DeviceAddr.String())
	}
	if alloc.PeerAddr != nil {
		delete(r.addrIndex, alloc.PeerAddr.String())
	}
	if devAllocs := r.deviceAllocs[alloc.DeviceKey]; devAllocs != nil {
		delete(devAllocs, id)
		if len(devAllocs) == 0 {
			delete(r.deviceAllocs, alloc.DeviceKey)
		}
	}
}

func (r *Relay) expiryLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.expireAllocations()
		}
	}
}

func (r *Relay) expireAllocations() {
	now := time.Now()

	r.mu.Lock()
	defer r.mu.Unlock()

	for id, alloc := range r.allocations {
		if now.After(alloc.ExpiresAt) {
			r.removeAllocationLocked(id)
			r.ActiveAllocations.Add(-1)
			r.logger.Info("turn allocation expired", zap.Uint32("id", id))
		}
	}
}

// Stats returns current relay statistics.
func (r *Relay) Stats() RelayStats {
	return RelayStats{
		TotalAllocations:  r.TotalAllocations.Load(),
		ActiveAllocations: r.ActiveAllocations.Load(),
		TotalBytesRelayed: r.TotalBytesRelayed.Load(),
	}
}

// RelayStats contains relay metrics.
type RelayStats struct {
	TotalAllocations  int64 `json:"total_allocations"`
	ActiveAllocations int64 `json:"active_allocations"`
	TotalBytesRelayed int64 `json:"total_bytes_relayed"`
}

// AllocateRequest creates an allocation request message.
func AllocateRequest(deviceKey, peerKey string) []byte {
	if len(deviceKey) > 32 {
		deviceKey = deviceKey[:32]
	}
	if len(peerKey) > 32 {
		peerKey = peerKey[:32]
	}

	msg := make([]byte, 65)
	msg[0] = MsgAllocate
	copy(msg[1:33], padOrTruncate(deviceKey, 32))
	copy(msg[33:65], padOrTruncate(peerKey, 32))
	return msg
}

// RefreshRequest creates a refresh request message.
func RefreshRequest(allocID uint32) []byte {
	msg := make([]byte, 5)
	msg[0] = MsgRefresh
	binary.BigEndian.PutUint32(msg[1:5], allocID)
	return msg
}

// ReleaseRequest creates a release request message.
func ReleaseRequest(allocID uint32) []byte {
	msg := make([]byte, 5)
	msg[0] = MsgRelease
	binary.BigEndian.PutUint32(msg[1:5], allocID)
	return msg
}

// DataMessage creates a data message to relay through the TURN server.
func DataMessage(allocID uint32, data []byte) []byte {
	msg := make([]byte, 5+len(data))
	msg[0] = MsgData
	binary.BigEndian.PutUint32(msg[1:5], allocID)
	copy(msg[5:], data)
	return msg
}

// ParseAllocateResponse parses an allocate/refresh response.
func ParseAllocateResponse(data []byte) (allocID uint32, expiresUnix int64, err error) {
	if len(data) < 1 {
		return 0, 0, errors.New("empty response")
	}
	if data[0] == RespError {
		return 0, 0, fmt.Errorf("error: %s", string(data[1:]))
	}
	if data[0] != RespOK || len(data) < 13 {
		return 0, 0, errors.New("invalid response")
	}
	allocID = binary.BigEndian.Uint32(data[1:5])
	expiresUnix = int64(binary.BigEndian.Uint64(data[5:13]))
	return allocID, expiresUnix, nil
}

// ParseChannelData parses a relayed data message.
func ParseChannelData(data []byte) (allocID uint32, payload []byte, err error) {
	if len(data) < 5 {
		return 0, nil, errors.New("too short")
	}
	if data[0] != MsgChannelData {
		return 0, nil, errors.New("not channel data")
	}
	allocID = binary.BigEndian.Uint32(data[1:5])
	payload = data[5:]
	return allocID, payload, nil
}

func padOrTruncate(s string, size int) []byte {
	b := make([]byte, size)
	copy(b, s)
	return b
}

func trimNull(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != 0 {
			return s[:i+1]
		}
	}
	return ""
}
