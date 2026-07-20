package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	jsonOutput bool
)

var quotaCmd = &cobra.Command{
	Use:     "quota [profile_name]",
	Aliases: []string{"q"},
	Short:   "Check model quota and usage for profile(s)",
	Long:    `Retrieve and display remaining quota percentage and refresh windows for one or all profiles.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		var targetProfiles []string

		if len(args) > 0 {
			profileName := args[0]
			exists, _, err := profile.Exists(profileName)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("profile %q does not exist", profileName)
			}
			targetProfiles = []string{profileName}
		} else {
			var err error
			targetProfiles, err = profile.List()
			if err != nil {
				return err
			}
			if len(targetProfiles) == 0 {
				baseDir, _ := profile.GetBaseDir()
				fmt.Printf("No profiles found in %s\nUse `agys add <profile_name>` to create one.\n", baseDir)
				return nil
			}
		}

		// Run queries in parallel
		var wg sync.WaitGroup
		results := make([]profile.ProfileQuotaInfo, len(targetProfiles))

		for i, pName := range targetProfiles {
			wg.Add(1)
			go func(index int, name string) {
				defer wg.Done()
				summary, err := profile.FetchQuota(ctx, name)
				if err != nil {
					results[index] = profile.ProfileQuotaInfo{
						ProfileName: name,
						Active:      false,
						Error:       err.Error(),
					}
				} else {
					results[index] = profile.ProfileQuotaInfo{
						ProfileName: name,
						Active:      true,
						Quota:       summary,
					}
				}
			}(i, pName)
		}

		wg.Wait()

		// Output JSON if requested
		if jsonOutput {
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(results)
		}

		// Print text output
		fmt.Println("Quota Status for Profiles:")
		fmt.Println("==================================================")
		for _, res := range results {
			fmt.Printf("\nProfile: %s\n", res.ProfileName)
			if !res.Active {
				fmt.Printf("  [!] Error or not logged in: %s\n", res.Error)
				continue
			}

			if res.Quota == nil || len(res.Quota.Groups) == 0 {
				fmt.Println("  [!] No quota information available.")
				continue
			}

			for _, group := range res.Quota.Groups {
				fmt.Printf("  ● %s (%s):\n", group.DisplayName, group.Description)
				for _, bucket := range group.Buckets {
					pct := bucket.RemainingFraction * 100
					bar := progressBar(bucket.RemainingFraction, 20)

					resetStr := ""
					if bucket.RemainingFraction < 1.0 {
						resetStr = formatResetTime(bucket.ResetTime)
						if resetStr != "" {
							resetStr = " (" + resetStr + ")"
						}
					}

					fmt.Printf("    ├── %s: [%s] %.1f%%%s\n", bucket.DisplayName, bar, pct, resetStr)
				}
				fmt.Println()
			}
		}

		return nil
	},
}

func progressBar(fraction float64, width int) string {
	if fraction < 0 {
		fraction = 0
	} else if fraction > 1 {
		fraction = 1
	}
	filled := int(fraction * float64(width))
	empty := width - filled

	bar := ""
	for i := 0; i < filled; i++ {
		bar += "█"
	}
	for i := 0; i < empty; i++ {
		bar += "░"
	}
	return bar
}

func formatResetTime(resetTime time.Time) string {
	if resetTime.IsZero() {
		return ""
	}
	duration := time.Until(resetTime)
	if duration <= 0 {
		return "refreshing soon"
	}

	days := int(duration.Hours()) / 24
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	if days > 0 {
		return fmt.Sprintf("refreshes in %d days %d hours", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("refreshes in %d hours %d minutes", hours, minutes)
	}
	return fmt.Sprintf("refreshes in %d minutes", minutes)
}

func init() {
	quotaCmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output results in JSON format")
	rootCmd.AddCommand(quotaCmd)
}
