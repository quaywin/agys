package cmd

import (
	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var autoCmd = &cobra.Command{
	Use:   "auto -- [agy_commands]",
	Short: "Execute agy command automatically using profile with the best 5h Gemini quota",
	Long:  `Queries 5-hour Gemini model quota across all active profiles in parallel, picks the profile with the highest remaining quota, and executes the agy command.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runWithProfile(cmd, profile.AutoProfileKeyword, args)
	},
}

func init() {
	autoCmd.DisableFlagParsing = false
	rootCmd.AddCommand(autoCmd)
}
