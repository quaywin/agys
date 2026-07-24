package cmd

import (
	"fmt"
	"os"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var addCmd = &cobra.Command{
	Use:   "add <profile_name>",
	Short: "Create a new profile and perform agy login",
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
		if exists {
			return fmt.Errorf("profile %q already exists at %s", profileName, profileDir)
		}

		createdDir, err := profile.Create(profileName)
		if err != nil {
			return err
		}
		fmt.Printf("Profile directory created at: %s\n", createdDir)
		fmt.Printf("Initiating `agy login` for profile %q...\n\n", profileName)

		if err := profile.RunCmdWithSignals(cmd.Context(), createdDir, "login"); err != nil {
			// If login fails, clean up created directory or inform user
			fmt.Fprintf(os.Stderr, "Warning: `agy login` exited with error: %v\n", err)
			return err
		}

		fmt.Printf("\nSuccessfully configured profile %q!\n", profileName)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(addCmd)
}
