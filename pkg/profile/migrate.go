package profile

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// MigrateConversation moves a conversation's brain folder, database, annotations,
// summaries, and history metadata from srcProfile to destProfile.
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

	hasConvDB := false
	for _, sub := range []string{filepath.Join(".gemini", "antigravity-cli", "conversations"), filepath.Join(".gemini", "antigravity", "conversations")} {
		if _, err := os.Stat(filepath.Join(srcProfileDir, sub, convID+".db")); err == nil {
			hasConvDB = true
			break
		}
	}

	if srcConvDir == "" && !hasConvDB {
		return fmt.Errorf("conversation %q not found in profile %q", convID, srcProfile)
	}

	// 2. Move brain directory if present
	if srcConvDir != "" {
		destBrainDir := filepath.Join(destProfileDir, subBrainRel)
		if err := os.MkdirAll(destBrainDir, 0700); err != nil {
			return fmt.Errorf("failed to create destination brain directory: %w", err)
		}
		destConvDir := filepath.Join(destBrainDir, convID)

		// Clean up stale destination directory if it already exists
		_ = os.RemoveAll(destConvDir)

		// Move conversation brain directory atomically
		if err := os.Rename(srcConvDir, destConvDir); err != nil {
			return fmt.Errorf("failed to move conversation brain directory: %w", err)
		}
	}

	// 3. Migrate conversation SQLite database (.db, .db-wal, .db-shm)
	migrateConversationDB(convID, srcProfileDir, destProfileDir)

	// 4. Migrate conversation annotations (.pbtxt)
	migrateConversationAnnotations(convID, srcProfileDir, destProfileDir)

	// 5. Migrate conversation summaries
	migrateConversationSummary(convID, srcProfileDir, destProfileDir)

	// 6. Migrate history.jsonl entries
	migrateHistoryEntry(convID, srcProfileDir, destProfileDir)

	// 7. Clean up stale presence lock in source profile if any
	cleanStalePresence(convID, srcProfileDir)

	// 8. Update last active conversation pointer to point to this convID
	_ = SaveLastConversation(convID)

	// 9. Invalidate stale session cache entry in source profile
	if cache, err := LoadSessionCache(); err == nil {
		delete(cache, srcProfile+":"+convID)
		_ = SaveSessionCache(cache)
	}

	return nil
}

func migrateConversationDB(convID, srcProfileDir, destProfileDir string) {
	for _, sub := range []string{
		filepath.Join(".gemini", "antigravity-cli", "conversations"),
		filepath.Join(".gemini", "antigravity", "conversations"),
	} {
		srcDir := filepath.Join(srcProfileDir, sub)
		destDir := filepath.Join(destProfileDir, sub)

		for _, ext := range []string{".db", ".db-wal", ".db-shm"} {
			srcFile := filepath.Join(srcDir, convID+ext)
			destFile := filepath.Join(destDir, convID+ext)
			if _, err := os.Stat(srcFile); err == nil {
				_ = os.MkdirAll(destDir, 0700)
				_ = os.Remove(destFile)
				_ = os.Rename(srcFile, destFile)
			}
		}
	}
}

func migrateConversationAnnotations(convID, srcProfileDir, destProfileDir string) {
	for _, sub := range []string{
		filepath.Join(".gemini", "antigravity-cli", "annotations"),
		filepath.Join(".gemini", "antigravity", "annotations"),
	} {
		srcDir := filepath.Join(srcProfileDir, sub)
		destDir := filepath.Join(destProfileDir, sub)

		srcFile := filepath.Join(srcDir, convID+".pbtxt")
		destFile := filepath.Join(destDir, convID+".pbtxt")
		if _, err := os.Stat(srcFile); err == nil {
			_ = os.MkdirAll(destDir, 0700)
			_ = os.Remove(destFile)
			_ = os.Rename(srcFile, destFile)
		}
	}
}

func cleanStalePresence(convID, srcProfileDir string) {
	for _, sub := range []string{
		filepath.Join(".gemini", "antigravity-cli", "presence"),
		filepath.Join(".gemini", "antigravity", "presence"),
	} {
		_ = os.Remove(filepath.Join(srcProfileDir, sub, convID+".lock"))
	}
}

func migrateConversationSummary(convID, srcProfileDir, destProfileDir string) {
	sqlitePath, err := exec.LookPath("sqlite3")
	if err != nil {
		return
	}

	escapedConvID := strings.ReplaceAll(convID, "'", "''")

	for _, sub := range []string{
		filepath.Join(".gemini", "antigravity-cli", "conversation_summaries.db"),
		filepath.Join(".gemini", "antigravity", "conversation_summaries.db"),
	} {
		srcDB := filepath.Join(srcProfileDir, sub)
		destDB := filepath.Join(destProfileDir, sub)

		if _, err := os.Stat(srcDB); err != nil {
			continue
		}

		_ = os.MkdirAll(filepath.Dir(destDB), 0700)

		// Check if source DB has this conversation
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		checkCmd := exec.CommandContext(ctx, sqlitePath, srcDB, fmt.Sprintf("SELECT count(*) FROM conversation_summaries WHERE conversation_id = '%s';", escapedConvID))
		out, err := checkCmd.Output()
		cancel()
		if err != nil || strings.TrimSpace(string(out)) == "0" {
			continue
		}

		// If destination DB doesn't exist yet, initialize schema from source
		if _, err := os.Stat(destDB); os.IsNotExist(err) {
			ctxSchema, cancelSchema := context.WithTimeout(context.Background(), 2*time.Second)
			dumpCmd := exec.CommandContext(ctxSchema, sqlitePath, srcDB, ".schema")
			schemaOut, schemaErr := dumpCmd.Output()
			cancelSchema()
			if schemaErr == nil && len(schemaOut) > 0 {
				ctxInit, cancelInit := context.WithTimeout(context.Background(), 2*time.Second)
				initCmd := exec.CommandContext(ctxInit, sqlitePath, destDB, string(schemaOut))
				_ = initCmd.Run()
				cancelInit()
			}
		}

		// Insert or replace row from source to destination
		escapedSrcDB := strings.ReplaceAll(srcDB, "'", "''")
		attachSQL := fmt.Sprintf(
			"ATTACH DATABASE '%s' AS src; INSERT OR REPLACE INTO conversation_summaries SELECT * FROM src.conversation_summaries WHERE conversation_id = '%s'; DETACH DATABASE src;",
			escapedSrcDB, escapedConvID,
		)
		ctxMigrate, cancelMigrate := context.WithTimeout(context.Background(), 2*time.Second)
		migrateCmd := exec.CommandContext(ctxMigrate, sqlitePath, destDB, attachSQL)
		_ = migrateCmd.Run()
		cancelMigrate()

		// Delete row from source DB to keep it clean
		ctxDel, cancelDel := context.WithTimeout(context.Background(), 2*time.Second)
		delCmd := exec.CommandContext(ctxDel, sqlitePath, srcDB, fmt.Sprintf("DELETE FROM conversation_summaries WHERE conversation_id = '%s';", escapedConvID))
		_ = delCmd.Run()
		cancelDel()
	}
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
