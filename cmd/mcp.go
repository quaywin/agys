package cmd

import (
	"fmt"
	"strings"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	mcpListAll bool
	mcpSyncAll bool
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage and synchronize MCP (Model Context Protocol) servers across profiles",
	Long:  `View configured MCP servers and synchronize mcp_config.json configurations between Antigravity profiles.`,
}

var mcpListCmd = &cobra.Command{
	Use:               "list [profile_name]",
	Aliases:           []string{"ls"},
	Short:             "List configured MCP servers in a profile or across all profiles",
	ValidArgsFunction: CompleteProfileNames,
	RunE: func(cmd *cobra.Command, args []string) error {
		if mcpListAll {
			profiles, err := profile.List()
			if err != nil {
				return err
			}
			if len(profiles) == 0 {
				fmt.Println("No profiles configured.")
				return nil
			}
			for _, p := range profiles {
				servers, err := profile.ReadMcpServers(p)
				if err != nil {
					fmt.Printf("● Profile %q: Error reading MCP servers: %v\n", p, err)
					continue
				}
				printProfileServers(p, servers)
			}
			return nil
		}

		targetProfile := ""
		if len(args) > 0 {
			targetProfile = args[0]
		}
		if targetProfile == "" || profile.IsAuto(targetProfile) {
			current, err := profile.GetCurrent()
			if err != nil {
				return err
			}
			if profile.IsAuto(current) || targetProfile == "auto" {
				best, _, err := profile.SelectBestProfile(cmd.Context())
				if err != nil {
					return err
				}
				targetProfile = best
			} else if current != "" {
				targetProfile = current
			} else {
				return fmt.Errorf("no profile specified and no default profile set")
			}
		}

		servers, err := profile.ReadMcpServers(targetProfile)
		if err != nil {
			return err
		}
		printProfileServers(targetProfile, servers)
		return nil
	},
}

var mcpSyncCmd = &cobra.Command{
	Use:               "sync <src_profile> [target_profile]",
	Short:             "Synchronize mcp_config.json from a source profile to target profile or all profiles",
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		srcProfile := args[0]
		if profile.IsAuto(srcProfile) {
			best, _, err := profile.SelectBestProfile(cmd.Context())
			if err != nil {
				return err
			}
			srcProfile = best
		}

		if mcpSyncAll || len(args) == 1 {
			synced, err := profile.SyncMcpConfigToAll(srcProfile)
			if err != nil {
				return fmt.Errorf("failed to sync MCP config to all profiles: %w", err)
			}
			if len(synced) == 0 {
				fmt.Printf("[agys] No other profiles to synchronize from %q.\n", srcProfile)
				return nil
			}
			fmt.Printf("[agys] Successfully synchronized MCP config from %q to %d profile(s): %s\n",
				srcProfile, len(synced), strings.Join(synced, ", "))
			return nil
		}

		targetProfile := args[1]
		if err := profile.SyncMcpConfig(srcProfile, targetProfile); err != nil {
			return fmt.Errorf("failed to sync MCP config from %q to %q: %w", srcProfile, targetProfile, err)
		}

		fmt.Printf("[agys] Successfully synchronized MCP config from %q to %q.\n", srcProfile, targetProfile)
		return nil
	},
}

func printProfileServers(profileName string, servers []profile.McpServerEntry) {
	fmt.Printf("\n\033[1;34m● Profile %q\033[0m (%d MCP server(s))\n", profileName, len(servers))
	if len(servers) == 0 {
		fmt.Printf("  \033[90m(No MCP servers configured in %s)\033[0m\n", profile.GetMcpConfigPath(filepathProfilePlaceholder(profileName)))
		return
	}
	for _, s := range servers {
		argsStr := strings.Join(s.Args, " ")
		if argsStr != "" {
			argsStr = " " + argsStr
		}
		fmt.Printf("  \033[1;32m✓\033[0m \033[1;37m%-20s\033[0m \033[90m➜ %s%s\033[0m\n", s.Name, s.Command, argsStr)
	}
}

func filepathProfilePlaceholder(profileName string) string {
	dir, err := profile.GetProfileDir(profileName)
	if err != nil {
		return profileName
	}
	return dir
}

func init() {
	mcpListCmd.Flags().BoolVarP(&mcpListAll, "all", "a", false, "List MCP servers across all configured profiles")
	mcpSyncCmd.Flags().BoolVarP(&mcpSyncAll, "all", "a", false, "Synchronize MCP config to all other configured profiles")

	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpSyncCmd)
	rootCmd.AddCommand(mcpCmd)
}
