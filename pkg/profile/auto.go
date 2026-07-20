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

// IsAuto checks if a profile name corresponds to the auto profile keyword.
func IsAuto(name string) bool {
	return strings.EqualFold(strings.TrimSpace(name), AutoProfileKeyword)
}

// Calculate5HQuotaScore extracts the highest remaining 5-hour quota fraction from a QuotaSummary.
// Returns -1.0 if no valid 5h quota bucket is found.
func Calculate5HQuotaScore(summary *QuotaSummary) float64 {
	if summary == nil || len(summary.Groups) == 0 {
		return -1.0
	}

	bestFraction := -1.0
	for _, group := range summary.Groups {
		for _, bucket := range group.Buckets {
			w := strings.ToLower(strings.TrimSpace(bucket.Window))
			d := strings.ToLower(strings.TrimSpace(bucket.DisplayName))
			b := strings.ToLower(strings.TrimSpace(bucket.BucketID))

			if w == "5h" || strings.Contains(w, "5h") || strings.Contains(d, "5h") || strings.Contains(b, "5h") {
				if bucket.RemainingFraction > bestFraction {
					bestFraction = bucket.RemainingFraction
				}
			}
		}
	}

	return bestFraction
}

// ProfileScore holds quota scoring results for a profile.
type ProfileScore struct {
	ProfileName string
	Priority    int
	Score       float64
	Active      bool
	Error       string
}

// SelectBestProfile queries all available profiles in parallel and selects the profile based on Priority & 50% Quota Threshold logic.
func SelectBestProfile(ctx context.Context) (string, float64, error) {
	profiles, err := List()
	if err != nil {
		return "", -1, fmt.Errorf("failed to list profiles: %w", err)
	}

	if len(profiles) == 0 {
		return "", -1, fmt.Errorf("no profiles found. Create one with `agys add <profile_name>`")
	}

	// Filter out reserved keywords
	var candidateProfiles []string
	for _, p := range profiles {
		if !IsAuto(p) {
			candidateProfiles = append(candidateProfiles, p)
		}
	}

	if len(candidateProfiles) == 0 {
		return "", -1, fmt.Errorf("no valid profiles available for auto-selection")
	}

	priorities, _ := GetAllPriorities()

	fetchCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	scores := make([]ProfileScore, len(candidateProfiles))

	for i, pName := range candidateProfiles {
		wg.Add(1)
		go func(index int, name string) {
			defer wg.Done()
			prio := priorities[name]
			summary, err := FetchQuota(fetchCtx, name)
			if err != nil {
				scores[index] = ProfileScore{
					ProfileName: name,
					Priority:    prio,
					Active:      false,
					Error:       err.Error(),
					Score:       -1.0,
				}
			} else {
				score := Calculate5HQuotaScore(summary)
				scores[index] = ProfileScore{
					ProfileName: name,
					Priority:    prio,
					Active:      true,
					Score:       score,
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
			return "", -1, fmt.Errorf("failed to retrieve quota for profiles: %s", strings.Join(errorMsgs, "; "))
		}
		return "", -1, fmt.Errorf("no active profiles with valid 5h quota information found")
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
			// Sort healthy candidates within tier: max score -> current profile -> alphabetical
			sort.SliceStable(healthyCandidates, func(i, j int) bool {
				if healthyCandidates[i].Score != healthyCandidates[j].Score {
					return healthyCandidates[i].Score > healthyCandidates[j].Score
				}
				if healthyCandidates[i].ProfileName == currentDefault {
					return true
				}
				if healthyCandidates[j].ProfileName == currentDefault {
					return false
				}
				return healthyCandidates[i].ProfileName < healthyCandidates[j].ProfileName
			})
			return healthyCandidates[0].ProfileName, healthyCandidates[0].Score, nil
		}
	}

	// 3. Fallback: If no profile has >= 50% quota, pick the profile with highest quota overall
	sort.SliceStable(validScores, func(i, j int) bool {
		if validScores[i].Score != validScores[j].Score {
			return validScores[i].Score > validScores[j].Score
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
	return winner.ProfileName, winner.Score, nil
}
