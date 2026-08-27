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
	"path/filepath"
	"strings"
	"time"

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

[ "${HERDR_ENV:-}" = "1" ] || emit_and_exit
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
if command -v agys >/dev/null 2>&1; then
  AGYS_BIN="$(command -v agys)"
elif [ -x "$REAL_HOME/.local/bin/agys" ]; then
  AGYS_BIN="$REAL_HOME/.local/bin/agys"
elif [ -x "$REAL_HOME/go/bin/agys" ]; then
  AGYS_BIN="$REAL_HOME/go/bin/agys"
elif [ -x "/opt/homebrew/bin/agys" ]; then
  AGYS_BIN="/opt/homebrew/bin/agys"
elif [ -x "/usr/local/bin/agys" ]; then
  AGYS_BIN="/usr/local/bin/agys"
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

// ResolveActiveModel returns the active model for a profile: explicit arg > .active_model > settings.json > default "gemini".
func ResolveActiveModel(profileDir, modelName string) string {
	if modelName != "" && modelName != "auto" {
		return NormalizeModelName(modelName)
	}
	if data, err := os.ReadFile(filepath.Join(profileDir, ".active_model")); err == nil {
		if m := strings.TrimSpace(string(data)); m != "" && m != "auto" {
			return NormalizeModelName(m)
		}
	}
	if sModel := ReadSettingsModel(profileDir); sModel != "" && sModel != "auto" {
		return NormalizeModelName(sModel)
	}
	return "gemini"
}

// HandleHerdrHook executes the Herdr lifecycle hook directly in pure Go without any Python dependency.
func HandleHerdrHook(ctx context.Context, action string, stdin io.Reader) error {
	if !IsInHerdrEnvironment() {
		fmt.Println("{}")
		return nil
	}

	// Auto-ensure Herdr 2-row sidebar config is applied
	configPath := GetHerdrConfigPath()
	if !IsHerdrConfiguredForAgys(configPath) {
		_ = ApplyHerdr2RowConfig(configPath)
	}

	paneID := os.Getenv("HERDR_PANE_ID")
	socketPath := os.Getenv("HERDR_SOCKET_PATH")

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
		}
	}

	fmt.Println("{}")
	return nil
}

// ToSuperscriptPercent converts an integer percentage into a compact unicode superscript string (e.g. 87 -> "⁸⁷%").
func ToSuperscriptPercent(pct int) string {
	superscripts := map[rune]rune{
		'0': '⁰', '1': '¹', '2': '²', '3': '³', '4': '⁴',
		'5': '⁵', '6': '⁶', '7': '⁷', '8': '⁸', '9': '⁹',
	}
	s := fmt.Sprintf("%d", pct)
	var out []rune
	for _, r := range s {
		if sup, ok := superscripts[r]; ok {
			out = append(out, sup)
		} else {
			out = append(out, r)
		}
	}
	return string(out) + "%"
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
	return os.Getenv("HERDR_ENV") == "1" && os.Getenv("HERDR_SOCKET_PATH") != "" && os.Getenv("HERDR_PANE_ID") != ""
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

// HerdrPaneMatch represents a matched Herdr pane along with its specific active model and existing tokens.
type HerdrPaneMatch struct {
	PaneID            string
	Model             string
	QuotaModelContext string
	QuotaContext      string
	Title             string
}

func getMatchingHerdrPanes(ctx context.Context, socketPath, currentPaneID, profileName, currentModel string) []HerdrPaneMatch {
	targetPanes := []HerdrPaneMatch{}
	seen := make(map[string]bool)
	if currentPaneID != "" {
		targetPanes = append(targetPanes, HerdrPaneMatch{PaneID: currentPaneID, Model: currentModel})
		seen[currentPaneID] = true
	}
	req, _ := json.Marshal(map[string]interface{}{
		"id":     fmt.Sprintf("agys:panes:%d", time.Now().UnixNano()),
		"method": "pane.list",
		"params": map[string]interface{}{},
	})

	resp := sendHerdrSocketRPC(ctx, socketPath, req)
	if len(resp) == 0 {
		return targetPanes
	}

	var parsed struct {
		Result struct {
			Panes []struct {
				PaneID       string            `json:"pane_id"`
				Agent        string            `json:"agent"`
				DisplayAgent string            `json:"display_agent"`
				Title        string            `json:"title"`
				Tokens       map[string]string `json:"tokens"`
			} `json:"panes"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp, &parsed); err == nil && len(parsed.Result.Panes) > 0 {
		for _, p := range parsed.Result.Panes {
			if p.PaneID == "" {
				continue
			}
			isMatch := false
			paneModel := ""
			var quotaModelContext, quotaContext string
			if p.Tokens != nil {
				if p.Tokens["profile"] == profileName {
					isMatch = true
				}
				paneModel = p.Tokens["model"]
				quotaModelContext = p.Tokens["quota_model_context"]
				quotaContext = p.Tokens["quota_context"]
			}
			if !isMatch {
				if strings.Contains(p.DisplayAgent, fmt.Sprintf("[%s:", profileName)) || strings.Contains(p.DisplayAgent, fmt.Sprintf("[%s]", profileName)) || strings.Contains(p.DisplayAgent, fmt.Sprintf("[%s ", profileName)) {
					isMatch = true
				} else if strings.Contains(p.Title, fmt.Sprintf("agys: %s", profileName)) {
					isMatch = true
				}
			}
			if isMatch {
				if !seen[p.PaneID] {
					targetPanes = append(targetPanes, HerdrPaneMatch{
						PaneID:            p.PaneID,
						Model:             paneModel,
						QuotaModelContext: quotaModelContext,
						QuotaContext:      quotaContext,
						Title:             p.Title,
					})
					seen[p.PaneID] = true
				} else {
					for i := range targetPanes {
						if targetPanes[i].PaneID == p.PaneID {
							if targetPanes[i].QuotaModelContext == "" {
								targetPanes[i].QuotaModelContext = quotaModelContext
							}
							if targetPanes[i].QuotaContext == "" {
								targetPanes[i].QuotaContext = quotaContext
							}
							if targetPanes[i].Title == "" {
								targetPanes[i].Title = p.Title
							}
						}
					}
				}
			}
		}
	}

	return targetPanes
}

// ReportHerdrMetadata communicates with Herdr via its UNIX domain socket to set display_agent, title, and quota for all matching panes.
func ReportHerdrMetadata(ctx context.Context, profileName string) error {
	return reportHerdrMetadataInternal(ctx, profileName, "", true)
}

// ReportHerdrMetadataWithModel communicates with Herdr via its UNIX domain socket to set metadata including live context window metrics.
func ReportHerdrMetadataWithModel(ctx context.Context, profileName, modelName string) error {
	return reportHerdrMetadataInternal(ctx, profileName, modelName, true)
}

// ReportHerdrQuotaOnly communicates with Herdr via its UNIX domain socket to update ONLY quota metrics (5H & Weekly) and reset countdowns,
// explicitly preserving existing context window tokens and title state to prevent conflicts with live turn-by-turn stream hooks.
func ReportHerdrQuotaOnly(ctx context.Context, profileName, modelName string) error {
	return reportHerdrMetadataInternal(ctx, profileName, modelName, false)
}

func reportHerdrMetadataInternal(ctx context.Context, profileName, modelName string, updateContext bool) error {
	if !IsInHerdrEnvironment() {
		return nil
	}
	paneID := os.Getenv("HERDR_PANE_ID")
	socketPath := os.Getenv("HERDR_SOCKET_PATH")

	// Resolve active model using full fallback chain: explicit arg > .active_model cache > settings.json
	if pDir, err := GetProfileDir(profileName); err == nil {
		modelName = ResolveActiveModel(pDir, modelName)
	}

	quotaCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	var targetPanes []HerdrPaneMatch
	if updateContext && paneID != "" {
		// Inline hook is per-pane: update ONLY the current pane to prevent triggering multi-pane render cascades or model overwrite.
		targetPanes = []HerdrPaneMatch{{
			PaneID: paneID,
			Model:  modelName,
		}}
	} else {
		targetPanes = getMatchingHerdrPanes(quotaCtx, socketPath, paneID, profileName, modelName)
	}

	for _, target := range targetPanes {
		if target.PaneID == "" {
			continue
		}

		targetModel := target.Model
		if targetModel == "" || targetModel == "auto" {
			targetModel = modelName
		}
		if targetModel == "" {
			targetModel = "gemini"
		}

		displayAgent := profileName
		title := fmt.Sprintf("agys: %s", profileName)
		if target.Title != "" {
			title = target.Title
		}
		tokens := map[string]string{
			"profile": profileName,
		}
		if targetModel != "" {
			tokens["model"] = targetModel
		}

		// Handle Row 2: Context window + Model
		var ctxPct int
		var hasCtx bool
		if updateContext {
			if pDir, pErr := GetProfileDir(profileName); pErr == nil {
				if ctxPct, hasCtx = GetSessionContext(pDir); hasCtx {
					tokens["quota_context"] = fmt.Sprintf("ctx %d%%", ctxPct)
				}
			}

			var modelCtxStr string
			if targetModel != "" {
				if hasCtx {
					modelCtxStr = fmt.Sprintf("%d%% ctx · %s", ctxPct, targetModel)
				} else {
					modelCtxStr = targetModel
				}
			} else if hasCtx {
				modelCtxStr = fmt.Sprintf("%d%% ctx", ctxPct)
			}
			if modelCtxStr != "" {
				tokens["quota_model_context"] = modelCtxStr
			}
		} else {
			// Watcher polling: strictly preserve existing context tokens on the pane to prevent any overwrite or conflict
			if target.QuotaModelContext != "" {
				tokens["quota_model_context"] = target.QuotaModelContext
			}
			if target.QuotaContext != "" {
				tokens["quota_context"] = target.QuotaContext
			}
		}

		details, err := GetProfileFullQuotaDetailsForModel(quotaCtx, profileName, targetModel)
		if err == nil && details != nil && details.Fraction5H >= 0 {
			pct5h := int(details.Fraction5H*100 + 0.5)
			pct5hStr := fmt.Sprintf("%d%%", pct5h)
			if details.GroupName != "" {
				tokens["group"] = details.GroupName
			}
			tokens["quota"] = pct5hStr

			var titleParts []string
			if updateContext {
				if hasCtx {
					titleParts = append(titleParts, fmt.Sprintf("Ctx: %d%%", ctxPct))
				}
			} else {
				// Preserve existing Ctx string in title
				if strings.Contains(target.Title, "Ctx: ") {
					idx := strings.Index(target.Title, "Ctx: ")
					rest := target.Title[idx:]
					end := strings.Index(rest, " •")
					if end == -1 {
						end = len(rest)
					}
					titleParts = append(titleParts, rest[:end])
				} else if target.QuotaContext != "" && strings.HasPrefix(target.QuotaContext, "ctx ") {
					pct := strings.TrimPrefix(target.QuotaContext, "ctx ")
					titleParts = append(titleParts, fmt.Sprintf("Ctx: %s", pct))
				}
			}

			if details.ResetStr5H != "" && details.ResetStr5H != "-" {
				titleParts = append(titleParts, fmt.Sprintf("5H: %s (%s)", pct5hStr, details.ResetStr5H))
				tokens["reset"] = details.ResetStr5H
			} else {
				titleParts = append(titleParts, fmt.Sprintf("5H: %s", pct5hStr))
			}

			// Format compact 5H token without "5h" prefix: "85% 2h" or "85%"
			quota5hStr := pct5hStr
			if details.CompactReset5H != "" {
				quota5hStr = fmt.Sprintf("%s %s", pct5hStr, details.CompactReset5H)
			}
			tokens["quota_5h"] = quota5hStr
			if pct5h >= 20 {
				tokens["quota_5h_normal"] = quota5hStr
			} else if pct5h > 5 {
				tokens["quota_5h_warning"] = quota5hStr
			} else {
				tokens["quota_5h_danger"] = quota5hStr
			}

			var quotaWkStr string
			if details.FractionWeekly >= 0 {
				pctWk := int(details.FractionWeekly*100 + 0.5)
				pctWkStr := fmt.Sprintf("%d%%", pctWk)
				tokens["quota_weekly"] = pctWkStr
				if details.ResetStrWeekly != "" && details.ResetStrWeekly != "-" {
					titleParts = append(titleParts, fmt.Sprintf("Wk: %s (%s)", pctWkStr, details.ResetStrWeekly))
					tokens["reset_weekly"] = details.ResetStrWeekly
				} else {
					titleParts = append(titleParts, fmt.Sprintf("Wk: %s", pctWkStr))
				}

				// Format compact Weekly token without "7d" prefix: "90% 3d" or "90%"
				quotaWkStr = pctWkStr
				if details.CompactResetWeekly != "" {
					quotaWkStr = fmt.Sprintf("%s %s", pctWkStr, details.CompactResetWeekly)
				}
				tokens["quota_week"] = quotaWkStr
				if pctWk >= 20 {
					tokens["quota_week_normal"] = quotaWkStr
				} else if pctWk > 5 {
					tokens["quota_week_warning"] = quotaWkStr
				} else {
					tokens["quota_week_danger"] = quotaWkStr
				}
			}

			// Format compact summary token for row 3 (Quota only): "85% 2h · 90% 3d"
			var summaryParts []string
			if quota5hStr != "" {
				summaryParts = append(summaryParts, quota5hStr)
			}
			if quotaWkStr != "" {
				summaryParts = append(summaryParts, quotaWkStr)
			}
			tokens["quota_summary"] = strings.Join(summaryParts, " · ")

			modelAbbr := FormatModelAbbreviation(targetModel, details.GroupName)
			if modelAbbr != "" {
				title = fmt.Sprintf("agys: %s [%s] %s", profileName, modelAbbr, strings.Join(titleParts, " • "))
			} else {
				title = fmt.Sprintf("agys: %s %s", profileName, strings.Join(titleParts, " • "))
			}

			if target.PaneID == paneID {
				SetTerminalTitle(title)
			}
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

// SetTerminalTitle sets the terminal/window/tab title using ANSI OSC escape sequence ONLY when inside Herdr.
func SetTerminalTitle(titleOrProfile string) {
	if !IsInHerdrEnvironment() || titleOrProfile == "" {
		return
	}
	title := titleOrProfile
	if !strings.HasPrefix(title, "agys") {
		title = fmt.Sprintf("agys [%s]", titleOrProfile)
	}
	fmt.Fprintf(os.Stderr, "\033]0;%s\007", title)
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

