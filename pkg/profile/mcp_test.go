package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadMcpServers(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	p1 := "work"
	dir1, err := Create(p1)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Case 1: Missing config returns nil, nil
	servers, err := ReadMcpServers(p1)
	if err != nil {
		t.Fatalf("ReadMcpServers on missing config failed: %v", err)
	}
	if len(servers) != 0 {
		t.Errorf("expected 0 servers, got %d", len(servers))
	}

	// Case 2: Config with servers
	cfgDir := filepath.Join(dir1, ".gemini", "antigravity-cli")
	_ = os.MkdirAll(cfgDir, 0700)
	configJSON := `{
  "mcpServers": {
    "chrome-devtools": {
      "command": "npx",
      "args": ["-y", "chrome-devtools-mcp@latest"]
    }
  }
}`
	_ = os.WriteFile(filepath.Join(cfgDir, "mcp_config.json"), []byte(configJSON), 0600)

	servers, err = ReadMcpServers(p1)
	if err != nil {
		t.Fatalf("ReadMcpServers failed: %v", err)
	}
	if len(servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(servers))
	}
	if servers[0].Name != "chrome-devtools" || servers[0].Command != "npx" {
		t.Errorf("unexpected server details: %+v", servers[0])
	}
}

func TestSyncMcpConfig(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("AGYS_DIR", filepath.Join(tempHome, ".agys"))

	p1 := "src"
	p2 := "target1"
	p3 := "target2"
	dir1, _ := Create(p1)
	_, _ = Create(p2)
	_, _ = Create(p3)

	cfgDir := filepath.Join(dir1, ".gemini", "antigravity-cli")
	_ = os.MkdirAll(cfgDir, 0700)
	configJSON := `{"mcpServers": {"sqlite": {"command": "uvx", "args": ["mcp-server-sqlite"]}}}`
	_ = os.WriteFile(filepath.Join(cfgDir, "mcp_config.json"), []byte(configJSON), 0600)

	// Sync to single target
	if err := SyncMcpConfig(p1, p2); err != nil {
		t.Fatalf("SyncMcpConfig failed: %v", err)
	}

	servers2, err := ReadMcpServers(p2)
	if err != nil || len(servers2) != 1 || servers2[0].Name != "sqlite" {
		t.Errorf("expected sqlite server synced to target1, got %v (err: %v)", servers2, err)
	}

	// Sync to all
	synced, err := SyncMcpConfigToAll(p1)
	if err != nil {
		t.Fatalf("SyncMcpConfigToAll failed: %v", err)
	}
	if len(synced) != 2 {
		t.Errorf("expected 2 profiles synced, got %d (%v)", len(synced), synced)
	}

	servers3, err := ReadMcpServers(p3)
	if err != nil || len(servers3) != 1 || servers3[0].Name != "sqlite" {
		t.Errorf("expected sqlite server synced to target2, got %v (err: %v)", servers3, err)
	}
}
