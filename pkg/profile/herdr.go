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

# Directly invoke agys binary in pure compiled Go (zero Python dependency)
AGYS_BIN="agys"
if command -v agys >/dev/null 2>&1; then
  AGYS_BIN="$(command -v agys)"
elif [ -x "$HOME/.local/bin/agys" ]; then
  AGYS_BIN="$HOME/.local/bin/agys"
elif [ -x "/usr/local/bin/agys" ]; then
  AGYS_BIN="/usr/local/bin/agys"
fi

exec "$AGYS_BIN" herdr-hook "${1:-session}"
`

// resolveProfileName returns the active profile name from AGYS_PROFILE env var,
// falling back to extracting it from the HOME path if running inside an agys-managed profile directory.
func resolveProfileName() string {
	profileName := os.Getenv("AGYS_PROFILE")
	if profileName == "" {
		home := os.Getenv("HOME")
		if strings.Contains(home, ".agys/profiles/") {
			profileName = filepath.Base(home)
		}
	}
	return profileName
}

// ReadSettingsModel reads the "model" field from the antigravity-cli settings.json for a given profile directory.
// This is the source of truth when the model is changed via UI settings (not --model CLI flag).
func ReadSettingsModel(profileDir string) string {
	settingsPath := filepath.Join(profileDir, ".gemini", "antigravity-cli", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return ""
	}
	var settings struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(data, &settings); err != nil {
		return ""
	}
	return strings.TrimSpace(settings.Model)
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

// ResolveActiveModel returns the best-known active model name for a profile directory,
// checking in priority order: explicit modelName arg > settings.json (UI source of truth) > .active_model cache.
// Returns a normalized API-style model ID.
func ResolveActiveModel(profileDir, modelName string) string {
	if modelName != "" && modelName != "auto" {
		return NormalizeModelName(modelName)
	}

	// settings.json is the source of truth for UI model selection (highest priority after explicit arg)
	if settingsModel := ReadSettingsModel(profileDir); settingsModel != "" && settingsModel != "auto" {
		return NormalizeModelName(settingsModel)
	}

	// Fall back to .active_model cache (set by --model CLI flag or hooks)
	if data, err := os.ReadFile(filepath.Join(profileDir, ".active_model")); err == nil {
		if m := strings.TrimSpace(string(data)); m != "" && m != "auto" {
			return NormalizeModelName(m)
		}
	}

	return ""
}

// HandleHerdrHook executes the Herdr lifecycle hook directly in pure Go without any Python dependency.
func HandleHerdrHook(ctx context.Context, action string, stdin io.Reader) error {
	if os.Getenv("HERDR_ENV") != "1" {
		fmt.Println("{}")
		return nil
	}
	paneID := os.Getenv("HERDR_PANE_ID")
	socketPath := os.Getenv("HERDR_SOCKET_PATH")
	if paneID == "" || socketPath == "" {
		fmt.Println("{}")
		return nil
	}

	switch action {
	case "session":
		var payload struct {
			ConversationID string `json:"conversationId"`
			TranscriptPath string `json:"transcriptPath"`
			ModelName      string `json:"modelName"`
		}
		if stdin != nil {
			_ = json.NewDecoder(stdin).Decode(&payload)
		}
		if payload.ConversationID != "" {
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

			// Cache active model name for this profile
			if payload.ModelName != "" {
				profileName := resolveProfileName()
				if profileName != "" {
					if pDir, err := GetProfileDir(profileName); err == nil {
						_ = WriteFileAtomic(filepath.Join(pDir, ".active_model"), []byte(NormalizeModelName(payload.ModelName)), 0600)
					}
				}
			}
		}
	case "quota":
		// Read modelName directly from hook stdin — this is always accurate, even after /model switch
		var quotaPayload struct {
			ModelName string `json:"modelName"`
		}
		if stdin != nil {
			_ = json.NewDecoder(stdin).Decode(&quotaPayload)
		}
		profileName := resolveProfileName()
		if profileName != "" {
			normalizedModel := NormalizeModelName(quotaPayload.ModelName)
			// Persist the current model so the watcher also picks it up on next cycle
			if normalizedModel != "" && normalizedModel != "auto" {
				if pDir, err := GetProfileDir(profileName); err == nil {
					_ = WriteFileAtomic(filepath.Join(pDir, ".active_model"), []byte(normalizedModel), 0600)
				}
			}
			quotaCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
			defer cancel()
			_ = ReportHerdrMetadataWithModel(quotaCtx, profileName, normalizedModel)
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

// IsHerdrAvailable checks if Herdr is installed on the system or actively running.
func IsHerdrAvailable() bool {
	if os.Getenv("HERDR_ENV") == "1" || os.Getenv("HERDR_SOCKET_PATH") != "" {
		return true
	}
	if _, err := exec.LookPath("herdr"); err == nil {
		return true
	}
	if userHome, err := os.UserHomeDir(); err == nil {
		agysSep := string(filepath.Separator) + ".agys"
		if idx := strings.Index(userHome, agysSep); idx != -1 {
			userHome = userHome[:idx]
		}
		if _, err := os.Stat(filepath.Join(userHome, ".gemini", "config", "hooks", "herdr-agent-state.sh")); err == nil {
			return true
		}
	}
	return false
}

// SyncHerdrIntegration ensures that Herdr's antigravity integration hook is configured for the given profile directory.
func SyncHerdrIntegration(profileDir string) error {
	if !IsHerdrAvailable() {
		return nil
	}

	hookDir := filepath.Join(profileDir, ".gemini", "config", "hooks")
	if err := os.MkdirAll(hookDir, 0700); err != nil {
		return fmt.Errorf("failed to create hooks directory: %w", err)
	}

	hookFile := filepath.Join(hookDir, "herdr-agent-state.sh")

	// Always write the latest synchronized hook script
	scriptContent := []byte(defaultHerdrHookScript)
	if err := WriteFileAtomic(hookFile, scriptContent, 0755); err != nil {
		return fmt.Errorf("failed to write herdr hook file: %w", err)
	}

	// Ensure executable permission
	_ = os.Chmod(hookFile, 0755)

	// Ensure hooks.json is configured with PreInvocation, PostInvocation, and Stop hooks
	hooksJSONPath := filepath.Join(profileDir, ".gemini", "config", "hooks.json")
	return ensureHooksJSON(hooksJSONPath, hookFile)
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

	return WriteFileAtomic(hooksJSONPath, []byte(string(updatedData)+"\n"), 0600)
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

// HerdrPaneMatch represents a matched Herdr pane along with its specific active model.
type HerdrPaneMatch struct {
	PaneID string
	Model  string
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
			if p.Tokens != nil {
				if p.Tokens["profile"] == profileName {
					isMatch = true
				}
				paneModel = p.Tokens["model"]
			}
			if !isMatch {
				if strings.Contains(p.DisplayAgent, fmt.Sprintf("[%s:", profileName)) || strings.Contains(p.DisplayAgent, fmt.Sprintf("[%s]", profileName)) || strings.Contains(p.DisplayAgent, fmt.Sprintf("[%s ", profileName)) {
					isMatch = true
				} else if strings.Contains(p.Title, fmt.Sprintf("agys: %s", profileName)) {
					isMatch = true
				}
			}
			if isMatch && !seen[p.PaneID] {
				targetPanes = append(targetPanes, HerdrPaneMatch{PaneID: p.PaneID, Model: paneModel})
				seen[p.PaneID] = true
			}
		}
	}

	return targetPanes
}

// ReportHerdrMetadata communicates with Herdr via its UNIX domain socket to set display_agent, title, and quota for all matching panes.
func ReportHerdrMetadata(ctx context.Context, profileName string) error {
	return ReportHerdrMetadataWithModel(ctx, profileName, "")
}

// ReportHerdrMetadataWithModel communicates with Herdr via its UNIX domain socket to set metadata matching each pane's active model.
func ReportHerdrMetadataWithModel(ctx context.Context, profileName, modelName string) error {
	if os.Getenv("HERDR_ENV") != "1" {
		return nil
	}
	paneID := os.Getenv("HERDR_PANE_ID")
	socketPath := os.Getenv("HERDR_SOCKET_PATH")
	if paneID == "" || socketPath == "" {
		return nil
	}

	// Resolve active model using full fallback chain: explicit arg > .active_model cache > settings.json
	if pDir, err := GetProfileDir(profileName); err == nil {
		modelName = ResolveActiveModel(pDir, modelName)
	}

	quotaCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	targetPanes := getMatchingHerdrPanes(quotaCtx, socketPath, paneID, profileName, modelName)
	for _, target := range targetPanes {
		if target.PaneID == "" {
			continue
		}

		targetModel := modelName // Always prefer fresh model from hook payload
		if targetModel == "" || targetModel == "auto" {
			// Fall back to cached pane token only when no fresh model is available
			targetModel = target.Model
		}

		displayAgent := fmt.Sprintf("agy[%s]", profileName)
		title := fmt.Sprintf("agys: %s", profileName)
		tokens := map[string]string{
			"profile": profileName,
		}
		if targetModel != "" {
			tokens["model"] = targetModel
		}

		details, err := GetProfileFullQuotaDetailsForModel(quotaCtx, profileName, targetModel)
		if err == nil && details != nil && details.Fraction5H >= 0 {
			pct5h := int(details.Fraction5H*100 + 0.5)
			pct5hStr := fmt.Sprintf("%d%%", pct5h)
			modelAbbr := FormatModelAbbreviation(targetModel, details.GroupName)
			if modelAbbr != "" {
				displayAgent = fmt.Sprintf("agy[%s:%s·%s]", profileName, modelAbbr, pct5hStr)
			} else {
				displayAgent = fmt.Sprintf("agy[%s:%s]", profileName, pct5hStr)
			}
			if details.GroupName != "" {
				tokens["group"] = details.GroupName
			}
			tokens["quota"] = pct5hStr

			var titleParts []string
			if details.ResetStr5H != "" && details.ResetStr5H != "-" {
				titleParts = append(titleParts, fmt.Sprintf("5H: %s (%s)", pct5hStr, details.ResetStr5H))
				tokens["reset"] = details.ResetStr5H
			} else {
				titleParts = append(titleParts, fmt.Sprintf("5H: %s", pct5hStr))
			}

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
			}

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

// SetTerminalTitle sets the terminal/window/tab title using ANSI OSC escape sequence.
func SetTerminalTitle(titleOrProfile string) {
	if titleOrProfile == "" {
		return
	}
	title := titleOrProfile
	if !strings.HasPrefix(title, "agys") {
		title = fmt.Sprintf("agys [%s]", titleOrProfile)
	}
	fmt.Fprintf(os.Stderr, "\033]0;%s\007", title)
}

// StartHerdrQuotaWatcher starts a background goroutine that watches and refreshes quota at reset time.
// One leader per profile (via OS file lock). Re-reads .active_model each cycle so /model switches are handled automatically.
// Returns a cleanup function to stop the watcher on session exit.
func StartHerdrQuotaWatcher(ctx context.Context, profileName string, modelName ...string) func() {
	if os.Getenv("HERDR_ENV") != "1" || os.Getenv("HERDR_PANE_ID") == "" || os.Getenv("HERDR_SOCKET_PATH") == "" {
		return func() {}
	}

	pDir, err := GetProfileDir(profileName)
	if err != nil {
		return func() {}
	}

	// Single lock per profile — model group doesn't matter for leader election
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

		for {
			// Re-read current model each cycle — handles /model switches and UI settings changes mid-session
			currentModel := ""
			if len(modelName) > 0 && modelName[0] != "" {
				currentModel = modelName[0]
			}
			currentModel = ResolveActiveModel(pDir, currentModel)

			// Calculate next wake-up time based on current model's reset time
			waitDuration := 10 * time.Minute
			fetchCtx, fetchCancel := context.WithTimeout(watchCtx, 3*time.Second)
			_, _, resetTime, _, err := GetProfile5HQuotaDetailsForModel(fetchCtx, profileName, currentModel)
			fetchCancel()

			if err == nil && !resetTime.IsZero() {
				untilReset := time.Until(resetTime)
				if untilReset > 0 {
					waitDuration = untilReset + 5*time.Second
					if waitDuration > 10*time.Minute {
						waitDuration = 10 * time.Minute
					}
				}
			}

			if waitDuration < 30*time.Second {
				waitDuration = 30 * time.Second
			}

			timer := time.NewTimer(waitDuration)
			select {
			case <-watchCtx.Done():
				timer.Stop()
				return
			case <-timer.C:
				// Re-read model again at broadcast time (may have changed since sleep started)
				currentModel = ResolveActiveModel(pDir, "")
				reportCtx, reportCancel := context.WithTimeout(watchCtx, 4*time.Second)
				_ = ReportHerdrMetadataWithModel(reportCtx, profileName, currentModel)
				reportCancel()
			}
		}
	}()

	return func() {
		cancel()
	}
}

