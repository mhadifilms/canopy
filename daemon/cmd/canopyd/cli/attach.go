package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/canopy-dev/canopyd/internal/attach"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var attachSessionID string

var attachCmd = &cobra.Command{
	Use:   "attach --session-id UUID -- command [args...]",
	Short: "Start a PTY proxy for a terminal session",
	Long: `Wraps a command in a PTY proxy that captures all I/O and forwards it
to the canopyd daemon. Used by the shell hook — not typically run manually.`,
	DisableFlagParsing: false,
	Args:               cobra.MinimumNArgs(1),
	// Silence cobra's usage/error printing — the child process's stderr has
	// already been shown and we want to pass through the raw exit code.
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if attachSessionID == "" {
			return fmt.Errorf("--session-id is required")
		}

		logger, err := zap.NewProduction()
		if err != nil {
			return fmt.Errorf("init logger: %w", err)
		}
		defer logger.Sync()

		opts := attach.Options{
			SessionID: attachSessionID,
			Command:   args[0],
			Args:      args[1:],
		}
		err = attach.Run(context.Background(), opts, logger)
		var exitErr *attach.ExitCodeError
		if errors.As(err, &exitErr) {
			// Propagate the child's exit code transparently.
			os.Exit(exitErr.Code)
		}
		return err
	},
}

func init() {
	attachCmd.Flags().StringVar(&attachSessionID, "session-id", "", "Session UUID")
	attachCmd.MarkFlagRequired("session-id")
}
