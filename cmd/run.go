package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/quaywin/agys/pkg/config"
	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var runCmd = &cobra.Command{
	Use:               "run [profile_name] -- [agy_commands]",
	Short:             "Execute agy command with specified profile, auto quota selection, or default profile",
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var profileName string
		var agyArgs []string

		firstArg := args[0]
		if profile.IsAuto(firstArg) {
			profileName = profile.AutoProfileKeyword
			agyArgs = args[1:]
		} else {
			exists, _, err := profile.Exists(firstArg)
			if err != nil {
				return err
			}

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
					return fmt.Errorf("profile %q does not exist. Use `agys add %s` to create it, or set a default profile with `agys use <profile_name>`", firstArg, firstArg)
				}
			}
		}

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

		return runWithProfile(cmd, profileName, agyArgs)
	},
}

var (
	flagFailover   bool
	flagNoFailover bool
)

func runWithProfile(cmd *cobra.Command, profileName string, agyArgs []string) error {
	cfg, _ := config.Load()
	effectiveFailover := (cfg.AutoFailover || flagFailover) && !flagNoFailover

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

	visited := make(map[string]bool)
	retries := 0
	maxRetries := cfg.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3
	}

	var runErr error

	isInteractive := true
	for _, arg := range agyArgs {
		if arg == "models" || arg == "agents" || arg == "agent" || arg == "changelog" || arg == "update" || arg == "help" || arg == "install" {
			isInteractive = false
			break
		}
		if arg == "-p" || arg == "--print" || arg == "--prompt" {
			isInteractive = false
			break
		}
	}

	for {
		visited[targetProfile] = true

		profileDir, err := profile.GetProfileDir(targetProfile)
		if err != nil {
			return err
		}

		execCmd := profile.BuildCmd(profileDir, agyArgs...)

		var outWriter *profile.QuotaInterceptorWriter
		var errWriter *profile.QuotaInterceptorWriter

		if effectiveFailover {
			outWriter = profile.NewQuotaInterceptorWriter(os.Stdout)
			errWriter = profile.NewQuotaInterceptorWriter(os.Stderr)
		}

		runErr = profile.RunWithFailover(execCmd, outWriter, errWriter, effectiveFailover, isInteractive)

		if effectiveFailover && runErr != nil {
			capturedOutput := ""
			if outWriter != nil {
				capturedOutput += outWriter.String() + "\n"
			}
			if errWriter != nil {
				capturedOutput += errWriter.String() + "\n"
			}

			isQuota := profile.IsQuotaError(capturedOutput)
			if !isQuota && isInteractive {
				if summary, err := profile.FetchQuota(cmd.Context(), targetProfile); err == nil {
					score := profile.Calculate5HQuotaScore(summary)
					if score <= 0 {
						isQuota = true
					}
				}
			}

			if isQuota && retries < maxRetries {
				nextProfile, score, failoverErr := profile.SelectNextBestProfile(cmd.Context(), visited)
				if failoverErr == nil && nextProfile != "" {
					retries++
					scoreStr := fmt.Sprintf("%.1f%%", score*100)
					if score < 0 {
						scoreStr = "N/A"
					}
					fmt.Fprintf(os.Stderr, "\n[agys] ⚠️ Profile %q hit quota limit. Auto-failing over to profile %q (5h Gemini quota: %s) [Retry %d/%d]...\n\n", targetProfile, nextProfile, scoreStr, retries, maxRetries)
					targetProfile = nextProfile
					continue
				}
			}
		}

		break
	}

	// Capture latest conversation info after execution
	idAfter, _, _ := profile.GetLatestConversationFileInfo(targetProfile)

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
			extraFlags = " " + strings.Join(preservedFlags, " ")
		}

		// Clear the last two lines printed by agy:
		// "Resume with -c (or command below):"
		// "agy --conversation=..."
		// using carriage return and cursor up ANSI codes.
		fmt.Print("\r\033[K\033[A\033[K\033[A\033[K")
		fmt.Println("Resume with -c (or command below):")
		fmt.Printf("agys run -- --conversation=%s%s\n", idAfter, extraFlags)
	}

	return runErr
}

func init() {
	runCmd.Flags().BoolVarP(&flagFailover, "failover", "f", false, "Enable auto quota failover for this command")
	runCmd.Flags().BoolVar(&flagNoFailover, "no-failover", false, "Disable auto quota failover for this command")
	// Disable flag parsing for arguments after `--` to pass raw flags directly to agy
	runCmd.DisableFlagParsing = false
	rootCmd.AddCommand(runCmd)
}

