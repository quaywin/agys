package cmd

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var forceIde bool

var ideCmd = &cobra.Command{
	Use:               "ide [profile_name] [project_path]",
	Short:             "Launch Antigravity IDE with a specified profile or default active profile",
	Long:              `Launches the Antigravity IDE after syncing the profile token into macOS Keychain.`,
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
				// First arg might be a project path, try getting current default profile
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
			selected, score, err := profile.SelectBestProfile(cmd.Context())
			if err != nil {
				return fmt.Errorf("auto profile selection failed: %w", err)
			}
			targetProfile = selected
			scoreStr := fmt.Sprintf("%.1f%%", score*100)
			if score < 0 {
				scoreStr = "N/A"
			}
			fmt.Fprintf(os.Stderr, "[agys] Auto-selected profile %q (5h Gemini quota: %s)\n", targetProfile, scoreStr)
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

		// Prompt if Antigravity IDE is currently running and force flag is not set
		if isIdeRunning() && !forceIde {
			fmt.Printf("An instance of Antigravity IDE is currently running. Close it and switch to profile %q? [y/N]: ", targetProfile)
			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read confirmation input: %w", err)
			}
			input = strings.ToLower(strings.TrimSpace(input))
			if input != "y" && input != "yes" {
				fmt.Println("IDE launch canceled.")
				return nil
			}
		}

		// Set as current profile
		_ = profile.SetCurrent(targetProfile)

		// Sync profile's OAuth token directly into macOS Keychain
		_ = profile.EnsureKeychain(profileDir)
		profile.SyncDiskTokenToKeychain(profileDir)

		// Terminate any running IDE process and wait for OS process cleanup
		terminateAndWaitIde()

		fmt.Printf("Launching Antigravity IDE (%s)...\n", targetProfile)

		openArgs := []string{"-a", "/Applications/Antigravity IDE.app"}
		if projectPath != "" {
			absPath, err := filepath.Abs(projectPath)
			if err == nil {
				openArgs = append(openArgs, absPath)
			} else {
				openArgs = append(openArgs, projectPath)
			}
		}

		openCmd := exec.Command("open", openArgs...)
		if err := openCmd.Run(); err != nil {
			// Fallback to Antigravity.app if Antigravity IDE.app is missing
			openArgs[1] = "/Applications/Antigravity.app"
			openCmd = exec.Command("open", openArgs...)
			if err := openCmd.Run(); err != nil {
				return fmt.Errorf("failed to launch Antigravity IDE: %w", err)
			}
		}

		return nil
	},
}

func isIdeRunning() bool {
	err := exec.Command("pgrep", "-f", "Antigravity IDE").Run()
	return err == nil
}

func terminateAndWaitIde() {
	if err := exec.Command("pkill", "-f", "Antigravity IDE").Run(); err != nil {
		return
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := exec.Command("pgrep", "-f", "Antigravity IDE").Run(); err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := exec.Command("pgrep", "-f", "Antigravity IDE").Run(); err == nil {
		_ = exec.Command("pkill", "-9", "-f", "Antigravity IDE").Run()
		time.Sleep(200 * time.Millisecond)
	}

	time.Sleep(200 * time.Millisecond)
}

func init() {
	ideCmd.Flags().BoolVarP(&forceIde, "force", "f", false, "Force close running IDE instance without confirmation prompt")
	rootCmd.AddCommand(ideCmd)
}
