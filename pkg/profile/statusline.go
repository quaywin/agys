package profile

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
	ConversationTitle   string    `json:"conversation_title,omitempty"`
	ConversationID      string    `json:"conversation_id,omitempty"`
	Cost                float64   `json:"cost,omitempty"`
	Effort              string    `json:"effort,omitempty"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// StatusLinePayload represents the JSON payload streamed to stdin by Antigravity CLI statusLine command.
type StatusLinePayload struct {
	ConversationID       string  `json:"conversation_id,omitempty"`
	SessionID            string  `json:"session_id,omitempty"`
	ConversationTitle    string  `json:"conversation_title"`
	ConversationTitleAlt string  `json:"conversationTitle,omitempty"`
	Title                string  `json:"title,omitempty"`
	Cost                 float64 `json:"cost"`
	Effort            string  `json:"effort,omitempty"`
	ReasoningEffort   string  `json:"reasoning_effort,omitempty"`
	Model             struct {
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

func getSessionContextPathForPane(profileDir, paneID string) string {
	if paneID != "" {
		sanitized := strings.ReplaceAll(paneID, ":", "_")
		sanitized = strings.ReplaceAll(sanitized, "/", "_")
		return filepath.Join(profileDir, fmt.Sprintf(".session_context_%s.json", sanitized))
	}
	return filepath.Join(profileDir, sessionContextFilename)
}

func getSessionContextPath(profileDir string) string {
	return getSessionContextPathForPane(profileDir, os.Getenv("HERDR_PANE_ID"))
}

// SaveSessionContextForPane saves session context for a specific pane.
func SaveSessionContextForPane(profileDir, paneID string, state *SessionContextState) error {
	if profileDir == "" || state == nil {
		return nil
	}
	state.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	targetPath := getSessionContextPathForPane(profileDir, paneID)
	return WriteFileAtomic(targetPath, data, 0600)
}

// SaveSessionContext saves the context window percentage and metrics to the profile directory.
func SaveSessionContext(profileDir string, state *SessionContextState) error {
	return SaveSessionContextForPane(profileDir, os.Getenv("HERDR_PANE_ID"), state)
}

// ResetSessionContext removes the cached session context file for a profile.
func ResetSessionContext(profileDir string) error {
	if profileDir == "" {
		return nil
	}
	targetPath := getSessionContextPath(profileDir)
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// GetSessionContextStateForPane returns the cached session context state for a specific pane.
func GetSessionContextStateForPane(profileDir, paneID string) (*SessionContextState, bool) {
	if profileDir == "" {
		return nil, false
	}
	targetPath := getSessionContextPathForPane(profileDir, paneID)
	data, err := os.ReadFile(targetPath)
	if err != nil {
		return nil, false
	}

	var state SessionContextState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, false
	}

	return &state, true
}

// GetSessionContextState returns the cached session context state if valid.
func GetSessionContextState(profileDir string) (*SessionContextState, bool) {
	return GetSessionContextStateForPane(profileDir, os.Getenv("HERDR_PANE_ID"))
}

// GetSessionContext returns the cached context window percentage (0-100) if valid and not expired (TTL 2 hours).
func GetSessionContext(profileDir string) (int, bool) {
	state, ok := GetSessionContextState(profileDir)
	if !ok || state == nil {
		return 0, false
	}
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

// FormatCost formats a dollar cost amount with 2 to 4 decimal places (e.g. $0.00, $0.0042, $1.25).
func FormatCost(cost float64) string {
	if cost <= 0 {
		return "$0.00"
	}
	s := fmt.Sprintf("%.4f", cost)
	s = strings.TrimRight(s, "0")
	if strings.HasSuffix(s, ".") {
		s += "00"
	} else {
		parts := strings.Split(s, ".")
		if len(parts) == 2 && len(parts[1]) < 2 {
			s += strings.Repeat("0", 2-len(parts[1]))
		}
	}
	if strings.HasPrefix(s, ".") {
		s = "0" + s
	}
	return "$" + s
}

// ResolveActiveEffort determines the active reasoning effort for a profile and model.
func ResolveActiveEffort(profileDir, modelName, explicitEffort string) string {
	if explicitEffort != "" {
		return explicitEffort
	}
	if profileDir != "" {
		// 1. Check .active_effort cache
		data, err := os.ReadFile(filepath.Join(profileDir, ".active_effort"))
		if err == nil && len(strings.TrimSpace(string(data))) > 0 {
			return strings.TrimSpace(string(data))
		}
		// 2. Check settings.json
		for _, subDir := range []string{"antigravity-cli", "antigravity", "antigravity-ide"} {
			sPath := filepath.Join(profileDir, ".gemini", subDir, "settings.json")
			if sData, err := os.ReadFile(sPath); err == nil {
				var settings map[string]interface{}
				if json.Unmarshal(sData, &settings) == nil {
					if eff, ok := settings["effort"].(string); ok && eff != "" {
						return eff
					}
					if eff, ok := settings["reasoning_effort"].(string); ok && eff != "" {
						return eff
					}
				}
			}
		}
	}
	// 3. Default to "high" for models supporting effort
	if ModelSupportsEffort(modelName) {
		return "high"
	}
	return ""
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

	activeModel := payload.Model.ID
	if activeModel == "" {
		activeModel = payload.Model.DisplayName
	}
	if profileDir != "" {
		activeModel = ResolveActiveModel(profileDir, activeModel)
	}

	var existingState *SessionContextState
	if profileDir != "" {
		existingState, _ = GetSessionContextState(profileDir)
	}

	costVal := payload.Cost
	effortVal := payload.Effort
	if effortVal == "" {
		effortVal = payload.ReasoningEffort
	}
	if effortVal == "" && profileDir != "" {
		effortVal = ResolveActiveEffort(profileDir, activeModel, "")
	}
	convID := payload.ConversationID
	if convID == "" {
		convID = payload.SessionID
	}
	convTitle := payload.ConversationTitle
	if convTitle == "" {
		convTitle = payload.ConversationTitleAlt
	}
	if convTitle == "" && existingState != nil && existingState.ConversationTitle != "" {
		if convID == "" || existingState.ConversationID == "" || existingState.ConversationID == convID {
			convTitle = existingState.ConversationTitle
		}
	}
	if convTitle == "" && profileDir != "" && convID != "" {
		convTitle = ResolveConversationTitle(profileDir, convID)
	}
	if convTitle != "" {
		convTitle = cleanPromptSummary(convTitle)
		if convTitle == "(No prompt summary)" {
			convTitle = ""
		}
	}

	if profileDir != "" && (hasCtx || convTitle != "" || payload.Cost > 0 || convID != "") {
		state := &SessionContextState{
			UsedPercentage:      ctxUsedPct,
			InputTokens:         inputTokens,
			CacheReadTokens:     cacheReadTokens,
			CacheCreationTokens: cacheCreationTokens,
			ModelID:             payload.Model.ID,
			ModelDisplayName:    payload.Model.DisplayName,
			ConversationTitle:   convTitle,
			ConversationID:      convID,
			Cost:                payload.Cost,
			Effort:              effortVal,
		}
		if existingState != nil {
			isSameConv := true
			if convID != "" && existingState.ConversationID != "" && convID != existingState.ConversationID {
				isSameConv = false
			}
			if isSameConv {
				if !hasCtx {
					state.UsedPercentage = existingState.UsedPercentage
					state.InputTokens = existingState.InputTokens
					state.CacheReadTokens = existingState.CacheReadTokens
					state.CacheCreationTokens = existingState.CacheCreationTokens
				} else if state.InputTokens == 0 && existingState.InputTokens > 0 {
					state.InputTokens = existingState.InputTokens
					state.CacheReadTokens = existingState.CacheReadTokens
					state.CacheCreationTokens = existingState.CacheCreationTokens
				}
				if state.ConversationTitle == "" && existingState.ConversationTitle != "" {
					state.ConversationTitle = existingState.ConversationTitle
				}
				if state.ConversationID == "" {
					state.ConversationID = existingState.ConversationID
				}
				if state.Cost == 0 {
					state.Cost = existingState.Cost
				}
			}
			if state.ModelID == "" {
				state.ModelID = existingState.ModelID
			}
			if state.ModelDisplayName == "" {
				state.ModelDisplayName = existingState.ModelDisplayName
			}
			if state.Effort == "" {
				state.Effort = existingState.Effort
			}
		}
		_ = SaveSessionContext(profileDir, state)
		costVal = state.Cost
		if state.Effort != "" {
			effortVal = state.Effort
		}
	}

	// If active model is provided in payload, update .active_model cache and settings.json only if changed
	if payload.Model.ID != "" && profileDir != "" {
		activeModelPath := filepath.Join(profileDir, ".active_model")
		if curr, err := os.ReadFile(activeModelPath); err != nil || strings.TrimSpace(string(curr)) != payload.Model.ID {
			_ = WriteFileAtomic(activeModelPath, []byte(payload.Model.ID+"\n"), 0600)
			SyncModelToSettings(profileDir, payload.Model.ID)
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
	statusLineStr := FormatStatusLineText(currentProfile, activeModel, effortVal, costVal, ctxPct, hasCtx, quotaDetails, useColor)

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
		resetSec := q.ResetInSeconds
		if resetSec == 0 && q.ResetInSecondsAlt > 0 {
			resetSec = q.ResetInSecondsAlt
		}
		var parsedReset time.Time
		if rTime != "" {
			if tVal, tErr := time.Parse(time.RFC3339, rTime); tErr == nil {
				parsedReset = tVal
			} else if tVal, tErr := time.Parse("2006-01-02T15:04:05Z", rTime); tErr == nil {
				parsedReset = tVal
			}
		} else if resetSec > 0 {
			parsedReset = time.Now().Add(time.Duration(resetSec) * time.Second)
		}
		isWeekly := strings.Contains(k, "week") || strings.Contains(k, "7d")
		is5H := strings.Contains(k, "5h") || (strings.Contains(k, "gemini") && !isWeekly)

		if isWeekly {
			details.FractionWeekly = frac
			details.ResetTimeWeekly = parsedReset
			details.CompactResetWeekly = FormatCompactResetTime(parsedReset, frac)
		} else if is5H {
			details.Fraction5H = frac
			details.ResetTime5H = parsedReset
			details.CompactReset5H = FormatCompactResetTime(parsedReset, frac)
		}
	}
	if details.Fraction5H >= 0 || details.FractionWeekly >= 0 {
		return details
	}
	return nil
}

// FormatStatusLineText formats the real-time statusline text rendered in Antigravity CLI's footer bar.
// Layout: [profile] · % ctx · model (effort) · cost · 5H: % (reset) · Wk: % (reset)
// Example: [davidnguyen] · 5% ctx · gemini-3.7-flash (high) · $0.0042 · 5H: 95% (1h26m) · Wk: 79% (6h35m)
func FormatStatusLineText(profileName, modelName, effort string, cost float64, ctxPct int, hasCtx bool, quotaDetails *ModelQuotaDetails, useColor bool) string {
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

	// 3. Active Model & Effort
	if modelName != "" {
		mStr := modelName
		if effort != "" {
			if useColor {
				mStr = fmt.Sprintf("\033[94m%s\033[0m \033[36m(%s)\033[0m", modelName, effort)
			} else {
				mStr = fmt.Sprintf("%s (%s)", modelName, effort)
			}
		} else {
			if useColor {
				mStr = fmt.Sprintf("\033[94m%s\033[0m", modelName)
			}
		}
		parts = append(parts, mStr)
	}

	// 4. Cumulative Cost (omitted when zero or unavailable)
	if cost > 0 {
		costStr := FormatCost(cost)
		if useColor {
			costStr = fmt.Sprintf("\033[32m%s\033[0m", costStr)
		}
		parts = append(parts, costStr)
	}

	// 5. 5H Quota
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

	// 6. Weekly Quota
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

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(cmdCtx, "cmd.exe", "/c", original.Command)
	} else {
		cmd = exec.CommandContext(cmdCtx, "sh", "-c", original.Command)
	}
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	_ = cmd.Run()
}

// SyncStatusLineSettings configures the "statusLine" entry in settings.json to call agys statusline-hook,
// preserving any pre-existing custom statusLine command in statusline.original.json.
func SyncStatusLineSettings(profileDir string) error {
	cliPath := filepath.Join(profileDir, ".gemini", "antigravity-cli", "settings.json")
	candidatePaths := []string{
		cliPath,
		filepath.Join(profileDir, ".gemini", "antigravity", "settings.json"),
		filepath.Join(profileDir, ".gemini", "antigravity-ide", "settings.json"),
	}

	hookCommand := "agys statusline-hook"

	for _, sPath := range candidatePaths {
		// Only auto-create directory for CLI settings; for GUI/IDE only update if already initialized
		if sPath != cliPath {
			if _, err := os.Stat(sPath); os.IsNotExist(err) {
				continue
			}
		} else {
			if err := os.MkdirAll(filepath.Dir(sPath), 0700); err != nil {
				continue
			}
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

// ResolveConversationTitle attempts to find a meaningful conversation title from:
// 1. Brain transcript.jsonl by conversation ID
// 2. Profile history.jsonl by conversation ID
func ResolveConversationTitle(profileDir, convID string) string {
	if profileDir == "" || convID == "" {
		return ""
	}

	// 1. Check transcript.jsonl if convID is provided
	for _, subDir := range []string{"antigravity-cli", "antigravity", "antigravity-ide"} {
		tPath := filepath.Join(profileDir, ".gemini", subDir, "brain", convID, ".system_generated", "logs", "transcript.jsonl")
		if title := ResolveConversationTitleFromTranscript(tPath); title != "" {
			return title
		}
	}

	// 2. Check history.jsonl by conversation ID
	for _, subDir := range []string{"antigravity-cli", "antigravity", "antigravity-ide"} {
		hPath := filepath.Join(profileDir, ".gemini", subDir, "history.jsonl")
		f, err := os.Open(hPath)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		buf := make([]byte, 128*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 || !bytes.Contains(line, []byte("display")) {
				continue
			}

			var item struct {
				Display        string `json:"display"`
				ConversationID string `json:"conversationId"`
			}
			if err := json.Unmarshal(line, &item); err == nil && item.ConversationID == convID && item.Display != "" {
				disp := strings.TrimSpace(item.Display)
				if !strings.HasPrefix(disp, "/") {
					cleaned := cleanPromptSummary(disp)
					if cleaned != "" && cleaned != "(No prompt summary)" {
						_ = f.Close()
						return cleaned
					}
				}
			}
		}
		_ = f.Close()
	}

	return ""
}

// ResolveConversationTitleFromTranscript extracts the initial user prompt from transcript.jsonl.
func ResolveConversationTitleFromTranscript(transcriptPath string) string {
	if transcriptPath == "" {
		return ""
	}
	f, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 512*1024)

	lineCount := 0
	for scanner.Scan() {
		line := scanner.Bytes()
		lineCount++
		if bytes.Contains(line, []byte("<USER_REQUEST>")) {
			var data struct {
				Content string `json:"content"`
			}
			if json.Unmarshal(line, &data) == nil && data.Content != "" {
				prompt := data.Content
				match := userRequestRegex.FindStringSubmatch(data.Content)
				if len(match) > 1 {
					prompt = match[1]
				}
				cleaned := cleanPromptSummary(prompt)
				if cleaned != "" && cleaned != "(No prompt summary)" {
					return cleaned
				}
			}
		}
		if lineCount > 20 {
			break
		}
	}
	return ""
}
