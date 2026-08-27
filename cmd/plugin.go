package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	pluginInstallAll   bool
	pluginListAll      bool
	pluginUninstallAll bool
)

var pluginCmd = &cobra.Command{
	Use:     "plugin",
	Aliases: []string{"plugins"},
	Short:   "Manage agy plugins across profiles",
	Long:    `Manage, install, list, and uninstall plugins for agy CLI across single or all profiles.`,
}

var pluginInstallCmd = &cobra.Command{
	Use:     "install <target> [profile_name]",
	Aliases: []string{"i", "add"},
	Short:   "Install an agy plugin",
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 1 {
			return CompleteProfileNames(cmd, args, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("requires plugin target (name or URL)")
		}
		pluginArg := args[0]
		var profileName string
		if len(args) > 1 {
			profileName = args[1]
		}
		return execPluginCmd("install", pluginArg, profileName, pluginInstallAll)
	},
}

var pluginListCmd = &cobra.Command{
	Use:               "list [profile_name]",
	Aliases:           []string{"ls"},
	Short:             "List installed agy plugins",
	ValidArgsFunction: CompleteProfileNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		var profileName string
		if len(args) > 0 {
			profileName = args[0]
		}
		return execPluginCmd("list", "", profileName, pluginListAll)
	},
}

var pluginUninstallCmd = &cobra.Command{
	Use:     "uninstall <target> [profile_name]",
	Aliases: []string{"remove", "rm"},
	Short:   "Uninstall an agy plugin",
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 1 {
			return CompleteProfileNames(cmd, args, toComplete)
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return fmt.Errorf("requires plugin target")
		}
		pluginArg := args[0]
		var profileName string
		if len(args) > 1 {
			profileName = args[1]
		}
		return execPluginCmd("uninstall", pluginArg, profileName, pluginUninstallAll)
	},
}

func execPluginCmd(action string, pluginArg string, profileName string, isAll bool) error {
	if isAll {
		profiles, err := profile.List()
		if err != nil {
			return err
		}
		if len(profiles) == 0 {
			return fmt.Errorf("no active profiles found")
		}

		var agyArgs []string
		if pluginArg != "" {
			agyArgs = []string{"plugin", action, pluginArg}
		} else {
			agyArgs = []string{"plugin", action}
		}

		var lastErr error
		for i, p := range profiles {
			profileDir, err := profile.GetProfileDir(p)
			if err != nil {
				fmt.Printf("[%d/%d] %-12s error getting profile dir: %v\n", i+1, len(profiles), p, err)
				lastErr = err
				continue
			}

			cmd := profile.BuildCmd(profileDir, agyArgs...)

			out, err := cmd.CombinedOutput()
			outStr := strings.TrimSpace(string(out))
			if err != nil {
				if outStr == "" {
					outStr = err.Error()
				}
				lastErr = err
			}

			if strings.Contains(outStr, "\n") {
				formattedOut := strings.ReplaceAll(outStr, "\n", "\n              ")
				fmt.Printf("[%d/%d] %-10s:\n              %s\n", i+1, len(profiles), p, formattedOut)
			} else {
				fmt.Printf("[%d/%d] %-10s: %s\n", i+1, len(profiles), p, outStr)
			}
		}
		return lastErr
	}

	if profileName == "" {
		current, err := profile.GetCurrent()
		if err != nil {
			return err
		}
		if current == "" {
			return fmt.Errorf("no profile specified and no default profile set. Specify a profile or set one with `agys use <profile_name>`")
		}
		profileName = current
	}

	exists, profileDir, err := profile.Exists(profileName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("profile %q does not exist. Use `agys add %s` to create it", profileName, profileName)
	}

	var agyArgs []string
	if pluginArg != "" {
		agyArgs = []string{"plugin", action, pluginArg}
	} else {
		agyArgs = []string{"plugin", action}
	}

	return profile.RunCmdWithSignals(context.Background(), profileDir, agyArgs...)
}

func init() {
	pluginInstallCmd.Flags().BoolVarP(&pluginInstallAll, "all", "a", false, "Install plugin across all profiles")
	pluginListCmd.Flags().BoolVarP(&pluginListAll, "all", "a", false, "List plugins across all profiles")
	pluginUninstallCmd.Flags().BoolVarP(&pluginUninstallAll, "all", "a", false, "Uninstall plugin across all profiles")

	pluginCmd.AddCommand(pluginInstallCmd)
	pluginCmd.AddCommand(pluginListCmd)
	pluginCmd.AddCommand(pluginUninstallCmd)

	rootCmd.AddCommand(pluginCmd)
}
