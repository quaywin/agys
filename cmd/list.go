package cmd

import (
	"fmt"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all active profile directories",
	RunE: func(cmd *cobra.Command, args []string) error {
		profiles, err := profile.List()
		if err != nil {
			return err
		}

		if len(profiles) == 0 {
			baseDir, _ := profile.GetBaseDir()
			fmt.Printf("No profiles found in %s\nUse `agys add <profile_name>` to create one.\n", baseDir)
			return nil
		}

		fmt.Println("Active Profiles:")
		for _, p := range profiles {
			dir, _ := profile.GetProfileDir(p)
			fmt.Printf("  - %s (%s)\n", p, dir)
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}
