package profile

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const mcpConfigRelativePath = ".gemini/antigravity-cli/mcp_config.json"

// GetMcpConfigPath returns the absolute path to the profile's mcp_config.json file.
func GetMcpConfigPath(profileDir string) string {
	return filepath.Join(profileDir, ".gemini", "antigravity-cli", "mcp_config.json")
}

// McpServerEntry represents a parsed MCP server entry from mcp_config.json.
type McpServerEntry struct {
	Name    string   `json:"name"`
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
}

// ReadMcpServers reads and parses all configured MCP servers for a given profile.
// Returns an empty slice if mcp_config.json does not exist.
func ReadMcpServers(profileName string) ([]McpServerEntry, error) {
	exists, profileDir, err := Exists(profileName)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, fmt.Errorf("profile %q does not exist", profileName)
	}

	configPath := GetMcpConfigPath(profileDir)
	data, err := os.ReadFile(configPath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to read mcp_config.json: %w", err)
	}

	var rawConfig struct {
		McpServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}

	if err := json.Unmarshal(data, &rawConfig); err != nil {
		return nil, fmt.Errorf("failed to parse mcp_config.json: %w", err)
	}

	var servers []McpServerEntry
	for name, s := range rawConfig.McpServers {
		servers = append(servers, McpServerEntry{
			Name:    name,
			Command: s.Command,
			Args:    s.Args,
		})
	}

	return servers, nil
}

// SyncMcpConfig copies the mcp_config.json from srcProfile to targetProfile.
func SyncMcpConfig(srcProfile, targetProfile string) error {
	if strings.EqualFold(srcProfile, targetProfile) {
		return nil
	}

	srcExists, srcDir, err := Exists(srcProfile)
	if err != nil {
		return err
	}
	if !srcExists {
		return fmt.Errorf("source profile %q does not exist", srcProfile)
	}

	targetExists, targetDir, err := Exists(targetProfile)
	if err != nil {
		return err
	}
	if !targetExists {
		return fmt.Errorf("target profile %q does not exist", targetProfile)
	}

	srcConfig := GetMcpConfigPath(srcDir)
	data, err := os.ReadFile(srcConfig)
	if err != nil {
		return fmt.Errorf("failed to read source mcp_config.json from %q: %w", srcProfile, err)
	}

	targetConfig := GetMcpConfigPath(targetDir)
	if err := os.MkdirAll(filepath.Dir(targetConfig), 0700); err != nil {
		return fmt.Errorf("failed to create target config directory: %w", err)
	}

	if err := WriteFileAtomic(targetConfig, data, 0600); err != nil {
		return fmt.Errorf("failed to write target mcp_config.json in %q: %w", targetProfile, err)
	}

	return nil
}

// SyncMcpConfigToAll synchronizes the mcp_config.json from srcProfile to all other configured profiles.
// Returns the list of successfully updated profiles.
func SyncMcpConfigToAll(srcProfile string) ([]string, error) {
	profiles, err := List()
	if err != nil {
		return nil, err
	}

	var synced []string
	for _, p := range profiles {
		if strings.EqualFold(p, srcProfile) {
			continue
		}
		if err := SyncMcpConfig(srcProfile, p); err != nil {
			return synced, err
		}
		synced = append(synced, p)
	}

	return synced, nil
}
