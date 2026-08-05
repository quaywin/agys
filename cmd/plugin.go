package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	Use:               "install <target> [profile_name]",
	Aliases:           []string{"i", "add"},
	Short:             "Install an agy plugin",
	ValidArgsFunction: CompleteProfileNames,
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
	Use:               "uninstall <target> [profile_name]",
	Aliases:           []string{"remove", "rm"},
	Short:             "Uninstall an agy plugin",
	ValidArgsFunction: CompleteProfileNames,
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

		agyPath := getAgyPath()

		var lastErr error
		for i, p := range profiles {
			profileDir, err := profile.GetProfileDir(p)
			if err != nil {
				fmt.Printf("[%d/%d] %-12s error getting profile dir: %v\n", i+1, len(profiles), p, err)
				lastErr = err
				continue
			}

			cmd := exec.Command(agyPath, agyArgs...)
			cmd.Env = getProfileEnv(profileDir)

			out, err := cmd.CombinedOutput()
			outStr := strings.TrimSpace(string(out))
			if err != nil {
				if outStr == "" {
					outStr = err.Error()
				}
				lastErr = err
			}
			fmt.Printf("[%d/%d] %-12s %s\n", i+1, len(profiles), p, outStr)
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

func getAgyPath() string {
	agyPath, err := exec.LookPath("agy")
	if err != nil {
		if userHome, errHome := os.UserHomeDir(); errHome == nil {
			agysSep := string(filepath.Separator) + ".agys"
			if idx := strings.Index(userHome, agysSep); idx != -1 {
				userHome = userHome[:idx]
			}
			candidates := []string{
				filepath.Join(userHome, ".local", "bin", "agy"),
				filepath.Join(userHome, "bin", "agy"),
				filepath.Join(userHome, ".gemini", "antigravity-cli", "bin", "agy"),
				"/usr/local/bin/agy",
			}
			for _, candidate := range candidates {
				if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
					agyPath = candidate
					err = nil
					break
				}
			}
		}
	}
	if err != nil {
		agyPath = "agy"
	}
	return agyPath
}

func getProfileEnv(profileDir string) []string {
	envMap := map[string]string{
		"HOME":            profileDir,
		"USERPROFILE":     profileDir,
		"GEMINI_DIR":      filepath.Join(profileDir, ".gemini"),
		"GEMINI_CLI_DIR":  filepath.Join(profileDir, ".gemini", "antigravity-cli"),
		"ANTIGRAVITY_DIR": filepath.Join(profileDir, ".gemini", "antigravity-cli"),
		"XDG_CONFIG_HOME": filepath.Join(profileDir, ".config"),
		"XDG_DATA_HOME":   filepath.Join(profileDir, ".local", "share"),
		"XDG_CACHE_HOME":  filepath.Join(profileDir, ".cache"),
	}

	env := os.Environ()
	newEnv := make([]string, 0, len(env))
	seen := make(map[string]bool)

	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			if newVal, ok := envMap[parts[0]]; ok {
				newEnv = append(newEnv, parts[0]+"="+newVal)
				seen[parts[0]] = true
				continue
			}
		}
		newEnv = append(newEnv, e)
	}

	for k, v := range envMap {
		if !seen[k] {
			newEnv = append(newEnv, k+"="+v)
		}
	}
	return newEnv
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
