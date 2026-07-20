package cmd

import (
	"fmt"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:               "run [profile_name] -- [agy_commands]",
	Short:             "Execute agy command with specified or default profile",
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var profileName string
		var agyArgs []string

		firstArg := args[0]
		exists, profileDir, err := profile.Exists(firstArg)
		if err != nil {
			return err
		}

		if exists {
			profileName = firstArg
			agyArgs = args[1:]
		} else {
			// Check if default profile is set
			current, err := profile.GetCurrent()
			if err != nil {
				return err
			}
			if current != "" {
				currentExists, currentDir, err := profile.Exists(current)
				if err != nil {
					return err
				}
				if currentExists {
					profileName = current
					profileDir = currentDir
					agyArgs = args
				}
			}

			if profileName == "" {
				return fmt.Errorf("profile %q does not exist. Use `agys add %s` to create it, or set a default profile with `agys use <profile_name>`", firstArg, firstArg)
			}
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
