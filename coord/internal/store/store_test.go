package store

import (
	"testing"
	"time"
)

func TestCheckinAndLookup(t *testing.T) {
	s := New()

	endpoints := []Endpoint{
		{IP: "203.0.113.42", Port: 51820, Type: "public"},
		{IP: "192.168.1.100", Port: 51820, Type: "local"},
	}

	s.Checkin("device-key-1", "wg-pub-1", endpoints, []string{"wg-pub-peer"}, nil)

	// Lookup by WG key.
	rec := s.LookupByWGKey("wg-pub-1")
	if rec == nil {
		t.Fatal("expected device record")
	}
	if rec.DeviceKey != "device-key-1" {
		t.Fatalf("device key: got %q, want %q", rec.DeviceKey, "device-key-1")
	}
	if len(rec.Endpoints) != 2 {
		t.Fatalf("endpoints: got %d, want 2", len(rec.Endpoints))
	}
	if rec.Endpoints[0].IP != "203.0.113.42" {
		t.Fatalf("endpoint IP: got %q, want %q", rec.Endpoints[0].IP, "203.0.113.42")
	}
	if rec.Endpoints[0].LastSeen == "" {
		t.Fatal("expected LastSeen to be set")
	}

	// Lookup by device key.
	rec2 := s.LookupByDeviceKey("device-key-1")
	if rec2 == nil {
		t.Fatal("expected device record by device key")
	}

	// Unknown key.
	if s.LookupByWGKey("unknown") != nil {
		t.Fatal("expected nil for unknown WG key")
	}
}

func TestCheckinUpdates(t *testing.T) {
	s := New()

	s.Checkin("dk", "wg1", []Endpoint{{IP: "1.1.1.1", Port: 100, Type: "public"}}, nil, nil)
	s.Checkin("dk", "wg1", []Endpoint{{IP: "2.2.2.2", Port: 200, Type: "public"}}, nil, nil)

	rec := s.LookupByWGKey("wg1")
	if rec == nil {
		t.Fatal("expected device record")
	}
	if rec.Endpoints[0].IP != "2.2.2.2" {
		t.Fatalf("expected updated endpoint, got %q", rec.Endpoints[0].IP)
	}
}

func TestPairing(t *testing.T) {
	s := New()

	s.RegisterPairing("device-a", "wg-pub-b")

	if !s.IsPaired("device-a", "wg-pub-b") {
		t.Fatal("expected pairing to be registered")
	}
	if s.IsPaired("device-a", "wg-pub-c") {
		t.Fatal("expected no pairing for unknown peer")
	}
	if s.IsPaired("device-x", "wg-pub-b") {
		t.Fatal("expected no pairing for unknown device")
	}
}

func TestCanLookup(t *testing.T) {
	s := New()

	// Device A checks in listing wg-pub-b as a paired device.
	s.Checkin("device-a", "wg-pub-a", nil, []string{"wg-pub-b"}, nil)
	s.Checkin("device-b", "wg-pub-b", nil, []string{"wg-pub-a"}, nil)

	// A can look up B via paired_devices list.
	if !s.CanLookup("device-a", "wg-pub-b") {
		t.Fatal("A should be able to look up B via paired_devices")
	}

	// B can look up A via paired_devices list.
	if !s.CanLookup("device-b", "wg-pub-a") {
		t.Fatal("B should be able to look up A via paired_devices")
	}

	// Unknown device cannot look up anything.
	if s.CanLookup("device-unknown", "wg-pub-a") {
		t.Fatal("unknown device should not be able to look up")
	}

	// Explicit pairing registration works.
	s.Checkin("device-c", "wg-pub-c", nil, nil, nil)
	s.RegisterPairing("device-c", "wg-pub-a")
	if !s.CanLookup("device-c", "wg-pub-a") {
		t.Fatal("C should be able to look up A via registered pairing")
	}
}

func TestIsOnline(t *testing.T) {
	s := New()

	s.Checkin("dk", "wg1", nil, nil, nil)
	if !s.IsOnline("wg1") {
		t.Fatal("just-checked-in device should be online")
	}

	if s.IsOnline("unknown") {
		t.Fatal("unknown device should not be online")
	}
}

func TestCleanup(t *testing.T) {
	s := New()

	s.Checkin("dk1", "wg1", nil, nil, nil)
	s.Checkin("dk2", "wg2", nil, nil, nil)

	// Manually age one record.
	s.mu.Lock()
	s.devices["dk1"].LastCheckin = time.Now().Add(-2 * time.Hour)
	s.mu.Unlock()

	removed := s.Cleanup(1 * time.Hour)
	if removed != 1 {
		t.Fatalf("removed: got %d, want 1", removed)
	}

	if s.LookupByDeviceKey("dk1") != nil {
		t.Fatal("stale device should have been removed")
	}
	if s.LookupByDeviceKey("dk2") == nil {
		t.Fatal("fresh device should still exist")
	}
}

func TestDeviceCount(t *testing.T) {
	s := New()
	if s.DeviceCount() != 0 {
		t.Fatalf("expected 0 devices, got %d", s.DeviceCount())
	}

	s.Checkin("dk1", "wg1", nil, nil, nil)
	s.Checkin("dk2", "wg2", nil, nil, nil)

	if s.DeviceCount() != 2 {
		t.Fatalf("expected 2 devices, got %d", s.DeviceCount())
	}
}

func TestAPNSTokens(t *testing.T) {
	s := New()

	s.Checkin("dk1", "wg1", nil, nil, []string{"token-a", "token-b"})

	tokens := s.GetAPNSTokens("wg1")
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %d", len(tokens))
	}

	if s.GetAPNSTokens("unknown") != nil {
		t.Fatal("expected nil for unknown")
	}
}
