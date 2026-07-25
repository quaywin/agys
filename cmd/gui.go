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

var forceGui bool

var guiCmd = &cobra.Command{
	Use:               "gui [profile_name]",
	Short:             "Launch Antigravity 2.0 Desktop App (GUI) with a specified profile or default active profile",
	Long:              `Launches the Antigravity 2.0 Desktop App GUI with isolated profile settings and environment variables.`,
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var profileName string

		if len(args) > 0 {
			profileName = args[0]
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

		// Prompt if Antigravity GUI is currently running and force flag is not set
		if isAntigravityRunning() && !forceGui {
			fmt.Printf("An instance of Antigravity GUI is currently running. Close it and switch to profile %q? [y/N]: ", targetProfile)
			reader := bufio.NewReader(os.Stdin)
			input, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read confirmation input: %w", err)
			}
			input = strings.ToLower(strings.TrimSpace(input))
			if input != "y" && input != "yes" {
				fmt.Println("GUI launch canceled.")
				return nil
			}
		}

		// Set as current profile
		_ = profile.SetCurrent(targetProfile)

		// Automatically sync CLI OAuth token to GUI location if present
		cliTokenPath := filepath.Join(profileDir, ".gemini", "antigravity-cli", "antigravity-oauth-token")
		if data, err := os.ReadFile(cliTokenPath); err == nil && len(data) > 0 {
			guiTokenDir := filepath.Join(profileDir, ".gemini", "antigravity")
			_ = os.MkdirAll(guiTokenDir, 0755)
			_ = os.WriteFile(filepath.Join(guiTokenDir, "antigravity-oauth-token"), data, 0600)
			_ = os.WriteFile(filepath.Join(profileDir, ".gemini", "oauth_creds.json"), data, 0600)
		}

		// Sync profile's OAuth token directly into macOS Keychain
		_ = profile.EnsureKeychain(profileDir)
		profile.SyncDiskTokenToKeychain(profileDir)

		// Make sure all existing Antigravity processes are 100% terminated before launching
		terminateAndWaitAntigravity()

		fmt.Printf("Launching Antigravity 2.0 GUI (%s)...\n", targetProfile)

		// Launch Antigravity GUI with isolated HOME environment
		openCmd := exec.Command("open", "-a", "/Applications/Antigravity.app", "--env", "HOME="+profileDir)
		if err := openCmd.Run(); err != nil {
			return fmt.Errorf("failed to launch Antigravity 2.0 GUI: %w", err)
		}

		return nil
	},
}

func isAntigravityRunning() bool {
	err := exec.Command("pgrep", "-f", "Antigravity").Run()
	return err == nil
}

func terminateAndWaitAntigravity() {
	if err := exec.Command("pkill", "-f", "Antigravity").Run(); err != nil {
		return
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if err := exec.Command("pgrep", "-f", "Antigravity").Run(); err != nil {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := exec.Command("pgrep", "-f", "Antigravity").Run(); err == nil {
		_ = exec.Command("pkill", "-9", "-f", "Antigravity").Run()
		time.Sleep(200 * time.Millisecond)
	}

	time.Sleep(200 * time.Millisecond)
}

func init() {
	guiCmd.Flags().BoolVarP(&forceGui, "force", "f", false, "Force close running GUI instance without confirmation prompt")
	rootCmd.AddCommand(guiCmd)
}
