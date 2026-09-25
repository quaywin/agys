package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var herdrAgentsSectionRegex = regexp.MustCompile(`(?m)^[ \t]*\[[ \t]*ui\.sidebar\.agents[ \t]*\][ \t]*(?:#.*)?(?:\r?\n)?`)

const (
	HerdrAgysRowMarker = "# herdr-agys-managed"
)

// GetHerdrConfigPath returns the absolute path to Herdr's config.toml.
func GetHerdrConfigPath() string {
	if custom := os.Getenv("HERDR_CONFIG_PATH"); custom != "" {
		return custom
	}
	if custom := os.Getenv("HERDR_CONFIG_FILE"); custom != "" {
		return custom
	}
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" && !strings.Contains(xdg, ".agys") {
		return filepath.Join(xdg, "herdr", "config.toml")
	}
	realHome, err := GetRealUserHome()
	if err == nil && realHome != "" {
		return filepath.Join(realHome, ".config", "herdr", "config.toml")
	}
	homeDir, _ := os.UserHomeDir()
	if homeDir == "" {
		homeDir = "/"
	}
	return filepath.Join(homeDir, ".config", "herdr", "config.toml")
}

// GetHerdrBackupConfigPath returns the path to the backup config file.
func GetHerdrBackupConfigPath() string {
	return filepath.Join(filepath.Dir(GetHerdrConfigPath()), "config.original.toml")
}

const Compact2RowTOML = `[ui.sidebar.agents]
rows = [
  ["state_icon", "workspace", { token = "agent", fg = "#38bdf8", bold = true }],
  [
    { token = "$conversation_title", fg = "#93c5fd" }
  ],
  [
    { token = "$quota_5h_normal", fg = "#4ade80", bold = true },
    { token = "$quota_5h_warning", fg = "#facc15", bold = true },
    { token = "$quota_5h_danger", fg = "#f87171", bold = true },
    { token = "$quota_week_normal", fg = "#a78bfa", bold = true },
    { token = "$quota_week_warning", fg = "#facc15", bold = true },
    { token = "$quota_week_danger", fg = "#f87171", bold = true }
  ]
] # herdr-agys-managed
`

const AgysAgentDetectionTOML = `id = "agy"
version = "2026.09.25.3"
min_engine_version = 1
updated_at = "2026-09-25T00:00:00Z"
aliases = ["antigravity", "antigravity-cli"]

[[rules]]
id = "permission_prompt"
state = "blocked"
priority = 1000
region = "whole_recent"
visible_blocker = true
contains = ["requesting permission for:"]
any = [
  { contains = ["do you want to proceed?"] },
  { contains = ["tab amend", "edit command"] },
]

[[rules]]
id = "prompt_box_idle"
state = "idle"
priority = 900
region = "prompt_box_body"
visible_idle = true
line_regex = ['^\s*>\s*']

[[rules]]
id = "spinner_working"
state = "working"
priority = 800
region = "bottom_non_empty_lines(6)"
visible_working = true
line_regex = ['^\s*[\u2800-\u28FF]+\s+\p{Alphabetic}+\w*ing\b']

[[rules]]
id = "background_tasks_working"
state = "working"
priority = 750
region = "bottom_non_empty_lines(5)"
visible_working = true
line_regex = ['(?i)·\s*[1-9][0-9]*\s+task']

[[rules]]
id = "prompt_idle_fallback"
state = "idle"
priority = 700
region = "bottom_non_empty_lines(3)"
visible_idle = true
line_regex = ['^\s*>\s*']
`

// EnsureHerdrAgentDetectionManifest ensures Herdr has a local override manifest for agy
// that includes an explicit prompt_idle rule and restricts spinner detection to recent bottom lines,
// preventing historical spinners from falsely reporting "working" status while idle.
func EnsureHerdrAgentDetectionManifest() error {
	configDir := filepath.Dir(GetHerdrConfigPath())
	targetDir := filepath.Join(configDir, "agent-detection")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return err
	}
	targetFile := filepath.Join(targetDir, "agy.toml")
	existing, err := os.ReadFile(targetFile)
	if err == nil && string(existing) == AgysAgentDetectionTOML {
		return nil
	}
	if err := WriteFileAtomic(targetFile, []byte(AgysAgentDetectionTOML), 0644); err != nil {
		return err
	}
	// Reload agent manifests via Herdr socket if running in Herdr
	if socketPath := os.Getenv("HERDR_SOCKET_PATH"); socketPath != "" {
		req, _ := json.Marshal(map[string]interface{}{
			"id":     fmt.Sprintf("agys:manifest_reload:%d", time.Now().UnixNano()),
			"method": "server.reload_agent_manifests",
			"params": map[string]interface{}{},
		})
		_ = sendHerdrSocketRPC(context.Background(), socketPath, req)
	}
	return nil
}

// IsHerdrConfiguredForAgys checks if Herdr's config.toml contains the agys 2-row sidebar configuration with conversation title.
func IsHerdrConfiguredForAgys(configPath string) bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	content := string(data)
	return strings.Contains(content, "$conversation_title") && strings.Contains(content, "$quota_5h")
}

// ApplyHerdr2RowConfig updates Herdr's config.toml with the compact 2-row sidebar layout.
func ApplyHerdr2RowConfig(configPath string) error {
	if configPath == "" {
		configPath = GetHerdrConfigPath()
	}

	backupPath := filepath.Join(filepath.Dir(configPath), "config.original.toml")
	_ = os.MkdirAll(filepath.Dir(configPath), 0755)

	var originalContent string
	if data, err := os.ReadFile(configPath); err == nil {
		originalContent = string(data)
		// Save backup if not already present
		if _, bErr := os.Stat(backupPath); os.IsNotExist(bErr) {
			_ = WriteFileAtomic(backupPath, data, 0644)
		}
	}

	updated := injectHerdrSidebarSection(originalContent)
	if err := WriteFileAtomic(configPath, []byte(updated), 0644); err != nil {
		return fmt.Errorf("failed to write Herdr config at %s: %w", configPath, err)
	}

	// Try to reload Herdr server configuration
	_ = ReloadHerdrServer()

	return nil
}

// UninstallHerdr2RowConfig restores the original Herdr config or removes agys-managed rows.
func UninstallHerdr2RowConfig(configPath string) error {
	if configPath == "" {
		configPath = GetHerdrConfigPath()
	}

	backupPath := filepath.Join(filepath.Dir(configPath), "config.original.toml")
	if data, err := os.ReadFile(backupPath); err == nil {
		if err := WriteFileAtomic(configPath, data, 0644); err == nil {
			_ = os.Remove(backupPath)
			_ = ReloadHerdrServer()
			return nil
		}
	}

	// If no backup, strip managed sections
	if data, err := os.ReadFile(configPath); err == nil {
		cleaned := removeHerdrSidebarSection(string(data))
		_ = WriteFileAtomic(configPath, []byte(cleaned), 0644)
	}

	_ = ReloadHerdrServer()
	return nil
}

func isTOMLTableHeader(line string) bool {
	trimmed := strings.TrimSpace(line)
	if idx := strings.Index(trimmed, "#"); idx != -1 {
		trimmed = strings.TrimSpace(trimmed[:idx])
	}
	if !strings.HasPrefix(trimmed, "[") || !strings.HasSuffix(trimmed, "]") {
		return false
	}
	inner := strings.Trim(trimmed, "[]")
	inner = strings.TrimSpace(inner)
	if inner == "" || strings.ContainsAny(inner, "\"'=,{}():;\n\r") {
		return false
	}
	return true
}

func injectHerdrSidebarSection(content string) string {
	if strings.TrimSpace(content) == "" {
		return Compact2RowTOML
	}

	loc := herdrAgentsSectionRegex.FindStringIndex(content)
	if loc != nil {
		rest := content[loc[1]:]
		lines := strings.Split(rest, "\n")
		var consumedBytes int
		endIdx := -1
		for _, line := range lines {
			if isTOMLTableHeader(line) {
				endIdx = loc[1] + consumedBytes
				break
			}
			consumedBytes += len(line) + 1 // +1 for newline
		}

		if endIdx != -1 {
			return content[:loc[0]] + Compact2RowTOML + "\n" + strings.TrimLeft(content[endIdx:], "\r\n")
		}
		return content[:loc[0]] + Compact2RowTOML
	}

	// Append to existing config
	return strings.TrimRight(content, "\r\n") + "\n\n" + Compact2RowTOML
}

func removeHerdrSidebarSection(content string) string {
	loc := herdrAgentsSectionRegex.FindStringIndex(content)
	if loc != nil {
		rest := content[loc[1]:]
		lines := strings.Split(rest, "\n")
		var consumedBytes int
		endIdx := -1
		for _, line := range lines {
			if isTOMLTableHeader(line) {
				endIdx = loc[1] + consumedBytes
				break
			}
			consumedBytes += len(line) + 1
		}

		defaultSection := "[ui.sidebar.agents]\nrows = [[\"state_icon\", \"agent\"]]\n"
		if endIdx != -1 {
			return content[:loc[0]] + defaultSection + strings.TrimLeft(content[endIdx:], "\r\n")
		}
		return content[:loc[0]] + defaultSection
	}
	return content
}

// ReloadHerdrServer sends a signal to Herdr server to reload configuration if herdr CLI is available.
func ReloadHerdrServer() error {
	if _, err := exec.LookPath("herdr"); err != nil {
		return nil
	}
	cmd := exec.Command("herdr", "server", "reload-config")
	return cmd.Run()
}
