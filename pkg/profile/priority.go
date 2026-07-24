package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

const prioritiesFilename = "priorities.json"

var priorityMu sync.RWMutex

// GetPrioritiesFilePath returns the absolute path to ~/.agys/priorities.json.
func GetPrioritiesFilePath() (string, error) {
	agysDir, err := GetAgysDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(agysDir, prioritiesFilename), nil
}

// GetAllPriorities loads all configured profile priorities from disk.
func GetAllPriorities() (map[string]int, error) {
	priorityMu.RLock()
	defer priorityMu.RUnlock()

	var priorities map[string]int

	err := WithFileLock(context.Background(), func() error {
		filePath, err := GetPrioritiesFilePath()
		if err != nil {
			return err
		}

		data, err := os.ReadFile(filePath)
		if os.IsNotExist(err) {
			priorities = make(map[string]int)
			return nil
		}
		if err != nil {
			return fmt.Errorf("failed to read priorities file: %w", err)
		}

		if err := json.Unmarshal(data, &priorities); err != nil {
			priorities = make(map[string]int)
			return nil
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	if priorities == nil {
		priorities = make(map[string]int)
	}
	return priorities, nil
}

// GetPriority returns the priority integer for a specific profile (default 0 if unset).
func GetPriority(profileName string) int {
	priorities, err := GetAllPriorities()
	if err != nil {
		return 0
	}
	return priorities[profileName]
}

// SetPriority saves the priority integer for a specific profile.
func SetPriority(profileName string, priority int) error {
	if IsAuto(profileName) {
		return fmt.Errorf("cannot set priority for reserved keyword %q", profileName)
	}

	exists, _, err := Exists(profileName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("profile %q does not exist", profileName)
	}

	priorityMu.Lock()
	defer priorityMu.Unlock()

	return WithFileLock(context.Background(), func() error {
		filePath, err := GetPrioritiesFilePath()
		if err != nil {
			return err
		}

		agysDir, err := GetAgysDir()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(agysDir, 0700); err != nil {
			return fmt.Errorf("failed to create agys directory: %w", err)
		}

		priorities := make(map[string]int)
		data, readErr := os.ReadFile(filePath)
		if readErr == nil {
			_ = json.Unmarshal(data, &priorities)
		}
		if priorities == nil {
			priorities = make(map[string]int)
		}

		priorities[profileName] = priority

		encoded, err := json.MarshalIndent(priorities, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to encode priorities: %w", err)
		}

		return WriteFileAtomic(filePath, encoded, 0600)
	})
}

