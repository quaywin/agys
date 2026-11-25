package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

// CompleteProfileNames returns matching profile names for tab-completion with rich descriptions.
func CompleteProfileNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	profiles, err := profile.List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	currentProfile, _ := profile.GetCurrent()
	var results []string

	autoDesc := profile.AutoProfileKeyword + "\tAutomatic 5h Gemini quota profile selection"
	if currentProfile == profile.AutoProfileKeyword {
		autoDesc += " (default)"
	}
	results = append(results, autoDesc)

	for _, p := range profiles {
		desc := p
		if p == currentProfile {
			desc += "\tDefault profile"
		} else {
			desc += "\tProfile directory"
		}
		email, _ := profile.GetCachedEmail(p)
		if email != "" {
			desc += " (" + email + ")"
		}
		results = append(results, desc)
	}

	return results, cobra.ShellCompDirectiveNoFileComp
}

// CompleteCloneOrRenameArgs handles completion for clone and rename commands.
func CompleteCloneOrRenameArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		profiles, err := profile.List()
		if err != nil {
			return nil, cobra.ShellCompDirectiveError
		}
		var results []string
		for _, p := range profiles {
			email, _ := profile.GetCachedEmail(p)
			desc := p + "\tSource profile"
			if email != "" {
				desc += " (" + email + ")"
			}
			results = append(results, desc)
		}
		return results, cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// CompleteModelNames returns completions for AI model names and aliases.
func CompleteModelNames() ([]string, cobra.ShellCompDirective) {
	var models []string
	seen := make(map[string]bool)

	// 1. Dynamic discovered models from cache if available
	if dm := profile.ReadCachedDiscoveredModels(); dm != nil && len(dm.AllModels) > 0 {
		for _, m := range dm.AllModels {
			desc := "AI Model"
			if m == dm.LatestFlash {
				desc = "Gemini Flash (Highest / Default)"
			} else if m == dm.LatestHydrogen && dm.LatestHydrogen != "" {
				desc = "Gemini 4 Flagship (Highest Reasoning)"
			} else if m == dm.LatestPro && dm.LatestPro != "" {
				desc = "Gemini Pro (Highest)"
			}
			if efforts := dm.ModelEfforts[m]; len(efforts) > 0 {
				desc += " [" + strings.Join(efforts, "|") + "]"
			}
			models = append(models, fmt.Sprintf("%s\t%s", m, desc))
			seen[m] = true
			for _, eff := range dm.ModelEfforts[m] {
				variant := m + "-" + eff
				if !seen[variant] {
					models = append(models, fmt.Sprintf("%s\t%s (%s effort)", variant, m, eff))
					seen[variant] = true
				}
			}
		}
	}

	// 2. Add aliases
	for _, a := range []string{
		"latest\tLatest Gemini Flash model (auto-detected)",
		"auto\tAuto model selection (latest Flash)",
		"hydrogen\tLatest Gemini 4 Hydrogen flagship model",
		"flash\tLatest Gemini Flash model",
		"pro\tLatest Gemini Pro / Flagship model",
	} {
		name := strings.Split(a, "\t")[0]
		if !seen[name] {
			models = append(models, a)
			seen[name] = true
		}
	}

	// 3. Fallback known models if cache was empty or incomplete
	knownDefaults := []string{
		"gemini-4-hydrogen\tGemini 4 Hydrogen (Low|Medium|High|XHigh)",
		"gemini-4-hydrogen-xhigh\tGemini 4 Hydrogen (XHigh Reasoning)",
		"gemini-4-hydrogen-high\tGemini 4 Hydrogen (High Reasoning)",
		"gemini-4-carbon\tGemini 4 Carbon (High Performance)",
		"gemini-4-neon\tGemini 4 Neon (Low|Medium|High|XHigh)",
		"gemini-4-argon\tGemini 4 Argon (Fast Reasoning)",
		"gemini-3.8-flash\tGemini 3.8 Flash (Latest Fast & Capable Reasoning)",
		"gemini-3.7-flash\tGemini 3.7 Flash (Fast & Capable Reasoning)",
		"gemini-3.6-flash\tGemini 3.6 Flash (Fast & Capable)",
		"gemini-3.1-pro\tGemini 3.1 Pro (High Reasoning)",
		"claude-sonnet-4-6\tClaude Sonnet 4.6 (Thinking)",
		"claude-opus-4-6-thinking\tClaude Opus 4.6 (Thinking)",
		"gpt-oss-120b-medium\tGPT-OSS 120B (Medium)",
	}
	for _, kd := range knownDefaults {
		name := strings.Split(kd, "\t")[0]
		if !seen[name] {
			models = append(models, kd)
			seen[name] = true
		}
	}

	return models, cobra.ShellCompDirectiveNoFileComp
}

// CompleteEffortLevels returns completions for `--effort` values, scoped to model if provided.
func CompleteEffortLevels(model string) ([]string, cobra.ShellCompDirective) {
	descriptions := map[string]string{
		"low":    "Low reasoning effort (fastest, minimal thinking)",
		"medium": "Medium reasoning effort (balanced speed and depth)",
		"high":   "High reasoning effort (default deep reasoning)",
		"xhigh":  "Extra-high reasoning effort (Gemini 4 Hydrogen / Neon)",
		"max":    "Maximum reasoning effort (when supported by model)",
	}
	allLevels := []string{"low", "medium", "high", "xhigh", "max"}
	levels := allLevels
	if model != "" {
		if resolved := profile.ResolveModelAlias(model); resolved != "" {
			model = resolved
		}
		if supported := profile.GetSupportedEfforts(model); len(supported) > 0 {
			levels = supported
		}
	}
	var out []string
	for _, l := range levels {
		out = append(out, fmt.Sprintf("%s\t%s", l, descriptions[l]))
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

func findModelFlagInArgs(args []string) string {
	for i := 0; i < len(args); i++ {
		if (args[i] == "-m" || args[i] == "--model") && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(args[i], "--model=") {
			return strings.TrimPrefix(args[i], "--model=")
		}
		if strings.HasPrefix(args[i], "-m=") {
			return strings.TrimPrefix(args[i], "-m=")
		}
	}
	return ""
}

var agySubcommandVerbCompletions = map[string][]string{
	"plugin": {
		"list\tList installed plugins",
		"import\tImport plugins from gemini or claude",
		"install\tInstall a plugin (plugin[@marketplace])",
		"uninstall\tUninstall a plugin",
		"enable\tEnable a plugin",
		"disable\tDisable a plugin",
		"validate\tValidate a plugin manifest, archive, or URL",
		"link\tGenerate link to a marketplace",
	},
	"mcp": {
		"list\tList configured MCP servers",
		"add\tAdd or update an MCP server",
		"remove\tRemove an MCP server",
		"enable\tEnable an MCP server",
		"disable\tDisable an MCP server",
	},
	"remote-control": {
		"start\tRegister and start the remote-control daemon",
		"status\tShow the remote-control daemon status",
		"stop\tStop the daemon and unregister it",
	},
	"auth": {
		"login\tSign in interactively",
		"logout\tSign out and purge cached credentials",
		"status\tShow authentication status",
		"token\tPrint current auth token",
		"refresh\tRefresh OAuth access token",
	},
	"config": {
		"list\tList configuration settings",
		"get\tGet a configuration value",
		"set\tSet a configuration value",
		"path\tPrint settings.json path",
	},
}

var agyFlagValueCompletions = map[string][]string{
	"--mode": {
		"auto\tSandbox auto-execution mode (default on v1.5.7+)",
		"accept-edits\tAuto-approve file edits in workspace",
		"plan\tResearch and outline plan before editing",
	},
	"--output-format": {
		"text\tPlain text output",
		"json\tStructured JSON output",
		"stream-json\tNewline-delimited JSON event stream",
	},
	"--input-format": {
		"text\tPlain text prompt input",
		"stream-json\tNDJSON message stream on stdin",
	},
}

// CompleteAgyArgs returns completions for agy CLI flags, models, and subcommands.
func CompleteAgyArgs(args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if strings.HasPrefix(toComplete, "--model=") || strings.HasPrefix(toComplete, "-m=") {
		return CompleteModelNames()
	}
	if strings.HasPrefix(toComplete, "--effort=") {
		return CompleteEffortLevels(findModelFlagInArgs(args))
	}
	for flag, vals := range agyFlagValueCompletions {
		if strings.HasPrefix(toComplete, flag+"=") {
			return vals, cobra.ShellCompDirectiveNoFileComp
		}
	}

	if len(args) > 0 {
		prev := args[len(args)-1]
		if prev == "-m" || prev == "--model" {
			return CompleteModelNames()
		}
		if prev == "--effort" {
			return CompleteEffortLevels(findModelFlagInArgs(args))
		}
		if vals, ok := agyFlagValueCompletions[prev]; ok {
			return vals, cobra.ShellCompDirectiveNoFileComp
		}
	}

	if strings.HasPrefix(toComplete, "-") {
		flags := []string{
			"-c\tResume latest conversation session",
			"--continue\tResume latest conversation session",
			"-r\tResume conversation by ID (alias for --conversation)",
			"--resume=\tResume conversation by ID",
			"--conversation=\tResume specific conversation by ID",
			"-m\tSelect AI model",
			"--model=\tSelect AI model",
			"--effort=\tSelect reasoning effort (low|medium|high|xhigh|max)",
			"--mode=\tSet agent execution mode (auto|accept-edits|plan)",
			"--agent=\tSelect custom agent for the session",
			"--add-dir=\tAdd directory to workspace (repeatable)",
			"--project=\tSet project ID or project name",
			"--new-project\tCreate a new project for this session",
			"-i\tRun initial prompt interactively and continue",
			"--prompt-interactive=\tRun initial prompt interactively and continue",
			"-p\tExecute prompt non-interactively",
			"--print\tExecute prompt non-interactively",
			"--prompt=\tExecute prompt non-interactively",
			"--output-format=\tPrint mode output format (text|json|stream-json)",
			"--input-format=\tPrint mode input format (text|stream-json)",
			"--json-schema=\tEnforce structured JSON schema in print mode",
			"--print-timeout=\tOptional time limit for print mode",
			"--disable-slash-commands\tDisable slash commands and skills in print mode",
			"--settings=\tSession-only settings JSON object or file path",
			"--dangerously-skip-permissions\tAuto-approve all tool permission requests",
			"--remote-control\tCreate a remote connection on startup",
			"--remote-agent\tRun conversations using remote agents",
			"--sandbox\tExecute in sandbox environment",
			"--log-file=\tOverride CLI log file path",
			"-h\tShow help",
			"--help\tShow help",
			"-v\tShow version",
			"--version\tShow version",
		}
		return flags, cobra.ShellCompDirectiveNoFileComp
	}

	if posIdx := findFirstPositionalIndex(args); posIdx >= 0 && isAgySubcommand(args[posIdx]) {
		if posIdx == len(args)-1 {
			sub := args[posIdx]
			if sub == "plugins" {
				sub = "plugin"
			}
			if verbs, ok := agySubcommandVerbCompletions[sub]; ok {
				return verbs, cobra.ShellCompDirectiveNoFileComp
			}
		}
		return nil, cobra.ShellCompDirectiveDefault
	}

	subcmds := []string{
		"acp\tStart Agent Client Protocol (ACP) stdio server",
		"agent\tList available agents",
		"agents\tList active subagents",
		"auth\tManage account authentication",
		"changelog\tView recent release notes",
		"config\tView or edit configuration settings",
		"help\tShow help for subcommands",
		"install\tConfigure environment paths and shell settings",
		"mcp\tManage MCP servers (add, remove, list, enable, disable)",
		"mic-serve\tServe this machine's microphone to a CLI on another host",
		"models\tList available AI models",
		"plugin\tManage plugins (list, install, import, validate, enable, disable)",
		"plugins\tAlias for plugin",
		"remote-control\tManage the remote-control background daemon",
		"update\tUpdate CLI",
	}
	return subcmds, cobra.ShellCompDirectiveNoFileComp
}

// CompleteRunArgs completes profile names for 1st arg, then agy parameters for subsequent args.
func CompleteRunArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return CompleteProfileNames(cmd, args, toComplete)
	}
	return CompleteAgyArgs(args, toComplete)
}

// CompleteSSHArgs completes SSH target / remote path / profile name and agy params.
func CompleteSSHArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	if len(args) == 1 {
		if strings.HasPrefix(toComplete, "/") || strings.HasPrefix(toComplete, "~") || strings.HasPrefix(toComplete, ".") {
			return nil, cobra.ShellCompDirectiveFilterDirs
		}
		profiles, _ := CompleteProfileNames(cmd, nil, toComplete)
		return profiles, cobra.ShellCompDirectiveDefault
	}
	return CompleteAgyArgs(args[1:], toComplete)
}

// CompletePriorityArgs handles subcommands and profile completion for agys priority.
func CompletePriorityArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		actions := []string{
			"set\tSet priority value for a profile",
			"get\tGet priority value for a profile",
			"list\tList all profile priorities",
		}
		return actions, cobra.ShellCompDirectiveNoFileComp
	}
	if len(args) == 1 && (args[0] == "set" || args[0] == "get") {
		return CompleteProfileNames(cmd, nil, toComplete)
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// CompleteImportArgs handles file completion for 1st arg.
func CompleteImportArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return nil, cobra.ShellCompDirectiveDefault
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// CompleteResumeArgs handles positional args for resume command and agy flags after '--'.
func CompleteResumeArgs(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if strings.HasPrefix(toComplete, "-") {
		return CompleteAgyArgs(args, toComplete)
	}
	if len(args) == 0 {
		profiles, _ := profile.List()
		var results []string
		for _, p := range profiles {
			results = append(results, p+"\tFilter sessions by profile")
		}
		return results, cobra.ShellCompDirectiveNoFileComp
	}
	return CompleteAgyArgs(args, toComplete)
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for agys.

To load completions:

Bash:
  $ source <(agys completion bash)
  # To load completions for each session, execute once:
  # Linux:
  $ agys completion bash > /etc/bash_completion.d/agys
  # macOS:
  $ agys completion bash > $(brew --prefix)/etc/bash_completion.d/agys

Zsh:
  # If shell completion is not already enabled in your environment,
  # add the following to your ~/.zshrc:
  autoload -U compinit; compinit

  # To load completions for each session:
  $ agys completion zsh > "${fpath[1]}/_agys"

Fish:
  $ agys completion fish | source
  # To load completions for each session:
  $ agys completion fish > ~/.config/fish/completions/agys.fish

PowerShell:
  PS> agys completion powershell | Out-String | Invoke-Expression
`,
	ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
	Args:      cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return rootCmd.GenBashCompletionV2(os.Stdout, true)
		case "zsh":
			return rootCmd.GenZshCompletion(os.Stdout)
		case "fish":
			return rootCmd.GenFishCompletion(os.Stdout, true)
		case "powershell":
			return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
		default:
			return nil
		}
	},
}

func init() {
	rootCmd.AddCommand(completionCmd)
}

