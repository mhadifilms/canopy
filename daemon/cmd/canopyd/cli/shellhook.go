package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var shellHookCmd = &cobra.Command{
	Use:       "shell-hook <zsh|bash|fish>",
	Short:     "Output the shell hook for the given shell",
	Long:      "Prints the shell hook snippet that should be added to the shell's rc file.",
	Args:      cobra.ExactArgs(1),
	ValidArgs: []string{"zsh", "bash", "fish"},
	RunE: func(cmd *cobra.Command, args []string) error {
		shell := args[0]
		switch shell {
		case "zsh":
			cmd.Println(zshHook)
		case "bash":
			cmd.Println(bashHook)
		case "fish":
			cmd.Println(fishHook)
		default:
			return fmt.Errorf("unsupported shell: %s (use zsh, bash, or fish)", shell)
		}
		return nil
	},
}

const zshHook = `# --- Canopy Hook (do not edit) ---
if [ -z "$CANOPY_SESSION_ID" ] && command -v canopyd &>/dev/null && canopyd daemon ping &>/dev/null; then
  export CANOPY_SESSION_ID=$(uuidgen)
  exec canopyd attach --session-id "$CANOPY_SESSION_ID" -- "$SHELL" -l
fi
# --- End Canopy Hook ---`

const bashHook = `# --- Canopy Hook (do not edit) ---
if [ -z "$CANOPY_SESSION_ID" ] && command -v canopyd &>/dev/null && canopyd daemon ping &>/dev/null; then
  export CANOPY_SESSION_ID=$(uuidgen)
  exec canopyd attach --session-id "$CANOPY_SESSION_ID" -- "$SHELL" -l
fi
# --- End Canopy Hook ---`

const fishHook = `# --- Canopy Hook (do not edit) ---
if not set -q CANOPY_SESSION_ID; and command -v canopyd &>/dev/null; and canopyd daemon ping &>/dev/null
  set -gx CANOPY_SESSION_ID (uuidgen)
  exec canopyd attach --session-id $CANOPY_SESSION_ID -- $SHELL -l
end
# --- End Canopy Hook ---`
