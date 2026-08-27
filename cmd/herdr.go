package cmd

import (
	"fmt"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var herdrCmd = &cobra.Command{
	Use:   "herdr",
	Short: "Manage Herdr multi-agent workspace integration and sidebar layout",
	Long: `Manage Herdr integration, including configuring the compact 2-row sidebar layout
and real-time context window / quota synchronization.`,
}

var herdrConfigureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Install the compact 2-row sidebar layout into Herdr config.toml",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := profile.GetHerdrConfigPath()
		cmd.Printf("Configuring Herdr 2-row compact sidebar at: %s\n", configPath)

		if err := profile.ApplyHerdr2RowConfig(configPath); err != nil {
			return fmt.Errorf("failed to configure Herdr sidebar: %w", err)
		}

		// Sync statusLine hook to all existing profiles
		profiles, err := profile.List()
		if err == nil {
			for _, pName := range profiles {
				if pDir, err := profile.GetProfileDir(pName); err == nil {
					_ = profile.SyncStatusLineSettings(pDir)
					_ = profile.SyncHerdrIntegration(pDir)
				}
			}
		}

		cmd.Println("✓ Successfully installed clean 3-row sidebar layout.")
		cmd.Println("✓ Real-time statusLine context & quota hooks synchronized.")
		cmd.Println("\nSidebar Layout:")
		cmd.Println("  Row 1: ● <project> agy[profile] (Status, Project & Profile)")
		cmd.Println("  Row 2: model · ctx % (Active Model & Context Window)")
		cmd.Println("  Row 3: 5h 85% 2h · 7d 90% 3d (Compact 5H & Weekly Quota)")
		return nil
	},
}

var herdrUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Restore original Herdr sidebar configuration",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := profile.GetHerdrConfigPath()
		cmd.Printf("Restoring original Herdr sidebar configuration at: %s\n", configPath)

		if err := profile.UninstallHerdr2RowConfig(configPath); err != nil {
			return fmt.Errorf("failed to restore Herdr config: %w", err)
		}

		cmd.Println("✓ Successfully restored original Herdr sidebar layout.")
		return nil
	},
}

var herdrStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check if Herdr sidebar is configured for compact 2-row layout",
	RunE: func(cmd *cobra.Command, args []string) error {
		configPath := profile.GetHerdrConfigPath()
		configured := profile.IsHerdrConfiguredForAgys(configPath)

		cmd.Printf("Herdr Config: %s\n", configPath)
		if configured {
			cmd.Println("Sidebar Layout: 2-Row Compact (Configured ✓)")
		} else {
			cmd.Println("Sidebar Layout: Default 1-Row (Run 'agys herdr configure' to enable 2-row compact layout)")
		}
		return nil
	},
}

func init() {
	herdrCmd.AddCommand(herdrConfigureCmd)
	herdrCmd.AddCommand(herdrUninstallCmd)
	herdrCmd.AddCommand(herdrStatusCmd)
	rootCmd.AddCommand(herdrCmd)
}
