package cmd

import (
	"fmt"
	"strconv"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var priorityCmd = &cobra.Command{
	Use:               "priority [action] [profile_name] [value]",
	Aliases:           []string{"prio", "p"},
	Short:             "Manage profile priorities for auto profile selection",
	ValidArgsFunction: CompletePriorityArgs,
	Long: `Set, view, or list profile priorities. Higher priority numbers are preferred in auto-selection mode as long as their 5h quota is >= 50%.

Subcommands/Actions:
  agys priority set <profile_name> <priority_value>
  agys priority get <profile_name>
  agys priority list`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 || args[0] == "list" {
			return listPriorities()
		}

		action := args[0]
		switch action {
		case "set":
			if len(args) < 3 {
				return fmt.Errorf("usage: agys priority set <profile_name> <value>")
			}
			pName := args[1]
			val, err := strconv.Atoi(args[2])
			if err != nil {
				return fmt.Errorf("invalid priority value %q: must be an integer", args[2])
			}
			if err := profile.SetPriority(pName, val); err != nil {
				return err
			}
			fmt.Printf("Priority for profile %q set to %d\n", pName, val)
			return nil

		case "get":
			if len(args) < 2 {
				return fmt.Errorf("usage: agys priority get <profile_name>")
			}
			pName := args[1]
			val := profile.GetPriority(pName)
			fmt.Printf("Priority for profile %q: %d\n", pName, val)
			return nil

		default:
			return fmt.Errorf("unknown priority action %q. Valid actions: set, get, list", action)
		}
	},
}

func listPriorities() error {
	profiles, err := profile.List()
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		fmt.Println("No profiles found.")
		return nil
	}

	priorities, err := profile.GetAllPriorities()
	if err != nil {
		return err
	}

	fmt.Println("Profile Priorities:")
	for _, p := range profiles {
		prio := priorities[p]
		fmt.Printf("  - %s: %d\n", p, prio)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(priorityCmd)
}
