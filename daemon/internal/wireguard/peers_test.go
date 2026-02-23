package wireguard

import (
	"path/filepath"
	"testing"
)

func TestPeerStoreAddAndGet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	ps := NewPeerStoreFromPath(path)

	peer := &PeerConfig{
		DeviceID:  "phone-1",
		Name:      "Hadi's iPhone",
		WGPubKey:  "abc123",
		AllowedIP: "100.100.42.1/32",
	}

	if err := ps.Add(peer); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got := ps.Get("phone-1")
	if got == nil {
		t.Fatal("Get returned nil")
	}
	if got.Name != "Hadi's iPhone" {
		t.Errorf("name: got %q", got.Name)
	}
	if got.AllowedIP != "100.100.42.1/32" {
		t.Errorf("allowed IP: got %q", got.AllowedIP)
	}
}

func TestPeerStoreRemove(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	ps := NewPeerStoreFromPath(path)

	ps.Add(&PeerConfig{DeviceID: "to-remove"})
	if err := ps.Remove("to-remove"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if ps.Get("to-remove") != nil {
		t.Error("peer should be removed")
	}
}

func TestPeerStoreList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	ps := NewPeerStoreFromPath(path)

	ps.Add(&PeerConfig{DeviceID: "a"})
	ps.Add(&PeerConfig{DeviceID: "b"})
	ps.Add(&PeerConfig{DeviceID: "c"})

	peers := ps.List()
	if len(peers) != 3 {
		t.Errorf("List: got %d, want 3", len(peers))
	}
}

func TestPeerStoreRename(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	ps := NewPeerStoreFromPath(path)

	ps.Add(&PeerConfig{DeviceID: "rename-me", Name: "Old Name"})
	if err := ps.Rename("rename-me", "New Name"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	got := ps.Get("rename-me")
	if got.Name != "New Name" {
		t.Errorf("name after rename: got %q", got.Name)
	}
}

func TestPeerStorePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")

	// Write peers.
	ps1 := NewPeerStoreFromPath(path)
	ps1.Add(&PeerConfig{DeviceID: "persist-test", Name: "My Phone", WGPubKey: "key123"})

	// Load from same path.
	ps2 := NewPeerStoreFromPath(path)
	got := ps2.Get("persist-test")
	if got == nil {
		t.Fatal("peer should persist across loads")
	}
	if got.Name != "My Phone" {
		t.Errorf("name: got %q", got.Name)
	}
	if got.WGPubKey != "key123" {
		t.Errorf("wg pub key: got %q", got.WGPubKey)
	}
}

func TestPeerStoreCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.json")
	ps := NewPeerStoreFromPath(path)

	if ps.Count() != 0 {
		t.Errorf("initial count: %d", ps.Count())
	}
	ps.Add(&PeerConfig{DeviceID: "1"})
	ps.Add(&PeerConfig{DeviceID: "2"})
	if ps.Count() != 2 {
		t.Errorf("count after 2 adds: %d", ps.Count())
	}
}
