package cmd

import (
	"fmt"
	"os"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add <profile_name>",
	Short: "Create a new profile and authenticate via agy",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		profileName := args[0]

		if err := profile.ValidateName(profileName); err != nil {
			return err
		}

		exists, profileDir, err := profile.Exists(profileName)
		if err != nil {
			return err
		}

		var createdDir string
		if exists {
			if profile.HasProfileToken(profileName) {
				return fmt.Errorf("profile %q already exists at %s", profileName, profileDir)
			}
			fmt.Printf("Profile %q exists but is not authenticated (%s).\n", profileName, profileDir)
			createdDir = profileDir
			_ = profile.EnsureKeychain(createdDir)
			_ = profile.SyncHerdrIntegration(createdDir)
			_ = profile.EnsureGitConfig(createdDir)
			_ = profile.EnsureOnboardingCompleted(createdDir)
		} else {
			var createErr error
			createdDir, createErr = profile.Create(profileName)
			if createErr != nil {
				return createErr
			}
			fmt.Printf("Profile directory created at: %s\n", createdDir)
		}

		fmt.Printf("Initiating authentication for profile %q via `agy`...\n\n", profileName)

		runErr := profile.RunCmdWithSignals(cmd.Context(), createdDir)

		// Persist newly created Keychain token to profile disk storage
		profile.SyncKeychainTokenToDisk(createdDir, "")
		_ = profile.SyncAllTokenLocations(createdDir)
		_ = profile.EnsureOnboardingCompleted(createdDir)

		if !profile.HasProfileToken(profileName) {
			if !exists {
				// Clean up incomplete profile directory if newly created
				_ = os.RemoveAll(createdDir)
			}
			if runErr != nil {
				return fmt.Errorf("authentication exited with error: %w", runErr)
			}
			return fmt.Errorf("authentication was not completed (no token found)")
		}

		fmt.Printf("\nSuccessfully configured profile %q!\n", profileName)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)
}
