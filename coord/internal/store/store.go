// Package store provides in-memory storage for device registrations and pairings.
// For the MVP, this is map + mutex. Production could use Redis for multi-instance.
package store

import (
	"sync"
	"time"
)

// Endpoint represents a network endpoint (IP + port) for a device.
type Endpoint struct {
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Type     string `json:"type"` // "public" or "local"
	LastSeen string `json:"last_seen"`
}

// DeviceRecord holds the latest check-in data for a device.
type DeviceRecord struct {
	DeviceKey    string     // Ed25519 public key (base64)
	WGPublicKey  string     // WireGuard public key (base64)
	Endpoints    []Endpoint
	PairedDevices []string  // Peer WG public keys
	APNSTokens   []string
	LastCheckin  time.Time
}

// PairingRecord tracks a registered pairing between two devices.
type PairingRecord struct {
	DeviceKey string // Ed25519 public key of the registering device
	PeerWGKey string // WireGuard public key of the peer
	CreatedAt time.Time
}

// PairingSession tracks a 6-digit code pairing handshake between Mac and iPhone.
type PairingSession struct {
	Code      string
	Status    string // "pending" or "confirmed"
	Hostname  string
	DeviceID  string
	WGPub     string
	Identity  string
	CreatedAt time.Time
}

// Store is the in-memory storage backend for the coordination server.
type Store struct {
	mu              sync.RWMutex
	devices         map[string]*DeviceRecord   // keyed by Ed25519 public key (base64)
	pairings        map[string]map[string]bool  // device_key -> set of peer WG keys
	wgIndex         map[string]string           // WG public key -> device Ed25519 key (for lookups)
	pairingSessions map[string]*PairingSession  // keyed by 6-digit code
}

// New creates a new empty Store.
func New() *Store {
	return &Store{
		devices:         make(map[string]*DeviceRecord),
		pairings:        make(map[string]map[string]bool),
		wgIndex:         make(map[string]string),
		pairingSessions: make(map[string]*PairingSession),
	}
}

// Checkin stores or updates a device's registration.
func (s *Store) Checkin(deviceKey, wgPubKey string, endpoints []Endpoint, pairedDevices []string, apnsTokens []string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339)

	// Set LastSeen on endpoints.
	for i := range endpoints {
		endpoints[i].LastSeen = nowStr
	}

	s.devices[deviceKey] = &DeviceRecord{
		DeviceKey:     deviceKey,
		WGPublicKey:   wgPubKey,
		Endpoints:     endpoints,
		PairedDevices: pairedDevices,
		APNSTokens:    apnsTokens,
		LastCheckin:   now,
	}

	// Index WG key -> device key.
	s.wgIndex[wgPubKey] = deviceKey
}

// LookupByWGKey retrieves a device record by its WireGuard public key.
// Returns nil if not found.
func (s *Store) LookupByWGKey(wgPubKey string) *DeviceRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deviceKey, ok := s.wgIndex[wgPubKey]
	if !ok {
		return nil
	}

	rec := s.devices[deviceKey]
	if rec == nil {
		return nil
	}

	// Return a copy to avoid data races.
	cp := *rec
	cp.Endpoints = make([]Endpoint, len(rec.Endpoints))
	copy(cp.Endpoints, rec.Endpoints)
	return &cp
}

// LookupByDeviceKey retrieves a device record by its Ed25519 public key.
func (s *Store) LookupByDeviceKey(deviceKey string) *DeviceRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rec := s.devices[deviceKey]
	if rec == nil {
		return nil
	}

	cp := *rec
	cp.Endpoints = make([]Endpoint, len(rec.Endpoints))
	copy(cp.Endpoints, rec.Endpoints)
	return &cp
}

// RegisterPairing records a pairing between a device and a peer.
func (s *Store) RegisterPairing(deviceKey, peerWGKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.pairings[deviceKey] == nil {
		s.pairings[deviceKey] = make(map[string]bool)
	}
	s.pairings[deviceKey][peerWGKey] = true
}

// IsPaired checks if a device has registered a pairing with a specific WG key.
func (s *Store) IsPaired(deviceKey, peerWGKey string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	peers := s.pairings[deviceKey]
	if peers == nil {
		return false
	}
	return peers[peerWGKey]
}

// CanLookup checks if requesterDeviceKey is allowed to look up peerWGKey.
// The requester must have the peer in its paired_devices list, OR the peer must
// have registered a pairing that includes the requester.
func (s *Store) CanLookup(requesterDeviceKey, peerWGKey string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check if requester's check-in listed this peer.
	if rec := s.devices[requesterDeviceKey]; rec != nil {
		for _, pk := range rec.PairedDevices {
			if pk == peerWGKey {
				return true
			}
		}
	}

	// Check explicit pairing registrations.
	if peers := s.pairings[requesterDeviceKey]; peers != nil && peers[peerWGKey] {
		return true
	}

	// Check if the peer has registered a pairing that includes requester's WG key.
	peerDeviceKey := s.wgIndex[peerWGKey]
	if peerDeviceKey != "" {
		if peers := s.pairings[peerDeviceKey]; peers != nil {
			// The peer registered; check if they registered the requester's WG key.
			if requesterRec := s.devices[requesterDeviceKey]; requesterRec != nil {
				if peers[requesterRec.WGPublicKey] {
					return true
				}
			}
		}
	}

	return false
}

// GetAPNSTokens returns the APNs tokens for a device identified by WG public key.
func (s *Store) GetAPNSTokens(wgPubKey string) []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deviceKey, ok := s.wgIndex[wgPubKey]
	if !ok {
		return nil
	}
	rec := s.devices[deviceKey]
	if rec == nil {
		return nil
	}
	tokens := make([]string, len(rec.APNSTokens))
	copy(tokens, rec.APNSTokens)
	return tokens
}

// IsOnline returns true if a device checked in within the last 90 seconds.
func (s *Store) IsOnline(wgPubKey string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	deviceKey, ok := s.wgIndex[wgPubKey]
	if !ok {
		return false
	}
	rec := s.devices[deviceKey]
	if rec == nil {
		return false
	}
	return time.Since(rec.LastCheckin) < 90*time.Second
}

// Cleanup removes stale device records older than maxAge.
func (s *Store) Cleanup(maxAge time.Duration) int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	removed := 0

	for key, rec := range s.devices {
		if rec.LastCheckin.Before(cutoff) {
			delete(s.wgIndex, rec.WGPublicKey)
			delete(s.devices, key)
			delete(s.pairings, key)
			removed++
		}
	}

	return removed
}

// DeviceCount returns the number of registered devices.
func (s *Store) DeviceCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.devices)
}

// CreatePairingSession stores a pending pairing session for a 6-digit code.
func (s *Store) CreatePairingSession(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.pairingSessions[code] = &PairingSession{
		Code:      code,
		Status:    "pending",
		CreatedAt: time.Now(),
	}
}

// ConfirmPairingSession marks a pairing session as confirmed with Mac device info.
func (s *Store) ConfirmPairingSession(code, hostname, deviceID, wgPub, identity string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, ok := s.pairingSessions[code]
	if !ok {
		return false
	}
	sess.Status = "confirmed"
	sess.Hostname = hostname
	sess.DeviceID = deviceID
	sess.WGPub = wgPub
	sess.Identity = identity
	return true
}

// GetPairingSession retrieves a pairing session by code.
// Returns nil if not found or expired (older than 5 minutes).
func (s *Store) GetPairingSession(code string) *PairingSession {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.pairingSessions[code]
	if !ok {
		return nil
	}
	if time.Since(sess.CreatedAt) > 5*time.Minute {
		return nil
	}
	cp := *sess
	return &cp
}

// CleanupPairingSessions removes pairing sessions older than 5 minutes.
func (s *Store) CleanupPairingSessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	cutoff := time.Now().Add(-5 * time.Minute)
	removed := 0
	for code, sess := range s.pairingSessions {
		if sess.CreatedAt.Before(cutoff) {
			delete(s.pairingSessions, code)
			removed++
		}
	}
	return removed
}
