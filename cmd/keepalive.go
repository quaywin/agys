package cmd

import (
	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var keepaliveCmd = &cobra.Command{
	Use:    "keepalive <profile_name>",
	Short:  "Internal: keep a profile's OAuth token fresh so launches reuse the existing authorization",
	Hidden: true,
	Args:   cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return profile.RunTokenKeepAlive(cmd.Context(), args[0])
	},
}

func init() {
	rootCmd.AddCommand(keepaliveCmd)
}
