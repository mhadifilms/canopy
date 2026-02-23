package main

import (
	"os"

	"github.com/canopy-dev/canopyd/cmd/canopyd/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
