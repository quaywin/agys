package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var acpCmd = &cobra.Command{
	Use:                "acp [profile_name] [flags]",
	Short:              "Start the Agent Client Protocol (ACP) server with profile isolation",
	Long:               `Starts the Agent Client Protocol (ACP) stdio JSON-RPC server for integration with IDEs (Zed, JetBrains, Neovim, etc.). Supports profile auto-selection (agys acp auto) to dynamically pick the profile with the highest remaining quota.`,
	DisableFlagParsing: true,
	ValidArgsFunction:  CompleteProfileNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		var profileName string
		var passThroughArgs []string

		if len(args) > 0 {
			first := args[0]
			if first == "-h" || first == "--help" {
				return cmd.Help()
			}
			if !strings.HasPrefix(first, "-") {
				exists, _, _ := profile.Exists(first)
				if !exists && !profile.IsAuto(first) {
					return fmt.Errorf("profile %q does not exist. Use `agys add %s` to create it", first, first)
				}
				profileName = first
				passThroughArgs = args[1:]
			} else {
				passThroughArgs = args
			}
		}

		if profileName == "" {
			defaultProf, err := resolveDefaultProfile()
			if err != nil {
				return err
			}
			if defaultProf == "" {
				profileName = profile.AutoProfileKeyword
			} else {
				profileName = defaultProf
			}
		}

		var targetProfile string
		if profile.IsAuto(profileName) {
			best, err := profile.SelectBestProfileDetailed(cmd.Context())
			if err != nil {
				return fmt.Errorf("auto profile selection failed: %w", err)
			}
			targetProfile = best.ProfileName
			scoreStr := fmt.Sprintf("%.1f%%", best.Score*100)
			if best.Score < 0 {
				scoreStr = "N/A"
			}
			var weeklyStr string
			if best.WeeklyScore >= 0 {
				weeklyStr = fmt.Sprintf(", weekly: %.1f%%", best.WeeklyScore*100)
			}
			fmt.Fprintf(os.Stderr, "[agys] Auto-selected ACP profile %q (5h Gemini quota: %s%s)\n", targetProfile, scoreStr, weeklyStr)
		} else {
			targetProfile = profileName
		}

		_ = os.Setenv("AGYS_PROFILE", targetProfile)

		profileDir, err := profile.GetProfileDir(targetProfile)
		if err != nil {
			return err
		}

		// Synchronize tokens across all candidate locations and ensure onboarding state
		_ = profile.SyncAllTokenLocations(profileDir)
		_ = profile.EnsureOnboardingCompleted(profileDir)
		_ = profile.SyncTrustedWorkspaces()
		profile.ArmTokenKeepAlive(targetProfile)

		var expectedRefreshToken string
		if initTok, readErr := profile.ReadToken(targetProfile); readErr == nil && initTok != nil {
			expectedRefreshToken = initTok.Token.RefreshToken
		}

		agyArgs := append([]string{"acp"}, passThroughArgs...)
		runErr := profile.RunCmdWithSignalsInDir(cmd.Context(), profileDir, "", agyArgs...)

		// Persist any token updated or created during execution to profile disk storage
		profile.SyncKeychainTokenToDisk(profileDir, expectedRefreshToken)

		return runErr
	},
}

func init() {
	rootCmd.AddCommand(acpCmd)
}
