package cmd

import (
	"fmt"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	unsetUse bool
)

var useCmd = &cobra.Command{
	Use:               "use [profile_name]",
	Short:             "Set or display the default active profile",
	Long:              `Set or display the default active profile used when executing 'agys run -- [command]'. Specify 'auto' to enable automatic 5h Gemini quota profile selection.`,
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if unsetUse {
			if err := profile.UnsetCurrent(); err != nil {
				return err
			}
			fmt.Println("Cleared default active profile.")
			return nil
		}

		if len(args) == 0 {
			current, err := profile.GetCurrent()
			if err != nil {
				return err
			}
			if current == "" {
				fmt.Println("No default profile set.")
				fmt.Println("Use `agys use <profile_name>` to set one.")
			} else if profile.IsAuto(current) {
				fmt.Println("Current default profile: auto (automatic 5h Gemini quota selection)")
			} else {
				dir, _ := profile.GetProfileDir(current)
				fmt.Printf("Current default profile: %s (%s)\n", current, dir)
			}
			return nil
		}

		profileName := args[0]
		if err := profile.SetCurrent(profileName); err != nil {
			return err
		}

		if profile.IsAuto(profileName) {
			fmt.Println("Default profile set to \"auto\" (automatic 5h Gemini quota selection)")
		} else {
			dir, _ := profile.GetProfileDir(profileName)
			fmt.Printf("Default profile set to %q (%s)\n", profileName, dir)
		}
		return nil
	},
}

func init() {
	useCmd.Flags().BoolVarP(&unsetUse, "unset", "u", false, "Unset current default profile")
	rootCmd.AddCommand(useCmd)
}
