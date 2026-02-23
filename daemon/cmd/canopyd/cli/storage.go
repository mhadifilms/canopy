package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/canopy-dev/canopyd/internal/config"
	"github.com/canopy-dev/canopyd/internal/storage"
	"github.com/spf13/cobra"
)

var storageCmd = &cobra.Command{
	Use:   "storage",
	Short: "Manage session storage",
}

var storageStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show storage usage",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewDefault()
		if err != nil {
			return fmt.Errorf("open storage: %w", err)
		}

		ids, err := store.ListSessions()
		if err != nil {
			return fmt.Errorf("list sessions: %w", err)
		}

		sessDir, err := config.SessionsDir()
		if err != nil {
			return err
		}

		var totalBytes int64
		for _, id := range ids {
			dir := filepath.Join(sessDir, id)
			entries, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range entries {
				info, err := e.Info()
				if err != nil {
					continue
				}
				totalBytes += info.Size()
			}
		}

		cmd.Printf("Sessions: %d\n", len(ids))
		cmd.Printf("Storage:  %.2f MB\n", float64(totalBytes)/(1024*1024))

		cfg, err := config.Load()
		if err == nil {
			cmd.Printf("Limit:    %d GB\n", cfg.MaxStorageGB)
		}
		return nil
	},
}

var storagePruneDays int

var storagePruneCmd = &cobra.Command{
	Use:   "prune",
	Short: "Prune old sessions to reclaim disk space",
	RunE: func(cmd *cobra.Command, args []string) error {
		store, err := storage.NewDefault()
		if err != nil {
			return fmt.Errorf("open storage: %w", err)
		}

		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		days := cfg.RetentionDays
		if storagePruneDays > 0 {
			days = storagePruneDays
		}
		cutoff := time.Now().AddDate(0, 0, -days)

		ids, err := store.ListSessions()
		if err != nil {
			return fmt.Errorf("list sessions: %w", err)
		}

		sessDir, err := config.SessionsDir()
		if err != nil {
			return err
		}

		pruned := 0
		for _, id := range ids {
			meta, err := store.LoadMeta(id)
			if err != nil {
				continue
			}
			if meta.EndedAt != nil && meta.EndedAt.Before(cutoff) {
				if err := os.RemoveAll(filepath.Join(sessDir, id)); err == nil {
					pruned++
				}
			}
		}

		cmd.Printf("Pruned %d sessions older than %d days.\n", pruned, days)
		return nil
	},
}

var storageExportCmd = &cobra.Command{
	Use:   "export <session-id>",
	Short: "Export a session's data",
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

		sessDir, err := config.SessionsDir()
		if err != nil {
			return err
		}
		dir := filepath.Join(sessDir, args[0])

		cmd.Printf("Session: %s\n", meta.SessionID)
		cmd.Printf("Title:   %s\n", meta.Title)
		cmd.Printf("Status:  %s\n", meta.Status)
		cmd.Printf("Dir:     %s\n", dir)

		entries, err := os.ReadDir(dir)
		if err != nil {
			return fmt.Errorf("read session dir: %w", err)
		}

		cmd.Println("\nFiles:")
		for _, e := range entries {
			info, err := e.Info()
			if err != nil {
				continue
			}
			cmd.Printf("  %-20s  %d bytes\n", e.Name(), info.Size())
		}
		return nil
	},
}

func init() {
	storagePruneCmd.Flags().IntVar(&storagePruneDays, "days", 0, "Override retention days (0 = use config)")
	storageCmd.AddCommand(storageStatusCmd)
	storageCmd.AddCommand(storagePruneCmd)
	storageCmd.AddCommand(storageExportCmd)
}
