package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var runAll bool

var runCmd = &cobra.Command{
	Use:               "run [profile_name] -- [agy_commands]",
	Short:             "Execute agy command with specified profile, auto quota selection, or default profile",
	ValidArgsFunction: CompleteRunArgs,
	Args:              cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		if runAll {
			profiles, err := profile.List()
			if err != nil {
				return err
			}
			if len(profiles) == 0 {
				return fmt.Errorf("no active profiles found")
			}
			agyArgs := args
			var lastErr error
			for i, p := range profiles {
				fmt.Fprintf(os.Stderr, "\n[agys] Executing on profile %q (%d/%d)...\n", p, i+1, len(profiles))
				if err := runWithProfile(cmd, p, agyArgs); err != nil {
					fmt.Fprintf(os.Stderr, "[agys] Profile %q failed: %v\n", p, err)
					lastErr = err
				}
			}
			return lastErr
		}

		var profileName string
		var agyArgs []string

		var firstArg string
		if len(args) > 0 {
			firstArg = args[0]
		}

		if firstArg != "" && profile.IsAuto(firstArg) {
			profileName = profile.AutoProfileKeyword
			agyArgs = args[1:]
		} else if firstArg != "" {
			exists, _, _ := profile.Exists(firstArg)
			if exists {
				profileName = firstArg
				agyArgs = args[1:]
			} else {
				// Check if default profile is set
				current, err := profile.GetCurrent()
				if err != nil {
					return err
				}
				if current != "" {
					if profile.IsAuto(current) {
						profileName = profile.AutoProfileKeyword
						agyArgs = args
					} else {
						currentExists, _, err := profile.Exists(current)
						if err != nil {
							return err
						}
						if currentExists {
							profileName = current
							agyArgs = args
						}
					}
				}

				if profileName == "" {
					if profile.ValidateName(firstArg) != nil && strings.HasPrefix(firstArg, "-") {
						return fmt.Errorf("no profile specified and no default profile set. Specify a profile or set one with `agys use <profile_name>`")
					}
					return fmt.Errorf("profile %q does not exist. Use `agys add %s` to create it, or set a default profile with `agys use <profile_name>`", firstArg, firstArg)
				}
			}
		} else {
			// Check if default profile is set when no args provided
			current, err := profile.GetCurrent()
			if err != nil {
				return err
			}
			if current != "" {
				if profile.IsAuto(current) {
					profileName = profile.AutoProfileKeyword
					agyArgs = args
				} else {
					currentExists, _, err := profile.Exists(current)
					if err != nil {
						return err
					}
					if currentExists {
						profileName = current
						agyArgs = args
					}
				}
			}

			if profileName == "" {
				return fmt.Errorf("no profile specified and no default profile set. Specify a profile or set one with `agys use <profile_name>`")
			}
		}

		return runWithProfile(cmd, profileName, agyArgs)
	},
}

func runWithProfile(cmd *cobra.Command, profileName string, agyArgs []string) error {
	agyArgs = EnsureDefaultModelAndEffort(agyArgs)
	agyArgs, _, _ = profile.EnsureAvailableHubPort(agyArgs)

	// Detect if the user is resuming a conversation and auto-switch to the owning profile
	var detectedProfile string
	var detectErr error

	for i := 0; i < len(agyArgs); i++ {
		arg := agyArgs[i]
		if arg == "--conversation" && i+1 < len(agyArgs) {
			convID := agyArgs[i+1]
			detectedProfile, detectErr = profile.FindProfileByConversation(convID)
			break
		} else if strings.HasPrefix(arg, "--conversation=") {
			convID := strings.TrimPrefix(arg, "--conversation=")
			detectedProfile, detectErr = profile.FindProfileByConversation(convID)
			break
		} else if arg == "-c" || arg == "--continue" {
			detectedProfile, detectErr = profile.FindProfileByLatestConversation()
			break
		}
	}

	if detectErr == nil && detectedProfile != "" {
		if profileName != detectedProfile {
			fmt.Fprintf(os.Stderr, "[agys] Resumed conversation detected. Auto-switching profile %q -> %q\n", profileName, detectedProfile)
			profileName = detectedProfile
		}
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
		targetProfile = profileName
	}

	profileDir, err := profile.GetProfileDir(targetProfile)
	if err != nil {
		return err
	}

	// Synchronize tokens across all candidate locations and ensure onboarding state
	_ = profile.SyncAllTokenLocations(profileDir)
	_ = profile.EnsureOnboardingCompleted(profileDir)

	var expectedRefreshToken string
	if initTok, readErr := profile.ReadToken(targetProfile); readErr == nil && initTok != nil {
		expectedRefreshToken = initTok.Token.RefreshToken
	}

	// Merge trusted workspaces across all profiles prior to execution
	_ = profile.SyncTrustedWorkspaces()

	// Extract active model if specified in agyArgs
	var activeModel string
	for i := 0; i < len(agyArgs); i++ {
		if agyArgs[i] == "--model" || agyArgs[i] == "-m" {
			if i+1 < len(agyArgs) {
				activeModel = agyArgs[i+1]
			}
			break
		}
		if strings.HasPrefix(agyArgs[i], "--model=") {
			activeModel = strings.TrimPrefix(agyArgs[i], "--model=")
			break
		}
	}
	// Resolve model using full fallback chain: CLI flag > .active_model cache > settings.json
	activeModel = profile.ResolveActiveModel(profileDir, activeModel)
	if activeModel != "" {
		_ = profile.WriteFileAtomic(filepath.Join(profileDir, ".active_model"), []byte(activeModel), 0600)
	}

	// Ensure Herdr integration hook and display metadata are active
	_ = profile.SyncHerdrIntegration(profileDir)
	profile.SetTerminalTitle(targetProfile)
	_ = profile.ReportHerdrMetadataWithModel(cmd.Context(), targetProfile, activeModel)

	// Start background watcher for reset timer / periodic refresh if running in Herdr
	stopWatcher := profile.StartHerdrQuotaWatcher(cmd.Context(), targetProfile, activeModel)
	defer stopWatcher()

	runErr := profile.RunCmdWithSignals(cmd.Context(), profileDir, agyArgs...)

	// Persist any token created in macOS Keychain during execution (e.g. login) to profile disk storage
	profile.SyncKeychainTokenToDisk(profileDir, expectedRefreshToken)

	// Capture latest conversation info after execution
	idAfter, _, _ := profile.GetLatestConversationFileInfo(targetProfile)

	isInteractive := true
	for _, arg := range agyArgs {
		if isAgySubcommand(arg) {
			isInteractive = false
			break
		}
		if arg == "-p" || arg == "--print" || arg == "--prompt" {
			isInteractive = false
			break
		}
	}

	if idAfter != "" && isInteractive {
		// Save to cache for O(1) next-time startup
		_ = profile.SaveLastConversation(idAfter)

		// Filter out conversation-triggering arguments from original args to preserve other flags
		var preservedFlags []string
		for i := 0; i < len(agyArgs); i++ {
			arg := agyArgs[i]
			if arg == "--conversation" {
				i++ // Skip the value
				continue
			}
			if strings.HasPrefix(arg, "--conversation=") {
				continue
			}
			if arg == "-c" || arg == "--continue" {
				continue
			}
			// Keep all other flags (like --dangerously-skip-permissions, --model, --sandbox, etc.)
			preservedFlags = append(preservedFlags, arg)
		}

		var extraFlags string
		if len(preservedFlags) > 0 {
			_ = profile.SaveSessionFlags(idAfter, preservedFlags)
			extraFlags = " " + strings.Join(preservedFlags, " ")
		}

		// Clear the last two lines printed by agy:
		// "Resume with -c (or command below):"
		// "agy --conversation=..."
		// using carriage return and cursor up ANSI codes.
		sshServer := os.Getenv("AGYS_SSH_SERVER")
		sshPath := os.Getenv("AGYS_SSH_PATH")
		if sshServer != "" {
			pathArg := ""
			if sshPath != "" {
				pathArg = " " + shellQuote(sshPath)
			}
			fmt.Printf("agys ssh %s%s %s -- --conversation=%s%s\n", sshServer, pathArg, targetProfile, idAfter, extraFlags)
		} else {
			fmt.Printf("agys run %s -- --conversation=%s%s\n", targetProfile, idAfter, extraFlags)
		}
	}

	return runErr
}

func init() {
	runCmd.Flags().BoolVarP(&runAll, "all", "a", false, "Execute agy command across all profiles sequentially")
	// Disable flag parsing for arguments after `--` to pass raw flags directly to agy
	runCmd.DisableFlagParsing = false
	rootCmd.AddCommand(runCmd)
}

var agySubcommands = map[string]bool{
	"agent":     true,
	"agents":    true,
	"changelog": true,
	"help":      true,
	"install":   true,
	"models":    true,
	"plugin":    true,
	"plugins":   true,
	"update":    true,
	"version":   true,
}

func isAgySubcommand(arg string) bool {
	return agySubcommands[arg]
}

// EnsureDefaultModelAndEffort ensures agyArgs has a default model (gemini-3.7-flash)
// and reasoning effort (high) if not explicitly provided by the user or subcommand.
func EnsureDefaultModelAndEffort(args []string) []string {
	// Check if first non-flag argument is an agy subcommand
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if isAgySubcommand(arg) {
			return args
		}
		break
	}

	hasModel := false
	hasEffort := false
	modelValue := ""

	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "-m" || arg == "--model" {
			hasModel = true
			if i+1 < len(args) {
				modelValue = args[i+1]
			}
		} else if strings.HasPrefix(arg, "--model=") {
			hasModel = true
			modelValue = strings.TrimPrefix(arg, "--model=")
		} else if arg == "--effort" || strings.HasPrefix(arg, "--effort=") {
			hasEffort = true
		}
	}

	finalArgs := make([]string, len(args), len(args)+4)
	copy(finalArgs, args)

	if !hasModel {
		finalArgs = append(finalArgs, "--model", "gemini-3.7-flash")
		modelValue = "gemini-3.7-flash"
	}

	if !hasEffort {
		// Only append --effort high if model supports effort (e.g. gemini-3.7-flash, flash, etc.)
		if modelValue == "" || modelValue == "gemini-3.7-flash" || modelValue == "gemini-3.6-flash" || modelValue == "gemini-2.5-flash" || modelValue == "flash" || modelValue == "gemini-2.5-flash-lite" {
			finalArgs = append(finalArgs, "--effort", "high")
		}
	}

	return finalArgs
}
