// Package protocol defines the Unix socket frame protocol between canopyd attach and the daemon.
package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Frame types for the Unix socket protocol (§3.4.2).
const (
	FrameSessionRegister byte = 0x01
	FrameOutputData      byte = 0x02
	FrameInputData       byte = 0x03
	FrameResize          byte = 0x04
	FrameSessionEnd      byte = 0x05
	FrameRemoteInput     byte = 0x06
	FrameHeartbeat       byte = 0x07
)

// MaxFrameSize is the maximum allowed frame payload (1MB).
const MaxFrameSize = 1 << 20

// Frame represents a single protocol frame on the Unix socket.
type Frame struct {
	Type    byte
	Payload []byte
}

// SessionRegister is the payload for FrameSessionRegister.
type SessionRegister struct {
	SessionID    string            `json:"session_id"`
	ShellPID     int               `json:"shell_pid"`
	Shell        string            `json:"shell"`
	TTYName      string            `json:"tty_name"`
	CWD          string            `json:"cwd"`
	Rows         int               `json:"rows"`
	Cols         int               `json:"cols"`
	Env          map[string]string `json:"env"`
	Hostname     string            `json:"hostname"`
	RegisteredAt time.Time         `json:"registered_at"`
}

// Resize is the payload for FrameResize.
type Resize struct {
	Rows int `json:"rows"`
	Cols int `json:"cols"`
}

// SessionEnd is the payload for FrameSessionEnd.
type SessionEnd struct {
	ExitCode int       `json:"exit_code"`
	EndedAt  time.Time `json:"ended_at"`
}

// WriteFrame writes a framed message to the writer.
// Wire format: [4 bytes big-endian length][1 byte type][payload].
func WriteFrame(w io.Writer, f Frame) error {
	length := uint32(1 + len(f.Payload))
	if err := binary.Write(w, binary.BigEndian, length); err != nil {
		return fmt.Errorf("write frame length: %w", err)
	}
	if _, err := w.Write([]byte{f.Type}); err != nil {
		return fmt.Errorf("write frame type: %w", err)
	}
	if len(f.Payload) > 0 {
		if _, err := w.Write(f.Payload); err != nil {
			return fmt.Errorf("write frame payload: %w", err)
		}
	}
	return nil
}

// ReadFrame reads a single frame from the reader.
func ReadFrame(r io.Reader) (Frame, error) {
	var length uint32
	if err := binary.Read(r, binary.BigEndian, &length); err != nil {
		return Frame{}, fmt.Errorf("read frame length: %w", err)
	}
	if length < 1 || length > MaxFrameSize {
		return Frame{}, fmt.Errorf("invalid frame length: %d", length)
	}

	buf := make([]byte, length)
	if _, err := io.ReadFull(r, buf); err != nil {
		return Frame{}, fmt.Errorf("read frame body: %w", err)
	}

	return Frame{
		Type:    buf[0],
		Payload: buf[1:],
	}, nil
}

// MarshalJSONFrame creates a frame with a JSON-encoded payload.
func MarshalJSONFrame(frameType byte, v any) (Frame, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return Frame{}, fmt.Errorf("marshal frame payload: %w", err)
	}
	return Frame{Type: frameType, Payload: data}, nil
}

// UnmarshalPayload decodes a JSON frame payload into v.
func UnmarshalPayload(f Frame, v any) error {
	return json.Unmarshal(f.Payload, v)
}
