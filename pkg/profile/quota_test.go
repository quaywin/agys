package profile

import (
	"bytes"
	"context"
	"encoding/json"
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

func TestFormatCompactResetTime(t *testing.T) {
	// Full quota (1.0 fraction) should return ""
	if got := FormatCompactResetTime(time.Now().Add(2*time.Hour), 1.0); got != "" {
		t.Errorf("expected '', got %q", got)
	}

	// Zero reset time should return ""
	if got := FormatCompactResetTime(time.Time{}, 0.5); got != "" {
		t.Errorf("expected '', got %q", got)
	}

	// Past reset time should return "due"
	if got := FormatCompactResetTime(time.Now().Add(-1*time.Minute), 0.5); got != "due" {
		t.Errorf("expected 'due', got %q", got)
	}

	// Future 2 days 4 hours -> "2d4h"
	t1 := time.Now().Add(2*24*time.Hour + 4*time.Hour + 30*time.Minute)
	got1 := FormatCompactResetTime(t1, 0.5)
	if !strings.HasPrefix(got1, "2d4h") {
		t.Errorf("expected '2d4h...', got %q", got1)
	}

	// Future 1 hour 45 minutes -> "1h45m"
	t2 := time.Now().Add(1*time.Hour + 45*time.Minute + 10*time.Second)
	got2 := FormatCompactResetTime(t2, 0.5)
	if !strings.HasPrefix(got2, "1h45m") && !strings.HasPrefix(got2, "1h44m") {
		t.Errorf("expected '1h45m...', got %q", got2)
	}

	// Future 15 minutes -> "15m"
	t3 := time.Now().Add(15 * time.Minute)
	got3 := FormatCompactResetTime(t3, 0.5)
	if got3 != "15m" && got3 != "14m" {
		t.Errorf("expected '15m' or '14m', got %q", got3)
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
		{
			ProfileName: "newaccount",
			Email:       "new@example.com",
			Active:      true,
			Quota: &QuotaSummary{
				Groups: []QuotaGroup{
					{
						DisplayName: "Gemini Models",
						Buckets: []QuotaBucket{
							{
								Window:            "weekly",
								RemainingFraction: 1.0,
								ResetTime:         resetIn3d,
							},
						},
					},
				},
			},
		},
	}

	var buf bytes.Buffer
	priorities := map[string]int{"work": 10, "personal": 0}
	RenderQuotaTable(&buf, results, "work", priorities)

	out := buf.String()
	t.Logf("Rendered table output:\n%s", out)

	if !strings.Contains(out, "PROFILE") || !strings.Contains(out, "CONFIG") || !strings.Contains(out, "RESET (5H)") || !strings.Contains(out, "RESET (WEEKLY)") {
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

	if !strings.Contains(out, "newaccount") || !strings.Contains(out, "100.0% [██████████]") {
		t.Errorf("expected newaccount to show full 100.0%% quota, got:\n%s", out)
	}

	// Verify partial weekly-only group (< 1.0) does NOT mirror weekly into 5H QUOTA
	partialWeeklyOnly := []ProfileQuotaInfo{
		{
			ProfileName: "partial",
			Email:       "partial@example.com",
			Active:      true,
			Quota: &QuotaSummary{
				Groups: []QuotaGroup{
					{
						DisplayName: "Gemini Models",
						Buckets: []QuotaBucket{
							{
								Window:            "weekly",
								RemainingFraction: 0.56,
								ResetTime:         resetIn3d,
							},
						},
					},
				},
			},
		},
	}
	var pBuf bytes.Buffer
	RenderQuotaTable(&pBuf, partialWeeklyOnly, "", nil)
	pOut := pBuf.String()
	if !strings.Contains(pOut, "N/A") || !strings.Contains(pOut, "56.0%") {
		t.Errorf("expected N/A for missing 5h when weekly is 56.0%%, got:\n%s", pOut)
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

func TestGetProfileFullQuotaDetailsForModel(t *testing.T) {
	// Test matching Gemini vs Claude vs Default
	tmpDir := t.TempDir()
	t.Setenv("AGYS_DIR", tmpDir)
	pName := "testmodelquota"
	pDir, err := Create(pName)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// Without token, should return error
	_, err = GetProfileFullQuotaDetailsForModel(context.Background(), pName, "claude-3-7-sonnet")
	if err == nil {
		t.Errorf("expected error without token, got nil")
	}

	// Write dummy token
	tokenPath := filepath.Join(pDir, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	_ = os.MkdirAll(filepath.Dir(tokenPath), 0700)
	_ = os.WriteFile(tokenPath, []byte(`{"token":{"access_token":"fake"}}`), 0600)
}

func TestExtractModelQuotaDetails(t *testing.T) {
	now := time.Now()
	summary := &QuotaSummary{
		Groups: []QuotaGroup{
			{
				DisplayName: "Gemini Models",
				Description: "gemini-2.5-pro, gemini-2.5-flash",
				Buckets: []QuotaBucket{
					{
						BucketID:          "5h",
						RemainingFraction: 0.75,
						ResetTime:         now.Add(2 * time.Hour),
					},
					{
						BucketID:          "weekly",
						RemainingFraction: 0.90,
						ResetTime:         now.Add(48 * time.Hour),
					},
				},
			},
			{
				DisplayName: "Claude Models",
				Description: "claude-3-7-sonnet, 3p",
				Buckets: []QuotaBucket{
					{
						BucketID:          "5h",
						RemainingFraction: 0.40,
						ResetTime:         now.Add(1 * time.Hour),
					},
					{
						BucketID:          "weekly",
						RemainingFraction: 0.60,
						ResetTime:         now.Add(24 * time.Hour),
					},
				},
			},
		},
	}

	// 1. Test nil summary
	if res := ExtractModelQuotaDetails(nil, "gemini-2.5-flash"); res != nil {
		t.Errorf("expected nil for nil summary, got %+v", res)
	}

	// 2. Test Gemini match
	geminiDetails := ExtractModelQuotaDetails(summary, "gemini-2.5-flash")
	if geminiDetails == nil || geminiDetails.Fraction5H != 0.75 || geminiDetails.FractionWeekly != 0.90 {
		t.Errorf("unexpected gemini details: %+v", geminiDetails)
	}

	// 3. Test Claude match
	claudeDetails := ExtractModelQuotaDetails(summary, "claude-3-7-sonnet")
	if claudeDetails == nil || claudeDetails.Fraction5H != 0.40 || claudeDetails.FractionWeekly != 0.60 {
		t.Errorf("unexpected claude details: %+v", claudeDetails)
	}

	// 4. Test Gemini 4 Hydrogen XHigh matching Gemini Models group and preferring dedicated Hydrogen group if present
	hSummary := &QuotaSummary{
		Groups: []QuotaGroup{
			{
				DisplayName: "Gemini Models",
				Description: "Models within this group: Gemini 3.8 Flash",
				Buckets: []QuotaBucket{
					{BucketID: "gemini-5h", Window: "5h", RemainingFraction: 0.90},
					{BucketID: "gemini-weekly", Window: "weekly", RemainingFraction: 0.80},
				},
			},
			{
				DisplayName: "Gemini 4 Hydrogen",
				Description: "Models within this group: Gemini 4 Hydrogen",
				Buckets: []QuotaBucket{
					{BucketID: "hydrogen-5h", Window: "5h", RemainingFraction: 0.55},
					{BucketID: "hydrogen-weekly", Window: "weekly", RemainingFraction: 0.65},
				},
			},
		},
	}
	hDetails := ExtractModelQuotaDetails(hSummary, "gemini-4-hydrogen-xhigh")
	if hDetails == nil || hDetails.Fraction5H != 0.55 || hDetails.GroupName != "Gemini 4 Hydrogen" {
		t.Errorf("expected dedicated Gemini 4 Hydrogen group (0.55), got %+v", hDetails)
	}

	// 5. Test Orchestrator matches Gemini/Hydrogen quota instead of falling back to Claude 3P
	orchSummary := &QuotaSummary{
		Groups: []QuotaGroup{
			{
				DisplayName: "Claude Models",
				Description: "claude-opus-4-6-thinking, 3p",
				Buckets: []QuotaBucket{
					{BucketID: "3p-5h", Window: "5h", RemainingFraction: 0.15},
				},
			},
			{
				DisplayName: "Gemini Models",
				Description: "Gemini 3.8 Flash",
				Buckets: []QuotaBucket{
					{BucketID: "gem-5h", Window: "5h", RemainingFraction: 0.85},
				},
			},
			{
				DisplayName: "Gemini 4 Hydrogen",
				Description: "Gemini 4 Hydrogen",
				Buckets: []QuotaBucket{
					{BucketID: "hyd-5h", Window: "5h", RemainingFraction: 0.72},
				},
			},
		},
	}
	orchDetails := ExtractModelQuotaDetails(orchSummary, "Orchestrator")
	if orchDetails == nil || orchDetails.Fraction5H != 0.72 || orchDetails.GroupName != "Gemini 4 Hydrogen" {
		t.Errorf("expected Orchestrator to match Gemini 4 Hydrogen quota (0.72), got %+v", orchDetails)
	}

	// 6. Test family isolation: Gemini with weekly=0.65 (no 5h bucket) must NOT borrow 3p-5h from Claude
	isolatedSummary := &QuotaSummary{
		Groups: []QuotaGroup{
			{
				DisplayName: "Claude and GPT models",
				Description: "claude-sonnet-4-6, 3p",
				Buckets: []QuotaBucket{
					{BucketID: "3p-5h", Window: "5h", RemainingFraction: 0.12},
					{BucketID: "3p-weekly", Window: "weekly", RemainingFraction: 0.25},
				},
			},
			{
				DisplayName: "Gemini Models",
				Description: "Models within this group: Gemini 3.8 Flash",
				Buckets: []QuotaBucket{
					{BucketID: "gemini-weekly", Window: "weekly", RemainingFraction: 0.65, ResetTime: now.Add(72 * time.Hour)},
				},
			},
		},
	}
	hydPartial := ExtractModelQuotaDetails(isolatedSummary, "gemini-4-hydrogen")
	if hydPartial == nil || hydPartial.GroupName != "Gemini Models" || hydPartial.FractionWeekly != 0.65 || hydPartial.Fraction5H != -1.0 {
		t.Errorf("expected Gemini Models weekly=0.65 and Fraction5H=-1 (no 3P bleed), got %+v", hydPartial)
	}

	// 7. Fresh Gemini account with weekly=1.0 (no 5h bucket) should report 1.0 5H with no timer and not borrow Claude's 0.12
	isolatedSummary.Groups[1].Buckets[0].RemainingFraction = 1.0
	freshHyd := ExtractModelQuotaDetails(isolatedSummary, "hydrogen")
	if freshHyd == nil || freshHyd.GroupName != "Gemini Models" || freshHyd.Fraction5H != 1.0 || freshHyd.FractionWeekly != 1.0 || freshHyd.ResetStr5H != "-" {
		t.Errorf("expected fresh Gemini 100%% 5H/Weekly with '-' reset, got %+v", freshHyd)
	}

	// 8. User-Agent must be >= 2.35.0
	ua := getAntigravityUserAgent()
	if !strings.HasPrefix(ua, "antigravity/") {
		t.Errorf("unexpected User-Agent: %q", ua)
	}
	ver := strings.Split(strings.TrimPrefix(ua, "antigravity/"), " ")[0]
	if compareSimpleVersions(ver, minAntigravityQuotaClientVer) < 0 {
		t.Errorf("User-Agent version %q must be >= %q", ver, minAntigravityQuotaClientVer)
	}

	// 9. Named Gemini group (score 50) must outrank an unrelated non-3P group (score 20) placed earlier
	unrelatedFirst := &QuotaSummary{
		Groups: []QuotaGroup{
			{
				DisplayName: "Experimental Tools",
				Buckets: []QuotaBucket{
					{BucketID: "exp-5h", Window: "5h", RemainingFraction: 0.10},
				},
			},
			{
				DisplayName: "Gemini Models",
				Description: "Models within this group: Gemini 3.8 Flash",
				Buckets: []QuotaBucket{
					{BucketID: "gemini-5h", Window: "5h", RemainingFraction: 0.88},
				},
			},
		},
	}
	resGemini := ExtractModelQuotaDetails(unrelatedFirst, "gemini-4-hydrogen")
	if resGemini == nil || resGemini.GroupName != "Gemini Models" || resGemini.Fraction5H != 0.88 {
		t.Errorf("expected Gemini Models (0.88) to outrank unrelated non-3P group, got %+v", resGemini)
	}
}

func TestGetProfileFullQuotaDetailsFast_FreshAndStale(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AGYS_DIR", tmpDir)

	pName := "testfastquota"
	_, err := Create(pName)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}

	// 1. When no cache exists -> false
	if _, ok := GetProfileFullQuotaDetailsFast(pName, "gemini-2.5-flash"); ok {
		t.Errorf("expected ok=false when cache does not exist")
	}

	// 2. Seed fresh cache (< 45s)
	summary := &QuotaSummary{
		Groups: []QuotaGroup{
			{
				DisplayName: "Gemini Models",
				Buckets: []QuotaBucket{
					{
						BucketID:          "5h",
						RemainingFraction: 0.82,
						ResetTime:         time.Now().Add(2 * time.Hour),
					},
				},
			},
		},
	}
	if err := SaveCachedQuota(pName, summary); err != nil {
		t.Fatalf("SaveCachedQuota error: %v", err)
	}

	details, ok := GetProfileFullQuotaDetailsFast(pName, "gemini-2.5-flash")
	if !ok || details == nil || details.Fraction5H != 0.82 {
		t.Fatalf("expected fresh cache to return 0.82, got ok=%v, details=%+v", ok, details)
	}

	// 3. Fake stale cache (> 45s but < 4 hours)
	pDir, _ := GetProfileDir(pName)
	cachePath := filepath.Join(pDir, quotaCacheFilename)
	cachedData := CachedProfileQuota{
		Summary:   summary,
		UpdatedAt: time.Now().Add(-2 * time.Minute),
	}
	staleBytes, _ := json.Marshal(cachedData)
	_ = os.WriteFile(cachePath, staleBytes, 0600)

	staleDetails, staleOk := GetProfileFullQuotaDetailsFast(pName, "gemini-2.5-flash")
	if !staleOk || staleDetails == nil || staleDetails.Fraction5H != 0.82 {
		t.Fatalf("expected stale cache to return 0.82 fallback, got ok=%v, details=%+v", staleOk, staleDetails)
	}

	// 4. Fake expired cache (> 4 hours, e.g. 5 hours)
	cachedData.UpdatedAt = time.Now().Add(-5 * time.Hour)
	expiredBytes, _ := json.Marshal(cachedData)
	_ = os.WriteFile(cachePath, expiredBytes, 0600)

	// Direct GetCachedQuota check: should return nil, false
	expiredSummary, expiredOk := GetCachedQuota(pName, 4*time.Hour)
	if expiredOk || expiredSummary != nil {
		t.Fatalf("expected cache older than 4 hours to return (nil, false), got ok=%v, summary=%+v", expiredOk, expiredSummary)
	}

	// Fast fallback check: should return nil, false (not using 5-hour expired cache)
	expDetails, expOk := GetProfileFullQuotaDetailsFast(pName, "gemini-2.5-flash")
	if expOk || expDetails != nil {
		t.Fatalf("expected cache older than 4 hours to NOT be used as fallback, got ok=%v, details=%+v", expOk, expDetails)
	}
}

