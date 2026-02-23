// Package cli defines all cobra commands for the canopyd CLI.
package cli

import (
	"github.com/canopy-dev/canopyd/internal/config"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "canopyd",
	Short: "Canopy daemon — your Mac's terminal, on your phone",
	Long:  "canopyd captures all terminal sessions and exposes them to the Canopy iOS app via an encrypted tunnel.",
}

func init() {
	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(attachCmd)
	rootCmd.AddCommand(pairCmd)
	rootCmd.AddCommand(devicesCmd)
	rootCmd.AddCommand(sessionsCmd)
	rootCmd.AddCommand(storageCmd)
	rootCmd.AddCommand(configCmd)
	rootCmd.AddCommand(shellHookCmd)
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(uninstallCmd)
	rootCmd.AddCommand(updateCmd)
	rootCmd.AddCommand(debugCmd)
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print canopyd version",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Printf("canopyd %s\n", config.Version)
	},
}
