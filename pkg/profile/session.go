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
	"runtime"
	"sort"
	"strings"
	"sync"
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
var userPathRegex = regexp.MustCompile(`(?:/Users/|/home/|/root/|/private/|/var/|/tmp/|/opt/|[A-Za-z]:[/\\])[a-zA-Z0-9_\-\.\/\\]+`)

// FindProjectRoot attempts to locate the root directory of a project given a file or directory path.
// It searches upwards for common project root indicator files (.git, go.mod, package.json, Cargo.toml, pyproject.toml, etc.).
// Results are cached in-memory for instant lookups across multiple sessions.
// Returns empty string if no project root is found.
func FindProjectRoot(path string) string {
	if path == "" {
		return ""
	}

	path = filepath.Clean(path)
	if cached, ok := projectRootCache.Load(path); ok {
		return cached.(string)
	}

	root := findProjectRootUncached(path)
	projectRootCache.Store(path, root)
	return root
}

func findProjectRootUncached(path string) string {
	info, err := os.Stat(path)
	curr := path
	if err == nil && !info.IsDir() {
		curr = filepath.Dir(path)
	}

	userHome, _ := os.UserHomeDir()
	agysDir, _ := GetAgysDir()

	for curr != "" && curr != "/" && curr != "." {
		if cached, ok := projectRootCache.Load(curr); ok {
			return cached.(string)
		}

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

type sessionCandidate struct {
	profile        string
	convID         string
	convDir        string
	transcriptPath string
	modTime        time.Time
	fileMTime      int64
	fileSize       int64
}

// ListSessions scans all profile brain directories and returns recorded conversation sessions.
// Utilizes persistent on-disk metadata caching and concurrent worker pools for fast lookups.
func ListSessions(ctx context.Context, filter SessionFilter) ([]ConversationSession, error) {
	profiles, err := List()
	if err != nil {
		return nil, err
	}

	cache, _ := LoadSessionCache()
	if cache == nil {
		cache = make(SessionCache)
	}

	var candidates []sessionCandidate
	seenConvIDs := make(map[string]bool)

	for _, p := range profiles {
		if filter.Profile != "" && !strings.EqualFold(filter.Profile, p) {
			continue
		}

		profileDir, err := GetProfileDir(p)
		if err != nil {
			continue
		}

		for _, brainDir := range getProfileBrainDirs(profileDir) {
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
				mapKey := p + ":" + convID
				if seenConvIDs[mapKey] {
					continue
				}
				seenConvIDs[mapKey] = true

				convDir := filepath.Join(brainDir, convID)
				transcriptPath := filepath.Join(convDir, ".system_generated", "logs", "transcript.jsonl")

				info, err := os.Stat(transcriptPath)
				var mTime time.Time
				var fileMTime int64
				var fileSize int64

				if err == nil {
					mTime = info.ModTime()
					fileMTime = info.ModTime().UnixNano()
					fileSize = info.Size()
				} else {
					dirInfo, err := os.Stat(convDir)
					if err != nil {
						continue
					}
					mTime = dirInfo.ModTime()
					fileMTime = dirInfo.ModTime().UnixNano()
					fileSize = 0
				}

				candidates = append(candidates, sessionCandidate{
					profile:        p,
					convID:         convID,
					convDir:        convDir,
					transcriptPath: transcriptPath,
					modTime:        mTime,
					fileMTime:      fileMTime,
					fileSize:       fileSize,
				})
			}
		}
	}

	var allSessions []ConversationSession
	var toParse []sessionCandidate

	for _, cand := range candidates {
		key := cand.profile + ":" + cand.convID
		if cachedItem, exists := cache[key]; exists &&
			cachedItem.TranscriptMTime == cand.fileMTime &&
			cachedItem.TranscriptSize == cand.fileSize {
			// Cache hit: use cached session info
			allSessions = append(allSessions, ConversationSession{
				Profile:     cachedItem.Profile,
				ConvID:      cachedItem.ConvID,
				ModTime:     cachedItem.ModTime,
				ProjectPath: cachedItem.ProjectPath,
				ProjectName: cachedItem.ProjectName,
				UserPrompt:  cachedItem.UserPrompt,
			})
		} else {
			// Cache miss or modified file: queue for parsing
			toParse = append(toParse, cand)
		}
	}

	// Concurrent parsing for cache misses
	if len(toParse) > 0 {
		workers := runtime.NumCPU() * 2
		if workers < 4 {
			workers = 4
		} else if workers > 32 {
			workers = 32
		}
		if workers > len(toParse) {
			workers = len(toParse)
		}

		type parseResult struct {
			cand sessionCandidate
			sess ConversationSession
		}

		tasksChan := make(chan sessionCandidate, len(toParse))
		resultsChan := make(chan parseResult, len(toParse))
		var wg sync.WaitGroup

		for w := 0; w < workers; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for cand := range tasksChan {
					select {
					case <-ctx.Done():
						return
					default:
					}
					sess := parseSessionInfo(cand.profile, cand.convID, cand.transcriptPath, cand.modTime)
					resultsChan <- parseResult{cand: cand, sess: sess}
				}
			}()
		}

		for _, item := range toParse {
			tasksChan <- item
		}
		close(tasksChan)

		wg.Wait()
		close(resultsChan)

		cacheChanged := false
		for res := range resultsChan {
			allSessions = append(allSessions, res.sess)
			cache[res.cand.profile+":"+res.cand.convID] = CachedSessionInfo{
				Profile:         res.sess.Profile,
				ConvID:          res.sess.ConvID,
				ModTime:         res.sess.ModTime,
				ProjectPath:     res.sess.ProjectPath,
				ProjectName:     res.sess.ProjectName,
				UserPrompt:      res.sess.UserPrompt,
				TranscriptMTime: res.cand.fileMTime,
				TranscriptSize:  res.cand.fileSize,
			}
			cacheChanged = true
		}

		if cacheChanged {
			_ = SaveSessionCache(cache)
		}
	}

	// Filter sessions by project if specified
	var filteredSessions []ConversationSession
	var pFilter string
	if filter.Project != "" {
		pFilter = strings.ToLower(filter.Project)
	}

	for _, sess := range allSessions {
		if pFilter != "" {
			projPath := strings.ToLower(sess.ProjectPath)
			projName := strings.ToLower(sess.ProjectName)
			if !strings.Contains(projPath, pFilter) && !strings.Contains(projName, pFilter) {
				continue
			}
		}
		filteredSessions = append(filteredSessions, sess)
	}

	// Sort sessions by modification time descending
	sort.Slice(filteredSessions, func(i, j int) bool {
		return filteredSessions[i].ModTime.After(filteredSessions[j].ModTime)
	})

	if filter.Limit > 0 && len(filteredSessions) > filter.Limit {
		filteredSessions = filteredSessions[:filter.Limit]
	}

	return filteredSessions, nil
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

			if bytes.Contains(line, []byte("/Users/")) || bytes.Contains(line, []byte("/home/")) || bytes.Contains(line, []byte("/private/")) || bytes.Contains(line, []byte("/var/")) || bytes.Contains(line, []byte("/tmp/")) || bytes.Contains(line, []byte("C:\\")) || bytes.Contains(line, []byte("D:\\")) {
				matches := userPathRegex.FindAll(line, 10)
				for _, m := range matches {
					pStr := strings.TrimRight(string(m), `"'\,);:`)
					if !strings.Contains(pStr, "/.agys/") && !strings.Contains(pStr, "\\.agys\\") && !strings.Contains(pStr, "/builtin/") {
						rawPaths = append(rawPaths, pStr)
						// Check if we can resolve root immediately
						if sess.ProjectPath == "" {
							root := FindProjectRoot(pStr)
							if root != "" {
								sess.ProjectPath = root
								sess.ProjectName = filepath.Base(root)
							}
						}
					}
				}
			}

			// Early exit if we found both prompt and project root
			if firstPrompt != "" && sess.ProjectPath != "" {
				break
			}
		}

		if err != nil || lineCount > 50 {
			break
		}
	}

	if firstPrompt != "" {
		sess.UserPrompt = firstPrompt
	}

	// Final fallback for project root resolution from raw paths if not yet resolved
	if sess.ProjectPath == "" {
		for _, p := range rawPaths {
			root := FindProjectRoot(p)
			if root != "" {
				sess.ProjectPath = root
				sess.ProjectName = filepath.Base(root)
				break
			}
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
