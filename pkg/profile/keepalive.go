package profile

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// keepAliveMargin refreshes the token this long before it expires so agy never
	// has to perform the OAuth refresh round trip during startup.
	keepAliveMargin = 10 * time.Minute
	// keepAliveMaxSteps bounds the wait/refresh loop so a keep-alive process has a
	// finite lifetime (~12 proactive refreshes for 1-hour access tokens).
	keepAliveMaxSteps = 48
	// keepAliveRetryDelay is how long to wait before retrying a transient refresh failure.
	keepAliveRetryDelay = 30 * time.Second
)

// keepAlivePIDFilePrefix is the filename prefix for per-profile keep-alive PID files in the agys dir.
const keepAlivePIDFilePrefix = "keepalive-"

// nextKeepAliveWait returns how long to wait before the next proactive refresh and whether the
// keep-alive loop should keep running for this token. ok=false means there is nothing to do:
// the token is missing, lacks a refresh token, or is already expired (the foreground launch
// path refreshes expired tokens).
func nextKeepAliveWait(token *OAuthToken, now time.Time) (time.Duration, bool) {
	if token == nil || token.Token.RefreshToken == "" || token.Token.AccessToken == "" {
		return 0, false
	}
	until := token.Token.Expiry.Sub(now)
	if until <= 0 {
		return 0, false
	}
	wait := until - keepAliveMargin
	if wait < 0 {
		wait = 0
	}
	return wait, true
}

// ArmTokenKeepAlive spawns a detached keep-alive process for the profile so its OAuth access
// token is proactively refreshed before expiry. Subsequent launches then reuse the existing
// authorization instead of paying the token-refresh round trip inside `agy`.
func ArmTokenKeepAlive(profileName string) {
	if profileName == "" || profileName == AutoProfileKeyword || os.Getenv("AGYS_NO_KEEPALIVE") == "1" {
		return
	}

	token, err := ReadToken(profileName)
	if err != nil {
		return
	}
	if _, ok := nextKeepAliveWait(token, time.Now()); !ok {
		return
	}

	agysDir, dirErr := GetAgysDir()
	if dirErr == nil {
		if isKeepAliveRunning(agysDir, profileName) {
			return
		}
	}

	exePath, err := os.Executable()
	if err != nil {
		return
	}

	cmd := exec.Command(exePath, "keepalive", profileName)
	cmd.Env = append(os.Environ(), "AGYS_NO_KEEPALIVE=1")
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	setupDetachedProcess(cmd)

	if err := cmd.Start(); err != nil {
		return
	}

	if dirErr == nil {
		writeKeepAlivePIDFile(agysDir, profileName, cmd.Process.Pid)
	}

	// Reap the detached process when it exits so it does not linger as a zombie.
	go func() { _ = cmd.Wait() }()
	_ = cmd.Process.Release()
}

// RunTokenKeepAlive is the keep-alive loop executed by `agys keepalive <profile>`.
// It sleeps until shortly before the token expires, refreshes it, and repeats for a
// bounded number of steps. If another writer refreshed the token meanwhile, the loop
// simply recomputes its wait from the newer expiry.
func RunTokenKeepAlive(ctx context.Context, profileName string) error {
	for step := 0; step < keepAliveMaxSteps; step++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		token, err := ReadToken(profileName)
		if err != nil || token == nil {
			// Profile gone or never authenticated: stop silently.
			return nil
		}
		wait, ok := nextKeepAliveWait(token, time.Now())
		if !ok {
			return nil
		}
		if wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}

		// Token is within the refresh margin: refresh it now.
		if err := RefreshToken(ctx, profileName); err != nil {
			if errors.Is(err, ErrUnauthenticated) {
				// Refresh token revoked or invalid: stop silently.
				return nil
			}
			// Transient failure: retry once.
			select {
			case <-time.After(keepAliveRetryDelay):
			case <-ctx.Done():
				return ctx.Err()
			}
			if retryErr := RefreshToken(ctx, profileName); retryErr != nil {
				return nil
			}
		}

		// Best-effort: warm the quota cache so `agys quota`, auto-selection and
		// Herdr hooks answer instantly afterwards.
		warmCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, _ = FetchQuota(warmCtx, profileName)
		cancel()
	}
	return nil
}

func keepAlivePIDPath(agysDir, profileName string) string {
	return filepath.Join(agysDir, keepAlivePIDFilePrefix+profileName+".pid")
}

// isKeepAliveRunning reports whether a keep-alive process previously armed for the
// profile is still alive, based on its PID file.
func isKeepAliveRunning(agysDir, profileName string) bool {
	data, err := os.ReadFile(keepAlivePIDPath(agysDir, profileName))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	return isProcessAlive(pid)
}

func writeKeepAlivePIDFile(agysDir, profileName string, pid int) {
	_ = WriteFileAtomic(keepAlivePIDPath(agysDir, profileName), []byte(strconv.Itoa(pid)+"\n"), 0600)
}
