package profile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func makeKeepAliveToken(expiresAt time.Time, refreshToken string) *OAuthToken {
	token := &OAuthToken{}
	token.Token.AccessToken = "acc"
	token.Token.RefreshToken = refreshToken
	token.Token.Expiry = expiresAt
	return token
}

func TestNextKeepAliveWait(t *testing.T) {
	now := time.Now()

	// Nil token -> nothing to do
	if _, ok := nextKeepAliveWait(nil, now); ok {
		t.Errorf("expected ok=false for nil token")
	}

	// Missing refresh token -> nothing to do
	noRefresh := makeKeepAliveToken(now.Add(30*time.Minute), "")
	if _, ok := nextKeepAliveWait(noRefresh, now); ok {
		t.Errorf("expected ok=false for token without refresh token")
	}

	// Already expired -> foreground path handles it
	expired := makeKeepAliveToken(now.Add(-5*time.Minute), "rt")
	if _, ok := nextKeepAliveWait(expired, now); ok {
		t.Errorf("expected ok=false for expired token")
	}

	// Zero expiry -> nothing to do
	zero := &OAuthToken{}
	zero.Token.AccessToken = "acc"
	zero.Token.RefreshToken = "rt"
	if _, ok := nextKeepAliveWait(zero, now); ok {
		t.Errorf("expected ok=false for zero expiry")
	}

	// Far from expiry -> wait until margin
	fresh := makeKeepAliveToken(now.Add(70*time.Minute), "rt")
	wait, ok := nextKeepAliveWait(fresh, now)
	if !ok {
		t.Fatalf("expected ok=true for fresh token")
	}
	expected := 70*time.Minute - keepAliveMargin
	if wait != expected {
		t.Errorf("expected wait %v, got %v", expected, wait)
	}

	// Within margin -> refresh immediately (wait 0)
	soon := makeKeepAliveToken(now.Add(5*time.Minute), "rt")
	wait, ok = nextKeepAliveWait(soon, now)
	if !ok {
		t.Fatalf("expected ok=true for token within margin")
	}
	if wait != 0 {
		t.Errorf("expected wait 0 for token within margin, got %v", wait)
	}
}

func TestRunTokenKeepAlive_MissingProfile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	// Nonexistent profile: the loop must return immediately without network access.
	if err := RunTokenKeepAlive(t.Context(), "no-such-keepalive-profile"); err != nil {
		t.Errorf("expected nil error for missing profile, got %v", err)
	}
}

func TestArmTokenKeepAlive_MissingToken(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))
	t.Setenv("AGYS_NO_KEEPALIVE", "")

	// No token file: arming must be a safe no-op (no process spawned).
	ArmTokenKeepAlive("no-such-keepalive-profile")

	// Auto keyword must never arm.
	ArmTokenKeepAlive(AutoProfileKeyword)
}

func TestArmTokenKeepAlive_DisabledByEnv(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))
	t.Setenv("AGYS_NO_KEEPALIVE", "1")

	ArmTokenKeepAlive("any-profile")
}

func TestKeepAlivePIDFile(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	agysDir, err := GetAgysDir()
	if err != nil {
		t.Fatalf("GetAgysDir error: %v", err)
	}

	profileName := "pidfile-profile"
	if isKeepAliveRunning(agysDir, profileName) {
		t.Errorf("expected no keep-alive running before PID file exists")
	}

	writeKeepAlivePIDFile(agysDir, profileName, os.Getpid())
	if !isKeepAliveRunning(agysDir, profileName) {
		t.Errorf("expected keep-alive considered running for live PID")
	}

	// Dead PID must not be reported as running.
	writeKeepAlivePIDFile(agysDir, profileName, 999999999)
	if isKeepAliveRunning(agysDir, profileName) {
		t.Errorf("expected keep-alive not running for dead PID")
	}
}
