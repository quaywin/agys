package profile

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MigrateConversation moves a conversation's brain folder and history metadata
// from srcProfile to destProfile.
func MigrateConversation(convID, srcProfile, destProfile string) error {
	if convID == "" {
		return fmt.Errorf("conversation ID cannot be empty")
	}
	if srcProfile == "" || destProfile == "" {
		return fmt.Errorf("source and destination profile names cannot be empty")
	}
	if srcProfile == destProfile {
		return nil
	}

	srcProfileDir, err := GetProfileDir(srcProfile)
	if err != nil {
		return fmt.Errorf("failed to get source profile directory: %w", err)
	}

	destProfileDir, err := GetProfileDir(destProfile)
	if err != nil {
		return fmt.Errorf("failed to get destination profile directory: %w", err)
	}

	// 1. Locate conversation directory in srcProfile
	var srcConvDir string
	var subBrainRel string

	for _, sub := range []string{filepath.Join(".gemini", "antigravity-cli", "brain"), filepath.Join(".gemini", "antigravity", "brain")} {
		candidate := filepath.Join(srcProfileDir, sub, convID)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			srcConvDir = candidate
			subBrainRel = sub
			break
		}
	}

	if srcConvDir == "" {
		return fmt.Errorf("conversation %q not found in profile %q", convID, srcProfile)
	}

	// 2. Determine destination directory and ensure parent directories exist
	destBrainDir := filepath.Join(destProfileDir, subBrainRel)
	if err := os.MkdirAll(destBrainDir, 0700); err != nil {
		return fmt.Errorf("failed to create destination brain directory: %w", err)
	}
	destConvDir := filepath.Join(destBrainDir, convID)

	// Clean up stale destination directory if it already exists
	_ = os.RemoveAll(destConvDir)

	// 3. Move conversation directory atomically
	if err := os.Rename(srcConvDir, destConvDir); err != nil {
		return fmt.Errorf("failed to move conversation brain directory: %w", err)
	}

	// 4. Migrate history.jsonl entries
	migrateHistoryEntry(convID, srcProfileDir, destProfileDir)

	// 5. Update last active conversation pointer to point to this convID
	_ = SaveLastConversation(convID)

	return nil
}

func migrateHistoryEntry(convID, srcProfileDir, destProfileDir string) {
	for _, rel := range []string{
		filepath.Join(".gemini", "antigravity-cli", "history.jsonl"),
		filepath.Join(".gemini", "antigravity", "history.jsonl"),
	} {
		srcHistory := filepath.Join(srcProfileDir, rel)
		data, err := os.ReadFile(srcHistory)
		if err != nil || len(data) == 0 {
			continue
		}

		destHistory := filepath.Join(destProfileDir, rel)
		existingData, _ := os.ReadFile(destHistory)
		existingContent := string(existingData)

		var toAppend []string
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || !strings.Contains(line, "conversationId") {
				continue
			}

			var item struct {
				ConversationID string `json:"conversationId"`
			}
			if err := json.Unmarshal([]byte(line), &item); err == nil && item.ConversationID == convID {
				if !strings.Contains(existingContent, line) {
					toAppend = append(toAppend, line)
				}
			}
		}

		if len(toAppend) > 0 {
			_ = os.MkdirAll(filepath.Dir(destHistory), 0700)
			f, err := os.OpenFile(destHistory, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
			if err == nil {
				for _, line := range toAppend {
					_, _ = f.WriteString(line + "\n")
				}
				_ = f.Close()
			}
		}
	}
}
