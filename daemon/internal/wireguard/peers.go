package wireguard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/canopy-dev/canopyd/internal/config"
)

// PeerConfig represents a paired device's WireGuard configuration.
type PeerConfig struct {
	DeviceID   string `json:"device_id"`
	Name       string `json:"name"`
	WGPubKey   string `json:"wg_public_key"`
	AllowedIP  string `json:"allowed_ip"`
	Endpoint   string `json:"endpoint,omitempty"`
	IdentityPub string `json:"identity_pub,omitempty"`
}

// PeerStore manages the paired devices on disk and in memory.
type PeerStore struct {
	mu    sync.RWMutex
	peers map[string]*PeerConfig // keyed by device_id
	path  string
}

// NewPeerStore creates a PeerStore backed by devices.json.
func NewPeerStore() (*PeerStore, error) {
	cfgDir, err := config.ConfigDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(cfgDir, "devices.json")

	ps := &PeerStore{
		peers: make(map[string]*PeerConfig),
		path:  path,
	}

	// Load existing peers.
	if data, err := os.ReadFile(path); err == nil {
		var peers []*PeerConfig
		if err := json.Unmarshal(data, &peers); err == nil {
			for _, p := range peers {
				ps.peers[p.DeviceID] = p
			}
		}
	}

	return ps, nil
}

// NewPeerStoreFromPath creates a PeerStore at a custom path (for testing).
func NewPeerStoreFromPath(path string) *PeerStore {
	ps := &PeerStore{
		peers: make(map[string]*PeerConfig),
		path:  path,
	}
	if data, err := os.ReadFile(path); err == nil {
		var peers []*PeerConfig
		if err := json.Unmarshal(data, &peers); err == nil {
			for _, p := range peers {
				ps.peers[p.DeviceID] = p
			}
		}
	}
	return ps
}

// Add registers a new peer and saves to disk.
func (ps *PeerStore) Add(peer *PeerConfig) error {
	ps.mu.Lock()
	ps.peers[peer.DeviceID] = peer
	ps.mu.Unlock()
	return ps.save()
}

// Remove removes a peer by device ID and saves.
func (ps *PeerStore) Remove(deviceID string) error {
	ps.mu.Lock()
	delete(ps.peers, deviceID)
	ps.mu.Unlock()
	return ps.save()
}

// Get returns a peer by device ID, or nil.
func (ps *PeerStore) Get(deviceID string) *PeerConfig {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.peers[deviceID]
}

// List returns all peers.
func (ps *PeerStore) List() []*PeerConfig {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	result := make([]*PeerConfig, 0, len(ps.peers))
	for _, p := range ps.peers {
		result = append(result, p)
	}
	return result
}

// Rename changes a peer's display name and saves.
func (ps *PeerStore) Rename(deviceID, newName string) error {
	ps.mu.Lock()
	if p, ok := ps.peers[deviceID]; ok {
		p.Name = newName
	}
	ps.mu.Unlock()
	return ps.save()
}

// Count returns the number of paired devices.
func (ps *PeerStore) Count() int {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return len(ps.peers)
}

func (ps *PeerStore) save() error {
	ps.mu.RLock()
	peers := make([]*PeerConfig, 0, len(ps.peers))
	for _, p := range ps.peers {
		peers = append(peers, p)
	}
	ps.mu.RUnlock()

	data, err := json.MarshalIndent(peers, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal peers: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(ps.path), 0755); err != nil {
		return fmt.Errorf("create dir: %w", err)
	}

	return os.WriteFile(ps.path, data, 0600)
}
