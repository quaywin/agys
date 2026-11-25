package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	runAll          bool
	runNoStatusLine bool
)

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

func extractModelAndEffortFlags(args []string) (explicitModel string, hasModel bool, explicitEffort string, hasEffort bool) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--model" || arg == "-m" {
			hasModel = true
			if i+1 < len(args) {
				explicitModel = args[i+1]
			}
		} else if strings.HasPrefix(arg, "--model=") {
			hasModel = true
			explicitModel = strings.TrimPrefix(arg, "--model=")
		} else if strings.HasPrefix(arg, "-m=") {
			hasModel = true
			explicitModel = strings.TrimPrefix(arg, "-m=")
		} else if arg == "--effort" {
			hasEffort = true
			if i+1 < len(args) {
				explicitEffort = args[i+1]
			}
		} else if strings.HasPrefix(arg, "--effort=") {
			hasEffort = true
			explicitEffort = strings.TrimPrefix(arg, "--effort=")
		}
	}
	return explicitModel, hasModel, explicitEffort, hasEffort
}

func prepareRunModelAndEffortArgs(profileDir string, agyArgs []string) ([]string, string) {
	explicitModel, hasExplicitModel, explicitEffort, hasExplicitEffort := extractModelAndEffortFlags(agyArgs)

	activeModel, activeEffort := profile.ResolveActiveModelAndEffort(profileDir, explicitModel, explicitEffort)
	if activeModel == "" || activeModel == "gemini" {
		activeModel = profile.GetLatestGeminiModel()
	}

	preparedArgs := EnsureDefaultModelAndEffortWithOptions(agyArgs, activeModel, activeEffort)

	effectiveModel, _, effectiveEffort, _ := extractModelAndEffortFlags(preparedArgs)
	if effectiveModel == "" {
		effectiveModel = activeModel
	}
	if effectiveEffort == "" {
		_, embeddedEff := profile.SplitModelAndEffort(effectiveModel)
		effectiveEffort = embeddedEff
	}

	if profileDir != "" {
		// Persist both .active_model and settings.json whenever either model or effort was explicitly specified,
		// preventing stale variant suffixes in .active_model (e.g. "(XHigh)") from overriding `--effort low`.
		if hasExplicitModel || hasExplicitEffort {
			settingsTarget := profile.FormatModelWithEffort(effectiveModel, effectiveEffort)
			if settingsTarget == "" {
				settingsTarget = effectiveModel
			}
			_ = profile.WriteFileAtomic(filepath.Join(profileDir, ".active_model"), []byte(settingsTarget), 0600)
			profile.SyncModelToSettings(profileDir, settingsTarget)
		}

		if effectiveEffort != "" {
			_ = profile.WriteFileAtomic(filepath.Join(profileDir, ".active_effort"), []byte(effectiveEffort+"\n"), 0600)
		} else {
			_ = os.Remove(filepath.Join(profileDir, ".active_effort"))
		}
	}

	return preparedArgs, activeModel
}

func runWithProfileAndDir(cmd *cobra.Command, profileName string, agyArgs []string, workingDir string) error {
	agyArgs, _, _ = profile.EnsureAvailableHubPort(agyArgs)

	// Normalize arguments (e.g. login, auth login, version) to canonical agy syntax
	agyArgs = normalizeAgyArgs(agyArgs)

	// Detect if the user is resuming a conversation and handle profile routing or migration
	var resumeErr error
	profileName, agyArgs, resumeErr = resolveResumeProfile(profileName, agyArgs, workingDir)
	if resumeErr != nil {
		return resumeErr
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
		fmt.Fprintf(os.Stderr, "[agys] Auto-selected profile %q (5h Gemini quota: %s%s)\n", targetProfile, scoreStr, weeklyStr)
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

	// Keep copy of original user args before model/effort injection for flag preservation and session detection
	originalUserArgs := append([]string(nil), agyArgs...)

	// Resolve, apply, and persist active model and reasoning effort
	agyArgs, activeModel := prepareRunModelAndEffortArgs(profileDir, agyArgs)

	// Clean up stale session context on fresh new interactive session start (not a resume)
	isResume, resumeConvID := extractResumeFlag(originalUserArgs)
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
	if runNoStatusLine || profile.IsStatusLineDisabled() {
		_ = os.Setenv("AGYS_NO_STATUSLINE", "1")
		_ = profile.RemoveStatusLineSettings(profileDir)
	} else {
		_ = profile.SyncStatusLineSettings(profileDir)
	}

	// Ensure Herdr integration hook and display metadata are active ONLY in Herdr environment
	if profile.IsInHerdrEnvironment() {
		_ = profile.SyncHerdrIntegration(profileDir)
		// Fast initial Herdr sidebar sync without blocking CLI startup
		fastQuota, hasFast := profile.GetProfileFullQuotaDetailsFast(targetProfile, activeModel)
		if hasFast && fastQuota != nil {
			_ = profile.ReportHerdrMetadataWithModel(cmd.Context(), targetProfile, activeModel, fastQuota)
		} else {
			// First run / no cache: launch agy immediately and update Herdr sidebar in background
			go func() {
				bgCtx, bgCancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer bgCancel()
				_ = profile.ReportHerdrMetadataWithModel(bgCtx, targetProfile, activeModel)
			}()
		}

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
		preservedFlags := filterPreservedSessionFlags(originalUserArgs)

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
	runCmd.Flags().BoolVar(&runNoStatusLine, "no-statusline", false, "Disable statusline footer and hook in agy for maximum performance")
	// Disable flag parsing for arguments after `--` to pass raw flags directly to agy
	runCmd.DisableFlagParsing = false
	rootCmd.AddCommand(runCmd)
}

var agySubcommands = map[string]bool{
	"acp":            true,
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
	"-c":                               true,
	"--continue":                       true,
	"--dangerously-skip-permissions":   true,
	"--disable-slash-commands":         true,
	"--disable-telemetry":              true,
	"--include-default-customizations": true,
	"--json":                           true,
	"--login":                          true,
	"--new-project":                    true,
	"--process-per-session":            true,
	"--remote-agent":                   true,
	"--remote-control":                 true,
	"--sandbox":                        true,
	"--skip-aliases":                   true,
	"--skip-path":                      true,
	"--stdio":                          true,
	"--strict":                         true,
	"--worker":                         true,
	"-h":                               true,
	"--help":                           true,
	"-v":                               true,
	"--version":                        true,
}

var pluginSubcommandVerbs = map[string]bool{
	"list": true, "import": true, "install": true, "uninstall": true,
	"enable": true, "disable": true, "validate": true, "link": true, "help": true,
}

var agySubcommandVerbs = map[string]map[string]bool{
	"auth":           {"login": true, "logout": true, "status": true, "token": true, "refresh": true},
	"config":         {"get": true, "set": true, "list": true, "path": true},
	"mcp":            {"list": true, "add": true, "remove": true, "enable": true, "disable": true, "status": true},
	"plugin":         pluginSubcommandVerbs,
	"plugins":        pluginSubcommandVerbs,
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
	return EnsureDefaultModelAndEffortWithOptions(args, profile.GetLatestGeminiModel(), profile.DefaultGeminiEffort)
}

// EnsureDefaultModelAndEffortWithModel ensures agyArgs has the specified model
// and reasoning effort (high) if not explicitly provided by the user or subcommand.
func EnsureDefaultModelAndEffortWithModel(args []string, defaultModel string) []string {
	return EnsureDefaultModelAndEffortWithOptions(args, defaultModel, profile.DefaultGeminiEffort)
}

// EnsureDefaultModelAndEffortWithOptions ensures agyArgs has the specified default model and effort
// while resolving aliases (auto/latest/flash/hydrogen/pro), preventing `--effort` conflicts when a
// variant-suffixed model ID (e.g. "gemini-4-hydrogen-xhigh") is supplied, and clamping effort to model capabilities.
func EnsureDefaultModelAndEffortWithOptions(args []string, defaultModel, defaultEffort string) []string {
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
	} else if resolvedAlias := profile.ResolveModelAlias(modelValue); resolvedAlias != "" {
		for i := 0; i < len(finalArgs); i++ {
			if (finalArgs[i] == "-m" || finalArgs[i] == "--model") && i+1 < len(finalArgs) {
				finalArgs[i] = "--model"
				finalArgs[i+1] = resolvedAlias
				modelValue = resolvedAlias
				break
			} else if strings.HasPrefix(finalArgs[i], "--model=") {
				finalArgs[i] = "--model=" + resolvedAlias
				modelValue = resolvedAlias
				break
			} else if strings.HasPrefix(finalArgs[i], "-m=") {
				finalArgs[i] = "--model=" + resolvedAlias
				modelValue = resolvedAlias
				break
			}
		}
	}

	if !hasEffort {
		baseModel, embeddedEffort := profile.SplitModelAndEffort(modelValue)
		// Never inject --effort when the model ID/name already encodes an effort variant (e.g. gemini-4-hydrogen-xhigh),
		// since agy rejects `--model <variant> --effort <other>` as a conflict.
		if embeddedEffort == "" && profile.ModelSupportsEffort(baseModel) {
			targetEffort := strings.TrimSpace(defaultEffort)
			if targetEffort == "" {
				targetEffort = profile.DefaultGeminiEffort
			}
			targetEffort = profile.ClampEffortForModel(baseModel, targetEffort)
			if targetEffort != "" {
				finalArgs = append(finalArgs, "--effort", targetEffort)
			}
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

func extractResumeFlag(args []string) (isResume bool, explicitConvID string) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "--conversation=") {
			return true, strings.TrimPrefix(arg, "--conversation=")
		}
		if strings.HasPrefix(arg, "--resume=") {
			return true, strings.TrimPrefix(arg, "--resume=")
		}
		if strings.HasPrefix(arg, "-r=") {
			return true, strings.TrimPrefix(arg, "-r=")
		}
		if (arg == "--conversation" || arg == "--resume" || arg == "-r") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			return true, args[i+1]
		}
		if arg == "-c" || arg == "--continue" || arg == "-r" || arg == "--resume" {
			return true, ""
		}
	}
	return false, ""
}

func filterPreservedSessionFlags(args []string) []string {
	var preserved []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--conversation=") || strings.HasPrefix(arg, "--resume=") || strings.HasPrefix(arg, "-r=") {
			continue
		}
		if arg == "--conversation" || arg == "--resume" || arg == "-r" {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++ // skip conversation ID
			}
			continue
		}
		if arg == "-c" || arg == "--continue" {
			continue
		}
		preserved = append(preserved, arg)
	}
	return preserved
}

func resolveResumeProfile(profileName string, agyArgs []string, workingDir ...string) (string, []string, error) {
	var detectedProfile string
	var detectedConvID string
	var detectErr error

	isResume, explicitConvID := extractResumeFlag(agyArgs)
	if isResume {
		if explicitConvID != "" {
			detectedConvID = explicitConvID
			detectedProfile, detectErr = profile.FindProfileByConversation(detectedConvID)
		} else {
			targetDir := ""
			if len(workingDir) > 0 && workingDir[0] != "" {
				targetDir = workingDir[0]
			} else {
				targetDir, _ = os.Getwd()
			}
			detectedProfile, detectedConvID, detectErr = profile.FindProfileAndConvByLatestConversationInWorkspace(targetDir)
		}
	}

	if detectErr != nil || detectedProfile == "" {
		return profileName, normalizeResumeArgs(agyArgs, ""), nil
	}

	if profile.IsAuto(profileName) {
		// Auto profile mode: automatically preserve and use the owning profile
		if profileName != detectedProfile {
			fmt.Fprintf(os.Stderr, "[agys] Resumed conversation detected. Auto-switching profile %q -> %q\n", profileName, detectedProfile)
			profileName = detectedProfile
		}
		return profileName, normalizeResumeArgs(agyArgs, detectedConvID), nil
	}

	if profileName != detectedProfile {
		// Explicit profile specified, different from detected owner:
		// Migrate the conversation brain to the specified profile so the user can continue with the new profile.
		fmt.Fprintf(os.Stderr, "[agys] Resuming conversation %s on specified profile %q (migrating brain from %q)...\n", detectedConvID, profileName, detectedProfile)
		if err := profile.MigrateConversation(detectedConvID, detectedProfile, profileName); err != nil {
			return profileName, agyArgs, fmt.Errorf("failed to migrate conversation %s from %s to %s: %w", detectedConvID, detectedProfile, profileName, err)
		}
	}

	return profileName, normalizeResumeArgs(agyArgs, detectedConvID), nil
}

func normalizeResumeArgs(args []string, convID string) []string {
	normalized := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "--resume=") {
			id := strings.TrimPrefix(arg, "--resume=")
			normalized = append(normalized, "--conversation="+id)
		} else if strings.HasPrefix(arg, "-r=") {
			id := strings.TrimPrefix(arg, "-r=")
			normalized = append(normalized, "--conversation="+id)
		} else if (arg == "--resume" || arg == "-r") && i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
			normalized = append(normalized, "--conversation", args[i+1])
			i++
		} else if arg == "-r" || arg == "--resume" {
			if convID != "" {
				normalized = append(normalized, "--conversation="+convID)
			} else {
				normalized = append(normalized, "--continue")
			}
		} else if arg == "-c" || arg == "--continue" {
			if convID != "" {
				normalized = append(normalized, "--conversation="+convID)
			} else {
				normalized = append(normalized, arg)
			}
		} else {
			normalized = append(normalized, arg)
		}
	}
	return normalized
}

func normalizeAgyArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}

	// 1. Normalize "auth login" or "login" to empty args so agy runs interactively to initiate login
	if (len(args) == 1 && args[0] == "login") ||
		(len(args) == 2 && args[0] == "auth" && args[1] == "login") {
		return []string{}
	}

	// 2. Normalize "version" to "--version" since agy has flag --version instead of subcommand version
	if len(args) == 1 && args[0] == "version" {
		return []string{"--version"}
	}

	return args
}
