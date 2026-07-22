package profile

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFormatResetTime(t *testing.T) {
	// Full quota (1.0 fraction) should return "-"
	if got := FormatResetTime(time.Now().Add(2*time.Hour), 1.0); got != "-" {
		t.Errorf("expected '-', got %q", got)
	}

	// Zero reset time should return "-"
	if got := FormatResetTime(time.Time{}, 0.5); got != "-" {
		t.Errorf("expected '-', got %q", got)
	}

	// Past reset time should return "refreshing"
	if got := FormatResetTime(time.Now().Add(-1*time.Minute), 0.5); got != "refreshing" {
		t.Errorf("expected 'refreshing', got %q", got)
	}

	// Future 2 days 4 hours
	t1 := time.Now().Add(2*24*time.Hour + 4*time.Hour + 30*time.Minute)
	got1 := FormatResetTime(t1, 0.5)
	if !strings.HasPrefix(got1, "in 2d 4h") {
		t.Errorf("expected 'in 2d 4h...', got %q", got1)
	}

	// Future 1 hour 45 minutes
	t2 := time.Now().Add(1*time.Hour + 45*time.Minute + 10*time.Second)
	got2 := FormatResetTime(t2, 0.5)
	if !strings.HasPrefix(got2, "in 1h 45m") {
		t.Errorf("expected 'in 1h 45m...', got %q", got2)
	}

	// Future 15 minutes
	t3 := time.Now().Add(15 * time.Minute)
	got3 := FormatResetTime(t3, 0.5)
	if !strings.HasPrefix(got3, "in 15m") && !strings.HasPrefix(got3, "in 14m") {
		t.Errorf("expected 'in 15m' or 'in 14m', got %q", got3)
	}

	// Less than 1 minute
	t4 := time.Now().Add(30 * time.Second)
	got4 := FormatResetTime(t4, 0.5)
	if got4 != "in <1m" && got4 != "in 1m" {
		t.Errorf("expected 'in <1m' or 'in 1m', got %q", got4)
	}
}

func TestProgressBar(t *testing.T) {
	if bar := ProgressBar(1.0, 10); bar != "██████████" {
		t.Errorf("expected '██████████', got %q", bar)
	}

	if bar := ProgressBar(0.5, 10); bar != "█████░░░░░" {
		t.Errorf("expected '█████░░░░░', got %q", bar)
	}

	if bar := ProgressBar(0.0, 10); bar != "░░░░░░░░░░" {
		t.Errorf("expected '░░░░░░░░░░', got %q", bar)
	}
}

func TestRenderQuotaTable(t *testing.T) {
	resetIn2h := time.Now().Add(2*time.Hour + 30*time.Minute)
	resetIn3d := time.Now().Add(3*24*time.Hour + 5*time.Hour)

	results := []ProfileQuotaInfo{
		{
			ProfileName: "work",
			Email:       "work@example.com",
			Active:      true,
			Quota: &QuotaSummary{
				Groups: []QuotaGroup{
					{
						DisplayName: "Gemini 2.5 Flash",
						Buckets: []QuotaBucket{
							{
								Window:            "5h",
								RemainingFraction: 0.5,
								ResetTime:         resetIn2h,
							},
							{
								Window:            "weekly",
								RemainingFraction: 0.8,
								ResetTime:         resetIn3d,
							},
						},
					},
				},
			},
		},
		{
			ProfileName: "personal",
			Email:       "personal@example.com",
			Active:      false,
			Error:       "not logged in",
		},
	}

	var buf bytes.Buffer
	priorities := map[string]int{"work": 10, "personal": 0}
	RenderQuotaTable(&buf, results, "work", priorities)

	out := buf.String()
	t.Logf("Rendered table output:\n%s", out)

	if !strings.Contains(out, "PROFILE") || !strings.Contains(out, "RESET (5H)") || !strings.Contains(out, "RESET (WEEKLY)") {
		t.Errorf("expected headers in table output, got:\n%s", out)
	}

	if !strings.Contains(out, "work (default)") {
		t.Errorf("expected 'work (default)' in output, got:\n%s", out)
	}

	if !strings.Contains(out, "in 2h 30m") && !strings.Contains(out, "in 2h 29m") {
		t.Errorf("expected remaining reset time 'in 2h 30m' for 5h bucket in output, got:\n%s", out)
	}

	if !strings.Contains(out, "in 3d 5h") && !strings.Contains(out, "in 3d 4h") {
		t.Errorf("expected remaining reset time 'in 3d 5h' for weekly bucket in output, got:\n%s", out)
	}

	if !strings.Contains(out, "[!] Error: not logged in") {
		t.Errorf("expected error message in output for inactive profile, got:\n%s", out)
	}
}

func TestTokenFingerprintedEmailCache(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AGYS_DIR", tmpDir)

	pName := "testprofile"
	pDir, err := Create(pName)
	if err != nil {
		t.Fatalf("failed to create profile: %v", err)
	}

	// No token -> GetCachedEmail should return error
	if _, err := GetCachedEmail(pName); err == nil {
		t.Errorf("expected error when token file does not exist, got nil")
	}

	// Create a dummy token
	tokenPath := filepath.Join(pDir, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0700); err != nil {
		t.Fatalf("failed to create token dir: %v", err)
	}
	dummyToken := `{"token":{"access_token":"access1","refresh_token":"refresh1"}}`
	if err := os.WriteFile(tokenPath, []byte(dummyToken), 0600); err != nil {
		t.Fatalf("failed to write token: %v", err)
	}

	// Save email cache
	if err := SaveCachedEmail(pName, "user1@example.com"); err != nil {
		t.Fatalf("SaveCachedEmail failed: %v", err)
	}

	// Read email cache with matching token -> should succeed
	email, err := GetCachedEmail(pName)
	if err != nil || email != "user1@example.com" {
		t.Fatalf("expected 'user1@example.com', got email=%q err=%v", email, err)
	}

	// Update token (simulating login with new account)
	newToken := `{"token":{"access_token":"access2","refresh_token":"refresh2"}}`
	if err := os.WriteFile(tokenPath, []byte(newToken), 0600); err != nil {
		t.Fatalf("failed to update token: %v", err)
	}

	// Read email cache with changed token -> fingerprint mismatch, should return error
	if _, err := GetCachedEmail(pName); err == nil {
		t.Errorf("expected error due to fingerprint mismatch after token change, got nil")
	}
}
