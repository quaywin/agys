package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	resumeAll         bool
	resumeProject     string
	resumeProfile     string
	resumeLimit       int
	resumeInteractive bool
	resumeJSON        bool
)

var resumeCmd = &cobra.Command{
	Use:               "resume [index_or_project] [-- agy_flags]",
	Aliases:           []string{"r"},
	Short:             "List and resume previous conversation sessions by project and profile",
	ValidArgsFunction: CompleteResumeArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		var selectedIndex int
		var extraAgyArgs []string
		var filterProjectArg string

		// Separate flags/args after '--' if present
		dashIdx := cmd.ArgsLenAtDash()
		if dashIdx != -1 {
			extraAgyArgs = args[dashIdx:]
			args = args[:dashIdx]
		}

		if len(args) == 1 {
			if idx, err := strconv.Atoi(args[0]); err == nil && idx > 0 {
				selectedIndex = idx
			} else {
				filterProjectArg = args[0]
			}
		} else if len(args) >= 2 {
			if idx, err := strconv.Atoi(args[0]); err == nil && idx > 0 {
				selectedIndex = idx
				filterProjectArg = args[1]
			} else if idx, err := strconv.Atoi(args[1]); err == nil && idx > 0 {
				filterProjectArg = args[0]
				selectedIndex = idx
			} else {
				filterProjectArg = args[0]
			}
		}

		// Detect current directory if project filter not explicitly set and --all is false
		cwd, _ := os.Getwd()
		var activeProjectFilter string

		if resumeProject != "" {
			activeProjectFilter = resumeProject
		} else if filterProjectArg != "" {
			activeProjectFilter = filterProjectArg
		} else if !resumeAll {
			// Auto-detect project root of current directory
			if cwd != "" {
				root := profile.FindProjectRoot(cwd)
				if root != "" {
					activeProjectFilter = root
				} else {
					activeProjectFilter = cwd
				}
			}
		}

		filter := profile.SessionFilter{
			Project: activeProjectFilter,
			Profile: resumeProfile,
			All:     resumeAll,
			Limit:   resumeLimit,
		}

		sessions, err := profile.ListSessions(cmd.Context(), filter)
		if err != nil {
			return fmt.Errorf("failed to list sessions: %w", err)
		}

		if resumeJSON {
			jsonOut, err := profile.RenderSessionsJSON(sessions)
			if err != nil {
				return err
			}
			fmt.Println(jsonOut)
			return nil
		}

		if len(sessions) == 0 {
			if activeProjectFilter != "" && !resumeAll {
				fmt.Printf("No previous conversation sessions found for project %q.\nUse 'agys resume -a' to view sessions across all projects.\n", filepath.Base(activeProjectFilter))
			} else {
				fmt.Println("No previous conversation sessions found.")
			}
			return nil
		}

		// Helper to resume selected session
		resumeSession := func(chosen profile.ConversationSession) error {
			fmt.Fprintf(os.Stderr, "[agys] Resuming session %s (Project: %s, Profile: %s)\n", chosen.ConvID, chosen.ProjectName, chosen.Profile)
			agyArgs := buildResumeAgyArgs(chosen.ConvID, extraAgyArgs)
			return runWithProfileAndDir(cmd, chosen.Profile, agyArgs, chosen.ProjectPath)
		}

		// If selectedIndex was passed directly as arg (e.g. `agys resume 1` or `agys resume caudata 1`)
		if selectedIndex > 0 {
			if selectedIndex > len(sessions) {
				return fmt.Errorf("invalid session index %d: out of range (1..%d)", selectedIndex, len(sessions))
			}
			chosen := sessions[selectedIndex-1]
			return resumeSession(chosen)
		}

		// Header notice
		if activeProjectFilter != "" && !resumeAll {
			fmt.Printf("Recent Sessions for Project %q (use -a / --all to view all projects):\n\n", filepath.Base(activeProjectFilter))
		} else {
			fmt.Println("Recent Sessions across all Projects & Profiles:")
			fmt.Println()
		}

		// Interactive mode handling
		isTTY := false
		fi, err := os.Stdin.Stat()
		if err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
			isTTY = true
		}

		if resumeInteractive || isTTY {
			chosen, err := selectSessionInteractive(sessions)
			if err != nil {
				// Fallback to static listing + standard input prompt
				for i, s := range sessions {
					fmt.Println(formatSessionLine(i+1, s, false, getTerminalWidth()))
				}
				fmt.Println()
				fmt.Printf("Select session to resume [1-%d, or Enter to exit]: ", len(sessions))
				reader := bufio.NewReader(os.Stdin)
				input, readErr := reader.ReadString('\n')
				if readErr != nil {
					return nil
				}
				input = strings.TrimSpace(input)
				if input == "" {
					return nil
				}

				choice, parseErr := strconv.Atoi(input)
				if parseErr != nil || choice < 1 || choice > len(sessions) {
					return fmt.Errorf("invalid choice %q: must be between 1 and %d", input, len(sessions))
				}
				chosen = &sessions[choice-1]
			}

			if chosen == nil {
				return nil
			}
			return resumeSession(*chosen)
		}

		// Non-interactive display (pipes / scripts)
		for i, s := range sessions {
			fmt.Println(formatSessionLine(i+1, s, false, getTerminalWidth()))
		}
		fmt.Println()
		fmt.Println("To resume a session, run: agys resume <NUM> or agys run <PROFILE> -- --conversation=<CONVERSATION_ID>")
		return nil
	},
}

func buildResumeAgyArgs(convID string, extraAgyArgs []string) []string {
	savedFlags, _ := profile.GetSessionFlags(convID)

	args := []string{"--conversation=" + convID}

	hasFlag := func(slice []string, flagName string) bool {
		for _, arg := range slice {
			if arg == flagName || strings.HasPrefix(arg, flagName+"=") {
				return true
			}
		}
		return false
	}

	for _, flag := range savedFlags {
		flagName := strings.SplitN(flag, "=", 2)[0]
		if !hasFlag(extraAgyArgs, flagName) {
			args = append(args, flag)
		}
	}

	args = append(args, extraAgyArgs...)
	return args
}

func init() {
	resumeCmd.Flags().BoolVarP(&resumeAll, "all", "a", false, "List all sessions across all projects")
	resumeCmd.Flags().StringVarP(&resumeProject, "project", "p", "", "Filter sessions by project name or directory path")
	resumeCmd.Flags().StringVar(&resumeProfile, "profile", "", "Filter sessions by profile name")
	resumeCmd.Flags().IntVarP(&resumeLimit, "limit", "n", 20, "Maximum number of sessions to display")
	resumeCmd.Flags().BoolVarP(&resumeInteractive, "interactive", "i", false, "Prompt interactively to choose a session")
	resumeCmd.Flags().BoolVar(&resumeJSON, "json", false, "Output session list in JSON format")

	_ = resumeCmd.RegisterFlagCompletionFunc("profile", CompleteProfileNames)
	_ = resumeCmd.RegisterFlagCompletionFunc("project", cobra.FixedCompletions(nil, cobra.ShellCompDirectiveFilterDirs))

	rootCmd.AddCommand(resumeCmd)
}
