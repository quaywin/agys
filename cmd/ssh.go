package cmd

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/quaywin/agys/pkg/profile"
	"github.com/spf13/cobra"
)

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n\r\"'\\$`<>|&;()") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}

func startLocalHTTPProxy() (int, func(), error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, func() {}, err
	}
	port := listener.Addr().(*net.TCPAddr).Port

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodConnect {
				destConn, err := net.DialTimeout("tcp", r.Host, 10*time.Second)
				if err != nil {
					http.Error(w, err.Error(), http.StatusServiceUnavailable)
					return
				}
				w.WriteHeader(http.StatusOK)
				hijacker, ok := w.(http.Hijacker)
				if !ok {
					http.Error(w, "Hijacking not supported", http.StatusInternalServerError)
					destConn.Close()
					return
				}
				clientConn, _, err := hijacker.Hijack()
				if err != nil {
					destConn.Close()
					return
				}
				go func() {
					_, _ = io.Copy(destConn, clientConn)
					destConn.Close()
				}()
				go func() {
					_, _ = io.Copy(clientConn, destConn)
					clientConn.Close()
				}()
			} else {
				req, err := http.NewRequest(r.Method, r.URL.String(), r.Body)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				req.Header = r.Header
				resp, err := http.DefaultClient.Do(req)
				if err != nil {
					http.Error(w, err.Error(), http.StatusBadGateway)
					return
				}
				defer resp.Body.Close()
				for k, vv := range resp.Header {
					for _, v := range vv {
						w.Header().Add(k, v)
					}
				}
				w.WriteHeader(resp.StatusCode)
				_, _ = io.Copy(w, resp.Body)
			}
		}),
	}

	go func() {
		_ = server.Serve(listener)
	}()

	cleanup := func() {
		_ = server.Close()
		_ = listener.Close()
	}

	return port, cleanup, nil
}

func syncProfileToRemote(ctx context.Context, server string, profileName string) error {
	localProfileDir, err := profile.GetProfileDir(profileName)
	if err != nil {
		return fmt.Errorf("failed to get local profile directory for %q: %w", profileName, err)
	}

	remoteCliDir := fmt.Sprintf("~/.agys/profiles/%s/.gemini/antigravity-cli", profileName)

	// Combine mkdir and settings.json existence check into 1 SSH call
	mkdirCmd := exec.CommandContext(ctx, "ssh", server,
		fmt.Sprintf("mkdir -p %s && test -f %s/settings.json", remoteCliDir, remoteCliDir))
	settingsExists := mkdirCmd.Run() == nil

	localCliDir := filepath.Join(localProfileDir, ".gemini", "antigravity-cli")

	// 1. Tokens & Auth State: Always sync to keep token and identity fresh
	alwaysSyncFiles := []string{
		"antigravity-oauth-token",
		"jetski-standalone-oauth-token",
		"jetski_state.pbtxt",
		"installation_id",
	}

	// 2. settings.json: Sync only if missing on remote to preserve remote workspace trust state
	if !settingsExists {
		alwaysSyncFiles = append(alwaysSyncFiles, "settings.json")
	}

	var filesToSync []string
	for _, file := range alwaysSyncFiles {
		localPath := filepath.Join(localCliDir, file)
		if info, err := os.Stat(localPath); err == nil && info.Size() > 0 {
			filesToSync = append(filesToSync, localPath)
		}
	}

	if len(filesToSync) > 0 {
		args := append([]string{"-q"}, filesToSync...)
		args = append(args, fmt.Sprintf("%s:%s/", server, remoteCliDir))
		_ = exec.CommandContext(ctx, "scp", args...).Run()
		_ = exec.CommandContext(ctx, "ssh", server, fmt.Sprintf("chmod 600 %s/*token* 2>/dev/null || true", remoteCliDir)).Run()
	}

	return nil
}

var sshCmd = &cobra.Command{
	Use:          "ssh <server> [remote_path] [profile_name] -- [agy_commands]",
	Short:        "Execute agys/agy natively on a remote server over SSH at a specific path",
	SilenceUsage: true,
	Long: `Connects to a remote host over SSH with pseudo-terminal (PTY) allocation (-t),
automatically syncing local profile credentials, tunneling API requests through local proxy, and executing agys/agy natively on the remote Linux host.

Examples:
  agys ssh user@remote-server
  agys ssh user@remote-server quaywin
  agys ssh user@remote-server /var/www/myproject quaywin
  agys ssh user@remote-server /var/www/myproject quaywin -- --dangerously-skip-permissions
`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		server := args[0]
		var remotePath string
		var profileName string
		var agyArgs []string

		remaining := args[1:]
		if len(remaining) > 0 {
			first := remaining[0]
			if strings.HasPrefix(first, "/") || strings.HasPrefix(first, "~") || strings.HasPrefix(first, "./") || strings.Contains(first, "/") {
				remotePath = first
				remaining = remaining[1:]
			}
		}

		if len(remaining) > 0 {
			first := remaining[0]
			if !strings.HasPrefix(first, "-") {
				exists, _, _ := profile.Exists(first)
				if profile.IsAuto(first) || exists {
					profileName = first
					agyArgs = remaining[1:]
				} else if remotePath == "" {
					remotePath = first
					remaining = remaining[1:]
					if len(remaining) > 0 && !strings.HasPrefix(remaining[0], "-") {
						profileName = remaining[0]
						agyArgs = remaining[1:]
					} else {
						agyArgs = remaining
					}
				} else {
					profileName = first
					agyArgs = remaining[1:]
				}
			} else {
				agyArgs = remaining
			}
		}

		if profileName == "" {
			current, _ := profile.GetCurrent()
			if current != "" {
				profileName = current
			} else {
				profileName = profile.AutoProfileKeyword
			}
		}

		// 1. Start local HTTP proxy and tunnel API requests back to local Mac (bypasses remote IP geo-blocking)
		proxyPort, cleanupProxy, err := startLocalHTTPProxy()
		if err != nil {
			return fmt.Errorf("failed to start local HTTP proxy: %w", err)
		}
		defer cleanupProxy()

		// 2. Sync profile credentials to remote if it's not auto
		if !profile.IsAuto(profileName) {
			exists, _, err := profile.Exists(profileName)
			if err != nil || !exists {
				return fmt.Errorf("local profile %q does not exist. Use `agys add %s` to create it first", profileName, profileName)
			}
			fmt.Fprintf(os.Stderr, "[agys] Syncing local profile %q to %s...\n", profileName, server)
			if err := syncProfileToRemote(cmd.Context(), server, profileName); err != nil {
				return err
			}
		}

		// 3. Prepare remote execution command with dynamic remote proxy port to support parallel SSH connections
		remotePort := 10800 + (os.Getpid() % 1000)
		var agyArgsStr string
		if len(agyArgs) > 0 {
			var quoted []string
			for _, arg := range agyArgs {
				quoted = append(quoted, shellQuote(arg))
			}
			agyArgsStr = " -- " + strings.Join(quoted, " ")
		}

		agysRunCmd := fmt.Sprintf("agys run %s", shellQuote(profileName))
		cdPrefix := ""
		if remotePath != "" {
			cdPrefix = fmt.Sprintf("cd %s && ", shellQuote(remotePath))
		}

		proxyEnv := fmt.Sprintf("export HTTP_PROXY=http://127.0.0.1:%d HTTPS_PROXY=http://127.0.0.1:%d http_proxy=http://127.0.0.1:%d https_proxy=http://127.0.0.1:%d ALL_PROXY=http://127.0.0.1:%d all_proxy=http://127.0.0.1:%d;",
			remotePort, remotePort, remotePort, remotePort, remotePort, remotePort)
		sshEnv := fmt.Sprintf("export AGYS_SSH_SERVER=%s; export AGYS_SSH_PATH=%s;", shellQuote(server), shellQuote(remotePath))

		innerCmd := fmt.Sprintf(
			`export PATH="$HOME/.local/bin:$HOME/bin:$HOME/.gemini/antigravity-cli/bin:/usr/local/bin:$PATH"; `+
				`%s`+
				`%s`+
				`if ! command -v agy >/dev/null 2>&1; then `+
				`echo "[agys] Auto-installing agy (Antigravity CLI) on %s..." >&2; `+
				`curl -fsSL https://antigravity.google/cli/install.sh | bash || true; `+
				`export PATH="$HOME/.local/bin:$HOME/bin:$HOME/.gemini/antigravity-cli/bin:/usr/local/bin:$PATH"; `+
				`fi; `+
				`if ! command -v agys >/dev/null 2>&1; then `+
				`echo "[agys] Auto-installing agys (profile switcher) on %s..." >&2; `+
				`curl -fsSL https://raw.githubusercontent.com/quaywin/agys/main/install.sh | bash || true; `+
				`export PATH="$HOME/.local/bin:$HOME/bin:$HOME/.gemini/antigravity-cli/bin:/usr/local/bin:$PATH"; `+
				`fi; `+
				`%sif command -v agys >/dev/null 2>&1; then exec %s%s; `+
				`elif command -v agy >/dev/null 2>&1; then exec agy%s; `+
				`else `+
				`echo "[agys] Error: Unable to locate agy or agys on %s." >&2; exit 127; `+
				`fi`,
			proxyEnv, sshEnv, server, server, cdPrefix, agysRunCmd, agyArgsStr, agyArgsStr, server,
		)

		remoteCmd := fmt.Sprintf("sh -c %s", shellQuote(innerCmd))

		if remotePath != "" {
			fmt.Fprintf(os.Stderr, "[agys] Connecting to %s (%s) over SSH with PTY (API tunnel active)... \n", server, remotePath)
		} else {
			fmt.Fprintf(os.Stderr, "[agys] Connecting to %s over SSH with PTY (API tunnel active)...\n", server)
		}

		sshExecCmd := exec.CommandContext(cmd.Context(), "ssh", "-R", fmt.Sprintf("%d:127.0.0.1:%d", remotePort, proxyPort), "-t", server, remoteCmd)
		sshExecCmd.Stdin = os.Stdin
		sshExecCmd.Stdout = os.Stdout
		sshExecCmd.Stderr = os.Stderr

		sigCtx, stopSignal := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
		defer stopSignal()

		if err := sshExecCmd.Start(); err != nil {
			return fmt.Errorf("failed to start SSH connection: %w", err)
		}

		done := make(chan error, 1)
		go func() {
			done <- sshExecCmd.Wait()
		}()

		select {
		case err := <-done:
			return err
		case <-sigCtx.Done():
			if sshExecCmd.Process != nil {
				_ = sshExecCmd.Process.Signal(syscall.SIGTERM)
			}
			return <-done
		}
	},
}

func init() {
	sshCmd.DisableFlagParsing = false
	rootCmd.AddCommand(sshCmd)
}
