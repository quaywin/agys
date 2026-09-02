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

// CompleteAgyArgs returns completions for agy CLI flags, models, and subcommands.
func CompleteAgyArgs(args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		prev := args[len(args)-1]
		if prev == "-m" || prev == "--model" || strings.HasPrefix(toComplete, "--model=") {
			var models []string
			seen := make(map[string]bool)

			// 1. Dynamic discovered models from cache if available
			if dm := profile.ReadCachedDiscoveredModels(); dm != nil && len(dm.AllModels) > 0 {
				for _, m := range dm.AllModels {
					desc := "AI Model"
					if m == dm.LatestFlash {
						desc = "Gemini Flash (Highest / Default)"
					} else if m == dm.LatestPro {
						desc = "Gemini Pro (Highest)"
					}
					models = append(models, fmt.Sprintf("%s\t%s", m, desc))
					seen[m] = true
				}
			}

			// 2. Add aliases
			for _, a := range []string{
				"latest\tLatest Gemini Flash model (auto-detected)",
				"auto\tAuto model selection",
			} {
				name := strings.Split(a, "\t")[0]
				if !seen[name] {
					models = append(models, a)
					seen[name] = true
				}
			}

			// 3. Fallback known models if cache was empty
			knownDefaults := []string{
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
	}

	if strings.HasPrefix(toComplete, "-") {
		flags := []string{
			"-c\tResume latest conversation session",
			"--continue\tResume latest conversation session",
			"--conversation=\tResume specific conversation by ID",
			"-m\tSelect AI model",
			"--model=\tSelect AI model",
			"-p\tExecute prompt non-interactively",
			"--prompt=\tExecute prompt non-interactively",
			"--dangerously-skip-permissions\tAuto-approve all tool permission requests",
			"--sandbox\tExecute in sandbox environment",
			"--workspace=\tSet workspace directory",
			"--verbose\tEnable verbose output",
			"-h\tShow help",
			"--help\tShow help",
			"-v\tShow version",
			"--version\tShow version",
		}
		return flags, cobra.ShellCompDirectiveNoFileComp
	}

	subcmds := []string{
		"models\tList available AI models",
		"agents\tList active subagents",
		"changelog\tView recent release notes",
		"auth\tManage account authentication",
		"config\tView or edit configuration settings",
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

