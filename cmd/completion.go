package cmd

import (
	"os"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

// CompleteProfileNames is a Cobra ValidArgsFunction that returns matching profile names for tab-completion.
func CompleteProfileNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}

	profiles, err := profile.List()
	if err != nil {
		return nil, cobra.ShellCompDirectiveError
	}

	return profiles, cobra.ShellCompDirectiveNoFileComp
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
