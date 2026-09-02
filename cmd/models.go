package cmd

import (
	"fmt"
	"time"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var refreshModelsFlag bool

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List and auto-detect available AI models from agy",
	Long:  "Query agy models, detect the highest Flash and Pro models, and display the cached discovery state.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		var dm *profile.DiscoveredModels
		var err error

		out := cmd.OutOrStdout()
		if refreshModelsFlag {
			fmt.Fprintln(out, "Refreshing available models from agy...")
			dm, err = profile.DiscoverLatestModels()
			if err != nil {
				return fmt.Errorf("failed to query models from agy: %w", err)
			}
		} else {
			dm = profile.GetOrRefreshModels()
		}

		if dm == nil {
			return fmt.Errorf("no model metadata available")
		}

		fmt.Fprintf(out, "Detected Highest Models:\n")
		fmt.Fprintf(out, "  • Flash (Default): \033[1;32m%s\033[0m\n", dm.LatestFlash)
		if dm.LatestPro != "" {
			fmt.Fprintf(out, "  • Pro:             \033[1;36m%s\033[0m\n", dm.LatestPro)
		}
		fmt.Fprintf(out, "  • Cache Updated:   %s\n\n", dm.FetchedAt.Format(time.RFC3339))

		if len(dm.AllModels) > 0 {
			fmt.Fprintln(out, "Available Base Models:")
			for _, m := range dm.AllModels {
				tag := ""
				if m == dm.LatestFlash {
					tag = " \033[1;32m(Highest Flash / Default)\033[0m"
				} else if m == dm.LatestPro {
					tag = " \033[1;36m(Highest Pro)\033[0m"
				}
				fmt.Fprintf(out, "  ● %-24s%s\n", m, tag)
			}
		}

		fmt.Fprintln(cmd.ErrOrStderr(), "\nTip: Run 'agys models -r' to force an immediate refresh when agy updates.")
		return nil
	},
}

func init() {
	modelsCmd.Flags().BoolVarP(&refreshModelsFlag, "refresh", "r", false, "Force an immediate refresh by querying agy models")
	rootCmd.AddCommand(modelsCmd)
}
