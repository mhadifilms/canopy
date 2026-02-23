package cli

import (
	"github.com/canopy-dev/canopyd/internal/update"
	"github.com/spf13/cobra"
)

var updateCheck bool

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update canopyd to the latest version",
	RunE: func(cmd *cobra.Command, args []string) error {
		if updateCheck {
			cmd.Println("Checking for updates...")
			result, err := update.Check()
			if err != nil {
				return err
			}
			cmd.Printf("Current version: %s\n", result.CurrentVersion)
			cmd.Printf("Latest version:  %s\n", result.LatestVersion)
			if result.UpdateAvail {
				cmd.Println("Update available! Run 'canopyd update' to install.")
			} else {
				cmd.Println("You are up to date.")
			}
			return nil
		}

		cmd.Println("Checking for updates...")
		result, err := update.Check()
		if err != nil {
			return err
		}
		if !result.UpdateAvail {
			cmd.Printf("Already up to date (version %s).\n", result.CurrentVersion)
			return nil
		}

		cmd.Printf("Updating from %s to %s...\n", result.CurrentVersion, result.LatestVersion)
		if err := update.Apply(); err != nil {
			return err
		}
		cmd.Printf("Updated to %s. Restart the daemon with: canopyd daemon restart\n", result.LatestVersion)
		return nil
	},
}

func init() {
	updateCmd.Flags().BoolVar(&updateCheck, "check", false, "Only check for updates, don't install")
}
