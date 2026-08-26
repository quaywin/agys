package cmd

import (
	"os"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var herdrHookCmd = &cobra.Command{
	Use:    "herdr-hook [session|quota]",
	Short:  "Internal lifecycle hook handler for Herdr multi-agent integration",
	Hidden: true,
	Args:   cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		action := "session"
		if len(args) > 0 {
			action = args[0]
		}
		return profile.HandleHerdrHook(cmd.Context(), action, os.Stdin)
	},
}

func init() {
	rootCmd.AddCommand(herdrHookCmd)
}
