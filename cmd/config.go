package cmd

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/quaywin/agys/pkg/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:     "config [subcommand]",
	Short:   "Manage agys global configuration settings",
	Aliases: []string{"cfg"},
	RunE: func(cmd *cobra.Command, args []string) error {
		return showConfig()
	},
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all configuration settings",
	RunE: func(cmd *cobra.Command, args []string) error {
		return showConfig()
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get the value of a configuration setting",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		key := strings.ToLower(strings.TrimSpace(args[0]))
		switch key {
		case "auto_failover", "autofailover", "failover":
			fmt.Println(cfg.AutoFailover)
		case "max_retries", "maxretries":
			fmt.Println(cfg.MaxRetries)
		default:
			return fmt.Errorf("unknown config key %q. Valid keys: auto_failover, max_retries", key)
		}
		return nil
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a configuration setting",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.Load()
		if err != nil {
			return err
		}

		key := strings.ToLower(strings.TrimSpace(args[0]))
		valStr := strings.TrimSpace(args[1])

		switch key {
		case "auto_failover", "autofailover", "failover":
			val, err := strconv.ParseBool(valStr)
			if err != nil {
				return fmt.Errorf("invalid boolean value %q (use true/false or 1/0)", valStr)
			}
			cfg.AutoFailover = val
			fmt.Printf("Updated config auto_failover = %v\n", val)
		case "max_retries", "maxretries":
			val, err := strconv.Atoi(valStr)
			if err != nil || val <= 0 {
				return fmt.Errorf("invalid integer value %q (must be > 0)", valStr)
			}
			cfg.MaxRetries = val
			fmt.Printf("Updated config max_retries = %d\n", val)
		default:
			return fmt.Errorf("unknown config key %q. Valid keys: auto_failover, max_retries", key)
		}

		return config.Save(cfg)
	},
}

func showConfig() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	cfgPath, _ := config.GetConfigPath()
	fmt.Printf("agys Configuration (%s):\n\n", cfgPath)
	fmt.Printf("  auto_failover : %v\n", cfg.AutoFailover)
	fmt.Printf("  max_retries   : %d\n", cfg.MaxRetries)
	return nil
}

func init() {
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configSetCmd)
	rootCmd.AddCommand(configCmd)
}
