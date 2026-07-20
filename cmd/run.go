package cmd

import (
	"fmt"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:   "run <profile_name> -- [agy_commands]",
	Short: "Execute agy command with specified profile",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		profileName := args[0]
		agyArgs := args[1:]

		exists, profileDir, err := profile.Exists(profileName)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("profile %q does not exist. Use `agys add %s` to create it", profileName, profileName)
		}

		execCmd := profile.BuildCmd(profileDir, agyArgs...)
		if err := execCmd.Run(); err != nil {
			return err
		}
		return nil
	},
}

func init() {
	// Disable flag parsing for arguments after `--` to pass raw flags directly to agy
	runCmd.DisableFlagParsing = false
	rootCmd.AddCommand(runCmd)
}
