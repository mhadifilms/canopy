package protocol

import (
	"bytes"
	"testing"
	"time"
)

func TestFrameRoundTrip(t *testing.T) {
	original := Frame{
		Type:    FrameOutputData,
		Payload: []byte("hello world"),
	}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, original); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}

	if got.Type != original.Type {
		t.Errorf("type: got %d, want %d", got.Type, original.Type)
	}
	if !bytes.Equal(got.Payload, original.Payload) {
		t.Errorf("payload: got %q, want %q", got.Payload, original.Payload)
	}
}

func TestJSONFrameRoundTrip(t *testing.T) {
	reg := SessionRegister{
		SessionID:    "test-123",
		ShellPID:     1234,
		TTYName:      "/dev/ttys001",
		CWD:          "/Users/test",
		Rows:         48,
		Cols:         120,
		Hostname:     "test-mac",
		RegisteredAt: time.Now().UTC().Truncate(time.Millisecond),
	}

	frame, err := MarshalJSONFrame(FrameSessionRegister, reg)
	if err != nil {
		t.Fatalf("MarshalJSONFrame: %v", err)
	}

	if frame.Type != FrameSessionRegister {
		t.Errorf("type: got %d, want %d", frame.Type, FrameSessionRegister)
	}

	var got SessionRegister
	if err := UnmarshalPayload(frame, &got); err != nil {
		t.Fatalf("UnmarshalPayload: %v", err)
	}

	if got.SessionID != reg.SessionID {
		t.Errorf("SessionID: got %q, want %q", got.SessionID, reg.SessionID)
	}
	if got.Rows != reg.Rows || got.Cols != reg.Cols {
		t.Errorf("dimensions: got %dx%d, want %dx%d", got.Rows, got.Cols, reg.Rows, reg.Cols)
	}
}

func TestReadFrameInvalidLength(t *testing.T) {
	// Frame length of 0 should fail.
	buf := bytes.NewBuffer([]byte{0, 0, 0, 0})
	_, err := ReadFrame(buf)
	if err == nil {
		t.Error("expected error for zero-length frame")
	}
}

func TestHeartbeatFrame(t *testing.T) {
	original := Frame{
		Type:    FrameHeartbeat,
		Payload: nil,
	}

	var buf bytes.Buffer
	if err := WriteFrame(&buf, original); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	got, err := ReadFrame(&buf)
	if err != nil {
		t.Fatalf("ReadFrame: %v", err)
	}

	if got.Type != FrameHeartbeat {
		t.Errorf("type: got %d, want %d", got.Type, FrameHeartbeat)
	}
}
