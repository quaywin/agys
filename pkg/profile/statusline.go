package profile

import (
	"bytes"
	"context"
	"encoding/json"
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

	// If active model is provided in payload, update .active_model cache only if changed
	if payload.Model.ID != "" && profileDir != "" {
		activeModelPath := filepath.Join(profileDir, ".active_model")
		if curr, err := os.ReadFile(activeModelPath); err != nil || strings.TrimSpace(string(curr)) != payload.Model.ID {
			_ = WriteFileAtomic(activeModelPath, []byte(payload.Model.ID+"\n"), 0600)
		}
	}

	// If inside Herdr environment, trigger immediate metadata refresh for instant zero-latency sidebar update
	if IsInHerdrEnvironment() && currentProfile != "" {
		activeModel := payload.Model.ID
		if activeModel == "" {
			activeModel = payload.Model.DisplayName
		}
		reportCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
		defer cancel()
		_ = ReportHerdrMetadataWithModel(reportCtx, currentProfile, activeModel)
	}

	// Chain previous statusLine command if one was preserved
	if profileDir != "" {
		chainPreviousStatusLine(ctx, profileDir, input, stdout, stderr)
	}

	return nil
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
