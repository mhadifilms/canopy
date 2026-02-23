package cli

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/canopy-dev/canopyd/internal/parser"
	"github.com/canopy-dev/canopyd/internal/protocol"
	"github.com/canopy-dev/canopyd/internal/storage"
	"github.com/spf13/cobra"
)

var debugSessionID string

var debugCmd = &cobra.Command{
	Use:   "debug",
	Short: "Debug and diagnostic tools",
}

var debugRecordCmd = &cobra.Command{
	Use:   "record <file>",
	Short: "Record raw PTY output from a session to a file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if debugSessionID == "" {
			return fmt.Errorf("--session is required")
		}

		outPath := args[0]
		outFile, err := os.Create(outPath)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer outFile.Close()

		store, err := storage.NewDefault()
		if err != nil {
			return fmt.Errorf("open storage: %w", err)
		}

		sessionDir := store.SessionDir(debugSessionID)
		rawPath := filepath.Join(sessionDir, "raw.log")

		rawFile, err := os.Open(rawPath)
		if err != nil {
			return fmt.Errorf("open raw.log for session %s: %w", debugSessionID, err)
		}
		defer rawFile.Close()

		// Write raw.log into our recording format: [4-byte big-endian length][payload] per chunk.
		buf := make([]byte, 32*1024)
		totalBytes := int64(0)
		totalChunks := 0
		for {
			n, err := rawFile.Read(buf)
			if n > 0 {
				if writeErr := binary.Write(outFile, binary.BigEndian, uint32(n)); writeErr != nil {
					return fmt.Errorf("write chunk length: %w", writeErr)
				}
				if _, writeErr := outFile.Write(buf[:n]); writeErr != nil {
					return fmt.Errorf("write chunk data: %w", writeErr)
				}
				totalBytes += int64(n)
				totalChunks++
			}
			if err == io.EOF {
				break
			}
			if err != nil {
				return fmt.Errorf("read raw.log: %w", err)
			}
		}

		cmd.Printf("Recorded %d bytes (%d chunks) from session %s to %s\n",
			totalBytes, totalChunks, debugSessionID, outPath)
		return nil
	},
}

var debugReplayCmd = &cobra.Command{
	Use:   "replay <file>",
	Short: "Replay a recorded PTY session through the parser",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		inPath := args[0]
		inFile, err := os.Open(inPath)
		if err != nil {
			return fmt.Errorf("open recording: %w", err)
		}
		defer inFile.Close()

		pipeline := parser.NewPipeline()

		// Drain events concurrently.
		var wg sync.WaitGroup
		var events []parser.Event
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ev := range pipeline.Events() {
				events = append(events, ev)
			}
		}()

		// Read chunks and feed through pipeline.
		totalChunks := 0
		for {
			var length uint32
			if err := binary.Read(inFile, binary.BigEndian, &length); err != nil {
				if err == io.EOF {
					break
				}
				pipeline.Close()
				wg.Wait()
				return fmt.Errorf("read chunk length: %w", err)
			}
			if length > protocol.MaxFrameSize {
				pipeline.Close()
				wg.Wait()
				return fmt.Errorf("chunk too large: %d bytes", length)
			}

			buf := make([]byte, length)
			if _, err := io.ReadFull(inFile, buf); err != nil {
				pipeline.Close()
				wg.Wait()
				return fmt.Errorf("read chunk data: %w", err)
			}

			pipeline.FeedOutput(buf)
			totalChunks++
		}

		// Close pipeline to flush remaining data and close events channel.
		pipeline.Close()
		wg.Wait()

		cmd.Printf("Replayed %d chunks, %d events parsed:\n", totalChunks, len(events))
		for _, ev := range events {
			data, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			cmd.Println(string(data))
		}
		return nil
	},
}

func init() {
	debugRecordCmd.Flags().StringVar(&debugSessionID, "session", "", "Session ID to record")
	debugCmd.AddCommand(debugRecordCmd)
	debugCmd.AddCommand(debugReplayCmd)
}
