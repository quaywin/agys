package profile

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

// AutoProfileKeyword is the reserved keyword for automatic profile selection.
const AutoProfileKeyword = "auto"

// QuotaThresholdPreferred is the quota percentage threshold (50%) above which high-priority profiles are favored.
const QuotaThresholdPreferred = 0.50

// QuotaThresholdAbundant is the quota percentage threshold (90%) above which 5h quota is considered abundant.
// When candidate profiles both have >= 90% 5h quota, weekly quota is prioritized to avoid exhausting weekly limits.
const QuotaThresholdAbundant = 0.90

// IsAuto checks if a profile name corresponds to the auto profile keyword.
func IsAuto(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), AutoProfileKeyword)
}

// Calculate5HQuotaScore extracts the highest remaining 5-hour quota fraction for Gemini models from a QuotaSummary.
// It prioritizes Gemini model groups over non-Gemini model groups (e.g. Claude/GPT models).
// Returns -1.0 if no valid 5h quota bucket is found.
func Calculate5HQuotaScore(summary *QuotaSummary) float64 {
	if summary == nil || len(summary.Groups) == 0 {
		return -1.0
	}

	bestGeminiFraction := -1.0
	bestAnyFraction := -1.0

	for _, group := range summary.Groups {
		gName := strings.ToLower(strings.TrimSpace(group.DisplayName))
		gDesc := strings.ToLower(strings.TrimSpace(group.Description))

		for _, bucket := range group.Buckets {
			w := strings.ToLower(strings.TrimSpace(bucket.Window))
			d := strings.ToLower(strings.TrimSpace(bucket.DisplayName))
			b := strings.ToLower(strings.TrimSpace(bucket.BucketID))

			if w == "5h" || strings.Contains(w, "5h") || strings.Contains(d, "5h") || strings.Contains(b, "5h") {
				isGemini := strings.Contains(gName, "gemini") || strings.Contains(gDesc, "gemini") || strings.Contains(d, "gemini") || strings.Contains(b, "gemini")

				if bucket.RemainingFraction > bestAnyFraction {
					bestAnyFraction = bucket.RemainingFraction
				}
				if isGemini && bucket.RemainingFraction > bestGeminiFraction {
					bestGeminiFraction = bucket.RemainingFraction
				}
			}
		}
	}

	if bestGeminiFraction >= 0 {
		return bestGeminiFraction
	}
	if bestAnyFraction >= 0 {
		return bestAnyFraction
	}

	// Fallback to weekly/available buckets if no 5h bucket exists (e.g., brand new account)
	for _, group := range summary.Groups {
		gName := strings.ToLower(strings.TrimSpace(group.DisplayName))
		gDesc := strings.ToLower(strings.TrimSpace(group.Description))

		for _, bucket := range group.Buckets {
			d := strings.ToLower(strings.TrimSpace(bucket.DisplayName))
			b := strings.ToLower(strings.TrimSpace(bucket.BucketID))
			isGemini := strings.Contains(gName, "gemini") || strings.Contains(gDesc, "gemini") || strings.Contains(d, "gemini") || strings.Contains(b, "gemini")

			if bucket.RemainingFraction > bestAnyFraction {
				bestAnyFraction = bucket.RemainingFraction
			}
			if isGemini && bucket.RemainingFraction > bestGeminiFraction {
				bestGeminiFraction = bucket.RemainingFraction
			}
		}
	}

	if bestGeminiFraction >= 0 {
		return bestGeminiFraction
	}
	return bestAnyFraction
}

// CalculateWeeklyQuotaScore extracts the highest remaining weekly quota fraction for Gemini models from a QuotaSummary.
// It prioritizes Gemini model groups over non-Gemini model groups (e.g. Claude/GPT models).
// Returns -1.0 if no valid weekly quota bucket is found.
func CalculateWeeklyQuotaScore(summary *QuotaSummary) float64 {
	if summary == nil || len(summary.Groups) == 0 {
		return -1.0
	}

	bestGeminiFraction := -1.0
	bestAnyFraction := -1.0

	for _, group := range summary.Groups {
		gName := strings.ToLower(strings.TrimSpace(group.DisplayName))
		gDesc := strings.ToLower(strings.TrimSpace(group.Description))

		for _, bucket := range group.Buckets {
			w := strings.ToLower(strings.TrimSpace(bucket.Window))
			d := strings.ToLower(strings.TrimSpace(bucket.DisplayName))
			b := strings.ToLower(strings.TrimSpace(bucket.BucketID))

			if w == "weekly" || strings.Contains(w, "week") || strings.Contains(w, "7d") ||
				strings.Contains(d, "week") || strings.Contains(b, "week") || strings.Contains(b, "7d") {
				isGemini := strings.Contains(gName, "gemini") || strings.Contains(gDesc, "gemini") ||
					strings.Contains(d, "gemini") || strings.Contains(b, "gemini")

				if bucket.RemainingFraction > bestAnyFraction {
					bestAnyFraction = bucket.RemainingFraction
				}
				if isGemini && bucket.RemainingFraction > bestGeminiFraction {
					bestGeminiFraction = bucket.RemainingFraction
				}
			}
		}
	}

	if bestGeminiFraction >= 0 {
		return bestGeminiFraction
	}
	return bestAnyFraction
}

// ProfileScore holds quota scoring results for a profile.
type ProfileScore struct {
	ProfileName string
	Priority    int
	Score       float64 // 5-hour quota remaining fraction (0.0 - 1.0, or -1.0 if unavailable)
	WeeklyScore float64 // Weekly quota remaining fraction (0.0 - 1.0, or -1.0 if unavailable)
	Active      bool
	Error       string
}

// isQuotaAbundant returns true if quota fraction is at or above the abundant threshold (90%).
func isQuotaAbundant(fraction float64) bool {
	return fraction >= QuotaThresholdAbundant-1e-6
}

// effectiveWeeklyScore returns the weekly quota fraction if available,
// falling back to the 5h quota fraction if no weekly bucket was reported.
func effectiveWeeklyScore(p ProfileScore) float64 {
	if p.WeeklyScore >= 0 {
		return p.WeeklyScore
	}
	return p.Score
}

// compareProfileScores determines if candidate a should be ranked before candidate b.
// When both candidates have abundant 5h quota (>= 90%), weekly quota is prioritized
// so that profiles with healthier weekly limits are preferred to run.
func compareProfileScores(a, b ProfileScore, currentDefault string) bool {
	aAbundant := isQuotaAbundant(a.Score)
	bAbundant := isQuotaAbundant(b.Score)

	// Case 1: Both candidates have abundant 5h quota (>= 90%)
	if aAbundant && bAbundant {
		aW := effectiveWeeklyScore(a)
		bW := effectiveWeeklyScore(b)
		if aW != bW {
			return aW > bW
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.ProfileName == currentDefault && b.ProfileName != currentDefault {
			return true
		}
		if b.ProfileName == currentDefault && a.ProfileName != currentDefault {
			return false
		}
		return a.ProfileName < b.ProfileName
	}

	// Case 2: One candidate is abundant (>= 90%) and the other is not (< 90%)
	if aAbundant != bAbundant {
		return aAbundant
	}

	// Case 3: Neither candidate is abundant (< 90%) - prioritize 5h quota
	if a.Score != b.Score {
		return a.Score > b.Score
	}
	aW := effectiveWeeklyScore(a)
	bW := effectiveWeeklyScore(b)
	if aW != bW {
		return aW > bW
	}
	if a.ProfileName == currentDefault && b.ProfileName != currentDefault {
		return true
	}
	if b.ProfileName == currentDefault && a.ProfileName != currentDefault {
		return false
	}
	return a.ProfileName < b.ProfileName
}

// SelectBestProfile queries all available profiles in parallel and selects the profile based on Priority, 5h and Weekly Quota logic.
func SelectBestProfile(ctx context.Context) (string, float64, error) {
	return SelectBestProfileFiltered(ctx, nil)
}

// SelectBestProfileFiltered queries available profiles (filtered by filterFn if provided) in parallel and selects the profile based on Priority, 5h and Weekly Quota logic.
func SelectBestProfileFiltered(ctx context.Context, filterFn func(profileName string) bool) (string, float64, error) {
	winner, err := SelectBestProfileFilteredDetailed(ctx, filterFn)
	if err != nil {
		return "", -1, err
	}
	return winner.ProfileName, winner.Score, nil
}

// SelectBestProfileDetailed queries all available profiles in parallel and returns the winning ProfileScore with detailed 5h and Weekly quota scores.
func SelectBestProfileDetailed(ctx context.Context) (*ProfileScore, error) {
	return SelectBestProfileFilteredDetailed(ctx, nil)
}

// SelectBestProfileFilteredDetailed queries available profiles (filtered by filterFn if provided) in parallel and returns the winning ProfileScore with detailed 5h and Weekly quota scores.
func SelectBestProfileFilteredDetailed(ctx context.Context, filterFn func(profileName string) bool) (*ProfileScore, error) {
	profiles, err := List()
	if err != nil {
		return nil, fmt.Errorf("failed to list profiles: %w", err)
	}

	if len(profiles) == 0 {
		return nil, fmt.Errorf("no profiles found. Create one with `agys add <profile_name>`")
	}

	// Filter out reserved keywords and apply custom filter function if provided
	var candidateProfiles []string
	for _, p := range profiles {
		if !IsAuto(p) {
			if filterFn == nil || filterFn(p) {
				candidateProfiles = append(candidateProfiles, p)
			}
		}
	}

	// Fallback to all non-auto profiles if custom filter matches no candidates
	if len(candidateProfiles) == 0 && filterFn != nil {
		for _, p := range profiles {
			if !IsAuto(p) {
				candidateProfiles = append(candidateProfiles, p)
			}
		}
	}

	if len(candidateProfiles) == 0 {
		return nil, fmt.Errorf("no valid profiles available for auto-selection")
	}

	// Filter candidate profiles: prefer configured profiles with valid tokens on disk
	var configuredCandidates []string
	var unconfiguredCandidates []string
	for _, p := range candidateProfiles {
		if HasProfileToken(p) {
			configuredCandidates = append(configuredCandidates, p)
		} else {
			unconfiguredCandidates = append(unconfiguredCandidates, p)
		}
	}

	targets := configuredCandidates
	if len(targets) == 0 {
		targets = unconfiguredCandidates
	}

	priorities, _ := GetAllPriorities()

	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	maxWorkers := 6
	if maxWorkers > len(targets) {
		maxWorkers = len(targets)
	}
	semaphore := make(chan struct{}, maxWorkers)

	var wg sync.WaitGroup
	scores := make([]ProfileScore, len(targets))

	for i, pName := range targets {
		wg.Add(1)
		go func(index int, name string) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()

			prio := priorities[name]
			summary, err := FetchQuota(fetchCtx, name)
			if err != nil {
				scores[index] = ProfileScore{
					ProfileName: name,
					Priority:    prio,
					Active:      false,
					Error:       err.Error(),
					Score:       -1.0,
					WeeklyScore: -1.0,
				}
			} else {
				score := Calculate5HQuotaScore(summary)
				weeklyScore := CalculateWeeklyQuotaScore(summary)
				scores[index] = ProfileScore{
					ProfileName: name,
					Priority:    prio,
					Active:      true,
					Score:       score,
					WeeklyScore: weeklyScore,
				}
			}
		}(i, pName)
	}

	wg.Wait()

	currentDefault, _ := GetCurrent()

	var validScores []ProfileScore
	var errorMsgs []string

	for _, s := range scores {
		if s.Active && s.Score >= 0 {
			validScores = append(validScores, s)
		} else if !s.Active {
			errorMsgs = append(errorMsgs, fmt.Sprintf("%s: %s", s.ProfileName, s.Error))
		} else {
			errorMsgs = append(errorMsgs, fmt.Sprintf("%s: no 5h quota info", s.ProfileName))
		}
	}

	if len(validScores) == 0 {
		if len(errorMsgs) > 0 {
			return nil, fmt.Errorf("failed to retrieve quota for profiles: %s", strings.Join(errorMsgs, "; "))
		}
		return nil, fmt.Errorf("no active profiles with valid 5h quota information found")
	}

	// 1. Group valid profiles by Priority tier
	priorityMap := make(map[int][]ProfileScore)
	var priorityTiers []int
	for _, s := range validScores {
		if _, exists := priorityMap[s.Priority]; !exists {
			priorityTiers = append(priorityTiers, s.Priority)
		}
		priorityMap[s.Priority] = append(priorityMap[s.Priority], s)
	}

	// Sort priority tiers descending
	sort.Sort(sort.Reverse(sort.IntSlice(priorityTiers)))

	// 2. Look for high-priority tiers with quota >= 50% (QuotaThresholdPreferred)
	for _, tier := range priorityTiers {
		candidates := priorityMap[tier]
		var healthyCandidates []ProfileScore
		for _, c := range candidates {
			if c.Score >= QuotaThresholdPreferred {
				healthyCandidates = append(healthyCandidates, c)
			}
		}

		if len(healthyCandidates) > 0 {
			// Sort healthy candidates within tier:
			// If both candidates have abundant 5h quota (>= 90%), prioritize weekly quota
			// Otherwise prioritize 5h quota.
			sort.SliceStable(healthyCandidates, func(i, j int) bool {
				return compareProfileScores(healthyCandidates[i], healthyCandidates[j], currentDefault)
			})
			winner := healthyCandidates[0]
			return &winner, nil
		}
	}

	// 3. Fallback: If no profile has >= 50% quota, pick the profile with highest quota overall
	sort.SliceStable(validScores, func(i, j int) bool {
		if validScores[i].Score != validScores[j].Score {
			return validScores[i].Score > validScores[j].Score
		}
		iW := effectiveWeeklyScore(validScores[i])
		jW := effectiveWeeklyScore(validScores[j])
		if iW != jW {
			return iW > jW
		}
		if validScores[i].Priority != validScores[j].Priority {
			return validScores[i].Priority > validScores[j].Priority
		}
		if validScores[i].ProfileName == currentDefault {
			return true
		}
		if validScores[j].ProfileName == currentDefault {
			return false
		}
		return validScores[i].ProfileName < validScores[j].ProfileName
	})

	winner := validScores[0]
	return &winner, nil
}
