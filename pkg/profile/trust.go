package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// readSettingsWorkspaces reads trustedWorkspaces from a settings.json file path and adds them to trustedMap.
func readSettingsWorkspaces(settingsPath string, trustedMap map[string]bool) {
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}

	if tw, ok := raw["trustedWorkspaces"].([]interface{}); ok {
		for _, item := range tw {
			if pathStr, isStr := item.(string); isStr {
				cleanPath := filepath.Clean(strings.TrimSpace(pathStr))
				if cleanPath != "" && cleanPath != "." {
					trustedMap[cleanPath] = true
				}
			}
		}
	}
}

// updateSettingsTrustedWorkspaces updates the trustedWorkspaces field in settings.json while preserving all other setting fields.
func updateSettingsTrustedWorkspaces(settingsPath string, allTrusted []string) error {
	var raw map[string]interface{}

	data, err := os.ReadFile(settingsPath)
	if err == nil {
		_ = json.Unmarshal(data, &raw)
	}

	if raw == nil {
		raw = make(map[string]interface{})
	}

	raw["trustedWorkspaces"] = allTrusted

	dir := filepath.Dir(settingsPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	updatedJSON, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return err
	}

	return WriteFileAtomic(settingsPath, []byte(string(updatedJSON)+"\n"), 0600)
}

// SyncTrustedWorkspaces merges trustedWorkspaces across all profiles so any workspace trusted in one profile is trusted in all.
func SyncTrustedWorkspaces() error {
	profiles, err := List()
	if err != nil {
		return err
	}

	trustedMap := make(map[string]bool)

	// Scan global system settings.json
	if userHome, homeErr := os.UserHomeDir(); homeErr == nil {
		agysSep := string(filepath.Separator) + ".agys"
		if idx := strings.Index(userHome, agysSep); idx != -1 {
			userHome = userHome[:idx]
		}
		globalSettingsPath := filepath.Join(userHome, ".gemini", "antigravity-cli", "settings.json")
		readSettingsWorkspaces(globalSettingsPath, trustedMap)
	}

	// Scan each profile's settings.json
	profileSettingsPaths := make(map[string]string)
	for _, p := range profiles {
		if IsAuto(p) {
			continue
		}
		pDir, pErr := GetProfileDir(p)
		if pErr == nil {
			s := filepath.Join(pDir, ".gemini", "antigravity-cli", "settings.json")
			profileSettingsPaths[p] = s
			readSettingsWorkspaces(s, trustedMap)
		}
	}

	if len(trustedMap) == 0 {
		return nil
	}

	var allTrusted []string
	for path := range trustedMap {
		allTrusted = append(allTrusted, path)
	}
	sort.Strings(allTrusted)

	// Update each profile's settings.json with merged trustedWorkspaces
	for _, sPath := range profileSettingsPaths {
		_ = updateSettingsTrustedWorkspaces(sPath, allTrusted)
	}

	return nil
}
