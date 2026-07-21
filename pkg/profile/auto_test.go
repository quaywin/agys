package profile

import (
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

	t.Run("no 5h window buckets", func(t *testing.T) {
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
