package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/canopy-dev/canopyd/internal/storage"
	"github.com/spf13/cobra"
)

var sessionsAll  bool
var sessionsJSON bool

var sessionsCmd = &cobra.Command{
	Use:   "sessions",
	Short: "List active terminal sessions",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewDefault()
		if err != nil {
			return fmt.Errorf("open storage: %w", err)
		}

		ids, err := store.ListSessions()
		if err != nil {
			return fmt.Errorf("list sessions: %w", err)
		}

		if len(ids) == 0 {
			cmd.Println("No sessions found.")
			return nil
		}

		type sessionEntry struct {
			ID     string `json:"session_id"`
			Title  string `json:"title"`
			Status string `json:"status"`
			Start  string `json:"started_at"`
		}
		var entries []sessionEntry

		for _, id := range ids {
			meta, err := store.LoadMeta(id)
			if err != nil {
				continue
			}
			if !sessionsAll && meta.Status == "ended" {
				continue
			}
			entries = append(entries, sessionEntry{
				ID:     meta.SessionID,
				Title:  meta.Title,
				Status: string(meta.Status),
				Start:  meta.StartedAt.Format(time.RFC3339),
			})
		}

		if len(entries) == 0 {
			cmd.Println("No active sessions.")
			return nil
		}

		if sessionsJSON {
			data, err := json.MarshalIndent(entries, "", "  ")
			if err != nil {
				return err
			}
			cmd.Println(string(data))
			return nil
		}

		for _, e := range entries {
			title := e.Title
			if title == "" {
				title = "(untitled)"
			}
			cmd.Printf("%-36s  %-8s  %s  %s\n", e.ID, e.Status, e.Start, title)
		}
		return nil
	},
}

var sessionsInfoCmd = &cobra.Command{
	Use:   "info <session-id>",
	Short: "Show detailed session info",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewDefault()
		if err != nil {
			return fmt.Errorf("open storage: %w", err)
		}

		meta, err := store.LoadMeta(args[0])
		if err != nil {
			return fmt.Errorf("load session: %w", err)
		}

		data, err := json.MarshalIndent(meta, "", "  ")
		if err != nil {
			return err
		}
		cmd.Println(string(data))
		return nil
	},
}

var sessionsEventsCmd = &cobra.Command{
	Use:   "events <session-id>",
	Short: "List session events",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewDefault()
		if err != nil {
			return fmt.Errorf("open storage: %w", err)
		}

		events, err := store.ReadEvents(args[0], nil, 0)
		if err != nil {
			return fmt.Errorf("read events: %w", err)
		}

		if len(events) == 0 {
			cmd.Println("No events.")
			return nil
		}

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

var sessionsKillCmd = &cobra.Command{
	Use:   "kill <session-id>",
	Short: "Kill a session",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// Killing a session requires connecting to the running daemon.
		// For now, just signal that this needs the daemon to be running.
		return fmt.Errorf("session kill requires a running daemon (use daemon ping to check)")
	},
}

func init() {
	sessionsCmd.Flags().BoolVar(&sessionsAll, "all", false, "Include ended sessions")
	sessionsCmd.Flags().BoolVar(&sessionsJSON, "json", false, "Output as JSON")
	sessionsCmd.AddCommand(sessionsInfoCmd)
	sessionsCmd.AddCommand(sessionsEventsCmd)
	sessionsCmd.AddCommand(sessionsKillCmd)
}
