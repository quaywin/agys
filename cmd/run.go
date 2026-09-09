package cmd

import (
	"context"
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
				defaultProf, err := resolveDefaultProfile()
				if err != nil {
					return err
				}
				if defaultProf != "" {
					profileName = defaultProf
					agyArgs = args
				} else {
					if profile.ValidateName(firstArg) != nil && strings.HasPrefix(firstArg, "-") {
						return fmt.Errorf("no profile specified and no default profile set. Specify a profile or set one with `agys use <profile_name>`")
					}
					return fmt.Errorf("profile %q does not exist. Use `agys add %s` to create it, or set a default profile with `agys use <profile_name>`", firstArg, firstArg)
				}
			}
		} else {
			defaultProf, err := resolveDefaultProfile()
			if err != nil {
				return err
			}
			if defaultProf != "" {
				profileName = defaultProf
				agyArgs = args
			} else {
				return fmt.Errorf("no profile specified and no default profile set. Specify a profile or set one with `agys use <profile_name>`")
			}
		}

		return runWithProfile(cmd, profileName, agyArgs)
	},
}

func resolveDefaultProfile() (string, error) {
	current, err := profile.GetCurrent()
	if err != nil {
		return "", err
	}
	if current == "" {
		return "", nil
	}
	if profile.IsAuto(current) {
		return profile.AutoProfileKeyword, nil
	}
	currentExists, _, err := profile.Exists(current)
	if err != nil {
		return "", err
	}
	if currentExists {
		return current, nil
	}
	return "", nil
}

func runWithProfile(cmd *cobra.Command, profileName string, agyArgs []string) error {
	return runWithProfileAndDir(cmd, profileName, agyArgs, "")
}

func runWithProfileAndDir(cmd *cobra.Command, profileName string, agyArgs []string, workingDir string) error {
	// Extract explicit model if specified in agyArgs before defaults
	var explicitModel string
	hasExplicitModel := false
	for i := 0; i < len(agyArgs); i++ {
		if agyArgs[i] == "--model" || agyArgs[i] == "-m" {
			hasExplicitModel = true
			if i+1 < len(agyArgs) {
				explicitModel = agyArgs[i+1]
			}
			break
		}
		if strings.HasPrefix(agyArgs[i], "--model=") {
			hasExplicitModel = true
			explicitModel = strings.TrimPrefix(agyArgs[i], "--model=")
			break
		}
		if strings.HasPrefix(agyArgs[i], "-m=") {
			hasExplicitModel = true
			explicitModel = strings.TrimPrefix(agyArgs[i], "-m=")
			break
		}
	}

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

	_ = os.Setenv("AGYS_PROFILE", targetProfile)

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

	// Keep the OAuth token refreshed in the background so future launches reuse the
	// existing authorization instead of re-authorizing inside agy at startup.
	profile.ArmTokenKeepAlive(targetProfile)

	// Merge trusted workspaces across all profiles prior to execution
	_ = profile.SyncTrustedWorkspaces()

	// Resolve active model following priority: explicit CLI flag > .active_model cache > settings.json > default Gemini (gemini-3.8-flash)
	activeModel := profile.ResolveActiveModel(profileDir, explicitModel)
	if activeModel == "" || activeModel == "gemini" {
		activeModel = profile.GetLatestGeminiModel()
	}

	// If explicit model was provided on CLI, persist it to .active_model and sync to settings.json
	if hasExplicitModel {
		_ = profile.WriteFileAtomic(filepath.Join(profileDir, ".active_model"), []byte(activeModel), 0600)
		profile.SyncModelToSettings(profileDir, activeModel)
	}

	// Keep copy of original user args before model/effort injection for flag preservation and session detection
	originalUserArgs := append([]string(nil), agyArgs...)

	// Apply default model and effort to agyArgs using resolved activeModel
	agyArgs = EnsureDefaultModelAndEffortWithModel(agyArgs, activeModel)

	// Persist active reasoning effort to profile cache
	activeEffort := ""
	for i := 0; i < len(agyArgs); i++ {
		if agyArgs[i] == "--effort" && i+1 < len(agyArgs) {
			activeEffort = agyArgs[i+1]
			break
		} else if strings.HasPrefix(agyArgs[i], "--effort=") {
			activeEffort = strings.TrimPrefix(agyArgs[i], "--effort=")
			break
		}
	}
	if activeEffort != "" {
		_ = profile.WriteFileAtomic(filepath.Join(profileDir, ".active_effort"), []byte(activeEffort+"\n"), 0600)
	} else {
		_ = os.Remove(filepath.Join(profileDir, ".active_effort"))
	}

	// Clean up stale session context on fresh new interactive session start (not a resume)
	isResume := false
	resumeConvID := ""
	for i := 0; i < len(originalUserArgs); i++ {
		a := originalUserArgs[i]
		if a == "-c" || a == "--continue" || a == "-r" || a == "--resume" {
			isResume = true
			break
		}
		if strings.HasPrefix(a, "--conversation=") {
			isResume = true
			resumeConvID = strings.TrimPrefix(a, "--conversation=")
			break
		}
		if strings.HasPrefix(a, "--resume=") {
			isResume = true
			resumeConvID = strings.TrimPrefix(a, "--resume=")
			break
		}
		if (a == "--conversation" || a == "--resume") && i+1 < len(originalUserArgs) {
			isResume = true
			resumeConvID = originalUserArgs[i+1]
			break
		}
	}
	if !isResume && isInteractiveSession(originalUserArgs) {
		_ = profile.ResetSessionContext(profileDir)
	} else if isResume && resumeConvID != "" {
		if existingState, ok := profile.GetSessionContextState(profileDir); ok && existingState != nil {
			if existingState.ConversationID != "" && existingState.ConversationID != resumeConvID {
				_ = profile.ResetSessionContext(profileDir)
			}
		}
	}

	// Ensure statusLine hook is configured in settings.json to capture real-time context window and render footer telemetry
	_ = profile.SyncStatusLineSettings(profileDir)

	// Ensure Herdr integration hook and display metadata are active ONLY in Herdr environment
	if profile.IsInHerdrEnvironment() {
		_ = profile.SyncHerdrIntegration(profileDir)
		profile.SetTerminalTitle(targetProfile)
		_ = profile.ReportHerdrMetadataWithModel(cmd.Context(), targetProfile, activeModel)

		// Start background watcher for reset timer / periodic refresh if running in Herdr
		stopWatcher := profile.StartHerdrQuotaWatcher(cmd.Context(), targetProfile, activeModel)
		defer func() {
			stopWatcher()
			_ = profile.ClearHerdrMetadata(context.Background())
		}()
	}

	runErr := profile.RunCmdWithSignalsInDir(cmd.Context(), profileDir, workingDir, agyArgs...)

	// Persist any token created in macOS Keychain during execution (e.g. login) to profile disk storage
	profile.SyncKeychainTokenToDisk(profileDir, expectedRefreshToken)

	// Capture latest conversation info after execution
	idAfter, _, _ := profile.GetLatestConversationFileInfo(targetProfile)

	isInteractive := isInteractiveSession(originalUserArgs)

	if idAfter != "" && isInteractive {
		// Save to cache for O(1) next-time startup
		_ = profile.SaveLastConversation(idAfter)

		// Filter out conversation-triggering arguments from original args to preserve other flags
		var preservedFlags []string
		for i := 0; i < len(originalUserArgs); i++ {
			arg := originalUserArgs[i]
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
			var quotedFlags []string
			for _, f := range preservedFlags {
				quotedFlags = append(quotedFlags, shellQuote(f))
			}
			extraFlags = " " + strings.Join(quotedFlags, " ")
		}

		// In interactive terminal (TTY), clear the raw child agy resume lines:
		// "Resume with -c (or command below):"
		// "agy --conversation=..."
		// and replace them with the agys-native command.
		isTTY := false
		if fi, err := os.Stdout.Stat(); err == nil && (fi.Mode()&os.ModeCharDevice) != 0 {
			isTTY = true
		}

		sshServer := os.Getenv("AGYS_SSH_SERVER")
		sshPath := os.Getenv("AGYS_SSH_PATH")

		var resumeCmdStr string
		if sshServer != "" {
			pathArg := ""
			if sshPath != "" {
				pathArg = " " + shellQuote(sshPath)
			}
			resumeCmdStr = fmt.Sprintf("agys ssh %s%s %s -- --conversation=%s%s", sshServer, pathArg, targetProfile, idAfter, extraFlags)
		} else {
			resumeCmdStr = fmt.Sprintf("agys run %s -- --conversation=%s%s", targetProfile, idAfter, extraFlags)
		}

		if isTTY {
			// Clear last 2 lines printed by agy and overwrite cleanly with agys command
			fmt.Print("\x1b[1A\x1b[2K\r\x1b[1A\x1b[2K\r")
			fmt.Printf("Resume with 'agys run -c' (or command below):\n%s\n", resumeCmdStr)
		} else {
			fmt.Println(resumeCmdStr)
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
	"agent":          true,
	"agents":         true,
	"auth":           true,
	"changelog":      true,
	"config":         true,
	"help":           true,
	"install":        true,
	"mcp":            true,
	"mic-serve":      true,
	"models":         true,
	"plugin":         true,
	"plugins":        true,
	"remote-control": true,
	"update":         true,
	"version":        true,
}

var agyBoolFlags = map[string]bool{
	"-c":                             true,
	"--continue":                     true,
	"--dangerously-skip-permissions": true,
	"--disable-slash-commands":       true,
	"--new-project":                  true,
	"--sandbox":                      true,
	"-h":                             true,
	"--help":                         true,
	"-v":                             true,
	"--version":                      true,
}

var agySubcommandVerbs = map[string]map[string]bool{
	"auth":           {"login": true, "logout": true, "status": true, "token": true, "refresh": true},
	"config":         {"get": true, "set": true, "list": true, "path": true},
	"mcp":            {"list": true, "add": true, "remove": true, "enable": true, "disable": true, "status": true},
	"plugin":         {"list": true, "install": true, "uninstall": true, "enable": true, "disable": true},
	"plugins":        {"list": true, "install": true, "uninstall": true, "enable": true, "disable": true},
	"agent":          {"list": true, "create": true, "delete": true, "show": true},
	"agents":         {"list": true},
	"remote-control": {"status": true, "start": true, "stop": true},
}

func isAgySubcommand(arg string) bool {
	return agySubcommands[arg]
}

func isAgySubcommandInvocation(args []string, idx int) bool {
	cmd := args[idx]
	if !isAgySubcommand(cmd) {
		return false
	}
	// Subcommand alone with no trailing arguments (e.g. "models", "version", "update")
	if idx == len(args)-1 {
		return true
	}
	// Followed by a flag (e.g. "models -h", "update --check")
	next := args[idx+1]
	if strings.HasPrefix(next, "-") {
		return true
	}
	// Followed by a known subcommand verb (e.g. "auth status", "mcp list")
	if verbs, ok := agySubcommandVerbs[cmd]; ok && verbs[next] {
		return true
	}
	return false
}

func findFirstPositionalIndex(args []string) int {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			// POSIX delimiter: anything after this is user prompt/arguments, not flags or subcommands
			return -1
		}
		if strings.HasPrefix(arg, "-") {
			if strings.Contains(arg, "=") || agyBoolFlags[arg] {
				continue
			}
			i++ // skip flag argument
			continue
		}
		return i
	}
	return -1
}

func isAgySubcommandCall(args []string) bool {
	idx := findFirstPositionalIndex(args)
	if idx < 0 {
		return false
	}
	return isAgySubcommandInvocation(args, idx)
}

func isInteractiveSession(agyArgs []string) bool {
	// First check explicit non-interactive flags
	for _, arg := range agyArgs {
		if arg == "-p" || arg == "--print" || arg == "--prompt" ||
			strings.HasPrefix(arg, "-p=") || strings.HasPrefix(arg, "--prompt=") || strings.HasPrefix(arg, "--print=") ||
			strings.HasPrefix(arg, "--input-format") || strings.HasPrefix(arg, "--output-format") ||
			arg == "-h" || arg == "--help" || arg == "-v" || arg == "--version" {
			return false
		}
	}

	return !isAgySubcommandCall(agyArgs)
}

// EnsureDefaultModelAndEffort ensures agyArgs has a default model (gemini-3.8-flash)
// and reasoning effort (high) if not explicitly provided by the user or subcommand.
func EnsureDefaultModelAndEffort(args []string) []string {
	return EnsureDefaultModelAndEffortWithModel(args, profile.GetLatestGeminiModel())
}

// EnsureDefaultModelAndEffortWithModel ensures agyArgs has the specified model
// and reasoning effort (high) if not explicitly provided by the user or subcommand.
func EnsureDefaultModelAndEffortWithModel(args []string, defaultModel string) []string {
	if defaultModel == "" {
		defaultModel = profile.GetLatestGeminiModel()
	}

	// Subcommands do not accept model/effort flags
	if isAgySubcommandCall(args) {
		return args
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
		} else if strings.HasPrefix(arg, "-m=") {
			hasModel = true
			modelValue = strings.TrimPrefix(arg, "-m=")
		} else if arg == "--effort" || strings.HasPrefix(arg, "--effort=") {
			hasEffort = true
		}
	}

	finalArgs := make([]string, len(args), len(args)+4)
	copy(finalArgs, args)

	if !hasModel {
		finalArgs = append(finalArgs, "--model", defaultModel)
		modelValue = defaultModel
	} else if modelValue == "auto" || modelValue == "latest" {
		targetModel := profile.GetLatestGeminiModel()
		for i := 0; i < len(finalArgs); i++ {
			if (finalArgs[i] == "-m" || finalArgs[i] == "--model") && i+1 < len(finalArgs) {
				finalArgs[i] = "--model"
				finalArgs[i+1] = targetModel
				modelValue = targetModel
				break
			} else if strings.HasPrefix(finalArgs[i], "--model=") {
				finalArgs[i] = "--model=" + targetModel
				modelValue = targetModel
				break
			} else if strings.HasPrefix(finalArgs[i], "-m=") {
				finalArgs[i] = "--model=" + targetModel
				modelValue = targetModel
				break
			}
		}
	}

	if !hasEffort {
		if profile.ModelSupportsEffort(modelValue) {
			finalArgs = append(finalArgs, "--effort", "high")
		}
	}

	// Normalize shorthand -m and -m= to canonical --model since agy only recognizes --model
	for i := 0; i < len(finalArgs); i++ {
		if finalArgs[i] == "-m" && i+1 < len(finalArgs) {
			finalArgs[i] = "--model"
		} else if strings.HasPrefix(finalArgs[i], "-m=") {
			finalArgs[i] = "--model=" + strings.TrimPrefix(finalArgs[i], "-m=")
		}
	}

	return finalArgs
}
