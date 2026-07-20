package profile

import (
	"os"
	"path/filepath"
	"time"
)

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
func FindProfileByLatestConversation() (string, error) {
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
