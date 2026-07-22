package cmd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	listQuota bool
)

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List all active profile directories",
	RunE: func(cmd *cobra.Command, args []string) error {
		profiles, err := profile.List()
		if err != nil {
			return err
		}

		if len(profiles) == 0 {
			baseDir, _ := profile.GetBaseDir()
			fmt.Printf("No profiles found in %s\nUse `agys add <profile_name>` to create one.\n", baseDir)
			return nil
		}

		currentProfile, _ := profile.GetCurrent()
		priorities, _ := profile.GetAllPriorities()

		if !listQuota {
			fmt.Println("Active Profiles:")
			for _, p := range profiles {
				dir, _ := profile.GetProfileDir(p)
				defaultBadge := ""
				if p == currentProfile {
					defaultBadge = " (default)"
				}
				prio := priorities[p]
				email, _ := profile.GetCachedEmail(p)
				emailBadge := ""
				if email != "" {
					emailBadge = fmt.Sprintf(" (%s)", email)
				}
				fmt.Printf("  - %s%s%s [prio: %d] (%s)\n", p, emailBadge, defaultBadge, prio, dir)
			}
			return nil
		}

		// Query quotas in parallel if listQuota is true
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		var wg sync.WaitGroup
		results := make([]profile.ProfileQuotaInfo, len(profiles))

		for i, pName := range profiles {
			wg.Add(1)
			go func(index int, name string) {
				defer wg.Done()
				email, _ := profile.FetchProfileEmail(ctx, name)
				summary, err := profile.FetchQuota(ctx, name)
				if err != nil {
					results[index] = profile.ProfileQuotaInfo{
						ProfileName: name,
						Email:       email,
						Active:      false,
						Error:       err.Error(),
					}
				} else {
					results[index] = profile.ProfileQuotaInfo{
						ProfileName: name,
						Email:       email,
						Active:      true,
						Quota:       summary,
					}
				}
			}(i, pName)
		}

		wg.Wait()

		fmt.Println("Active Profiles:")
		for _, res := range results {
			dir, _ := profile.GetProfileDir(res.ProfileName)
			defaultBadge := ""
			if res.ProfileName == currentProfile {
				defaultBadge = " (default)"
			}
			prio := priorities[res.ProfileName]
			emailBadge := ""
			if res.Email != "" {
				emailBadge = fmt.Sprintf(" (%s)", res.Email)
			}
			fmt.Printf("  - %s%s%s [prio: %d] (%s)\n", res.ProfileName, emailBadge, defaultBadge, prio, dir)
			if !res.Active {
				fmt.Printf("    └── [!] Error or not logged in: %s\n", res.Error)
				continue
			}

			if res.Quota == nil || len(res.Quota.Groups) == 0 {
				fmt.Println("    └── [!] No quota information available.")
				continue
			}

			for _, group := range res.Quota.Groups {
				var limit5h, limitWeekly string
				for _, bucket := range group.Buckets {
					pct := bucket.RemainingFraction * 100
					if bucket.Window == "5h" {
						limit5h = fmt.Sprintf("%.1f%%", pct)
					} else if bucket.Window == "weekly" {
						limitWeekly = fmt.Sprintf("%.1f%%", pct)
					}
				}

				if limit5h == "" {
					limit5h = "N/A"
				}
				if limitWeekly == "" {
					limitWeekly = "N/A"
				}

				fmt.Printf("    ├── %s: %s (5h) / %s (weekly)\n", group.DisplayName, limit5h, limitWeekly)
			}
		}

		return nil
	},
}

func init() {
	listCmd.Flags().BoolVarP(&listQuota, "quota", "q", false, "Show quota summary for each profile")
	rootCmd.AddCommand(listCmd)
}
