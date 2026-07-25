package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var ideCmd = &cobra.Command{
	Use:               "ide [profile_name] [project_path]",
	Short:             "Launch Antigravity IDE isolated to a specific profile",
	Long:              `Launches the Antigravity IDE with an isolated profile user-data-dir, allowing multiple independent account sessions.`,
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MaximumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		var profileName string
		var projectPath string

		if len(args) > 0 {
			firstArg := args[0]
			exists, _, _ := profile.Exists(firstArg)
			if exists || profile.IsAuto(firstArg) {
				profileName = firstArg
				if len(args) > 1 {
					projectPath = args[1]
				}
			} else {
				current, err := profile.GetCurrent()
				if err != nil {
					return err
				}
				if current != "" {
					profileName = current
					projectPath = firstArg
				} else {
					return fmt.Errorf("profile %q does not exist and no default profile set. Specify a profile or set one with `agys use <profile_name>`", firstArg)
				}
			}
		} else {
			current, err := profile.GetCurrent()
			if err != nil {
				return err
			}
			if current == "" {
				return fmt.Errorf("no profile specified and no default profile set. Specify a profile or set one with `agys use <profile_name>`")
			}
			profileName = current
		}

		var targetProfile string
		if profile.IsAuto(profileName) {
			selected, score, err := profile.SelectBestProfileFiltered(cmd.Context(), func(p string) bool {
				pDir, err := profile.GetProfileDir(p)
				if err != nil {
					return false
				}
				// Prioritize candidate profiles that have already been initialized / logged in for IDE
				_, statErr := os.Stat(filepath.Join(pDir, "ide-data", "User"))
				return statErr == nil
			})
			if err != nil {
				return fmt.Errorf("auto profile selection failed: %w", err)
			}
			targetProfile = selected
			scoreStr := fmt.Sprintf("%.1f%%", score*100)
			if score < 0 {
				scoreStr = "N/A"
			}
			fmt.Fprintf(os.Stderr, "[agys] Auto-selected IDE profile %q (5h Gemini quota: %s)\n", targetProfile, scoreStr)
		} else {
			exists, _, err := profile.Exists(profileName)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("profile %q does not exist. Use `agys add %s` to create it", profileName, profileName)
			}
			targetProfile = profileName
		}

		profileDir, err := profile.GetProfileDir(targetProfile)
		if err != nil {
			return err
		}

		// Set as current profile
		_ = profile.SetCurrent(targetProfile)

		ideDataDir := filepath.Join(profileDir, "ide-data")
		_ = os.MkdirAll(ideDataDir, 0755)

		fmt.Printf("Launching Antigravity IDE (%s)...\n", targetProfile)

		openArgs := []string{"-n", "-a", "/Applications/Antigravity IDE.app"}
		if projectPath != "" {
			absPath, err := filepath.Abs(projectPath)
			if err == nil {
				openArgs = append(openArgs, absPath)
			} else {
				openArgs = append(openArgs, projectPath)
			}
		}
		openArgs = append(openArgs, "--args", "--user-data-dir="+ideDataDir)

		openCmd := exec.Command("open", openArgs...)
		if err := openCmd.Run(); err != nil {
			// Fallback to Antigravity.app if Antigravity IDE.app is missing
			openArgs[2] = "/Applications/Antigravity.app"
			openCmd = exec.Command("open", openArgs...)
			if err := openCmd.Run(); err != nil {
				return fmt.Errorf("failed to launch Antigravity IDE: %w", err)
			}
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(ideCmd)
}
