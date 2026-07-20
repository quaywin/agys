package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/quaywin/agys/pkg/updater"
	"github.com/quaywin/agys/pkg/version"
	"github.com/spf13/cobra"
)

var (
	upgradeCheckOnly bool
	upgradeForce     bool
)

var upgradeCmd = &cobra.Command{
	Use:     "upgrade",
	Aliases: []string{"update"},
	Short:   "Upgrade agys CLI to the latest version",
	Long:    `Check for available releases on GitHub and upgrade the local agys binary in-place.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		currentVer := version.Version
		fmt.Printf("Current version: v%s (%s/%s)\n", updater.CleanVersion(currentVer), runtime.GOOS, runtime.GOARCH)
		fmt.Println("Checking for latest release on GitHub...")

		rel, err := updater.FetchLatestRelease(updater.DefaultRepoOwner, updater.DefaultRepoName)
		if err != nil {
			return fmt.Errorf("failed to check for updates: %w", err)
		}

		latestVer := updater.CleanVersion(rel.TagName)
		isNewer := updater.IsNewer(currentVer, latestVer)

		if !isNewer && !upgradeForce {
			fmt.Printf("agys is already up to date (v%s).\n", updater.CleanVersion(currentVer))
			return nil
		}

		if isNewer {
			fmt.Printf("New release available: v%s (current: v%s)\n", latestVer, updater.CleanVersion(currentVer))
		} else if upgradeForce {
			fmt.Printf("Re-installing version v%s (--force specified)...\n", latestVer)
		}

		if upgradeCheckOnly {
			if isNewer {
				fmt.Println("Run `agys upgrade` to install the update.")
			}
			return nil
		}

		// Find target asset for current OS/Arch
		expectedArchive := updater.GetArchiveName(latestVer, runtime.GOOS, runtime.GOARCH)
		var downloadURL string

		for _, asset := range rel.Assets {
			if asset.Name == expectedArchive {
				downloadURL = asset.BrowserDownloadURL
				break
			}
		}

		// Fallback search if exact asset name differs slightly
		if downloadURL == "" {
			for _, asset := range rel.Assets {
				nameLower := strings.ToLower(asset.Name)
				if strings.Contains(nameLower, runtime.GOOS) && strings.Contains(nameLower, runtime.GOARCH) && strings.HasSuffix(nameLower, ".tar.gz") {
					downloadURL = asset.BrowserDownloadURL
					break
				}
			}
		}

		if downloadURL == "" {
			return fmt.Errorf("no release binary found for platform %s/%s in release %s", runtime.GOOS, runtime.GOARCH, rel.TagName)
		}

		fmt.Printf("Downloading binary from %s...\n", downloadURL)
		extractedBinary, err := updater.DownloadAndExtractBinary(downloadURL)
		if err != nil {
			return fmt.Errorf("failed to download and extract release: %w", err)
		}
		defer os.RemoveAll(filepath.Dir(extractedBinary))

		fmt.Println("Replacing existing binary...")
		if err := updater.InstallBinary(extractedBinary); err != nil {
			return err
		}

		fmt.Printf("Successfully upgraded agys to v%s!\n", latestVer)
		return nil
	},
}

func init() {
	upgradeCmd.Flags().BoolVar(&upgradeCheckOnly, "check", false, "Check if a new version is available without installing")
	upgradeCmd.Flags().BoolVarP(&upgradeForce, "force", "f", false, "Force re-installation of the latest version")
	rootCmd.AddCommand(upgradeCmd)
}
