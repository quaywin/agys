package profile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyAndUninstallHerdr2RowConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "config.toml")
	t.Setenv("HERDR_CONFIG_FILE", configPath)

	// 1. Initial status on missing file
	if IsHerdrConfiguredForAgys(configPath) {
		t.Errorf("expected IsHerdrConfiguredForAgys to be false for non-existent config")
	}

	// 2. Apply on empty / new file
	if err := ApplyHerdr2RowConfig(configPath); err != nil {
		t.Fatalf("ApplyHerdr2RowConfig failed: %v", err)
	}

	if !IsHerdrConfiguredForAgys(configPath) {
		t.Errorf("expected IsHerdrConfiguredForAgys to be true after apply")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "[ui.sidebar.agents]") || !strings.Contains(content, "$quota_5h_normal") {
		t.Errorf("expected config to contain sidebar section with $quota_5h_normal, got:\n%s", content)
	}

	// 3. Apply on existing config with other tables
	existingContent := `[keys]
key = "prefix+shift+r"

[ui.sidebar.agents]
rows = [["state_icon", "agent"]]

[panes]
id = "test"
`
	_ = os.WriteFile(configPath, []byte(existingContent), 0644)
	if err := ApplyHerdr2RowConfig(configPath); err != nil {
		t.Fatalf("ApplyHerdr2RowConfig on existing config failed: %v", err)
	}

	data2, _ := os.ReadFile(configPath)
	content2 := string(data2)
	if !strings.Contains(content2, "[keys]") || !strings.Contains(content2, "[panes]") {
		t.Errorf("expected config to preserve other sections, got:\n%s", content2)
	}
	if !strings.Contains(content2, "$quota_5h_normal") {
		t.Errorf("expected updated sidebar to contain $quota_5h_normal, got:\n%s", content2)
	}

	// 4. Uninstall
	if err := UninstallHerdr2RowConfig(configPath); err != nil {
		t.Fatalf("UninstallHerdr2RowConfig failed: %v", err)
	}

	data3, _ := os.ReadFile(configPath)
	content3 := string(data3)
	if strings.Contains(content3, "$quota_5h_normal") {
		t.Errorf("expected $quota_5h_normal to be removed after uninstall, got:\n%s", content3)
	}
}
