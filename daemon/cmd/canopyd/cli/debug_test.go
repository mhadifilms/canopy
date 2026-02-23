package cli

import (
	"bytes"
	"encoding/binary"
	"io"
	"os"
	"testing"
)

// TestDebugRecordFileFormat verifies the record file format:
// each chunk is [4-byte big-endian length][payload].
func TestDebugRecordFileFormat(t *testing.T) {
	// Write chunks to a temp file.
	tmpFile, err := os.CreateTemp(t.TempDir(), "record-*.bin")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	defer tmpFile.Close()

	chunks := [][]byte{
		[]byte("hello world\r\n"),
		[]byte("\x1b[32mgreen text\x1b[0m"),
		[]byte("final chunk"),
	}

	for _, chunk := range chunks {
		if err := binary.Write(tmpFile, binary.BigEndian, uint32(len(chunk))); err != nil {
			t.Fatalf("write length: %v", err)
		}
		if _, err := tmpFile.Write(chunk); err != nil {
			t.Fatalf("write payload: %v", err)
		}
	}
	tmpFile.Close()

	// Read them back.
	readFile, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer readFile.Close()

	var got [][]byte
	for {
		var length uint32
		if err := binary.Read(readFile, binary.BigEndian, &length); err != nil {
			if err == io.EOF {
				break
			}
			t.Fatalf("read length: %v", err)
		}

		buf := make([]byte, length)
		if _, err := io.ReadFull(readFile, buf); err != nil {
			t.Fatalf("read payload: %v", err)
		}
		got = append(got, buf)
	}

	if len(got) != len(chunks) {
		t.Fatalf("chunks: got %d, want %d", len(got), len(chunks))
	}
	for i, chunk := range chunks {
		if !bytes.Equal(got[i], chunk) {
			t.Errorf("chunk[%d]: got %q, want %q", i, got[i], chunk)
		}
	}
}

// TestDebugRecordFileEmpty verifies reading an empty record file returns no chunks.
func TestDebugRecordFileEmpty(t *testing.T) {
	tmpFile, err := os.CreateTemp(t.TempDir(), "record-*.bin")
	if err != nil {
		t.Fatalf("create temp file: %v", err)
	}
	tmpFile.Close()

	readFile, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer readFile.Close()

	var length uint32
	err = binary.Read(readFile, binary.BigEndian, &length)
	if err != io.EOF {
		t.Errorf("expected EOF for empty file, got err=%v length=%d", err, length)
	}
}
