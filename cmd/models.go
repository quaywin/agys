package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var refreshModelsFlag bool

var modelsCmd = &cobra.Command{
	Use:   "models",
	Short: "List and auto-detect available AI models from agy",
	Long:  "Query agy models, detect the highest Flash, Hydrogen flagship, and Pro models along with supported reasoning efforts, and display the cached discovery state.",
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
		fmt.Fprintf(out, "  • Flash (Default):     \033[1;32m%s\033[0m\n", dm.LatestFlash)
		if dm.LatestHydrogen != "" {
			fmt.Fprintf(out, "  • Flagship (Hydrogen): \033[1;35m%s\033[0m\n", dm.LatestHydrogen)
		}
		if dm.LatestPro != "" {
			fmt.Fprintf(out, "  • Pro:                 \033[1;36m%s\033[0m\n", dm.LatestPro)
		}
		fmt.Fprintf(out, "  • Cache Updated:       %s\n\n", dm.FetchedAt.Format(time.RFC3339))

		if len(dm.AllModels) > 0 {
			fmt.Fprintln(out, "Available Base Models:")
			for _, m := range dm.AllModels {
				tag := ""
				if m == dm.LatestFlash {
					tag = " \033[1;32m(Highest Flash / Default)\033[0m"
				} else if m == dm.LatestHydrogen && dm.LatestHydrogen != "" {
					tag = " \033[1;35m(Highest Flagship / Hydrogen)\033[0m"
				} else if m == dm.LatestPro && dm.LatestPro != "" {
					tag = " \033[1;36m(Highest Pro)\033[0m"
				}
				effortsStr := ""
				if efforts := dm.ModelEfforts[m]; len(efforts) > 0 {
					effortsStr = fmt.Sprintf(" \033[90m[%s]\033[0m", strings.Join(efforts, ", "))
				}
				if effortsStr == "" && tag == "" {
					fmt.Fprintf(out, "  ● %s\n", m)
				} else {
					fmt.Fprintf(out, "  ● %-24s%s%s\n", m, effortsStr, tag)
				}
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
