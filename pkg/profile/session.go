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
var userPathRegex = regexp.MustCompile(`(?:/Volumes/|/Users/|/home/|/root/|/private/|/var/|/tmp/|/opt/|/mnt/|[a-zA-Z]:[/\\][a-zA-Z0-9_])[a-zA-Z0-9_\-\.\/\\]*`)
var workspaceUriRegex = regexp.MustCompile(`(?:file://)?((?:/Volumes/|/Users/|/home/|/root/|/private/|/var/|/tmp/|/opt/|/mnt/|[a-zA-Z]:[/\\][a-zA-Z0-9_])[^\s"'\n\r>]+)\s*->`)

// NormalizePath resolves symlinks and cleans a path if possible.
func NormalizePath(p string) string {
	if p == "" {
		return ""
	}
	p = filepath.Clean(p)
	if resolved, err := filepath.EvalSymlinks(p); err == nil && resolved != "" {
		return filepath.Clean(resolved)
	}
	return p
}

// FindProjectRoot attempts to locate the root directory of a project given a file or directory path.
// It searches upwards for common project root indicator files (.git, go.mod, package.json, Cargo.toml, pyproject.toml, AGENTS.md, etc.).
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
	path = ExpandTilde(path)
	info, err := os.Stat(path)
	curr := path
	if err == nil && !info.IsDir() {
		curr = filepath.Dir(path)
	}

	userHome, _ := GetRealUserHome()
	agysDir, _ := GetAgysDir()

	for curr != "" && curr != "/" && curr != "." && curr != filepath.VolumeName(curr)+"\\" && curr != filepath.VolumeName(curr)+"/" {
		if cached, ok := projectRootCache.Load(curr); ok {
			return cached.(string)
		}

		if userHome != "" && curr == userHome {
			break
		}
		if agysDir != "" && strings.HasPrefix(curr, agysDir) {
			break
		}
		if curr == "/Volumes" || curr == "/private" || curr == "/tmp" || curr == "/var" || curr == "/opt" || curr == "/mnt" {
			break
		}

		markers := []string{
			".git", "go.mod", "package.json", "Cargo.toml", "pyproject.toml",
			"pom.xml", "build.gradle", "build.gradle.kts", "mix.exs", "Makefile",
			"CMakeLists.txt", "Gemfile", "composer.json", "requirements.txt",
			"setup.py", "Pipfile", "deno.json", "bun.lockb",
			"AGENTS.md", "GEMINI.md", "CLAUDE.md", ".agent", ".agents", ".claude",
		}
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

func isSystemOrHomeDir(p string) bool {
	if p == "" || p == "/" || p == "." {
		return true
	}
	if strings.ContainsAny(p, "\n\r\t") || strings.Contains(p, `\n`) || strings.Contains(p, `\r`) {
		return true
	}
	p = filepath.Clean(p)
	userHome, _ := GetRealUserHome()
	if userHome != "" && (p == userHome || p == NormalizePath(userHome)) {
		return true
	}
	agysDir, _ := GetAgysDir()
	if agysDir != "" && strings.HasPrefix(p, agysDir) {
		return true
	}
	// System root paths
	switch p {
	case "/Users", "/home", "/root", "/Volumes", "/private", "/tmp", "/var", "/opt", "/mnt", "/etc", "/usr":
		return true
	}
	// Windows volume root like C:\ or C:
	if p == filepath.VolumeName(p) || p == filepath.VolumeName(p)+"\\" || p == filepath.VolumeName(p)+"/" {
		return true
	}
	// External volume mount root (e.g. /Volumes/QUAYWIN or /mnt/drive)
	if strings.HasPrefix(p, "/Volumes/") && len(strings.Split(filepath.ToSlash(p), "/")) <= 3 {
		return true
	}
	return false
}

// ResolveProjectPathAndName attempts to derive a meaningful project path and name from a given path.
// If FindProjectRoot succeeds, it uses the root directory.
// Otherwise, it falls back to the cleaned directory path and base name (ignoring system root directories).
func ResolveProjectPathAndName(rawPath string) (projectPath, projectName string) {
	if rawPath == "" {
		return "", ""
	}
	if strings.ContainsAny(rawPath, "\n\r\t") || strings.Contains(rawPath, `\n`) || strings.Contains(rawPath, `\r`) {
		return "", ""
	}
	rawPath = ExpandTilde(rawPath)

	// Try finding project root first
	root := FindProjectRoot(rawPath)
	if root != "" && !isSystemOrHomeDir(root) {
		return root, filepath.Base(root)
	}

	// Fallback to directory path if valid
	cleaned := filepath.Clean(rawPath)
	if isSystemOrHomeDir(cleaned) {
		return "", ""
	}

	info, err := os.Stat(cleaned)
	if err == nil && !info.IsDir() {
		cleaned = filepath.Dir(cleaned)
	}

	if isSystemOrHomeDir(cleaned) {
		return "", ""
	}

	base := filepath.Base(cleaned)
	if base == "" || base == "/" || base == "." || strings.ContainsAny(base, ":\n\r\t") {
		return "", ""
	}

	return cleaned, base
}

// MatchProject returns true if the session matches the given project filter string.
// Handles case-insensitive comparison, folder names, full paths, and symlink resolution.
func MatchProject(filterProject string, sess ConversationSession) bool {
	if filterProject == "" {
		return true
	}
	filterProject = ExpandTilde(filterProject)

	fLower := strings.ToLower(strings.TrimSpace(filterProject))
	pNameLower := strings.ToLower(strings.TrimSpace(sess.ProjectName))

	if pNameLower == "(global)" || pNameLower == "" {
		return false
	}

	// If filter is a path (contains / or \), match by exact folder name and canonical paths
	if strings.ContainsAny(filterProject, "/\\") {
		fBase := strings.ToLower(filepath.Base(filterProject))
		if fBase != "" && fBase != "." && fBase != "/" && pNameLower == fBase {
			return true
		}

		if sess.ProjectPath != "" && !isSystemOrHomeDir(sess.ProjectPath) {
			normFilter := strings.ToLower(NormalizePath(filterProject))
			normSessPath := strings.ToLower(NormalizePath(sess.ProjectPath))

			if normFilter != "" && normSessPath != "" {
				if normFilter == normSessPath {
					return true
				}
				if strings.HasPrefix(normSessPath, normFilter+string(filepath.Separator)) {
					return true
				}
				if strings.HasPrefix(normFilter, normSessPath+string(filepath.Separator)) {
					return true
				}
			}
		}
		return false
	}

	// Filter is a plain name (e.g. "agys", "caudata")
	if pNameLower == fLower || strings.Contains(pNameLower, fLower) {
		return true
	}

	return false
}

type historyEntry struct {
	workspace string
	display   string
	timestamp int64
}

// loadProfileHistory reads history.jsonl from profile directory and returns map of convID -> historyEntry.
func loadProfileHistory(profileDir string) map[string]historyEntry {
	res := make(map[string]historyEntry)
	if profileDir == "" {
		return res
	}

	historyPaths := []string{
		filepath.Join(profileDir, ".gemini", "antigravity-cli", "history.jsonl"),
		filepath.Join(profileDir, ".gemini", "antigravity", "history.jsonl"),
	}

	for _, hp := range historyPaths {
		f, err := os.Open(hp)
		if err != nil {
			continue
		}

		scanner := bufio.NewScanner(f)
		buf := make([]byte, 128*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := scanner.Bytes()
			if len(line) == 0 || !bytes.Contains(line, []byte("conversationId")) {
				continue
			}

			var item struct {
				Display        string `json:"display"`
				Workspace      string `json:"workspace"`
				ConversationID string `json:"conversationId"`
				Timestamp      int64  `json:"timestamp"`
				Type           string `json:"type"`
			}

			if err := json.Unmarshal(line, &item); err == nil && item.ConversationID != "" {
				existing, exists := res[item.ConversationID]
				if !exists {
					res[item.ConversationID] = historyEntry{
						workspace: item.Workspace,
						display:   item.Display,
						timestamp: item.Timestamp,
					}
				} else {
					if existing.workspace == "" && item.Workspace != "" {
						existing.workspace = item.Workspace
					}
					if (existing.display == "" || strings.HasPrefix(existing.display, "/")) && item.Display != "" && !strings.HasPrefix(item.Display, "/") {
						existing.display = item.Display
					}
					res[item.ConversationID] = existing
				}
			}
		}
		_ = f.Close()
	}

	return res
}

type sessionCandidate struct {
	profile        string
	convID         string
	convDir        string
	transcriptPath string
	modTime        time.Time
	fileMTime      int64
	fileSize       int64
	history        historyEntry
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

		histMap := loadProfileHistory(profileDir)

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
					history:        histMap[convID],
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
			cachedItem.TranscriptSize == cand.fileSize &&
			cachedItem.ProjectName != "" &&
			cachedItem.ProjectName != "(Global)" &&
			cachedItem.ProjectPath != "" &&
			!isSystemOrHomeDir(cachedItem.ProjectPath) {
			// Cache hit: use cached session info with valid project metadata
			allSessions = append(allSessions, ConversationSession{
				Profile:     cachedItem.Profile,
				ConvID:      cachedItem.ConvID,
				ModTime:     cachedItem.ModTime,
				ProjectPath: cachedItem.ProjectPath,
				ProjectName: cachedItem.ProjectName,
				UserPrompt:  cachedItem.UserPrompt,
			})
		} else {
			// Cache miss or missing/invalid project info: queue for parsing
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
					sess := parseSessionInfo(cand.profile, cand.convID, cand.transcriptPath, cand.modTime, cand.history)
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

	// Filter sessions by project if specified and filter out automated AI commit/tool checks
	var filteredSessions []ConversationSession
	for _, sess := range allSessions {
		if IsInternalAutomatedSession(sess.UserPrompt) {
			continue
		}
		if !filter.All && filter.Project != "" {
			if !MatchProject(filter.Project, sess) {
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

func parseSessionInfo(profileName, convID, transcriptPath string, defaultTime time.Time, hist historyEntry) ConversationSession {
	sess := ConversationSession{
		Profile:     profileName,
		ConvID:      convID,
		ModTime:     defaultTime,
		ProjectName: "(Global)",
		UserPrompt:  "(No prompt summary)",
	}

	// 1. Check history entry workspace first
	if hist.workspace != "" {
		pPath, pName := ResolveProjectPathAndName(hist.workspace)
		if pName != "" {
			sess.ProjectPath = pPath
			sess.ProjectName = pName
		}
	}

	if hist.display != "" && !strings.HasPrefix(hist.display, "/") {
		sess.UserPrompt = cleanPromptSummary(hist.display)
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
						firstPrompt = cleanPromptSummary(match[1])
					}
				}
			}

			// Check workspace URI from <user_information> block
			if sess.ProjectPath == "" && (bytes.Contains(line, []byte("<user_information>")) || bytes.Contains(line, []byte("active workspaces")) || bytes.Contains(line, []byte("->"))) {
				if match := workspaceUriRegex.FindSubmatch(line); len(match) > 1 {
					wsPath := strings.TrimSpace(string(match[1]))
					pPath, pName := ResolveProjectPathAndName(wsPath)
					if pName != "" {
						sess.ProjectPath = pPath
						sess.ProjectName = pName
					}
				}
			}

			if bytes.Contains(line, []byte("/Volumes/")) ||
				bytes.Contains(line, []byte("/Users/")) ||
				bytes.Contains(line, []byte("/home/")) ||
				bytes.Contains(line, []byte("/private/")) ||
				bytes.Contains(line, []byte("/var/")) ||
				bytes.Contains(line, []byte("/tmp/")) ||
				bytes.Contains(line, []byte("/opt/")) ||
				bytes.Contains(line, []byte("/mnt/")) ||
				bytes.Contains(line, []byte(":\\")) ||
				bytes.Contains(line, []byte(":/")) {
				matches := userPathRegex.FindAll(line, 10)
				for _, m := range matches {
					pStr := strings.TrimRight(string(m), `"'\,);:`)
					if !strings.Contains(pStr, "/.agys/") &&
						!strings.Contains(pStr, "\\.agys\\") &&
						!strings.Contains(pStr, "/builtin/") &&
						!strings.Contains(pStr, "/antigravity-cli/brain") &&
						!strings.Contains(pStr, "/antigravity/brain") {
						rawPaths = append(rawPaths, pStr)
						if sess.ProjectPath == "" {
							pPath, pName := ResolveProjectPathAndName(pStr)
							if pName != "" {
								sess.ProjectPath = pPath
								sess.ProjectName = pName
							}
						}
					}
				}
			}

			if firstPrompt != "" && sess.ProjectPath != "" {
				break
			}
		}

		if err != nil || lineCount > 100 {
			break
		}
	}

	if firstPrompt != "" {
		sess.UserPrompt = firstPrompt
	}

	// Final fallback for project root resolution from raw paths if not yet resolved
	if sess.ProjectPath == "" {
		for _, p := range rawPaths {
			pPath, pName := ResolveProjectPathAndName(p)
			if pName != "" {
				sess.ProjectPath = pPath
				sess.ProjectName = pName
				break
			}
		}
	}

	return sess
}

// IsInternalAutomatedSession checks if a session was generated by an internal automated tool (like agys commit check).
// Uses strict prefix matching and internal signature markers to completely eliminate false positives.
func IsInternalAutomatedSession(userPrompt string) bool {
	p := strings.TrimSpace(userPrompt)
	if strings.HasPrefix(p, "[AGYS_INTERNAL_COMMIT_CHECK]") ||
		strings.HasPrefix(p, "You are an expert software developer and Git assistant") {
		return true
	}
	return false
}

func cleanPromptSummary(raw string) string {
	raw = strings.TrimSpace(raw)
	raw = strings.ReplaceAll(raw, "\r\n", " ")
	raw = strings.ReplaceAll(raw, "\n", " ")
	raw = strings.ReplaceAll(raw, "\r", " ")
	raw = strings.ReplaceAll(raw, "\t", " ")
	raw = strings.ReplaceAll(raw, "\\n", " ")
	raw = strings.ReplaceAll(raw, "\\r", " ")
	raw = strings.ReplaceAll(raw, "\\t", " ")
	for strings.Contains(raw, "  ") {
		raw = strings.ReplaceAll(raw, "  ", " ")
	}
	raw = strings.TrimSpace(raw)
	if len(raw) > 90 {
		raw = raw[:87] + "..."
	}
	if raw == "" {
		return "(No prompt summary)"
	}
	return raw
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
