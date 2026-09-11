package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/quaywin/agys/pkg/profile"
)

func TestMcpCmdRegistration(t *testing.T) {
	if mcpCmd.Name() != "mcp" {
		t.Errorf("expected command name 'mcp', got %q", mcpCmd.Name())
	}
	subcommands := mcpCmd.Commands()
	hasList := false
	hasSync := false
	for _, sub := range subcommands {
		if sub.Name() == "list" {
			hasList = true
		}
		if sub.Name() == "sync" {
			hasSync = true
		}
	}
	if !hasList || !hasSync {
		t.Errorf("expected mcpCmd to have list and sync subcommands, got %v", subcommands)
	}
}

func TestMcpListAndSyncCmd(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	p1 := "work"
	p2 := "personal"
	dir1, _ := profile.Create(p1)
	_, _ = profile.Create(p2)

	// Add dummy mcp_config.json to work
	cfgDir := filepath.Join(dir1, ".gemini", "antigravity-cli")
	_ = os.MkdirAll(cfgDir, 0700)
	configJSON := `{"mcpServers": {"github": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-github"]}}}`
	_ = os.WriteFile(filepath.Join(cfgDir, "mcp_config.json"), []byte(configJSON), 0600)

	// Test list command execution
	err := mcpListCmd.RunE(mcpListCmd, []string{p1})
	if err != nil {
		t.Errorf("mcpListCmd failed: %v", err)
	}

	// Test sync command execution
	err = mcpSyncCmd.RunE(mcpSyncCmd, []string{p1, p2})
	if err != nil {
		t.Errorf("mcpSyncCmd failed: %v", err)
	}

	// Verify target received config
	servers2, err := profile.ReadMcpServers(p2)
	if err != nil || len(servers2) != 1 || servers2[0].Name != "github" {
		t.Errorf("expected github server in personal profile, got %v (err: %v)", servers2, err)
	}
}
