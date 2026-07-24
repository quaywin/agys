package cmd

import (
	"fmt"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var cloneCmd = &cobra.Command{
	Use:               "clone <source_profile> <target_profile>",
	Aliases:           []string{"cp"},
	Short:             "Clone an existing profile to a new profile",
	Long:              `Duplicate all credentials, configuration, and state from an existing profile to a new profile.`,
	ValidArgsFunction: CompleteCloneOrRenameArgs,
	Args:              cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		src := args[0]
		dst := args[1]

		if err := profile.Clone(src, dst); err != nil {
			return err
		}

		fmt.Printf("Successfully cloned profile %q to %q.\n", src, dst)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(cloneCmd)
}
