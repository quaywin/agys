package profile

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ConversationSession represents a recorded conversation session across profiles.
type ConversationSession struct {
	Profile     string    `json:"profile"`
	ConvID      string    `json:"conversation_id"`
	ModTime     time.Time `json:"mod_time"`
	ProjectPath string    `json:"project_path"`
	ProjectName string    `json:"project_name"`
	UserPrompt  string    `json:"user_prompt"`
}

// SessionFilter provides criteria for filtering sessions.
type SessionFilter struct {
	Project string
	Profile string
	All     bool
	Limit   int
}

var userRequestRegex = regexp.MustCompile(`(?s)<USER_REQUEST>\s*(.*?)\s*</USER_REQUEST>`)
var userPathRegex = regexp.MustCompile(`(?:/Users/|/home/|/root/|[A-Za-z]:[/\\])[a-zA-Z0-9_\-\.\/\\]+`)

// FindProjectRoot attempts to locate the root directory of a project given a file or directory path.
// It searches upwards for common project root indicator files (.git, go.mod, package.json, Cargo.toml, pyproject.toml, etc.).
// Returns empty string if no project root is found.
func FindProjectRoot(path string) string {
	if path == "" {
		return ""
	}

	path = filepath.Clean(path)
	info, err := os.Stat(path)
	curr := path
	if err == nil && !info.IsDir() {
		curr = filepath.Dir(path)
	}

	userHome, _ := os.UserHomeDir()
	agysDir, _ := GetAgysDir()

	for curr != "" && curr != "/" && curr != "." {
		if userHome != "" && curr == userHome {
			break
		}
		if agysDir != "" && strings.HasPrefix(curr, agysDir) {
			break
		}

		markers := []string{".git", "go.mod", "package.json", "Cargo.toml", "pyproject.toml", "pom.xml", "build.gradle"}
		for _, marker := range markers {
			markerPath := filepath.Join(curr, marker)
			if _, err := os.Stat(markerPath); err == nil {
				return curr
			}
		}

		parent := filepath.Dir(curr)
		if parent == curr {
			break
		}
		curr = parent
	}

	return ""
}

// ListSessions scans all profile brain directories and returns recorded conversation sessions.
func ListSessions(ctx context.Context, filter SessionFilter) ([]ConversationSession, error) {
	profiles, err := List()
	if err != nil {
		return nil, err
	}

	var sessions []ConversationSession

	for _, p := range profiles {
		if filter.Profile != "" && !strings.EqualFold(filter.Profile, p) {
			continue
		}

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
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			if !entry.IsDir() {
				continue
			}

			convID := entry.Name()
			convDir := filepath.Join(brainDir, convID)
			transcriptPath := filepath.Join(convDir, ".system_generated", "logs", "transcript.jsonl")

			info, err := os.Stat(transcriptPath)
			mTime := time.Time{}
			if err == nil {
				mTime = info.ModTime()
			} else {
				dirInfo, err := os.Stat(convDir)
				if err != nil {
					continue
				}
				mTime = dirInfo.ModTime()
			}

			sess := parseSessionInfo(p, convID, transcriptPath, mTime)

			// Apply project filtering if specified
			if filter.Project != "" {
				pFilter := strings.ToLower(filter.Project)
				projPath := strings.ToLower(sess.ProjectPath)
				projName := strings.ToLower(sess.ProjectName)

				if !strings.Contains(projPath, pFilter) && !strings.Contains(projName, pFilter) {
					continue
				}
			}

			sessions = append(sessions, sess)
		}
	}

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].ModTime.After(sessions[j].ModTime)
	})

	if filter.Limit > 0 && len(sessions) > filter.Limit {
		sessions = sessions[:filter.Limit]
	}

	return sessions, nil
}

func parseSessionInfo(profileName, convID, transcriptPath string, defaultTime time.Time) ConversationSession {
	sess := ConversationSession{
		Profile:     profileName,
		ConvID:      convID,
		ModTime:     defaultTime,
		ProjectName: "(Global)",
		UserPrompt:  "(No prompt summary)",
	}

	f, err := os.Open(transcriptPath)
	if err != nil {
		return sess
	}
	defer f.Close()

	reader := bufio.NewReaderSize(f, 64*1024)
	var firstPrompt string
	var rawPaths []string

	lineCount := 0
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			lineCount++
			if firstPrompt == "" && bytes.Contains(line, []byte("<USER_REQUEST>")) {
				var data struct {
					Content string `json:"content"`
				}
				if json.Unmarshal(line, &data) == nil && data.Content != "" {
					match := userRequestRegex.FindStringSubmatch(data.Content)
					if len(match) > 1 {
						cleanPrompt := strings.TrimSpace(match[1])
						cleanPrompt = strings.ReplaceAll(cleanPrompt, "\r\n", " ")
						cleanPrompt = strings.ReplaceAll(cleanPrompt, "\n", " ")
						cleanPrompt = strings.ReplaceAll(cleanPrompt, "\r", " ")
						cleanPrompt = strings.ReplaceAll(cleanPrompt, "\t", " ")
						cleanPrompt = strings.ReplaceAll(cleanPrompt, "\\n", " ")
						cleanPrompt = strings.ReplaceAll(cleanPrompt, "\\r", " ")
						cleanPrompt = strings.ReplaceAll(cleanPrompt, "\\t", " ")
						for strings.Contains(cleanPrompt, "  ") {
							cleanPrompt = strings.ReplaceAll(cleanPrompt, "  ", " ")
						}
						cleanPrompt = strings.TrimSpace(cleanPrompt)
						if len(cleanPrompt) > 90 {
							cleanPrompt = cleanPrompt[:87] + "..."
						}
						firstPrompt = cleanPrompt
					}
				}
			}

			if bytes.Contains(line, []byte("/Users/")) || bytes.Contains(line, []byte("/home/")) || bytes.Contains(line, []byte("C:\\")) {
				matches := userPathRegex.FindAll(line, 10)
				for _, m := range matches {
					pStr := strings.TrimRight(string(m), `"'\,);:`)
					if !strings.Contains(pStr, "/.agys/") && !strings.Contains(pStr, "\\.agys\\") && !strings.Contains(pStr, "/builtin/") {
						rawPaths = append(rawPaths, pStr)
					}
				}
			}
		}

		if err != nil || lineCount > 50 {
			break
		}
	}

	if firstPrompt != "" {
		sess.UserPrompt = firstPrompt
	}

	// Resolve project root from raw paths
	for _, p := range rawPaths {
		root := FindProjectRoot(p)
		if root != "" {
			sess.ProjectPath = root
			sess.ProjectName = filepath.Base(root)
			break
		}
	}

	return sess
}

// FormatRelativeTime formats a time.Time into a human-readable relative duration.
func FormatRelativeTime(t time.Time) string {
	if t.IsZero() {
		return "N/A"
	}
	diff := time.Since(t)
	if diff < time.Minute {
		return "just now"
	} else if diff < time.Hour {
		mins := int(diff.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	} else if diff < 24*time.Hour {
		hours := int(diff.Hours())
		if hours == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hours)
	} else {
		days := int(diff.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// RenderSessionsJSON converts a slice of sessions into formatted JSON string.
func RenderSessionsJSON(sessions []ConversationSession) (string, error) {
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
