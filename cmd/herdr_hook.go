package cmd

import (
	"os"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	herdrHookProfile    string
	herdrHookPane       string
	herdrHookConv       string
	herdrHookTranscript string
)

var herdrHookCmd = &cobra.Command{
	Use:    "herdr-hook [session|quota|summarize]",
	Short:  "Internal lifecycle hook handler for Herdr multi-agent integration",
	Hidden: true,
	Args:   cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		action := "session"
		if len(args) > 0 {
			action = args[0]
		}
		if action == "summarize" {
			return profile.HandleHerdrSummarize(cmd.Context(), herdrHookProfile, herdrHookPane, herdrHookConv, herdrHookTranscript)
		}
		return profile.HandleHerdrHook(cmd.Context(), action, os.Stdin)
	},
}

func init() {
	herdrHookCmd.Flags().StringVar(&herdrHookProfile, "profile", "", "Profile name for async summarizer")
	herdrHookCmd.Flags().StringVar(&herdrHookPane, "pane", "", "Herdr pane ID for async summarizer")
	herdrHookCmd.Flags().StringVar(&herdrHookConv, "conv", "", "Conversation ID for async summarizer")
	herdrHookCmd.Flags().StringVar(&herdrHookTranscript, "transcript", "", "Transcript path for async summarizer")
	rootCmd.AddCommand(herdrHookCmd)
}
