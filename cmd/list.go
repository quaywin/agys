package cmd

import (
	"context"
	"fmt"
	"os"
	"sync"
	"text/tabwriter"
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

		duplicates, _ := profile.DetectDuplicateTokens()

		if !listQuota {
			fmt.Println("Active Profiles:")
			tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(tw, "PROFILE\tPRIO\tEMAIL\tPATH")
			for _, p := range profiles {
				dir, _ := profile.GetProfileDir(p)
				pName := p
				if p == currentProfile {
					pName += " (default)"
				}
				prio := priorities[p]
				email, _ := profile.GetCachedEmail(p)
				if email == "" {
					email = "-"
				}
				if dupList, isDup := duplicates[p]; isDup {
					email += fmt.Sprintf(" [!] DUPLICATE TOKEN (shared with %v)", dupList)
				}
				fmt.Fprintf(tw, "%s\t%d\t%s\t%s\n", pName, prio, email, dir)
			}
			tw.Flush()
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

		fmt.Println("Active Profiles & Quota Status:")
		profile.RenderQuotaTable(os.Stdout, results, currentProfile, priorities)
		return nil
	},
}

func init() {
	listCmd.Flags().BoolVarP(&listQuota, "quota", "q", false, "Show quota summary for each profile")
	rootCmd.AddCommand(listCmd)
}
