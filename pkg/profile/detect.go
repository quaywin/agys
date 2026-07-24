package profile

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	lastConversationFilename = "last_conversation"
	sessionFlagsFilename     = "session_flags.json"
)

// SaveLastConversation saves the last active conversation ID to a global cache file.
func SaveLastConversation(convID string) error {
	if convID == "" {
		return nil
	}
	agysDir, err := GetAgysDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(agysDir, 0700); err != nil {
		return err
	}
	cacheFile := filepath.Join(agysDir, lastConversationFilename)
	return WriteFileAtomic(cacheFile, []byte(strings.TrimSpace(convID)+"\n"), 0600)
}

// GetLastConversation retrieves the last active conversation ID from the global cache file.
func GetLastConversation() (string, error) {
	agysDir, err := GetAgysDir()
	if err != nil {
		return "", err
	}
	cacheFile := filepath.Join(agysDir, lastConversationFilename)
	data, err := os.ReadFile(cacheFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// FindProfileByConversation looks up which profile contains the given conversation ID in its brain directory.
// Returns the profile name, or empty string if not found.
func FindProfileByConversation(convID string) (string, error) {
	if convID == "" {
		return "", nil
	}

	profiles, err := List()
	if err != nil {
		return "", err
	}

	for _, p := range profiles {
		profileDir, err := GetProfileDir(p)
		if err != nil {
			continue
		}

		convDir := filepath.Join(profileDir, ".gemini", "antigravity-cli", "brain", convID)
		info, err := os.Stat(convDir)
		if err == nil && info.IsDir() {
			return p, nil
		}
	}

	return "", nil
}

// FindProfileByLatestConversation scans all profiles and returns the profile name
// that has the most recently modified conversation in its brain directory.
// It attempts to use the global last active conversation cache file first for O(1) performance.
func FindProfileByLatestConversation() (string, error) {
	// Try reading cache first for O(1) performance
	lastConvID, err := GetLastConversation()
	if err == nil && lastConvID != "" {
		p, err := FindProfileByConversation(lastConvID)
		if err == nil && p != "" {
			return p, nil
		}
	}

	profiles, err := List()
	if err != nil {
		return "", err
	}

	var latestProfile string
	var latestTime time.Time

	for _, p := range profiles {
		profileDir, err := GetProfileDir(p)
		if err != nil {
			continue
		}

		brainDir := filepath.Join(profileDir, ".gemini", "antigravity-cli", "brain")
		entries, err := os.ReadDir(brainDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			dirPath := filepath.Join(brainDir, entry.Name())
			info, err := os.Stat(dirPath)
			if err != nil {
				continue
			}

			// Check transcript.jsonl modification time if it exists, otherwise use directory time
			checkPath := filepath.Join(dirPath, ".system_generated", "logs", "transcript.jsonl")
			checkInfo, err := os.Stat(checkPath)
			mTime := info.ModTime()
			if err == nil {
				mTime = checkInfo.ModTime()
			}

			if mTime.After(latestTime) {
				latestTime = mTime
				latestProfile = p
			}
		}
	}

	return latestProfile, nil
}

// GetLatestConversationFileInfo returns the ID and modification time of the latest conversation in a profile.
func GetLatestConversationFileInfo(profileName string) (string, time.Time, error) {
	profileDir, err := GetProfileDir(profileName)
	if err != nil {
		return "", time.Time{}, err
	}

	brainDir := filepath.Join(profileDir, ".gemini", "antigravity-cli", "brain")
	entries, err := os.ReadDir(brainDir)
	if err != nil {
		return "", time.Time{}, err
	}

	var latestID string
	var latestTime time.Time

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		dirPath := filepath.Join(brainDir, entry.Name())
		info, err := os.Stat(dirPath)
		if err != nil {
			continue
		}

		checkPath := filepath.Join(dirPath, ".system_generated", "logs", "transcript.jsonl")
		checkInfo, err := os.Stat(checkPath)
		mTime := info.ModTime()
		if err == nil {
			mTime = checkInfo.ModTime()
		}

		if mTime.After(latestTime) {
			latestTime = mTime
			latestID = entry.Name()
		}
	}

	return latestID, latestTime, nil
}

// SaveSessionFlags saves preserved CLI flags associated with a conversation ID.
func SaveSessionFlags(convID string, flags []string) error {
	if convID == "" || len(flags) == 0 {
		return nil
	}
	agysDir, err := GetAgysDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(agysDir, 0700); err != nil {
		return err
	}
	filePath := filepath.Join(agysDir, sessionFlagsFilename)

	m := make(map[string][]string)
	data, err := os.ReadFile(filePath)
	if err == nil {
		_ = json.Unmarshal(data, &m)
	}

	m[convID] = flags

	updatedData, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return WriteFileAtomic(filePath, updatedData, 0600)
}

// GetSessionFlags retrieves saved CLI flags for a given conversation ID.
func GetSessionFlags(convID string) ([]string, error) {
	if convID == "" {
		return nil, nil
	}
	agysDir, err := GetAgysDir()
	if err != nil {
		return nil, err
	}
	filePath := filepath.Join(agysDir, sessionFlagsFilename)
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var m map[string][]string
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return m[convID], nil
}
