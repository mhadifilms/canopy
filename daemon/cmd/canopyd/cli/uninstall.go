package cli

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/canopy-dev/canopyd/internal/install"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Uninstall canopyd completely",
	Long: `Stops the daemon, removes the launchd plist, shell hooks, binary, and
optionally the config/session data directory.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println("Uninstalling canopyd...")

		// Prompt for data removal
		removeData := false
		cmd.Print("Remove session history and config? (~/.config/canopy/) [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer == "y" || answer == "yes" {
			removeData = true
		}

		if err := install.Uninstall(removeData); err != nil {
			return fmt.Errorf("uninstall: %w", err)
		}

		cmd.Println("  Daemon stopped")
		cmd.Println("  Launchd plist removed")
		cmd.Println("  Shell hooks removed")
		cmd.Println("  Binary removed")
		if removeData {
			cmd.Println("  Config and session data removed")
		}
		cmd.Println("\ncanopyd has been uninstalled. Open a new terminal tab for changes to take effect.")

		return nil
	},
}
