package cmd

import (
	"fmt"
	"os"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:               "run [profile_name] -- [agy_commands]",
	Short:             "Execute agy command with specified profile, auto quota selection, or default profile",
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var profileName string
		var agyArgs []string

		firstArg := args[0]
		if profile.IsAuto(firstArg) {
			profileName = profile.AutoProfileKeyword
			agyArgs = args[1:]
		} else {
			exists, _, err := profile.Exists(firstArg)
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
					if profile.IsAuto(current) {
						profileName = profile.AutoProfileKeyword
						agyArgs = args
					} else {
						currentExists, _, err := profile.Exists(current)
						if err != nil {
							return err
						}
						if currentExists {
							profileName = current
							agyArgs = args
						}
					}
				}

				if profileName == "" {
					return fmt.Errorf("profile %q does not exist. Use `agys add %s` to create it, or set a default profile with `agys use <profile_name>`", firstArg, firstArg)
				}
			}
		}

		return runWithProfile(cmd, profileName, agyArgs)
	},
}

func runWithProfile(cmd *cobra.Command, profileName string, agyArgs []string) error {
	var targetProfile string
	if profile.IsAuto(profileName) {
		selected, score, err := profile.SelectBestProfile(cmd.Context())
		if err != nil {
			return fmt.Errorf("auto profile selection failed: %w", err)
		}
		targetProfile = selected
		scoreStr := fmt.Sprintf("%.1f%%", score*100)
		if score < 0 {
			scoreStr = "N/A"
		}
		fmt.Fprintf(os.Stderr, "[agys] Auto-selected profile %q (5h Gemini quota: %s)\n", targetProfile, scoreStr)
	} else {
		targetProfile = profileName
	}

	profileDir, err := profile.GetProfileDir(targetProfile)
	if err != nil {
		return err
	}

	execCmd := profile.BuildCmd(profileDir, agyArgs...)
	if err := execCmd.Run(); err != nil {
		return err
	}
	return nil
}

func init() {
	// Disable flag parsing for arguments after `--` to pass raw flags directly to agy
	runCmd.DisableFlagParsing = false
	rootCmd.AddCommand(runCmd)
}
