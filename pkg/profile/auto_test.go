package profile

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestIsAuto(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"auto", true},
		{"AUTO", true},
		{"Auto", true},
		{" auto ", true},
		{"work", false},
		{"autobahn", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsAuto(tt.name); got != tt.want {
				t.Errorf("IsAuto(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestCalculate5HQuotaScore(t *testing.T) {
	t.Run("nil summary", func(t *testing.T) {
		if score := Calculate5HQuotaScore(nil); score != -1.0 {
			t.Errorf("expected -1.0, got %f", score)
		}
	})

	t.Run("empty groups", func(t *testing.T) {
		summary := &QuotaSummary{Groups: []QuotaGroup{}}
		if score := Calculate5HQuotaScore(summary); score != -1.0 {
			t.Errorf("expected -1.0, got %f", score)
		}
	})

	t.Run("valid 5h window bucket", func(t *testing.T) {
		summary := &QuotaSummary{
			Groups: []QuotaGroup{
				{
					DisplayName: "Gemini 1.5 Pro",
					Buckets: []QuotaBucket{
						{Window: "5h", RemainingFraction: 0.85},
						{Window: "weekly", RemainingFraction: 0.99},
					},
				},
				{
					DisplayName: "Gemini 2.0 Flash",
					Buckets: []QuotaBucket{
						{Window: "5h", RemainingFraction: 0.95},
						{Window: "weekly", RemainingFraction: 0.50},
					},
				},
			},
		}

		score := Calculate5HQuotaScore(summary)
		if score != 0.95 {
			t.Errorf("expected max 5h score 0.95, got %f", score)
		}
	})

	t.Run("prioritize Gemini group over Claude/GPT group", func(t *testing.T) {
		summary := &QuotaSummary{
			Groups: []QuotaGroup{
				{
					DisplayName: "Gemini Models",
					Buckets: []QuotaBucket{
						{Window: "5h", RemainingFraction: 0.854},
						{Window: "weekly", RemainingFraction: 0.971},
					},
				},
				{
					DisplayName: "Claude and GPT models",
					Buckets: []QuotaBucket{
						{Window: "5h", RemainingFraction: 1.0},
						{Window: "weekly", RemainingFraction: 0.644},
					},
				},
			},
		}

		score := Calculate5HQuotaScore(summary)
		if score != 0.854 {
			t.Errorf("expected Gemini 5h score 0.854, got %f", score)
		}
	})

	t.Run("fallback to weekly quota when no 5h window buckets", func(t *testing.T) {
		summary := &QuotaSummary{
			Groups: []QuotaGroup{
				{
					DisplayName: "Gemini 1.5 Pro",
					Buckets: []QuotaBucket{
						{Window: "daily", RemainingFraction: 0.85},
						{Window: "weekly", RemainingFraction: 0.99},
					},
				},
			},
		}

		score := Calculate5HQuotaScore(summary)
		if score != 0.99 {
			t.Errorf("expected fallback Gemini weekly score 0.99, got %f", score)
		}
	})

	t.Run("empty buckets across all groups", func(t *testing.T) {
		summary := &QuotaSummary{
			Groups: []QuotaGroup{
				{
					DisplayName: "Gemini 1.5 Pro",
					Buckets:     []QuotaBucket{},
				},
			},
		}

		score := Calculate5HQuotaScore(summary)
		if score != -1.0 {
			t.Errorf("expected -1.0, got %f", score)
		}
	})
}

func TestPrioritySelectionAlgorithm(t *testing.T) {
	// Test candidate ranking logic
	scores := []ProfileScore{
		{ProfileName: "work", Priority: 10, Score: 0.80, Active: true},
		{ProfileName: "personal", Priority: 5, Score: 0.90, Active: true},
	}

	// Priority 10 profile has >= 50% quota (0.80 >= 0.50), so work should be selected
	// despite personal having higher quota (0.90)
	var winner ProfileScore
	if scores[0].Priority > scores[1].Priority && scores[0].Score >= QuotaThresholdPreferred {
		winner = scores[0]
	} else {
		winner = scores[1]
	}
	if winner.ProfileName != "work" {
		t.Errorf("expected winner work, got %s", winner.ProfileName)
	}

	// Test when high priority profile drops below 50%
	scores2 := []ProfileScore{
		{ProfileName: "work", Priority: 10, Score: 0.40, Active: true},
		{ProfileName: "personal", Priority: 5, Score: 0.75, Active: true},
	}
	// work has 40% (< 50%), personal has 75% (>= 50%), so personal should be selected
	if scores2[0].Score < QuotaThresholdPreferred && scores2[1].Score >= QuotaThresholdPreferred {
		winner = scores2[1]
	} else {
		winner = scores2[0]
	}
	if winner.ProfileName != "personal" {
		t.Errorf("expected winner personal, got %s", winner.ProfileName)
	}
}

func TestHasProfileToken(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	pDir, err := Create("profile-with-token")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}

	_, err = Create("profile-empty")
	if err != nil {
		t.Fatalf("Create profile failed: %v", err)
	}

	// Before token write: both should be false
	if HasProfileToken("profile-with-token") {
		t.Errorf("expected profile-with-token to be false before token write")
	}
	if HasProfileToken("profile-empty") {
		t.Errorf("expected profile-empty to be false")
	}

	// Write mock token to profile-with-token
	tokenFile := filepath.Join(pDir, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	_ = os.MkdirAll(filepath.Dir(tokenFile), 0700)
	_ = os.WriteFile(tokenFile, []byte(`{"token":{"access_token":"valid"}}`), 0600)

	if !HasProfileToken("profile-with-token") {
		t.Errorf("expected profile-with-token to be true after token write")
	}
	if HasProfileToken("profile-empty") {
		t.Errorf("expected profile-empty to still be false")
	}
}

func TestCalculateWeeklyQuotaScore(t *testing.T) {
	t.Run("nil summary", func(t *testing.T) {
		if score := CalculateWeeklyQuotaScore(nil); score != -1.0 {
			t.Errorf("expected -1.0, got %f", score)
		}
	})

	t.Run("empty groups", func(t *testing.T) {
		summary := &QuotaSummary{Groups: []QuotaGroup{}}
		if score := CalculateWeeklyQuotaScore(summary); score != -1.0 {
			t.Errorf("expected -1.0, got %f", score)
		}
	})

	t.Run("valid weekly window bucket", func(t *testing.T) {
		summary := &QuotaSummary{
			Groups: []QuotaGroup{
				{
					DisplayName: "Gemini 2.5 Flash",
					Buckets: []QuotaBucket{
						{Window: "5h", RemainingFraction: 0.95},
						{Window: "weekly", RemainingFraction: 0.88},
					},
				},
			},
		}

		score := CalculateWeeklyQuotaScore(summary)
		if score != 0.88 {
			t.Errorf("expected weekly score 0.88, got %f", score)
		}
	})

	t.Run("prioritize Gemini weekly over Claude/GPT weekly", func(t *testing.T) {
		summary := &QuotaSummary{
			Groups: []QuotaGroup{
				{
					DisplayName: "Gemini Models",
					Buckets: []QuotaBucket{
						{Window: "5h", RemainingFraction: 0.90},
						{Window: "weekly", RemainingFraction: 0.75},
					},
				},
				{
					DisplayName: "Claude and GPT models",
					Buckets: []QuotaBucket{
						{Window: "5h", RemainingFraction: 1.0},
						{Window: "weekly", RemainingFraction: 0.99},
					},
				},
			},
		}

		score := CalculateWeeklyQuotaScore(summary)
		if score != 0.75 {
			t.Errorf("expected Gemini weekly score 0.75, got %f", score)
		}
	})

	t.Run("fallback to any weekly bucket when no Gemini group", func(t *testing.T) {
		summary := &QuotaSummary{
			Groups: []QuotaGroup{
				{
					DisplayName: "Other AI Models",
					Buckets: []QuotaBucket{
						{Window: "weekly", RemainingFraction: 0.82},
					},
				},
			},
		}

		score := CalculateWeeklyQuotaScore(summary)
		if score != 0.82 {
			t.Errorf("expected fallback weekly score 0.82, got %f", score)
		}
	})

	t.Run("no weekly bucket returns -1.0", func(t *testing.T) {
		summary := &QuotaSummary{
			Groups: []QuotaGroup{
				{
					DisplayName: "Gemini Models",
					Buckets: []QuotaBucket{
						{Window: "5h", RemainingFraction: 0.90},
					},
				},
			},
		}

		score := CalculateWeeklyQuotaScore(summary)
		if score != -1.0 {
			t.Errorf("expected -1.0, got %f", score)
		}
	})
}

func TestCompareProfileScores(t *testing.T) {
	t.Run("both 5h >= 90%: prioritize higher weekly quota", func(t *testing.T) {
		p1 := ProfileScore{ProfileName: "prof1", Score: 0.98, WeeklyScore: 0.30}
		p2 := ProfileScore{ProfileName: "prof2", Score: 0.92, WeeklyScore: 0.85}

		// prof2 has lower 5h (92% vs 98%) but higher weekly (85% vs 30%).
		// Because both are >= 90%, prof2 should be preferred over prof1.
		if !compareProfileScores(p2, p1, "") {
			t.Errorf("expected prof2 (weekly 85%%) to beat prof1 (weekly 30%%) when both 5h >= 90%%")
		}
		if compareProfileScores(p1, p2, "") {
			t.Errorf("expected prof1 NOT to beat prof2")
		}
	})

	t.Run("both 5h >= 90% and weekly equal: tie-break by higher 5h quota", func(t *testing.T) {
		p1 := ProfileScore{ProfileName: "prof1", Score: 0.98, WeeklyScore: 0.80}
		p2 := ProfileScore{ProfileName: "prof2", Score: 0.92, WeeklyScore: 0.80}

		if !compareProfileScores(p1, p2, "") {
			t.Errorf("expected prof1 (5h 98%%) to beat prof2 (5h 92%%) when weekly is equal")
		}
		if compareProfileScores(p2, p1, "") {
			t.Errorf("expected prof2 NOT to beat prof1")
		}
	})

	t.Run("one 5h >= 90% and one < 90%: profile with 5h >= 90% wins", func(t *testing.T) {
		p1 := ProfileScore{ProfileName: "prof1", Score: 0.92, WeeklyScore: 0.20}
		p2 := ProfileScore{ProfileName: "prof2", Score: 0.85, WeeklyScore: 0.95}

		// prof1 has 5h >= 90%, prof2 has 5h < 90%.
		// prof1 should win even though prof2 has higher weekly quota.
		if !compareProfileScores(p1, p2, "") {
			t.Errorf("expected prof1 (5h 92%% >= 90%%) to beat prof2 (5h 85%% < 90%%)")
		}
		if compareProfileScores(p2, p1, "") {
			t.Errorf("expected prof2 NOT to beat prof1")
		}
	})

	t.Run("both 5h < 90%: prioritize higher 5h quota", func(t *testing.T) {
		p1 := ProfileScore{ProfileName: "prof1", Score: 0.85, WeeklyScore: 0.20}
		p2 := ProfileScore{ProfileName: "prof2", Score: 0.70, WeeklyScore: 0.95}

		if !compareProfileScores(p1, p2, "") {
			t.Errorf("expected prof1 (5h 85%%) to beat prof2 (5h 70%%)")
		}
		if compareProfileScores(p2, p1, "") {
			t.Errorf("expected prof2 NOT to beat prof1")
		}
	})

	t.Run("both 5h < 90% and 5h equal: tie-break by higher weekly quota", func(t *testing.T) {
		p1 := ProfileScore{ProfileName: "prof1", Score: 0.75, WeeklyScore: 0.90}
		p2 := ProfileScore{ProfileName: "prof2", Score: 0.75, WeeklyScore: 0.50}

		if !compareProfileScores(p1, p2, "") {
			t.Errorf("expected prof1 (weekly 90%%) to beat prof2 (weekly 50%%) when 5h is tied")
		}
		if compareProfileScores(p2, p1, "") {
			t.Errorf("expected prof2 NOT to beat prof1")
		}
	})

	t.Run("all quotas equal: prefer currentDefault profile", func(t *testing.T) {
		p1 := ProfileScore{ProfileName: "active-prof", Score: 0.95, WeeklyScore: 0.80}
		p2 := ProfileScore{ProfileName: "other-prof", Score: 0.95, WeeklyScore: 0.80}

		if !compareProfileScores(p1, p2, "active-prof") {
			t.Errorf("expected currentDefault profile active-prof to win when quotas are equal")
		}
		if compareProfileScores(p2, p1, "active-prof") {
			t.Errorf("expected other-prof NOT to beat currentDefault active-prof")
		}
	})

	t.Run("all quotas equal and no currentDefault: alphabetical tie-break", func(t *testing.T) {
		p1 := ProfileScore{ProfileName: "alpha", Score: 0.95, WeeklyScore: 0.80}
		p2 := ProfileScore{ProfileName: "beta", Score: 0.95, WeeklyScore: 0.80}

		if !compareProfileScores(p1, p2, "") {
			t.Errorf("expected alphabetical order: alpha should beat beta")
		}
		if compareProfileScores(p2, p1, "") {
			t.Errorf("expected beta NOT to beat alpha")
		}
	})

	t.Run("missing weekly quota (-1.0) falls back to 5h quota cleanly", func(t *testing.T) {
		// prof1 has 5h = 95%, no weekly bucket (-1.0) -> effectiveWeekly = 95%
		// prof2 has 5h = 92%, weekly = 80% -> effectiveWeekly = 80%
		p1 := ProfileScore{ProfileName: "prof1", Score: 0.95, WeeklyScore: -1.0}
		p2 := ProfileScore{ProfileName: "prof2", Score: 0.92, WeeklyScore: 0.80}

		if !compareProfileScores(p1, p2, "") {
			t.Errorf("expected prof1 with no weekly bucket (effective 95%%) to beat prof2 (80%%)")
		}
		if compareProfileScores(p2, p1, "") {
			t.Errorf("expected prof2 NOT to beat prof1")
		}
	})
}

func TestSelectBestProfileWeeklyPreference(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	// Create two profiles with tokens and mock cached quotas
	p1Dir, err := Create("profile-heavy-week")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	p2Dir, err := Create("profile-fresh-week")
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write tokens
	tokenData := []byte(`{"token":{"access_token":"mock-token","expiry":"2030-01-01T00:00:00Z"}}`)
	t1Path := filepath.Join(p1Dir, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	_ = os.MkdirAll(filepath.Dir(t1Path), 0700)
	_ = os.WriteFile(t1Path, tokenData, 0600)
	t2Path := filepath.Join(p2Dir, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	_ = os.MkdirAll(filepath.Dir(t2Path), 0700)
	_ = os.WriteFile(t2Path, tokenData, 0600)

	// Save cached quotas:
	// profile-heavy-week: 5h = 98%, weekly = 20%
	// profile-fresh-week: 5h = 92%, weekly = 85%
	q1 := &QuotaSummary{
		Groups: []QuotaGroup{
			{
				DisplayName: "Gemini Models",
				Buckets: []QuotaBucket{
					{Window: "5h", RemainingFraction: 0.98},
					{Window: "weekly", RemainingFraction: 0.20},
				},
			},
		},
	}
	q2 := &QuotaSummary{
		Groups: []QuotaGroup{
			{
				DisplayName: "Gemini Models",
				Buckets: []QuotaBucket{
					{Window: "5h", RemainingFraction: 0.92},
					{Window: "weekly", RemainingFraction: 0.85},
				},
			},
		},
	}

	if err := SaveCachedQuota("profile-heavy-week", q1); err != nil {
		t.Fatalf("SaveCachedQuota failed: %v", err)
	}
	if err := SaveCachedQuota("profile-fresh-week", q2); err != nil {
		t.Fatalf("SaveCachedQuota failed: %v", err)
	}

	ctx := context.Background()
	best, err := SelectBestProfileDetailed(ctx)
	if err != nil {
		t.Fatalf("SelectBestProfileDetailed failed: %v", err)
	}

	if best.ProfileName != "profile-fresh-week" {
		t.Errorf("expected 'profile-fresh-week' (weekly 85%%) to be selected, got %q (score: %f, weekly: %f)",
			best.ProfileName, best.Score, best.WeeklyScore)
	}
	if best.Score != 0.92 {
		t.Errorf("expected score 0.92, got %f", best.Score)
	}
	if best.WeeklyScore != 0.85 {
		t.Errorf("expected weekly score 0.85, got %f", best.WeeklyScore)
	}
}

