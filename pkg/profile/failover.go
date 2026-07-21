package profile

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"regexp"
	"sync"
)

var quotaErrorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)RESOURCE_EXHAUSTED`),
	regexp.MustCompile(`(?i)429\s+Too\s+Many\s+Requests`),
	regexp.MustCompile(`(?i)quota\s+exceeded`),
	regexp.MustCompile(`(?i)rate\s+limit\s+reached`),
	regexp.MustCompile(`(?i)you\s+have\s+reached\s+your\s+quota`),
}

// IsQuotaError checks if an error message or output string indicates a quota limit exhaustion.
func IsQuotaError(output string) bool {
	if output == "" {
		return false
	}
	for _, pattern := range quotaErrorPatterns {
		if pattern.MatchString(output) {
			return true
		}
	}
	return false
}

// QuotaInterceptorWriter wraps an io.Writer to pass output through in real-time
// while capturing a buffer of recent output to inspect for quota errors.
type QuotaInterceptorWriter struct {
	writer io.Writer
	mu     sync.Mutex
	buf    bytes.Buffer
}

// NewQuotaInterceptorWriter creates a new interceptor wrapping the target writer.
func NewQuotaInterceptorWriter(target io.Writer) *QuotaInterceptorWriter {
	return &QuotaInterceptorWriter{
		writer: target,
	}
}

func (w *QuotaInterceptorWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	// Keep up to 64KB of buffer history
	if w.buf.Len()+len(p) > 65536 {
		// Truncate old data if buffer is growing too large
		overflow := (w.buf.Len() + len(p)) - 65536
		if overflow < w.buf.Len() {
			w.buf.Next(overflow)
		} else {
			w.buf.Reset()
		}
	}
	w.buf.Write(p)
	w.mu.Unlock()

	return w.writer.Write(p)
}

// String returns the captured output buffer content.
func (w *QuotaInterceptorWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

// SelectNextBestProfile queries available profiles excluding those marked as excluded.
func SelectNextBestProfile(ctx context.Context, excludeProfiles map[string]bool) (string, float64, error) {
	allProfiles, err := List()
	if err != nil {
		return "", -1, fmt.Errorf("failed to list profiles: %w", err)
	}

	var candidates []string
	for _, p := range allProfiles {
		if !IsAuto(p) && !excludeProfiles[p] {
			candidates = append(candidates, p)
		}
	}

	if len(candidates) == 0 {
		return "", -1, fmt.Errorf("no remaining profiles available for failover")
	}

	priorities, _ := GetAllPriorities()

	scores := make([]ProfileScore, len(candidates))
	var wg sync.WaitGroup

	for i, pName := range candidates {
		wg.Add(1)
		go func(index int, name string) {
			defer wg.Done()
			prio := priorities[name]
			summary, err := FetchQuota(ctx, name)
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

	var validScores []ProfileScore
	for _, s := range scores {
		if s.Active && s.Score >= 0 {
			validScores = append(validScores, s)
		}
	}

	if len(validScores) == 0 {
		return "", -1, fmt.Errorf("no valid remaining profiles with quota info")
	}

	// Pick profile with highest remaining quota score & priority
	best := validScores[0]
	for _, s := range validScores[1:] {
		if s.Score > best.Score || (s.Score == best.Score && s.Priority > best.Priority) {
			best = s
		}
	}

	return best.ProfileName, best.Score, nil
}
