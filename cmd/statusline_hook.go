package cmd

import (
	"os"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var statuslineHookCmd = &cobra.Command{
	Use:    "statusline-hook",
	Short:  "Internal statusLine hook handler for Antigravity context window and quota capture",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return profile.HandleStatusLine(cmd.Context(), os.Stdin, os.Stdout, os.Stderr)
	},
}

func init() {
	rootCmd.AddCommand(statuslineHookCmd)
}
