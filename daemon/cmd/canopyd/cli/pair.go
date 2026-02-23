package cli

import (
	"context"
	"time"

	"github.com/canopy-dev/canopyd/internal/pairing"
	"github.com/spf13/cobra"
)

var pairTimeout int

var pairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Start pairing with an iPhone",
	Long:  "Generates a 6-digit pairing code. Enter it in the Canopy iOS app to pair.",
	RunE: func(cmd *cobra.Command, args []string) error {
		timeout := time.Duration(pairTimeout) * time.Second
		return pairing.StartPairing(context.Background(), timeout)
	},
}

func init() {
	pairCmd.Flags().IntVar(&pairTimeout, "timeout", 300, "Pairing timeout in seconds")
}
