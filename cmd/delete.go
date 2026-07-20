package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var forceDelete bool

var deleteCmd = &cobra.Command{
	Use:               "delete <profile_name>",
	Aliases:           []string{"rm"},
	Short:             "Delete a profile directory",
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		profileName := args[0]

		exists, profileDir, err := profile.Exists(profileName)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("profile %q does not exist", profileName)
		}

		if !forceDelete {
			fmt.Printf("Are you sure you want to delete profile %q (%s)? [y/N]: ", profileName, profileDir)
			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read confirmation input: %w", err)
			}
			input = strings.ToLower(strings.TrimSpace(input))
			if input != "y" && input != "yes" {
				fmt.Println("Deletion canceled.")
				return nil
			}
		}

		if err := profile.Delete(profileName); err != nil {
			return err
		}

		fmt.Printf("Profile %q successfully deleted.\n", profileName)
		return nil
	},
}

func init() {
	deleteCmd.Flags().BoolVarP(&forceDelete, "force", "f", false, "Force deletion without prompt")
	rootCmd.AddCommand(deleteCmd)
}
