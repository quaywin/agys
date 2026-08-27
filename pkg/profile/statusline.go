package profile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	sessionContextFilename = ".session_context"
	statuslineBackupFile   = "statusline.original.json"
)

// SessionContextState stores cached context window metrics for an active session.
type SessionContextState struct {
	UsedPercentage      float64   `json:"used_percentage"`
	InputTokens         int64     `json:"input_tokens,omitempty"`
	CacheReadTokens     int64     `json:"cache_read_tokens,omitempty"`
	CacheCreationTokens int64     `json:"cache_creation_tokens,omitempty"`
	ModelID             string    `json:"model_id,omitempty"`
	ModelDisplayName    string    `json:"model_display_name,omitempty"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// StatusLinePayload represents the JSON payload streamed to stdin by Antigravity CLI statusLine command.
type StatusLinePayload struct {
	Model struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	ContextWindow *struct {
		UsedPercentage float64 `json:"used_percentage"`
		CurrentUsage   struct {
			InputTokens         int64 `json:"input_tokens"`
			CacheReadTokens     int64 `json:"cache_read_input_tokens"`
			CacheCreationTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"current_usage"`
	} `json:"context_window"`
	ContextWindowAlt *struct {
		UsedPercentage float64 `json:"usedPercentage"`
	} `json:"contextWindow"`
	Quota map[string]struct {
		RemainingFraction    float64 `json:"remaining_fraction"`
		RemainingFractionAlt float64 `json:"remainingFraction"`
		ResetTime            string  `json:"reset_time"`
		ResetTimeAlt         string  `json:"resetTime"`
		ResetInSeconds       uint64  `json:"reset_in_seconds"`
		ResetInSecondsAlt    uint64  `json:"resetInSeconds"`
	} `json:"quota"`
}

// SaveSessionContext saves the context window percentage and metrics to the profile directory.
func SaveSessionContext(profileDir string, state *SessionContextState) error {
	if profileDir == "" || state == nil {
		return nil
	}
	state.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	targetPath := filepath.Join(profileDir, sessionContextFilename)
	return WriteFileAtomic(targetPath, data, 0600)
}

// GetSessionContext returns the cached context window percentage (0-100) if valid and not expired (TTL 2 hours).
func GetSessionContext(profileDir string) (int, bool) {
	if profileDir == "" {
		return 0, false
	}
	targetPath := filepath.Join(profileDir, sessionContextFilename)
	data, err := os.ReadFile(targetPath)
	if err != nil {
		return 0, false
	}

	var state SessionContextState
	if err := json.Unmarshal(data, &state); err != nil {
		return 0, false
	}

	// Invalidate if older than 2 hours
	if time.Since(state.UpdatedAt) > 2*time.Hour {
		return 0, false
	}

	pct := int(state.UsedPercentage + 0.5)
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	return pct, true
}

// HandleStatusLine processes the statusLine input from Antigravity CLI, updates local session cache,
// reports real-time metadata to Herdr, and chains previous statusLine command if one was configured.
func HandleStatusLine(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer) error {
	var input []byte
	if stdin != nil {
		input, _ = io.ReadAll(stdin)
	}

	var payload StatusLinePayload
	if len(input) > 0 {
		_ = json.Unmarshal(input, &payload)
	}

	// Resolve active profile and profile directory from current session environment
	currentProfile, profileDir := ResolveProfileFromEnv()

	// Extract context window metrics
	var ctxUsedPct float64
	var hasCtx bool
	var inputTokens, cacheReadTokens, cacheCreationTokens int64

	if payload.ContextWindow != nil {
		ctxUsedPct = payload.ContextWindow.UsedPercentage
		hasCtx = true
		inputTokens = payload.ContextWindow.CurrentUsage.InputTokens
		cacheReadTokens = payload.ContextWindow.CurrentUsage.CacheReadTokens
		cacheCreationTokens = payload.ContextWindow.CurrentUsage.CacheCreationTokens
	} else if payload.ContextWindowAlt != nil {
		ctxUsedPct = payload.ContextWindowAlt.UsedPercentage
		hasCtx = true
	}

	if hasCtx && profileDir != "" {
		_ = SaveSessionContext(profileDir, &SessionContextState{
			UsedPercentage:      ctxUsedPct,
			InputTokens:         inputTokens,
			CacheReadTokens:     cacheReadTokens,
			CacheCreationTokens: cacheCreationTokens,
			ModelID:             payload.Model.ID,
			ModelDisplayName:    payload.Model.DisplayName,
		})
	}

	activeModel := payload.Model.ID
	if activeModel == "" {
		activeModel = payload.Model.DisplayName
	}
	if profileDir != "" {
		activeModel = ResolveActiveModel(profileDir, activeModel)
	}

	// If active model is provided in payload, update .active_model cache only if changed
	if payload.Model.ID != "" && profileDir != "" {
		activeModelPath := filepath.Join(profileDir, ".active_model")
		if curr, err := os.ReadFile(activeModelPath); err != nil || strings.TrimSpace(string(curr)) != payload.Model.ID {
			_ = WriteFileAtomic(activeModelPath, []byte(payload.Model.ID+"\n"), 0600)
		}
	}

	// Retrieve real-time quota details for CLI statusline footer & Herdr update
	var quotaDetails *ModelQuotaDetails
	if currentProfile != "" {
		quotaCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		quotaDetails, _ = GetProfileFullQuotaDetailsForModel(quotaCtx, currentProfile, activeModel)
		cancel()
	}

	// Fallback to quota in stdin payload if API returned no quota
	if (quotaDetails == nil || quotaDetails.Fraction5H < 0) && len(payload.Quota) > 0 {
		if fb := parsePayloadQuota(payload.Quota); fb != nil {
			quotaDetails = fb
		}
	}

	// If inside Herdr environment, trigger immediate metadata refresh for instant zero-latency sidebar update
	if IsInHerdrEnvironment() && currentProfile != "" {
		reportCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		_ = ReportHerdrMetadataWithModel(reportCtx, currentProfile, activeModel, quotaDetails)
	}

	// Format high-contrast real-time telemetry string for Antigravity CLI footer
	useColor := os.Getenv("NO_COLOR") == ""
	ctxPct := int(ctxUsedPct + 0.5)
	statusLineStr := FormatStatusLineText(currentProfile, activeModel, ctxPct, hasCtx, quotaDetails, useColor)

	if stdout != nil && statusLineStr != "" {
		fmt.Fprintln(stdout, statusLineStr)
	}

	// Chain previous statusLine command if one was preserved
	if profileDir != "" {
		chainPreviousStatusLine(ctx, profileDir, input, stdout, stderr)
	}

	return nil
}

func parsePayloadQuota(quotaMap map[string]struct {
	RemainingFraction    float64 `json:"remaining_fraction"`
	RemainingFractionAlt float64 `json:"remainingFraction"`
	ResetTime            string  `json:"reset_time"`
	ResetTimeAlt         string  `json:"resetTime"`
	ResetInSeconds       uint64  `json:"reset_in_seconds"`
	ResetInSecondsAlt    uint64  `json:"resetInSeconds"`
}) *ModelQuotaDetails {
	if len(quotaMap) == 0 {
		return nil
	}
	details := &ModelQuotaDetails{
		Fraction5H:     -1.0,
		FractionWeekly: -1.0,
	}
	for key, q := range quotaMap {
		k := strings.ToLower(key)
		frac := q.RemainingFraction
		if frac == 0 && q.RemainingFractionAlt > 0 {
			frac = q.RemainingFractionAlt
		}
		rTime := q.ResetTime
		if rTime == "" {
			rTime = q.ResetTimeAlt
		}
		var parsedReset time.Time
		if rTime != "" {
			if tVal, tErr := time.Parse(time.RFC3339, rTime); tErr == nil {
				parsedReset = tVal
			} else if tVal, tErr := time.Parse("2006-01-02T15:04:05Z", rTime); tErr == nil {
				parsedReset = tVal
			}
		}
		if strings.Contains(k, "5h") || strings.Contains(k, "gemini") {
			details.Fraction5H = frac
			details.ResetTime5H = parsedReset
			details.CompactReset5H = FormatCompactResetTime(parsedReset, frac)
		} else if strings.Contains(k, "week") || strings.Contains(k, "7d") {
			details.FractionWeekly = frac
			details.ResetTimeWeekly = parsedReset
			details.CompactResetWeekly = FormatCompactResetTime(parsedReset, frac)
		}
	}
	if details.Fraction5H >= 0 || details.FractionWeekly >= 0 {
		return details
	}
	return nil
}

// FormatStatusLineText formats the real-time statusline text rendered in Antigravity CLI's footer bar.
// Layout: [profile] · % ctx · model · 5H: % (reset) · Wk: % (reset)
// Example: [davidnguyen] · 5% ctx · gemini-3.7-flash · 5H: 95% (1h26m) · Wk: 79% (6h35m)
func FormatStatusLineText(profileName, modelName string, ctxPct int, hasCtx bool, quotaDetails *ModelQuotaDetails, useColor bool) string {
	var parts []string

	sep := " · "
	if useColor {
		sep = "\033[90m · \033[0m"
	}

	// 1. Profile Name
	if profileName != "" {
		pStr := fmt.Sprintf("[%s]", profileName)
		if useColor {
			pStr = fmt.Sprintf("\033[1;36m[%s]\033[0m", profileName)
		}
		parts = append(parts, pStr)
	}

	// 2. % Context Window
	if hasCtx {
		ctxStr := fmt.Sprintf("%d%% ctx", ctxPct)
		if useColor {
			if ctxPct >= 80 {
				ctxStr = fmt.Sprintf("\033[1;31m%s\033[0m", ctxStr)
			} else if ctxPct >= 50 {
				ctxStr = fmt.Sprintf("\033[33m%s\033[0m", ctxStr)
			} else {
				ctxStr = fmt.Sprintf("\033[36m%s\033[0m", ctxStr)
			}
		}
		parts = append(parts, ctxStr)
	}

	// 3. Active Model
	if modelName != "" {
		mStr := modelName
		if useColor {
			mStr = fmt.Sprintf("\033[94m%s\033[0m", modelName)
		}
		parts = append(parts, mStr)
	}

	// 4. 5H Quota
	if quotaDetails != nil && quotaDetails.Fraction5H >= 0 {
		pct5h := int(quotaDetails.Fraction5H*100 + 0.5)
		q5hStr := fmt.Sprintf("%d%%", pct5h)
		if quotaDetails.CompactReset5H != "" {
			q5hStr = fmt.Sprintf("%d%% (%s)", pct5h, quotaDetails.CompactReset5H)
		}
		if useColor {
			if pct5h >= 20 {
				q5hStr = fmt.Sprintf("\033[32m%s\033[0m", q5hStr)
			} else if pct5h > 5 {
				q5hStr = fmt.Sprintf("\033[33m%s\033[0m", q5hStr)
			} else {
				q5hStr = fmt.Sprintf("\033[1;31m%s\033[0m", q5hStr)
			}
		}
		parts = append(parts, q5hStr)
	}

	// 5. Weekly Quota
	if quotaDetails != nil && quotaDetails.FractionWeekly >= 0 {
		pctWk := int(quotaDetails.FractionWeekly*100 + 0.5)
		qWkStr := fmt.Sprintf("%d%%", pctWk)
		if quotaDetails.CompactResetWeekly != "" {
			qWkStr = fmt.Sprintf("%d%% (%s)", pctWk, quotaDetails.CompactResetWeekly)
		}
		if useColor {
			if pctWk >= 20 {
				qWkStr = fmt.Sprintf("\033[35m%s\033[0m", qWkStr)
			} else if pctWk > 5 {
				qWkStr = fmt.Sprintf("\033[33m%s\033[0m", qWkStr)
			} else {
				qWkStr = fmt.Sprintf("\033[1;31m%s\033[0m", qWkStr)
			}
		}
		parts = append(parts, qWkStr)
	}

	if len(parts) == 0 {
		return ""
	}

	return strings.Join(parts, sep)
}

func chainPreviousStatusLine(ctx context.Context, profileDir string, input []byte, stdout, stderr io.Writer) {
	backupPath := filepath.Join(profileDir, ".gemini", "config", statuslineBackupFile)
	data, err := os.ReadFile(backupPath)
	if err != nil || len(data) == 0 {
		return
	}

	var original struct {
		Type    string `json:"type"`
		Command string `json:"command"`
	}
	if err := json.Unmarshal(data, &original); err != nil || original.Command == "" {
		return
	}

	// Avoid infinite recursion if command points to agys statusline-hook
	if strings.Contains(original.Command, "statusline-hook") {
		return
	}

	cmdCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "sh", "-c", original.Command)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	_ = cmd.Run()
}

// SyncStatusLineSettings configures the "statusLine" entry in settings.json to call agys statusline-hook,
// preserving any pre-existing custom statusLine command in statusline.original.json.
func SyncStatusLineSettings(profileDir string) error {
	candidatePaths := []string{
		filepath.Join(profileDir, ".gemini", "antigravity-cli", "settings.json"),
		filepath.Join(profileDir, ".gemini", "antigravity", "settings.json"),
		filepath.Join(profileDir, ".gemini", "antigravity-ide", "settings.json"),
	}

	hookCommand := "agys statusline-hook"

	for _, sPath := range candidatePaths {
		if err := os.MkdirAll(filepath.Dir(sPath), 0700); err != nil {
			continue
		}

		var settings map[string]interface{}
		data, err := os.ReadFile(sPath)
		if err == nil {
			_ = json.Unmarshal(data, &settings)
		}
		if settings == nil {
			settings = make(map[string]interface{})
		}

		// Check if already installed
		if sl, ok := settings["statusLine"].(map[string]interface{}); ok {
			if cmdStr, ok := sl["command"].(string); ok && strings.Contains(cmdStr, "statusline-hook") {
				continue
			}
			// Backup original statusLine if not already backed up
			backupPath := filepath.Join(profileDir, ".gemini", "config", statuslineBackupFile)
			if _, err := os.Stat(backupPath); os.IsNotExist(err) {
				_ = os.MkdirAll(filepath.Dir(backupPath), 0700)
				bData, _ := json.MarshalIndent(sl, "", "  ")
				_ = WriteFileAtomic(backupPath, bData, 0600)
			}
		}

		settings["statusLine"] = map[string]interface{}{
			"type":    "command",
			"command": hookCommand,
		}

		out, err := json.MarshalIndent(settings, "", "  ")
		if err == nil {
			_ = WriteFileAtomic(sPath, []byte(string(out)+"\n"), 0600)
		}
	}

	return nil
}
