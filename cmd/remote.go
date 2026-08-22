package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

var (
	remotePort            int
	remoteName            string
	remoteForeground      bool
	remoteSkipPermissions bool
	remoteForce           bool
	remoteStopAll         bool
	remoteFollowLogs      bool
	remoteLogLines        int
)

var remoteCmd = &cobra.Command{
	Use:     "remote [profile_name]",
	Aliases: []string{"rc"},
	Short:   "Manage background Antigravity Remote Control daemons",
	Long: `Start, stop, monitor, and view logs for Antigravity Remote Control daemons across profiles.

By default, running 'agys remote [profile_name]' starts a detached background daemon that keeps
Antigravity active and accessible from both http://localhost:PORT and https://antigravity.google.`,
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var profileName string
		if len(args) > 0 {
			profileName = args[0]
		}
		return runRemoteStart(cmd, profileName)
	},
}

var remoteStartCmd = &cobra.Command{
	Use:               "start [profile_name]",
	Short:             "Start Remote Control daemon in background",
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var profileName string
		if len(args) > 0 {
			profileName = args[0]
		}
		return runRemoteStart(cmd, profileName)
	},
}

func runRemoteStart(cmd *cobra.Command, profileName string) error {
	if profileName == "" {
		current, err := profile.GetCurrent()
		if err != nil {
			return err
		}
		if current != "" {
			profileName = current
		} else {
			return fmt.Errorf("no profile specified and no default profile set. Specify a profile or set one with `agys use <profile_name>`")
		}
	}

	targetProfile := profileName
	if !profile.IsAuto(profileName) {
		exists, _, err := profile.Exists(profileName)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("profile %q does not exist. Use `agys add %s` to create it", profileName, profileName)
		}
	}

	if remoteForce && !profile.IsAuto(targetProfile) {
		_, _ = profile.StopRemoteDaemon(targetProfile)
	}

	extraArgs := EnsureDefaultModelAndEffort(nil)
	info, err := profile.StartRemoteDaemon(cmd.Context(), targetProfile, remotePort, remoteName, remoteSkipPermissions, remoteForeground, extraArgs)
	if err != nil {
		return err
	}

	if remoteForeground {
		return nil
	}

	fmt.Printf("\n[agys] Remote Control daemon started for profile %q (PID: %d)\n", info.Profile, info.PID)
	fmt.Printf("  ➜ Local Web UI:    http://localhost:%d\n", info.Port)
	fmt.Printf("  ➜ Cloud Portal:    https://antigravity.google\n")
	if info.Name != "" {
		fmt.Printf("  ➜ Instance Name:   %s\n", info.Name)
	}
	fmt.Printf("  ➜ View logs:       agys remote logs %s -f\n", info.Profile)
	fmt.Printf("  ➜ Stop daemon:     agys remote stop %s\n\n", info.Profile)

	return nil
}

var remoteStopCmd = &cobra.Command{
	Use:               "stop [profile_name]",
	Aliases:           []string{"down", "kill"},
	Short:             "Stop running Remote Control daemon",
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if remoteStopAll {
			daemons, err := profile.ListRunningRemoteDaemons()
			if err != nil {
				return err
			}
			if len(daemons) == 0 {
				fmt.Println("No active remote control daemons running.")
				return nil
			}
			for _, d := range daemons {
				_, stopErr := profile.StopRemoteDaemon(d.Profile)
				if stopErr != nil {
					fmt.Fprintf(os.Stderr, "Failed to stop daemon for %q: %v\n", d.Profile, stopErr)
				} else {
					fmt.Printf("[agys] Stopped Remote Control daemon for profile %q (PID: %d, Port: %d)\n", d.Profile, d.PID, d.Port)
				}
			}
			return nil
		}

		var profileName string
		if len(args) > 0 {
			profileName = args[0]
		} else {
			current, err := profile.GetCurrent()
			if err != nil {
				return err
			}
			if current != "" {
				profileName = current
			} else {
				return fmt.Errorf("no profile specified. Specify a profile or use --all to stop all daemons")
			}
		}

		info, err := profile.StopRemoteDaemon(profileName)
		if err != nil {
			return err
		}

		fmt.Printf("[agys] Stopped Remote Control daemon for profile %q (PID: %d, Port: %d)\n", info.Profile, info.PID, info.Port)
		return nil
	},
}

var remoteStatusCmd = &cobra.Command{
	Use:     "status",
	Aliases: []string{"ls", "ps", "list"},
	Short:   "List active Remote Control daemons",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		daemons, err := profile.ListRunningRemoteDaemons()
		if err != nil {
			return err
		}

		if len(daemons) == 0 {
			fmt.Println("No active remote control daemons running.")
			fmt.Println("Start one with: agys remote start <profile_name>")
			return nil
		}

		fmt.Printf("%-20s %-8s %-7s %-6s %-25s %-10s %s\n", "PROFILE", "STATUS", "PID", "PORT", "LOCAL URL", "UPTIME", "INSTANCE NAME")
		for _, d := range daemons {
			uptime := time.Since(d.StartedAt).Round(time.Second).String()
			localURL := fmt.Sprintf("http://localhost:%d", d.Port)
			instName := d.Name
			if instName == "" {
				instName = "-"
			}
			fmt.Printf("%-20s %-8s %-7d %-6d %-25s %-10s %s\n", d.Profile, "Running", d.PID, d.Port, localURL, uptime, instName)
		}
		return nil
	},
}

var remoteLogsCmd = &cobra.Command{
	Use:               "logs [profile_name]",
	Aliases:           []string{"log"},
	Short:             "Display logs from Remote Control daemon",
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var profileName string
		if len(args) > 0 {
			profileName = args[0]
		} else {
			current, err := profile.GetCurrent()
			if err != nil {
				return err
			}
			if current != "" {
				profileName = current
			} else {
				return fmt.Errorf("no profile specified. Specify a profile or set default with `agys use <profile>`")
			}
		}

		exists, profileDir, err := profile.Exists(profileName)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("profile %q does not exist", profileName)
		}

		logPath := profile.GetDaemonLogPath(profileDir)
		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			return fmt.Errorf("no log file found for profile %q at %s", profileName, logPath)
		}

		if remoteFollowLogs {
			// Stream log using tail -f if available
			tailCmd := exec.Command("tail", "-n", fmt.Sprintf("%d", remoteLogLines), "-f", logPath)
			tailCmd.Stdout = os.Stdout
			tailCmd.Stderr = os.Stderr
			return tailCmd.Run()
		}

		// Read and display last N lines
		file, err := os.Open(logPath)
		if err != nil {
			return err
		}
		defer file.Close()

		var lines []string
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		if err := scanner.Err(); err != nil && err != io.EOF {
			return err
		}

		start := 0
		if len(lines) > remoteLogLines {
			start = len(lines) - remoteLogLines
		}

		fmt.Printf("--- Log output for profile %q (%s) ---\n", profileName, logPath)
		for _, line := range lines[start:] {
			fmt.Println(line)
		}
		return nil
	},
}

var remoteRestartCmd = &cobra.Command{
	Use:               "restart [profile_name]",
	Short:             "Restart Remote Control daemon",
	ValidArgsFunction: CompleteProfileNames,
	Args:              cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		var profileName string
		if len(args) > 0 {
			profileName = args[0]
		} else {
			current, err := profile.GetCurrent()
			if err != nil {
				return err
			}
			if current != "" {
				profileName = current
			} else {
				return fmt.Errorf("no profile specified. Specify a profile or set default with `agys use <profile>`")
			}
		}

		_, _ = profile.StopRemoteDaemon(profileName)
		return runRemoteStart(cmd, profileName)
	},
}

func init() {
	// Flags for root remote / remote start
	for _, c := range []*cobra.Command{remoteCmd, remoteStartCmd} {
		c.Flags().IntVarP(&remotePort, "port", "p", 0, "Specific hub port to bind (default: auto-allocate 4400 or next free port)")
		c.Flags().StringVarP(&remoteName, "name", "n", "", "Custom instance nickname shown on Google Cloud Remote Control")
		c.Flags().BoolVarP(&remoteForeground, "foreground", "f", false, "Run in foreground instead of detached background daemon")
		c.Flags().BoolVarP(&remoteSkipPermissions, "dangerously-skip-permissions", "y", true, "Auto-approve all tool permission requests without prompting")
		c.Flags().BoolVar(&remoteForce, "force", false, "Force restart if daemon is already running")
	}

	// Flags for remote stop
	remoteStopCmd.Flags().BoolVarP(&remoteStopAll, "all", "a", false, "Stop all active remote control daemons")

	// Flags for remote logs
	remoteLogsCmd.Flags().BoolVarP(&remoteFollowLogs, "follow", "f", false, "Follow log output (tail -f)")
	remoteLogsCmd.Flags().IntVarP(&remoteLogLines, "lines", "n", 30, "Number of log lines to show")

	// Add subcommands to remoteCmd
	remoteCmd.AddCommand(remoteStartCmd)
	remoteCmd.AddCommand(remoteStopCmd)
	remoteCmd.AddCommand(remoteStatusCmd)
	remoteCmd.AddCommand(remoteLogsCmd)
	remoteCmd.AddCommand(remoteRestartCmd)

	rootCmd.AddCommand(remoteCmd)
}
