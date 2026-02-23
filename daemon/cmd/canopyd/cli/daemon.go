package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/canopy-dev/canopyd/internal/config"
	"github.com/canopy-dev/canopyd/internal/daemon"
	"github.com/canopy-dev/canopyd/internal/install"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the canopyd background daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the daemon in the foreground",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return fmt.Errorf("load config: %w", err)
		}

		logger, err := zap.NewProduction()
		if err != nil {
			return fmt.Errorf("init logger: %w", err)
		}
		defer logger.Sync()

		d := daemon.New(cfg, logger)
		return d.Start(context.Background())
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Send a clean shutdown by connecting to the socket and immediately closing.
		// The daemon's KeepAlive with SuccessfulExit=false means launchctl
		// will NOT restart after a clean exit, but WILL restart after a crash.
		// To cleanly stop: unload the launchd job.
		cmd.Println("Stopping daemon...")
		install.UnloadPlist()
		cmd.Println("Daemon stopped.")
		return nil
	},
}

var daemonStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon status",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := daemon.Ping(); err != nil {
			cmd.Println("Daemon is not running.")
			if install.IsDaemonLoaded() {
				cmd.Println("Launchd job is loaded (daemon may be starting).")
			}
			return nil
		}

		cmd.Println("Daemon is running.")

		// Show socket info.
		sockPath := config.SocketPath()
		if info, err := os.Stat(sockPath); err == nil {
			cmd.Printf("Socket: %s (modified %s)\n", sockPath, info.ModTime().Format(time.RFC3339))
		}

		if install.IsDaemonLoaded() {
			cmd.Println("Launchd: loaded")
		}

		return nil
	},
}

var daemonPingCmd = &cobra.Command{
	Use:          "ping",
	Short:        "Check if the daemon is alive (exit code 0 = alive)",
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return daemon.Ping()
	},
}

var daemonRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart the daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		cmd.Println("Restarting daemon...")

		// Unload to stop.
		install.UnloadPlist()

		// Wait briefly for the socket to be released.
		for i := 0; i < 10; i++ {
			if err := daemon.Ping(); err != nil {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}

		// Reload to start.
		if err := install.LoadPlist(); err != nil {
			return fmt.Errorf("reload plist: %w", err)
		}

		// Wait for daemon to come up.
		for i := 0; i < 20; i++ {
			time.Sleep(200 * time.Millisecond)
			conn, err := net.DialTimeout("unix", config.SocketPath(), time.Second)
			if err == nil {
				conn.Close()
				cmd.Println("Daemon restarted.")
				return nil
			}
		}

		cmd.Println("Daemon restart requested. It may take a moment to start.")
		return nil
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
	daemonCmd.AddCommand(daemonStatusCmd)
	daemonCmd.AddCommand(daemonPingCmd)
	daemonCmd.AddCommand(daemonRestartCmd)
}
