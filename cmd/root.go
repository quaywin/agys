package cmd

import (
	"fmt"
	"os"

	"github.com/quaywin/agys/pkg/version"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "agys",
	Short: "agys (Antigravity CLI Switcher) manages isolated account profiles for agy CLI",
	Long: `agys isolates account profiles by dynamically overriding the HOME environment
variable for the agy command to profile-specific base directories (~/.agys/profiles/<profile_name>/).`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	rootCmd.Version = version.GetVersionInfo()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
