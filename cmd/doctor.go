package cmd

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/quaywin/agys/pkg/updater"
	"github.com/quaywin/agys/pkg/version"
	"github.com/spf13/cobra"
)

var doctorCmd = &cobra.Command{
	Use:     "doctor",
	Aliases: []string{"health"},
	Short:   "Perform diagnostic health checks on agys, agy CLI, profiles, and Herdr integration",
	Long:    `Inspect system binaries, OAuth credentials, macOS Keychain integrity, model discovery, and Herdr hooks.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runDoctor(cmd.Context())
	},
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(ctx context.Context) error {
	fmt.Printf("\n\033[1;36m[agys]\033[0m \033[1;37mRunning Antigravity & Herdr Environment Health Check...\033[0m\n\n")

	issues := 0
	warnings := 0

	// 1. agys Binary Info
	fmt.Printf("\033[1;34m● agys Switcher\033[0m\n")
	fmt.Printf("  \033[1;32m✓\033[0m Version: v%s (%s/%s, %s)\n", updater.CleanVersion(version.Version), runtime.GOOS, runtime.GOARCH, runtime.Version())

	// 2. agy Binary Check
	fmt.Printf("\n\033[1;34m● Antigravity CLI (agy)\033[0m\n")
	agyPath, err := exec.LookPath("agy")
	if err != nil {
		// Fallback check common locations
		home, _ := profile.GetRealUserHome()
		candidates := []string{
			filepath.Join(home, ".local", "bin", "agy"),
			"/opt/homebrew/bin/agy",
			"/usr/local/bin/agy",
		}
		for _, c := range candidates {
			if _, statErr := os.Stat(c); statErr == nil {
				agyPath = c
				break
			}
		}
	}

	if agyPath == "" {
		fmt.Printf("  \033[1;31m✗\033[0m agy binary: Not found in PATH or standard locations (~/.local/bin, /opt/homebrew/bin)\n")
		issues++
	} else {
		fmt.Printf("  \033[1;32m✓\033[0m Binary found: %s\n", agyPath)
		verCmd := exec.CommandContext(ctx, agyPath, "--version")
		verOut, verErr := verCmd.Output()
		if verErr == nil {
			installedAgyVer := strings.TrimSpace(string(verOut))
			fmt.Printf("  \033[1;32m✓\033[0m Installed version: %s\n", installedAgyVer)

			// Check GitHub latest release
			rel, relErr := updater.FetchLatestRelease("google-antigravity", "antigravity-cli")
			if relErr == nil && rel != nil {
				latestVer := updater.CleanVersion(rel.TagName)
				cleanInstalled := updater.CleanVersion(installedAgyVer)
				if updater.IsNewer(cleanInstalled, latestVer) {
					fmt.Printf("  \033[1;33m!\033[0m New agy release available: v%s (run 'agy update' to upgrade)\n", latestVer)
					warnings++
				} else {
					fmt.Printf("  \033[1;32m✓\033[0m Status: Up to date with latest release (v%s)\n", latestVer)
				}
			}
		} else {
			fmt.Printf("  \033[1;33m!\033[0m Failed to execute 'agy --version': %v\n", verErr)
			warnings++
		}
	}

	// 3. Multi-Account Profiles Check
	profiles, err := profile.List()
	if err != nil {
		fmt.Printf("\n\033[1;31m✗ Failed to list profiles: %v\033[0m\n", err)
		issues++
	} else {
		currentProf, _ := profile.GetCurrent()
		fmt.Printf("\n\033[1;34m● Sandboxed Profiles (%d configured)\033[0m\n", len(profiles))

		if len(profiles) == 0 {
			fmt.Printf("  \033[1;33m!\033[0m No profiles found. Create one using 'agys add <name>'\n")
			warnings++
		}

		for _, p := range profiles {
			isDefault := (p == currentProf)
			suffix := ""
			if isDefault {
				suffix = " \033[1;36m(default)\033[0m"
			}

			pDir, _ := profile.GetProfileDir(p)
			token, tokenErr := profile.ReadToken(p)
			email, _ := profile.GetCachedEmail(p)

			if tokenErr != nil || token == nil {
				fmt.Printf("  \033[1;33m!\033[0m Profile '%s'%s: Not authenticated or token missing\n", p, suffix)
				fmt.Printf("    - Sandbox: %s\n", pDir)
				fmt.Printf("    - Tip: Run 'agys run %s -- auth login' to authenticate\n", p)
				warnings++
				continue
			}

			// Token status
			emailDisplay := email
			if emailDisplay == "" {
				emailDisplay = "(Email not cached yet)"
			}

			fmt.Printf("  \033[1;32m✓\033[0m Profile '%s'%s\n", p, suffix)
			fmt.Printf("    - Google Account: %s\n", emailDisplay)

			// Expiry check
			if !token.Token.Expiry.IsZero() {
				rem := time.Until(token.Token.Expiry)
				if rem > 0 {
					fmt.Printf("    - OAuth Token: Valid (expires in %s, auto-refresh armed)\n", formatRemainingTime(rem))
				} else {
					fmt.Printf("    - OAuth Token: Expired (auto-refresh armed via refresh_token)\n")
				}
			} else {
				fmt.Printf("    - OAuth Token: Present\n")
			}

			// Quota status
			if summary, ok := profile.GetCachedQuota(p, 24*time.Hour); ok && summary != nil {
				details, _ := profile.GetProfileFullQuotaDetailsForModel(ctx, p, "")
				if details != nil {
					pct5h := "N/A"
					if details.Fraction5H >= 0 {
						pct5h = fmt.Sprintf("%.1f%% (%s)", details.Fraction5H*100, details.ResetStr5H)
					}
					pctWk := "N/A"
					if details.FractionWeekly >= 0 {
						pctWk = fmt.Sprintf("%.1f%% (%s)", details.FractionWeekly*100, details.ResetStrWeekly)
					}
					fmt.Printf("    - Quota HUD: 5H: %s • Weekly: %s\n", pct5h, pctWk)
				}
			}

			// Keychain check on macOS
			if runtime.GOOS == "darwin" {
				profileKeychainsDir := filepath.Join(pDir, "Library", "Keychains")
				if info, lErr := os.Lstat(profileKeychainsDir); lErr == nil && (info.Mode()&os.ModeSymlink != 0) {
					fmt.Printf("    - macOS Keychain: Linked and isolated\n")
				}
			}
		}
	}

	// 4. Customizations & Model Discovery Check
	fmt.Printf("\n\033[1;34m● Model Catalog & Discovery\033[0m\n")
	dm := profile.GetOrRefreshModels()
	if dm != nil && (dm.LatestFlash != "" || dm.LatestPro != "") {
		fmt.Printf("  \033[1;32m✓\033[0m Discovered Flash: %s\n", dm.LatestFlash)
		fmt.Printf("  \033[1;32m✓\033[0m Discovered Pro:   %s\n", dm.LatestPro)
		fmt.Printf("  \033[1;32m✓\033[0m Cache timestamp:  %s\n", dm.FetchedAt.Format(time.RFC3339))
	} else {
		fmt.Printf("  \033[1;33m!\033[0m Model cache empty. Run 'agys models -r' to discover available models from agy.\n")
		warnings++
	}

	// 5. Herdr Multi-Agent Environment Check
	fmt.Printf("\n\033[1;34m● Herdr Integration\033[0m\n")
	if profile.IsInHerdrEnvironment() {
		paneID := os.Getenv("HERDR_PANE_ID")
		sockPath := os.Getenv("HERDR_SOCKET_PATH")
		fmt.Printf("  \033[1;32m✓\033[0m Running inside Herdr workspace\n")
		fmt.Printf("    - Active Pane ID: %s\n", paneID)
		if sockPath != "" {
			conn, netErr := net.DialTimeout("unix", sockPath, 500*time.Millisecond)
			if netErr == nil {
				_ = conn.Close()
				fmt.Printf("    - Socket RPC: Reachable (%s)\n", sockPath)
			} else {
				fmt.Printf("    \033[1;33m!\033[0m Socket RPC: Unreachable (%v)\n", netErr)
				warnings++
			}
		}
	} else {
		fmt.Printf("  - Environment: Standalone Terminal (not inside Herdr pane)\n")
	}

	// 6. Final Summary
	fmt.Println()
	if issues == 0 && warnings == 0 {
		fmt.Printf("\033[1;32m✓ All system checks passed with zero issues!\033[0m\n\n")
	} else if issues == 0 {
		fmt.Printf("\033[1;33m! Health check completed: 0 issues, %d warning(s).\033[0m\n\n", warnings)
	} else {
		fmt.Printf("\033[1;31m✗ Health check found %d issue(s) and %d warning(s).\033[0m\n\n", issues, warnings)
	}

	return nil
}

func formatRemainingTime(d time.Duration) string {
	if d <= 0 {
		return "0m"
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh%02dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

