package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	commitMsg     string
	commitAll     bool
	commitYes     bool
	commitNoCheck bool
	commitDryRun  bool
	commitModel   string
	commitPrompt  string
)

var commitCmd = &cobra.Command{
	Use:               "commit [profile_name] [flags]",
	Short:             "Check staged git files with AI and commit using auto-selected or specified profile",
	Long:              `Inspects staged git changes using an AI profile (auto-selected based on 5h Gemini quota or specified), performs a code review check, generates or validates a commit message, and commits the changes.`,
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		// 1. Check if inside a git repository
		if !profile.IsGitRepository("") {
			return fmt.Errorf("not a git repository (or any of the parent directories)")
		}

		// 2. Stage tracked modified/deleted files if -a / --all is set
		if commitAll {
			if err := profile.StageTrackedFiles(""); err != nil {
				return err
			}
		}

		// 3. Get staged files
		stagedFiles, err := profile.GetStagedFiles("")
		if err != nil {
			return err
		}
		if len(stagedFiles) == 0 {
			fmt.Println("No staged changes found. Use `git add <files>` to stage changes before committing.")
			return nil
		}

		// 4. Determine target profile
		var profileName string
		if len(args) > 0 {
			profileName = args[0]
		} else {
			current, err := profile.GetCurrent()
			if err != nil {
				return err
			}
			if current != "" {
				profileName = current
			} else {
				profileName = profile.AutoProfileKeyword
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

		// 5. Get staged diff
		stagedDiff, err := profile.GetStagedDiff("")
		if err != nil {
			return err
		}

		fmt.Fprintf(os.Stderr, "[agys] Inspecting %d staged file(s) under profile %q...\n", len(stagedFiles), targetProfile)

		var finalMessage string
		var checkSummary string

		if commitNoCheck && commitMsg != "" {
			finalMessage = commitMsg
			checkSummary = "Skipped (--no-check specified)."
		} else {
			result, err := profile.RunAgyCommitCheck(cmd.Context(), profileDir, stagedFiles, stagedDiff, commitMsg, commitNoCheck, commitModel, commitPrompt)
			if err != nil {
				if commitMsg != "" {
					fmt.Fprintf(os.Stderr, "[agys] Warning: AI code check failed (%v). Falling back to provided message.\n", err)
					finalMessage = commitMsg
					checkSummary = "AI check failed."
				} else {
					return fmt.Errorf("AI commit check failed: %w", err)
				}
			} else {
				finalMessage = result.CommitMessage
				checkSummary = result.CheckSummary
			}
		}

		// Display Code Review Summary
		if checkSummary != "" {
			fmt.Println("\n--- Code Review Summary ---")
			fmt.Println(checkSummary)
		}

		// Display Proposed Commit Message
		fmt.Println("\n--- Proposed Commit Message ---")
		fmt.Println(finalMessage)
		fmt.Println()

		if commitDryRun {
			fmt.Println("[agys] Dry-run complete. No changes committed.")
			return nil
		}

		// Ask for confirmation if not auto-accepted (-y / --yes)
		if !commitYes {
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Commit with this message? [Y/n/e(dit)]: ")
			input, _ := reader.ReadString('\n')
			input = strings.TrimSpace(strings.ToLower(input))

			if input == "n" || input == "no" {
				fmt.Println("Commit cancelled.")
				return nil
			} else if input == "e" || input == "edit" {
				fmt.Print("Enter custom commit message: ")
				customMsg, _ := reader.ReadString('\n')
				customMsg = strings.TrimSpace(customMsg)
				if customMsg == "" {
					return fmt.Errorf("commit message cannot be empty")
				}
				finalMessage = customMsg
			}
		}

		// Execute Git Commit
		fmt.Printf("[agys] Executing git commit...\n")
		return profile.ExecuteGitCommit("", finalMessage)
	},
}

func init() {
	commitCmd.Flags().StringVarP(&commitMsg, "message", "m", "", "Specify commit message directly")
	commitCmd.Flags().BoolVarP(&commitAll, "all", "a", false, "Automatically stage modified/deleted tracked files before commit")
	commitCmd.Flags().BoolVarP(&commitYes, "yes", "y", false, "Automatically accept commit message and commit without interactive prompt")
	commitCmd.Flags().BoolVar(&commitNoCheck, "no-check", false, "Skip AI code review check")
	commitCmd.Flags().BoolVar(&commitDryRun, "dry-run", false, "Perform AI review and message generation without executing git commit")
	commitCmd.Flags().StringVar(&commitModel, "model", "", "Override model for agy (e.g. gemini-2.5-pro)")
	commitCmd.Flags().StringVar(&commitPrompt, "prompt", "", "Additional custom prompt instructions for commit check")

	rootCmd.AddCommand(commitCmd)
}
