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
	Use:               "quota [profile_name]",
	Aliases:           []string{"q"},
	Short:             "Check model quota and usage for profile(s)",
	Long:              `Retrieve and display remaining quota percentage and refresh windows for one or all profiles.`,
	ValidArgsFunction: CompleteProfileNames,
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

		// Output JSON if requested
		if jsonOutput {
			encoder := json.NewEncoder(os.Stdout)
			encoder.SetIndent("", "  ")
			return encoder.Encode(results)
		}

		currentProfile, _ := profile.GetCurrent()
		priorities, _ := profile.GetAllPriorities()

		// Print text output
		fmt.Println("Quota Status for Profiles:")
		profile.RenderQuotaTable(os.Stdout, results, currentProfile, priorities)
		return nil
	},
}

func init() {
	quotaCmd.Flags().BoolVarP(&jsonOutput, "json", "j", false, "Output results in JSON format")
	rootCmd.AddCommand(quotaCmd)
}
