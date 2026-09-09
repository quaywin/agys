package profile

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"

	"github.com/gofrs/flock"
)

const defaultHerdrHookScript = `#!/bin/sh
# installed by herdr / synced by agys
# HERDR_INTEGRATION_ID=antigravity_cli
# HERDR_INTEGRATION_VERSION=3

set -eu

emit_and_exit() {
  printf '{}\n'
  exit 0
}

[ -n "${HERDR_SOCKET_PATH:-}" ] || emit_and_exit
[ -n "${HERDR_PANE_ID:-}" ] || emit_and_exit

# Locate real user home if $HOME is pointing to an isolated profile directory
REAL_HOME="$HOME"
case "$HOME" in
  */.agys/profiles/*)
    REAL_HOME="${HOME%%/.agys/profiles/*}"
    ;;
esac

# Directly invoke agys binary in pure compiled Go (zero Python dependency)
AGYS_BIN="agys"
if [ -x "$REAL_HOME/.local/bin/agys" ]; then
  AGYS_BIN="$REAL_HOME/.local/bin/agys"
elif [ -x "$REAL_HOME/go/bin/agys" ]; then
  AGYS_BIN="$REAL_HOME/go/bin/agys"
elif [ -x "/opt/homebrew/bin/agys" ]; then
  AGYS_BIN="/opt/homebrew/bin/agys"
elif [ -x "/usr/local/bin/agys" ]; then
  AGYS_BIN="/usr/local/bin/agys"
elif command -v agys >/dev/null 2>&1; then
  AGYS_BIN="$(command -v agys)"
fi

exec "$AGYS_BIN" herdr-hook "${1:-session}"
`

// ReadSettingsModel reads the "model" field from the newest settings.json across all product variants (cli, ide, antigravity).
func ReadSettingsModel(profileDir string) string {
	candidatePaths := []string{
		filepath.Join(profileDir, ".gemini", "antigravity-cli", "settings.json"),
		filepath.Join(profileDir, ".gemini", "antigravity", "settings.json"),
		filepath.Join(profileDir, ".gemini", "antigravity-ide", "settings.json"),
	}
	var latestModel string
	var latestMod time.Time
	for _, sPath := range candidatePaths {
		if info, err := os.Stat(sPath); err == nil {
			if info.ModTime().After(latestMod) {
				if data, err := os.ReadFile(sPath); err == nil {
					var settings struct {
						Model string `json:"model"`
					}
					if err := json.Unmarshal(data, &settings); err == nil && settings.Model != "" {
						latestModel = strings.TrimSpace(settings.Model)
						latestMod = info.ModTime()
					}
				}
			}
		}
	}
	return latestModel
}

// SyncModelToSettings updates the "model" field in settings.json to match the active model,
// preventing startup flicker where the previous model is shown for a few seconds.
func SyncModelToSettings(profileDir, modelName string) {
	if profileDir == "" || modelName == "" {
		return
	}
	candidatePaths := []string{
		filepath.Join(profileDir, ".gemini", "antigravity-cli", "settings.json"),
		filepath.Join(profileDir, ".gemini", "antigravity", "settings.json"),
		filepath.Join(profileDir, ".gemini", "antigravity-ide", "settings.json"),
	}
	for _, sPath := range candidatePaths {
		data, err := os.ReadFile(sPath)
		if err != nil {
			continue
		}
		var settings map[string]interface{}
		if err := json.Unmarshal(data, &settings); err != nil {
			continue
		}
		currModel, _ := settings["model"].(string)
		if NormalizeModelName(currModel) == NormalizeModelName(modelName) {
			continue
		}
		settings["model"] = modelName
		out, err := json.MarshalIndent(settings, "", "  ")
		if err == nil {
			_ = WriteFileAtomic(sPath, []byte(string(out)+"\n"), 0600)
		}
	}
}

// NormalizeModelName converts a display name (e.g. "Claude Opus 4.6 (Thinking)", "Gemini 2.5 Pro")
// into a lowercase, hyphenated API-style ID (e.g. "claude-opus-4", "gemini-2.5-pro").
// If the name is already in API format, it is returned as-is (lowercased).
func NormalizeModelName(displayName string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		return ""
	}

	// Strip parenthetical suffixes like "(Thinking)", "(Preview)", etc.
	if idx := strings.Index(name, "("); idx > 0 {
		name = strings.TrimSpace(name[:idx])
	}

	// Already looks like an API ID (lowercase with hyphens, no spaces)
	lower := strings.ToLower(name)
	if !strings.Contains(lower, " ") {
		return lower
	}

	// Convert "Claude Opus 4.6" -> "claude-opus-4.6" -> "claude-opus-4"
	// Convert "Gemini 2.5 Pro" -> "gemini-2.5-pro"
	parts := strings.Fields(lower)
	result := strings.Join(parts, "-")

	// Trim trailing minor version from model names (e.g. "4.6" -> "4") for cleaner matching
	// but keep major.minor for models like "2.5" where it's part of the identity
	// Heuristic: if the last part is a version like X.Y where X >= 3, drop .Y
	if len(parts) > 0 {
		last := parts[len(parts)-1]
		if dotIdx := strings.Index(last, "."); dotIdx > 0 {
			major := last[:dotIdx]
			if len(major) > 0 && major[0] >= '3' && major[0] <= '9' {
				result = strings.Join(parts[:len(parts)-1], "-") + "-" + major
			}
		}
	}

	return result
}

// ResolveActiveModel returns the active model for a profile: explicit arg > .active_model > settings.json > default latest Gemini.
func ResolveActiveModel(profileDir, modelName string) string {
	if modelName == "latest" || modelName == "auto" {
		return GetLatestGeminiModel()
	}
	if modelName != "" && modelName != "gemini" {
		return NormalizeModelName(modelName)
	}

	var cachedModel string
	if data, err := os.ReadFile(filepath.Join(profileDir, ".active_model")); err == nil {
		if m := strings.TrimSpace(string(data)); m != "" && m != "auto" && m != "latest" && m != "gemini" {
			cachedModel = NormalizeModelName(m)
		}
	}
	if cachedModel == "" {
		if sModel := ReadSettingsModel(profileDir); sModel != "" && sModel != "auto" && sModel != "latest" && sModel != "gemini" {
			cachedModel = NormalizeModelName(sModel)
		}
	}

	latestFlash := GetLatestGeminiModel()
	if cachedModel != "" && cachedModel != "gemini" {
		// If cached model is a Gemini Flash model (e.g. gemini-3.7-flash, gemini-3.6-flash),
		// check if it is older than the discovered latest Flash model.
		// If it is older, automatically upgrade it so profiles don't stay stuck on superseded models.
		if vCached, ok := ExtractGeminiVersion(cachedModel); ok && strings.Contains(cachedModel, "flash") {
			if vLatest, okLatest := ExtractGeminiVersion(latestFlash); okLatest {
				if vLatest.GreaterThan(vCached) {
					if profileDir != "" {
						_ = WriteFileAtomic(filepath.Join(profileDir, ".active_model"), []byte(latestFlash), 0600)
						SyncModelToSettings(profileDir, latestFlash)
					}
					return latestFlash
				}
			}
		}
		return cachedModel
	}

	return latestFlash
}

// resolveHerdrProfile resolves the active profile and its directory with pane and auto fallbacks.
func resolveHerdrProfile(ctx context.Context, profileName, paneID string, panes []HerdrRawPane) (string, string) {
	if !IsAuto(profileName) && profileName != "" {
		pDir, _ := GetProfileDir(profileName)
		return profileName, pDir
	}
	if paneID != "" && len(panes) > 0 {
		if paneProf := resolveProfileFromPaneList(panes, paneID); paneProf != "" {
			pDir, _ := GetProfileDir(paneProf)
			return paneProf, pDir
		}
	}
	if best, _, err := SelectBestProfile(ctx); err == nil && best != "" {
		pDir, _ := GetProfileDir(best)
		return best, pDir
	}
	return profileName, ""
}

// HandleHerdrHook executes the Herdr lifecycle hook directly in pure Go without any Python dependency.
func HandleHerdrHook(ctx context.Context, action string, stdin io.Reader) error {
	if !IsInHerdrEnvironment() {
		fmt.Println("{}")
		return nil
	}

	paneID := os.Getenv("HERDR_PANE_ID")
	socketPath := os.Getenv("HERDR_SOCKET_PATH")

	var panes []HerdrRawPane
	if action != "session" && socketPath != "" {
		panes = listHerdrPanes(ctx, socketPath)
	}

	// Herdr plugin events (e.g. pane.focused) are pane-global.
	// They can fire while another CLI such as Codex, Claude, or Droid owns the pane.
	// Only lifecycle/session hooks may run before Herdr has classified the pane; all
	// quota updates must never touch a pane that Herdr has identified as non-agys.
	if action != "session" && paneID != "" && len(panes) > 0 {
		for _, p := range panes {
			if p.PaneID == paneID && IsKnownNonAgysAgent(p.Agent) {
				// Herdr may still contain metadata written by an older agys binary before
				// pane ownership checks existed. Clear that stale telemetry once; otherwise
				// the Codex/Claude pane can continue displaying an old agys title/sidebar forever.
				_ = ClearHerdrMetadata(ctx)
				fmt.Println("{}")
				return nil
			}
		}
	}

	// Auto-ensure Herdr 2-row sidebar config is applied
	configPath := GetHerdrConfigPath()
	if !IsHerdrConfiguredForAgys(configPath) {
		_ = ApplyHerdr2RowConfig(configPath)
	}

	currentProfile, _ := ResolveProfileFromEnv()
	currentProfile, profileDir := resolveHerdrProfile(ctx, currentProfile, paneID, panes)
	activeModel := ResolveActiveModel(profileDir, "")

	if action == "session" && stdin != nil {
		var payload struct {
			ConversationID string `json:"conversationId"`
			TranscriptPath string `json:"transcriptPath"`
		}
		if err := json.NewDecoder(stdin).Decode(&payload); err == nil && payload.ConversationID != "" {
			seq := time.Now().UnixNano()
			params := map[string]interface{}{
				"pane_id":          paneID,
				"source":           "herdr:antigravity_cli",
				"agent":            "agy",
				"seq":              seq,
				"agent_session_id": payload.ConversationID,
			}
			if payload.TranscriptPath != "" {
				params["agent_session_path"] = payload.TranscriptPath
			}
			req, _ := json.Marshal(map[string]interface{}{
				"id":     fmt.Sprintf("herdr:antigravity_cli:%d", seq),
				"method": "pane.report_agent_session",
				"params": params,
			})
			sendHerdrSocketRPC(ctx, socketPath, req)

			// Proactively resolve conversation title and sync Herdr metadata immediately
			if profileDir != "" {
				resolvedTitle := ResolveConversationTitleFromTranscript(payload.TranscriptPath)
				if resolvedTitle == "" && payload.ConversationID != "" {
					resolvedTitle = ResolveConversationTitle(profileDir, payload.ConversationID)
				}
				if state, ok := GetSessionContextState(profileDir); ok && state != nil {
					if state.ConversationID != payload.ConversationID {
						state.ConversationID = payload.ConversationID
						state.ConversationTitle = resolvedTitle
						state.Cost = 0
						state.UsedPercentage = 0
						state.InputTokens = 0
						state.CacheReadTokens = 0
						state.CacheCreationTokens = 0
					} else if state.ConversationTitle == "" && resolvedTitle != "" {
						state.ConversationTitle = resolvedTitle
					}
					_ = SaveSessionContext(profileDir, state)
				} else {
					_ = SaveSessionContext(profileDir, &SessionContextState{
						ConversationTitle: resolvedTitle,
						ConversationID:    payload.ConversationID,
					})
				}
				reportCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				_ = ReportHerdrMetadataWithModel(reportCtx, currentProfile, activeModel)
				cancel()

				// Asynchronously trigger AI title summarization in background
				if resolvedTitle != "" && resolvedTitle != "(No prompt summary)" && paneID != "" {
					SpawnAsyncTitleSummarizer(currentProfile, paneID, payload.ConversationID, payload.TranscriptPath)
				}
			}
		}
	} else if action == "quota" || action == "stop" {
		if currentProfile != "" {
			quotaCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
			defer cancel()
			if action == "quota" {
				_ = ReportHerdrQuotaOnly(quotaCtx, currentProfile, activeModel)
			} else {
				_ = ReportHerdrMetadataWithModel(quotaCtx, currentProfile, activeModel)
			}
		}
	}

	fmt.Println("{}")
	return nil
}

// FormatModelAbbreviation converts a full model name or group name into a clean, compact 3-letter abbreviation.
// (e.g. "gemini-2.5-pro" -> "gem", "claude-3-7-sonnet" -> "cld", "gpt-4o" -> "gpt", "deepseek" -> "dsk").
func FormatModelAbbreviation(modelName, groupName string) string {
	m := strings.ToLower(strings.TrimSpace(modelName))
	g := strings.ToLower(strings.TrimSpace(groupName))

	// Split model name into tokens for precise matching (e.g. "gpt-4o" -> ["gpt","4o"])
	tokens := strings.FieldsFunc(m, func(r rune) bool {
		return r == '-' || r == '_' || r == '.' || r == '/'
	})
	hasToken := func(tok string) bool {
		for _, t := range tokens {
			if t == tok {
				return true
			}
		}
		return false
	}

	if strings.Contains(m, "claude") || strings.Contains(m, "sonnet") || strings.Contains(m, "opus") || strings.Contains(m, "haiku") || strings.Contains(m, "anthropic") {
		return "cld"
	}
	if strings.Contains(m, "gpt") || strings.Contains(m, "openai") || hasToken("o1") || hasToken("o3") || hasToken("o4") {
		return "gpt"
	}
	if strings.Contains(m, "deepseek") {
		return "dsk"
	}
	if strings.Contains(m, "qwen") {
		return "qwn"
	}
	if strings.Contains(m, "gemini") || hasToken("flash") || hasToken("pro") {
		return "gem"
	}

	// Fallback to groupName checks
	if strings.Contains(g, "claude") || strings.Contains(g, "3p") {
		return "cld"
	}
	if strings.Contains(g, "gemini") {
		return "gem"
	}
	if strings.Contains(g, "gpt") {
		return "gpt"
	}

	if m != "" && m != "auto" {
		if len(m) > 3 {
			return m[:3]
		}
		return m
	}
	return "gem"
}

// IsInHerdrEnvironment checks if the current process is actively executing inside a Herdr workspace/pane session.
func IsInHerdrEnvironment() bool {
	if os.Getenv("HERDR_SOCKET_PATH") != "" && os.Getenv("HERDR_PANE_ID") != "" {
		return true
	}
	return os.Getenv("HERDR_ENV") == "1" || os.Getenv("HERDR_ENV") == "true"
}

// SyncHerdrIntegration ensures that Herdr's antigravity integration hook is configured for the given profile directory.
// It ONLY executes when running inside an active Herdr environment to prevent modifying non-Herdr profiles.
func SyncHerdrIntegration(profileDir string) error {
	if !IsInHerdrEnvironment() {
		return nil
	}

	// Auto-ensure Herdr 2-row sidebar config is applied
	configPath := GetHerdrConfigPath()
	if !IsHerdrConfiguredForAgys(configPath) {
		_ = ApplyHerdr2RowConfig(configPath)
	}

	// Remove any shadowed legacy .config/herdr in profileDir to ensure global config is always respected
	_ = os.RemoveAll(filepath.Join(profileDir, ".config", "herdr"))

	hookDir := filepath.Join(profileDir, ".gemini", "config", "hooks")
	if err := os.MkdirAll(hookDir, 0700); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	hookFile := filepath.Join(hookDir, "herdr-agent-state.sh")

	// Write synchronized hook script if missing or updated
	scriptContent := []byte(defaultHerdrHookScript)
	if existingData, err := os.ReadFile(hookFile); err != nil || !bytes.Equal(existingData, scriptContent) {
		if err := WriteFileAtomic(hookFile, scriptContent, 0755); err != nil {
			return fmt.Errorf("failed to write herdr hook file: %w", err)
		}
		_ = os.Chmod(hookFile, 0755)
	}

	// Ensure hooks.json is configured with PreInvocation, PostInvocation, and Stop hooks
	hooksJSONPath := filepath.Join(profileDir, ".gemini", "config", "hooks.json")
	if err := ensureHooksJSON(hooksJSONPath, hookFile); err != nil {
		return err
	}

	// Ensure statusLine hook is configured in settings.json to capture real-time context window
	return SyncStatusLineSettings(profileDir)
}

func ensureHooksJSON(hooksJSONPath, hookScriptPath string) error {
	var hooksConfig map[string]interface{}

	data, err := os.ReadFile(hooksJSONPath)
	if err == nil {
		_ = json.Unmarshal(data, &hooksConfig)
	}
	if hooksConfig == nil {
		hooksConfig = make(map[string]interface{})
	}

	herdrMap, ok := hooksConfig["herdr"].(map[string]interface{})
	if !ok {
		herdrMap = make(map[string]interface{})
		hooksConfig["herdr"] = herdrMap
	}

	sessionCmd := "bash '$HOME/.gemini/config/hooks/herdr-agent-state.sh' session"
	quotaCmd := "bash '$HOME/.gemini/config/hooks/herdr-agent-state.sh' quota"

	herdrMap["PreInvocation"] = []interface{}{
		map[string]interface{}{
			"command": sessionCmd,
			"timeout": 10,
			"type":    "command",
		},
	}

	herdrMap["PostInvocation"] = []interface{}{
		map[string]interface{}{
			"command": quotaCmd,
			"timeout": 10,
			"type":    "command",
		},
	}

	herdrMap["Stop"] = []interface{}{
		map[string]interface{}{
			"command": quotaCmd,
			"timeout": 10,
			"type":    "command",
		},
	}

	updatedData, err := json.MarshalIndent(hooksConfig, "", "  ")
	if err != nil {
		return err
	}

	newBytes := []byte(string(updatedData) + "\n")
	if data != nil && bytes.Equal(data, newBytes) {
		return nil
	}

	return WriteFileAtomic(hooksJSONPath, newBytes, 0600)
}

func sendHerdrSocketRPC(ctx context.Context, socketPath string, data []byte) []byte {
	dialCtx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	var d net.Dialer
	conn, err := d.DialContext(dialCtx, "unix", socketPath)
	if err != nil {
		return nil
	}
	defer conn.Close()

	_ = conn.SetDeadline(time.Now().Add(1 * time.Second))
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return nil
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil
	}
	return bytes.TrimSpace(line)
}

// IsAgysAgent returns true ONLY if the agent name strictly identifies Antigravity / agys.
func IsAgysAgent(agent string) bool {
	a := strings.ToLower(strings.TrimSpace(agent))
	if a == "" {
		return false
	}
	return a == "agy" || a == "agys" || a == "antigravity" ||
		strings.HasPrefix(a, "agy") ||
		strings.HasPrefix(a, "herdr:antigravity") ||
		strings.HasPrefix(a, "herdr:agy") ||
		strings.HasPrefix(a, "antigravity")
}

// Known competing AI agents that might run in a terminal pane.
var knownNonAgysAgents = []string{
	"claude",
	"codex",
	"droid",
	"aider",
	"opencode",
	"copilot",
	"cursor",
	"herdr:claude",
	"herdr:codex",
	"herdr:droid",
	"herdr:aider",
	"herdr:opencode",
	"herdr:copilot",
	"herdr:cursor",
}

// IsKnownNonAgysAgent checks if the pane is actively running another known AI agent/CLI tool (e.g., claude, codex, droid).
func IsKnownNonAgysAgent(agent string) bool {
	a := strings.ToLower(strings.TrimSpace(agent))
	if a == "" || IsAgysAgent(a) {
		return false
	}
	for _, known := range knownNonAgysAgents {
		if a == known || strings.HasPrefix(a, known) {
			return true
		}
	}
	return false
}

// IsNonAgysAgent checks if the pane is actively running another known AI agent/CLI tool (e.g., claude, codex, droid).
func IsNonAgysAgent(agent string) bool {
	return IsKnownNonAgysAgent(agent)
}

// isPaneActiveAgys verifies if a pane is genuinely and actively executing an agys session.
// Strictly returns false for competing non-agys AI agents (Codex/Claude/Droid) and plain shells without agys titles.
func isPaneActiveAgys(agent, title, terminalTitle, terminalTitleStripped string, tokens ...map[string]string) bool {
	if IsKnownNonAgysAgent(agent) {
		return false
	}
	if IsAgysAgent(agent) {
		return true
	}
	termTitle := terminalTitle
	if termTitle == "" {
		termTitle = terminalTitleStripped
	}
	// Active agys sessions always set the terminal title or reported title starting with "agys" or containing "agys [" / "agys: "
	if strings.HasPrefix(termTitle, "agys") || strings.Contains(termTitle, "agys: ") || strings.Contains(termTitle, "agys [") {
		return true
	}
	if strings.HasPrefix(title, "agys") || strings.Contains(title, "agys: ") || strings.Contains(title, "agys [") {
		return true
	}
	return false
}

// HerdrRawPane represents raw pane telemetry returned by Herdr's pane.list RPC.
type HerdrRawPane struct {
	PaneID                string            `json:"pane_id"`
	Agent                 string            `json:"agent"`
	DisplayAgent          string            `json:"display_agent"`
	Title                 string            `json:"title"`
	TerminalTitle         string            `json:"terminal_title"`
	TerminalTitleStripped string            `json:"terminal_title_stripped"`
	Tokens                map[string]string `json:"tokens"`
}

// listHerdrPanes queries Herdr's UNIX socket via pane.list RPC to retrieve all active panes.
func listHerdrPanes(ctx context.Context, socketPath string) []HerdrRawPane {
	if socketPath == "" {
		return nil
	}
	req, _ := json.Marshal(map[string]interface{}{
		"id":     fmt.Sprintf("agys:panes:%d", time.Now().UnixNano()),
		"method": "pane.list",
		"params": map[string]interface{}{},
	})
	resp := sendHerdrSocketRPC(ctx, socketPath, req)
	if len(resp) == 0 {
		return nil
	}

	var parsed struct {
		Result struct {
			Panes []HerdrRawPane `json:"panes"`
		} `json:"result"`
	}
	if err := json.Unmarshal(resp, &parsed); err != nil {
		return nil
	}
	return parsed.Result.Panes
}

var (
	quotaTierKeys = []string{
		"quota_5h_normal", "quota_5h_warning", "quota_5h_danger",
		"quota_week_normal", "quota_week_warning", "quota_week_danger",
	}

	agysHerdrTokenKeys = []string{
		"profile", "model", "group", "conversation_title", "cost",
		"quota_context", "quota_model_context",
		"quota_5h_normal", "quota_5h_warning", "quota_5h_danger",
		"quota_week_normal", "quota_week_warning", "quota_week_danger",
	}
)

// HerdrPaneMatch represents a matched Herdr pane along with its specific active model and existing tokens.
type HerdrPaneMatch struct {
	PaneID            string
	Model             string
	DisplayAgent      string
	QuotaModelContext string
	QuotaContext      string
	Title             string
	Tokens            map[string]string
}

func createPaneMatch(p HerdrRawPane, profileName, currentModel string) HerdrPaneMatch {
	disp := p.DisplayAgent
	if disp == "" {
		disp = profileName
	}
	model := currentModel
	var quotaModelContext, quotaContext string
	if p.Tokens != nil {
		if p.Tokens["model"] != "" && (currentModel == "" || currentModel == "auto") {
			model = p.Tokens["model"]
		}
		quotaModelContext = p.Tokens["quota_model_context"]
		quotaContext = p.Tokens["quota_context"]
	}
	return HerdrPaneMatch{
		PaneID:            p.PaneID,
		Model:             model,
		DisplayAgent:      disp,
		QuotaModelContext: quotaModelContext,
		QuotaContext:      quotaContext,
		Title:             p.Title,
		Tokens:            p.Tokens,
	}
}

func getHerdrCurrentPaneFromList(panes []HerdrRawPane, paneID, profileName, currentModel string) HerdrPaneMatch {
	for _, p := range panes {
		if p.PaneID == paneID {
			return createPaneMatch(p, profileName, currentModel)
		}
	}
	return HerdrPaneMatch{
		PaneID:       paneID,
		Model:        currentModel,
		DisplayAgent: profileName,
	}
}

func getMatchingHerdrPanesFromList(ctx context.Context, panes []HerdrRawPane, socketPath, currentPaneID, profileName, currentModel string) []HerdrPaneMatch {
	if len(panes) == 0 {
		if currentPaneID != "" {
			return []HerdrPaneMatch{{PaneID: currentPaneID, Model: currentModel, DisplayAgent: profileName}}
		}
		return nil
	}

	targetPanes := []HerdrPaneMatch{}
	seen := make(map[string]bool)

	for _, p := range panes {
		if p.PaneID == "" {
			continue
		}

		// Retire stale agys metadata from non-agys agents (e.g. Codex, Claude)
		if p.Agent != "" && IsNonAgysAgent(p.Agent) && hasStaleAgysTelemetry(p.Title, p.Tokens) {
			_ = clearHerdrPaneMetadata(ctx, socketPath, p.PaneID)
			continue
		}

		// Strictly allow ONLY panes actively executing agys/Antigravity
		if !isPaneActiveAgys(p.Agent, p.Title, p.TerminalTitle, p.TerminalTitleStripped, p.Tokens) {
			continue
		}

		isMatch := false
		if currentPaneID != "" && p.PaneID == currentPaneID {
			isMatch = true
		} else if p.Tokens != nil && p.Tokens["profile"] == profileName {
			isMatch = true
		} else if p.DisplayAgent == profileName || p.Agent == profileName {
			isMatch = true
		}

		if isMatch && !seen[p.PaneID] {
			targetPanes = append(targetPanes, createPaneMatch(p, profileName, currentModel))
			seen[p.PaneID] = true
		}
	}

	return targetPanes
}

func setQuotaTierTokens(tokens map[string]string, prefix, quotaStr string, pct int) {
	tokens[prefix+"_normal"] = ""
	tokens[prefix+"_warning"] = ""
	tokens[prefix+"_danger"] = ""
	if quotaStr == "" {
		return
	}
	if pct >= 20 {
		tokens[prefix+"_normal"] = quotaStr
	} else if pct > 5 {
		tokens[prefix+"_warning"] = quotaStr
	} else {
		tokens[prefix+"_danger"] = quotaStr
	}
}

func getMatchingHerdrPanes(ctx context.Context, socketPath, currentPaneID, profileName, currentModel string) []HerdrPaneMatch {
	panes := listHerdrPanes(ctx, socketPath)
	return getMatchingHerdrPanesFromList(ctx, panes, socketPath, currentPaneID, profileName, currentModel)
}

func resolveProfileFromPaneList(panes []HerdrRawPane, paneID string) string {
	for _, p := range panes {
		if p.PaneID == paneID {
			if !isPaneActiveAgys(p.Agent, p.Title, p.TerminalTitle, p.TerminalTitleStripped) {
				return ""
			}
			title := p.TerminalTitle
			if title == "" {
				title = p.TerminalTitleStripped
			}
			if strings.HasPrefix(title, "agys: ") {
				parts := strings.Fields(strings.TrimPrefix(title, "agys: "))
				if len(parts) > 0 && !IsAuto(parts[0]) {
					if _, err := GetProfileDir(parts[0]); err == nil {
						return parts[0]
					}
				}
			}
			if p.Tokens != nil && p.Tokens["profile"] != "" && !IsAuto(p.Tokens["profile"]) {
				if _, err := GetProfileDir(p.Tokens["profile"]); err == nil {
					return p.Tokens["profile"]
				}
			}
			if p.DisplayAgent != "" && !IsAuto(p.DisplayAgent) {
				if _, err := GetProfileDir(p.DisplayAgent); err == nil {
					return p.DisplayAgent
				}
			}
			break
		}
	}
	return ""
}

func resolveProfileFromPane(ctx context.Context, socketPath, paneID string) string {
	if socketPath == "" || paneID == "" {
		return ""
	}
	panes := listHerdrPanes(ctx, socketPath)
	return resolveProfileFromPaneList(panes, paneID)
}

func lookupPaneAgentStateFromList(panes []HerdrRawPane, paneID string) (found, active bool) {
	for _, pane := range panes {
		if pane.PaneID == paneID {
			return true, isPaneActiveAgys(
				pane.Agent,
				pane.Title,
				pane.TerminalTitle,
				pane.TerminalTitleStripped,
				pane.Tokens,
			)
		}
	}
	return false, false
}

// ReportHerdrMetadata communicates with Herdr via its UNIX domain socket to set display_agent, title, and quota for all matching panes.
func ReportHerdrMetadata(ctx context.Context, profileName string) error {
	return reportHerdrMetadataInternal(ctx, profileName, "", false)
}

// ReportHerdrMetadataWithModel communicates with Herdr via its UNIX domain socket to set metadata including live context window metrics.
func ReportHerdrMetadataWithModel(ctx context.Context, profileName, modelName string, preloadedDetails ...*ModelQuotaDetails) error {
	return reportHerdrMetadataInternal(ctx, profileName, modelName, false, preloadedDetails...)
}

// ReportHerdrQuotaOnly communicates with Herdr via its UNIX domain socket to update ONLY quota metrics (5H & Weekly) and reset countdowns,
// explicitly preserving existing context window tokens and title state to prevent conflicts with live turn-by-turn stream hooks.
func ReportHerdrQuotaOnly(ctx context.Context, profileName, modelName string) error {
	return reportHerdrMetadataInternal(ctx, profileName, modelName, true)
}

func reportHerdrMetadataInternal(ctx context.Context, profileName, modelName string, isQuotaOnly bool, preloadedDetails ...*ModelQuotaDetails) error {
	if !IsInHerdrEnvironment() {
		return nil
	}
	paneID := os.Getenv("HERDR_PANE_ID")
	socketPath := os.Getenv("HERDR_SOCKET_PATH")

	quotaCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	var panes []HerdrRawPane
	if socketPath != "" {
		panes = listHerdrPanes(quotaCtx, socketPath)
	}

	// Inline statusline and quota reports are pane-local. If another CLI owns the
	// pane, agys must neither overwrite its sidebar row nor leave stale telemetry.
	if paneID != "" && socketPath != "" && len(panes) > 0 {
		for _, p := range panes {
			if p.PaneID == paneID && IsKnownNonAgysAgent(p.Agent) {
				_ = clearHerdrPaneMetadata(ctx, socketPath, paneID)
				return nil
			}
		}
	}

	profileName, pDir := resolveHerdrProfile(quotaCtx, profileName, paneID, panes)

	// Resolve explicit active model if provided; otherwise let each target pane
	// preserve its own active model token before falling back to profile default.
	if modelName != "" {
		if pDir != "" {
			modelName = ResolveActiveModel(pDir, modelName)
		} else {
			modelName = NormalizeModelName(modelName)
		}
	}

	var targetPanes []HerdrPaneMatch
	if paneID != "" {
		// When inside a specific pane, update ONLY this pane to prevent cross-pane contamination or overwriting other agents.
		targetPanes = []HerdrPaneMatch{
			getHerdrCurrentPaneFromList(panes, paneID, profileName, modelName),
		}
	} else {
		targetPanes = getMatchingHerdrPanesFromList(quotaCtx, panes, socketPath, paneID, profileName, modelName)
	}

	for _, target := range targetPanes {
		if target.PaneID == "" {
			continue
		}

		targetModel := modelName
		if targetModel == "" || targetModel == "auto" {
			targetModel = target.Model
		}
		if targetModel == "" || targetModel == "auto" || targetModel == "gemini" {
			if pDir != "" {
				targetModel = ResolveActiveModel(pDir, "")
			} else {
				targetModel = GetLatestGeminiModel()
			}
		}

		// Load session context state for this pane (context %, conversation title, and cost)
		var sessionState *SessionContextState
		var ctxPct int
		var hasCtx bool
		if pDir != "" {
			sessionState, _ = GetSessionContextStateForPane(pDir, target.PaneID)
			if sessionState != nil {
				if time.Since(sessionState.UpdatedAt) <= 2*time.Hour {
					ctxPct = int(sessionState.UsedPercentage + 0.5)
					hasCtx = true
				}
			}
		}

		convTitle := ""
		costVal := 0.0
		if sessionState != nil {
			convTitle = sessionState.ConversationTitle
			costVal = sessionState.Cost
		}

		// If convTitle is empty, strictly preserve existing conversation title from target.Tokens
		// during periodic quota updates or whenever an active session state exists for this pane.
		// (Fresh sessions without a session state and !isQuotaOnly will clear stale titles from prior sessions).
		if convTitle == "" && (isQuotaOnly || sessionState != nil) {
			if target.Tokens != nil && target.Tokens["conversation_title"] != "" {
				convTitle = target.Tokens["conversation_title"]
			}
		}

		isCurrentPane := paneID != "" && target.PaneID == paneID

		displayAgent := profileName
		if !isCurrentPane && target.DisplayAgent != "" {
			displayAgent = target.DisplayAgent
		}

		title := target.Title
		if isCurrentPane {
			if convTitle != "" {
				if strings.HasPrefix(convTitle, "agys") {
					title = convTitle
				} else {
					title = fmt.Sprintf("agys: %s", convTitle)
				}
			} else if isQuotaOnly && target.Title != "" {
				title = target.Title
			} else {
				title = fmt.Sprintf("agys: %s", profileName)
			}
		} else if title == "" {
			title = fmt.Sprintf("agys: %s", profileName)
		}

		tokens := map[string]string{
			"profile": profileName,
		}
		if targetModel != "" {
			tokens["model"] = targetModel
		}

		// Handle Row 2: Conversation Title (replaces context window & cost on sidebar)
		if isCurrentPane {
			tokens["conversation_title"] = convTitle
			tokens["quota_model_context"] = convTitle

			// Context window and cost metrics remain available in tokens
			if hasCtx {
				tokens["quota_context"] = fmt.Sprintf("ctx %d%%", ctxPct)
			} else if (isQuotaOnly || sessionState != nil) && target.Tokens != nil && target.Tokens["quota_context"] != "" {
				tokens["quota_context"] = target.Tokens["quota_context"]
			} else {
				tokens["quota_context"] = ""
			}

			if costVal > 0 {
				tokens["cost"] = FormatCost(costVal)
			} else if (isQuotaOnly || sessionState != nil) && target.Tokens != nil && target.Tokens["cost"] != "" {
				tokens["cost"] = target.Tokens["cost"]
			} else {
				tokens["cost"] = ""
			}
		} else {
			// For other panes: strictly preserve their own conversation_title and context metrics!
			if target.Tokens != nil {
				tokens["conversation_title"] = target.Tokens["conversation_title"]
				tokens["quota_model_context"] = target.Tokens["quota_model_context"]
				tokens["quota_context"] = target.Tokens["quota_context"]
				tokens["cost"] = target.Tokens["cost"]
			}
		}

		var details *ModelQuotaDetails
		var err error
		if len(preloadedDetails) > 0 && preloadedDetails[0] != nil {
			details = preloadedDetails[0]
		} else {
			details, err = GetProfileFullQuotaDetailsForModel(quotaCtx, profileName, targetModel)
		}
		if err == nil && details != nil && details.Fraction5H >= 0 {
			pct5h := int(details.Fraction5H*100 + 0.5)
			quota5hStr := fmt.Sprintf("%d%%", pct5h)
			if details.CompactReset5H != "" {
				quota5hStr = fmt.Sprintf("%s %s", quota5hStr, details.CompactReset5H)
			}
			if details.GroupName != "" {
				tokens["group"] = details.GroupName
			}
			setQuotaTierTokens(tokens, "quota_5h", quota5hStr, pct5h)

			if details.FractionWeekly >= 0 {
				pctWk := int(details.FractionWeekly*100 + 0.5)
				quotaWkStr := fmt.Sprintf("%d%%", pctWk)
				if details.CompactResetWeekly != "" {
					quotaWkStr = fmt.Sprintf("%s %s", quotaWkStr, details.CompactResetWeekly)
				}
				setQuotaTierTokens(tokens, "quota_week", quotaWkStr, pctWk)
			} else {
				setQuotaTierTokens(tokens, "quota_week", "", 0)
			}
		} else {
			// Details unavailable: preserve existing quota tokens from pane if available so Row 3 does not vanish and flicker
			hasExistingQuota := false
			if target.Tokens != nil {
				for _, k := range quotaTierKeys {
					if target.Tokens[k] != "" {
						hasExistingQuota = true
						tokens[k] = target.Tokens[k]
					}
				}
				if target.Tokens["group"] != "" {
					tokens["group"] = target.Tokens["group"]
				}
			}
			if !hasExistingQuota {
				setQuotaTierTokens(tokens, "quota_5h", "", 0)
				setQuotaTierTokens(tokens, "quota_week", "", 0)
			}
		}

		if target.PaneID == paneID {
			SetTerminalTitle(title)
		}

		if isPaneMetadataUnchanged(target, displayAgent, title, tokens) {
			continue
		}

		payload := map[string]interface{}{
			"id":     fmt.Sprintf("agys:metadata:%d", time.Now().UnixNano()),
			"method": "pane.report_metadata",
			"params": map[string]interface{}{
				"pane_id":       target.PaneID,
				"source":        "agys",
				"display_agent": displayAgent,
				"title":         title,
				"tokens":        tokens,
			},
		}

		data, err := json.Marshal(payload)
		if err != nil {
			continue
		}

		sendHerdrSocketRPC(ctx, socketPath, data)
	}

	return nil
}

func isPaneMetadataUnchanged(target HerdrPaneMatch, newDisplayAgent, newTitle string, newTokens map[string]string) bool {
	if target.Tokens == nil {
		return false
	}
	if target.DisplayAgent != newDisplayAgent {
		return false
	}
	if target.Title != newTitle {
		return false
	}
	for k, v := range newTokens {
		if target.Tokens[k] != v {
			return false
		}
	}
	for k, oldVal := range target.Tokens {
		if (strings.HasPrefix(k, "quota_") || k == "profile" || k == "model" || k == "group" || k == "conversation_title" || k == "cost") && oldVal != "" {
			if newTokens[k] != oldVal {
				return false
			}
		}
	}
	return true
}

var (
	lastTerminalTitleMu sync.Mutex
	lastTerminalTitle   string
)

// SetTerminalTitle sets the terminal/window/tab title using ANSI OSC escape sequence ONLY when inside Herdr.
func SetTerminalTitle(titleOrProfile string) {
	if !IsInHerdrEnvironment() || titleOrProfile == "" {
		return
	}
	title := titleOrProfile
	if !strings.HasPrefix(title, "agys") {
		title = fmt.Sprintf("agys [%s]", titleOrProfile)
	}
	// Sanitize title to prevent ANSI escape / OSC sequence injection (CWE-150 / CWE-116)
	title = sanitizeTerminalTitle(title)
	if title == "" {
		return
	}

	lastTerminalTitleMu.Lock()
	if lastTerminalTitle == title {
		lastTerminalTitleMu.Unlock()
		return
	}
	lastTerminalTitle = title
	lastTerminalTitleMu.Unlock()

	fmt.Fprintf(os.Stderr, "\033]0;%s\007", title)
}

func sanitizeTerminalTitle(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		// Strip control characters (< 0x20 and DEL 0x7F), including \033, \007, \r, \n
		if r < 0x20 || r == 0x7f {
			b.WriteByte(' ')
		} else {
			b.WriteRune(r)
		}
	}
	cleaned := strings.TrimSpace(b.String())
	for strings.Contains(cleaned, "  ") {
		cleaned = strings.ReplaceAll(cleaned, "  ", " ")
	}
	if len(cleaned) > 100 {
		cleaned = strings.TrimSpace(cleaned[:97]) + "..."
	}
	return cleaned
}

// ResetTerminalTitle resets the terminal/window/tab title back to default shell title.
func ResetTerminalTitle() {
	if !IsInHerdrEnvironment() {
		return
	}
	lastTerminalTitleMu.Lock()
	lastTerminalTitle = ""
	lastTerminalTitleMu.Unlock()

	fmt.Fprintf(os.Stderr, "\033]0;\007")
}

// ClearHerdrMetadata clears agys tokens and resets terminal title for the current pane upon session exit.
func ClearHerdrMetadata(ctx context.Context) error {
	if !IsInHerdrEnvironment() {
		return nil
	}
	paneID := os.Getenv("HERDR_PANE_ID")
	socketPath := os.Getenv("HERDR_SOCKET_PATH")
	if paneID == "" || socketPath == "" {
		return nil
	}

	ResetTerminalTitle()
	return clearHerdrPaneMetadata(ctx, socketPath, paneID)
}

// hasStaleAgysTelemetry reports whether a pane carries agys-owned display data.
func hasStaleAgysTelemetry(title string, tokens map[string]string) bool {
	if strings.HasPrefix(title, "agys") || strings.Contains(title, "agys: ") || strings.Contains(title, "agys [") {
		return true
	}
	for _, key := range agysHerdrTokenKeys {
		if tokens[key] != "" {
			return true
		}
	}
	return false
}

// clearHerdrPaneMetadata removes agys-owned display-only pane metadata. Herdr's
// metadata API requires explicit clear flags (and null token values); empty
// strings are treated as no-op updates by newer Herdr versions.
func clearHerdrPaneMetadata(ctx context.Context, socketPath, paneID string) error {
	if socketPath == "" || paneID == "" {
		return nil
	}

	clearTokens := make(map[string]*string, len(agysHerdrTokenKeys))
	for _, key := range agysHerdrTokenKeys {
		clearTokens[key] = nil
	}

	payload := map[string]interface{}{
		"id":     fmt.Sprintf("agys:clear_meta:%d", time.Now().UnixNano()),
		"method": "pane.report_metadata",
		"params": map[string]interface{}{
			"pane_id":             paneID,
			"source":              "agys",
			"applies_to_source":   "agys",
			"clear_title":         true,
			"clear_display_agent": true,
			"clear_state_labels":  true,
			"tokens":              clearTokens,
		},
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	sendHerdrSocketRPC(ctx, socketPath, data)
	return nil
}

// StartHerdrQuotaWatcher starts a background goroutine that periodically updates quota every 60 seconds.
// One leader per profile (via OS file lock). Returns a cleanup function to stop the watcher on session exit.
func StartHerdrQuotaWatcher(ctx context.Context, profileName string, modelName ...string) func() {
	if !IsInHerdrEnvironment() {
		return func() {}
	}

	pDir, err := GetProfileDir(profileName)
	if err != nil {
		return func() {}
	}

	lockFile := filepath.Join(pDir, ".quota_watcher.lock")
	fileLock := flock.New(lockFile)

	watchCtx, cancel := context.WithCancel(ctx)

	go func() {
		locked, err := fileLock.TryLock()
		if err != nil || !locked {
			// Another pane for this profile is already the active watcher leader
			return
		}
		defer func() {
			_ = fileLock.Unlock()
		}()

		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-watchCtx.Done():
				return
			case <-ticker.C:
				currentModel := ResolveActiveModel(pDir, "")
				reportCtx, reportCancel := context.WithTimeout(watchCtx, 6*time.Second)
				_ = ReportHerdrQuotaOnly(reportCtx, profileName, currentModel)
				reportCancel()
			}
		}
	}()

	return func() {
		cancel()
	}
}

// SpawnAsyncTitleSummarizer launches a detached background worker to generate an AI title via LLM.
func SpawnAsyncTitleSummarizer(profileName, paneID, convID, transcriptPath string) {
	exe, err := os.Executable()
	if err != nil || exe == "" {
		return
	}

	args := []string{
		"herdr-hook", "summarize",
		"--profile", profileName,
		"--pane", paneID,
		"--conv", convID,
		"--transcript", transcriptPath,
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	_ = cmd.Start()
}

// HandleHerdrSummarize generates an AI title using the latest Gemini Flash model and updates Herdr metadata.
func HandleHerdrSummarize(ctx context.Context, profileName, paneID, convID, transcriptPath string) error {
	if profileName == "" || paneID == "" || convID == "" {
		return nil
	}

	pDir, err := GetProfileDir(profileName)
	if err != nil || pDir == "" {
		return nil
	}

	origPaneID := os.Getenv("HERDR_PANE_ID")
	defer func() {
		if origPaneID == "" {
			_ = os.Unsetenv("HERDR_PANE_ID")
		} else {
			_ = os.Setenv("HERDR_PANE_ID", origPaneID)
		}
	}()
	os.Setenv("HERDR_PANE_ID", paneID)

	prompt := ResolveConversationTitleFromTranscript(transcriptPath)
	if prompt == "" {
		prompt = ResolveConversationTitle(pDir, convID)
	}
	if prompt == "" || prompt == "(No prompt summary)" {
		return nil
	}

	sumCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	flashModel := GetLatestGeminiModel()
	extraArgs := []string{"--model", flashModel}
	if ModelSupportsEffort(flashModel) {
		extraArgs = append(extraArgs, "--effort", "low")
	}

	llmPrompt := fmt.Sprintf("[AGYS_INTERNAL_TITLE_GEN] Summarize into 3 to 5 words in the same language, no quotes, no markdown, no punctuation: %s", prompt)
	outStr, err := ExecAgyPrompt(sumCtx, pDir, llmPrompt, extraArgs...)
	if err != nil || strings.TrimSpace(outStr) == "" {
		return nil
	}

	llmTitle := strings.TrimSpace(outStr)
	llmTitle = strings.Trim(llmTitle, "\"`'*“”‘’")
	llmTitle = strings.TrimRight(llmTitle, ".!?")
	llmTitle = strings.Join(strings.Fields(llmTitle), " ")
	runes := []rune(llmTitle)
	if len(runes) > 0 {
		runes[0] = unicode.ToUpper(runes[0])
		llmTitle = string(runes)
	}
	llmTitle = TruncateTitle(llmTitle, 45)

	// Verify pane context is still on the same conversation
	state, ok := GetSessionContextStateForPane(pDir, paneID)
	if !ok || state == nil {
		return nil
	}
	if state.ConversationID != "" && state.ConversationID != convID {
		return nil
	}

	if state.ConversationTitle == llmTitle {
		return nil
	}

	state.ConversationTitle = llmTitle
	_ = SaveSessionContextForPane(pDir, paneID, state)

	activeModel := ResolveActiveModel(pDir, "")
	reportCtx, reportCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer reportCancel()
	return ReportHerdrMetadataWithModel(reportCtx, profileName, activeModel)
}
