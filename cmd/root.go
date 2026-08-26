package cmd

import (
	"fmt"
	"os"

	"github.com/quaywin/agys/pkg/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "agys",
	Short: "agys (Antigravity Switcher) manages isolated account profiles and real-time Herdr multi-agent quota tracking",
	Long: `agys isolates multi-account profiles across the Google Antigravity ecosystem (CLI, IDE, GUI, Remote)
and provides native, real-time profile quota tracking (5H & Weekly) and lifecycle hooks for Herdr multi-agent workspaces.`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	rootCmd.Version = version.GetVersionInfo()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
