// Package commands holds the cobra CLI: serve, migrate, import-legacy.
package commands

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:           "directory",
	Short:         "federated boson network directory",
	SilenceUsage:  true,
	SilenceErrors: false,
}

// Execute runs the root command and exits non-zero on error.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
