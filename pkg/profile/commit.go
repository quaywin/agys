package profile

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	MaxDiffBytesForPrompt = 30000 // 30 KB limit for prompt context
	DefaultCommitModel    = "gemini-3.7-flash"
	DefaultCommitEffort   = "low"
)

// CommitCheckResult holds the parsed output from agy code review and commit message generation.
type CommitCheckResult struct {
	CheckSummary  string
	CommitMessage string
}

// IsGitRepository checks whether the target directory is inside a git repository.
func IsGitRepository(repoDir string) bool {
	cmd := exec.Command("git", "rev-parse", "--is-inside-work-tree")
	if repoDir != "" {
		cmd.Dir = repoDir
	}
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// StageTrackedFiles stages modified and deleted tracked files (equivalent to git add -u).
func StageTrackedFiles(repoDir string) error {
	cmd := exec.Command("git", "add", "-u")
	if repoDir != "" {
		cmd.Dir = repoDir
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to stage files (git add -u): %w: %s", err, strings.TrimSpace(string(output)))
	}
	return nil
}

// GetStagedFiles returns a list of relative file paths currently staged in git.
func GetStagedFiles(repoDir string) ([]string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	if repoDir != "" {
		cmd.Dir = repoDir
	}
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to get staged file list: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var files []string
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			files = append(files, trimmed)
		}
	}
	return files, nil
}

// GetStagedDiff returns the raw git diff of all staged changes.
func GetStagedDiff(repoDir string) (string, error) {
	cmd := exec.Command("git", "diff", "--cached")
	if repoDir != "" {
		cmd.Dir = repoDir
	}
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get staged diff: %w", err)
	}
	return string(output), nil
}

// GetStagedDiffStat returns the high-level git diff --stat summary of staged changes.
func GetStagedDiffStat(repoDir string) (string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--stat")
	if repoDir != "" {
		cmd.Dir = repoDir
	}
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get staged diff stat: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// ExecuteGitCommit executes `git commit -m <message>` in repoDir.
func ExecuteGitCommit(repoDir string, commitMessage string) error {
	cmd := exec.Command("git", "commit", "-m", commitMessage)
	if repoDir != "" {
		cmd.Dir = repoDir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git commit failed: %w", err)
	}
	return nil
}

// GetCurrentBranch returns the active git branch name.
func GetCurrentBranch(repoDir string) (string, error) {
	cmd := exec.Command("git", "branch", "--show-current")
	if repoDir != "" {
		cmd.Dir = repoDir
	}
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get current branch: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

// ExecuteGitPush pushes committed changes to the current remote branch.
func ExecuteGitPush(repoDir string) error {
	branch, _ := GetCurrentBranch(repoDir)

	var stderrBuf bytes.Buffer
	cmd := exec.Command("git", "push")
	if repoDir != "" {
		cmd.Dir = repoDir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderrBuf
	cmd.Stdin = os.Stdin

	err := cmd.Run()
	if err != nil {
		errStr := stderrBuf.String()
		if errStr != "" {
			fmt.Fprint(os.Stderr, errStr)
		}

		// Handle missing upstream branch automatically (e.g. git push -u origin <branch>)
		if branch != "" && (strings.Contains(errStr, "has no upstream branch") || strings.Contains(errStr, "set-upstream")) {
			fmt.Fprintf(os.Stderr, "[agys] Setting upstream and pushing to origin %s...\n", branch)
			setUpstreamCmd := exec.Command("git", "push", "-u", "origin", branch)
			if repoDir != "" {
				setUpstreamCmd.Dir = repoDir
			}
			setUpstreamCmd.Stdout = os.Stdout
			setUpstreamCmd.Stderr = os.Stderr
			setUpstreamCmd.Stdin = os.Stdin
			return setUpstreamCmd.Run()
		}
		return fmt.Errorf("git push failed: %w", err)
	}

	return nil
}

const MaxPerFileDiffBytes = 5000 // 5 KB limit per file to avoid single file hogging prompt

// IsIgnoredOrLockFile returns true if the file is a lockfile, minified asset, binary, or auto-generated output.
func IsIgnoredOrLockFile(filename string) bool {
	base := filepath.Base(filename)
	baseLower := strings.ToLower(base)

	lockFiles := map[string]bool{
		"package-lock.json": true,
		"yarn.lock":         true,
		"pnpm-lock.yaml":    true,
		"bun.lockb":         true,
		"go.sum":            true,
		"cargo.lock":        true,
		"poetry.lock":       true,
		"pipfile.lock":      true,
		"composer.lock":     true,
		"mix.lock":          true,
		"flake.lock":        true,
	}
	if lockFiles[baseLower] {
		return true
	}

	ignoredExts := []string{
		".min.js", ".min.css", ".map", ".svg", ".png", ".jpg",
		".jpeg", ".gif", ".ico", ".wasm", ".pdf", ".zip", ".gz", ".tar",
		".exe", ".dll", ".so", ".dylib", ".o", ".a",
	}
	for _, ext := range ignoredExts {
		if strings.HasSuffix(baseLower, ext) {
			return true
		}
	}

	cleanPath := filepath.ToSlash(filename)
	ignoredDirs := []string{"/vendor/", "/dist/", "/build/", "/.next/", "/node_modules/"}
	for _, dir := range ignoredDirs {
		if strings.Contains("/"+cleanPath, dir) {
			return true
		}
	}

	return false
}

// splitGitDiff splits raw git diff into per-file diff blocks.
func splitGitDiff(rawDiff string) []string {
	rawDiff = strings.TrimSpace(rawDiff)
	if rawDiff == "" {
		return nil
	}
	if !strings.HasPrefix(rawDiff, "diff --git ") {
		return []string{rawDiff}
	}

	parts := strings.Split(rawDiff, "diff --git ")
	var fileDiffs []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			fileDiffs = append(fileDiffs, "diff --git "+p)
		}
	}
	return fileDiffs
}

// extractFilePathFromDiffHeader attempts to parse the relative file path from a diff --git header.
func extractFilePathFromDiffHeader(fileDiff string) string {
	lines := strings.Split(fileDiff, "\n")
	if len(lines) == 0 {
		return ""
	}
	header := lines[0]
	if strings.HasPrefix(header, "diff --git ") {
		parts := strings.Fields(header)
		if len(parts) >= 4 {
			bPath := parts[3]
			if strings.HasPrefix(bPath, "b/") {
				return bPath[2:]
			}
			return bPath
		}
	}
	return ""
}

// FormatDiffForPrompt formats staged file list and diff content for prompt (maintained for compatibility).
func FormatDiffForPrompt(stagedFiles []string, diffContent string) string {
	return FormatDiffForPromptWithStat(stagedFiles, diffContent, "")
}

// FormatDiffForPromptWithStat formats staged files, git diff stat, and smart per-file diffs for the AI prompt context.
func FormatDiffForPromptWithStat(stagedFiles []string, diffContent string, diffStat string) string {
	var sb strings.Builder
	sb.WriteString("Staged Files Summary:\n")
	for _, f := range stagedFiles {
		sb.WriteString("- ")
		sb.WriteString(f)
		if IsIgnoredOrLockFile(f) {
			sb.WriteString(" (lockfile/asset - diff omitted)")
		}
		sb.WriteString("\n")
	}

	if strings.TrimSpace(diffStat) != "" {
		sb.WriteString("\nGit Diff Stat:\n")
		sb.WriteString(strings.TrimSpace(diffStat))
		sb.WriteString("\n")
	}

	sb.WriteString("\nStaged Code Diffs:\n")

	if strings.TrimSpace(diffContent) == "" {
		sb.WriteString("(No diff content available)\n")
		return sb.String()
	}

	fileDiffs := splitGitDiff(diffContent)
	totalBytes := sb.Len()

	for _, fileDiff := range fileDiffs {
		filePath := extractFilePathFromDiffHeader(fileDiff)
		if filePath != "" && IsIgnoredOrLockFile(filePath) {
			sb.WriteString(fmt.Sprintf("\n--- %s ---\n[Lockfile / auto-generated diff omitted]\n", filePath))
			continue
		}

		formattedFileDiff := fileDiff
		if len(fileDiff) > MaxPerFileDiffBytes {
			formattedFileDiff = fileDiff[:MaxPerFileDiffBytes] + fmt.Sprintf("\n... [Diff for %s truncated: first %d bytes shown] ...\n", filePath, MaxPerFileDiffBytes)
		}

		if totalBytes+len(formattedFileDiff) > MaxDiffBytesForPrompt {
			rem := MaxDiffBytesForPrompt - totalBytes
			if rem > 200 {
				sb.WriteString(formattedFileDiff[:rem])
				sb.WriteString("\n... [Overall staged diff truncated due to prompt size limit] ...\n")
			} else {
				sb.WriteString("\n... [Overall staged diff truncated due to prompt size limit] ...\n")
			}
			break
		}

		sb.WriteString(formattedFileDiff)
		sb.WriteString("\n\n")
		totalBytes = sb.Len()
	}

	return sb.String()
}

// CleanTerminalMarkdown strips markdown symbols, LaTeX math expressions, and formatting artifacts for clean CLI and Git log display.
func CleanTerminalMarkdown(text string) string {
	if text == "" {
		return ""
	}

	// 1. Replace LaTeX arrows and math symbols
	text = strings.ReplaceAll(text, `$\rightarrow$`, "->")
	text = strings.ReplaceAll(text, `$\to$`, "->")
	text = strings.ReplaceAll(text, `\rightarrow`, "->")
	text = strings.ReplaceAll(text, `\to`, "->")
	text = strings.ReplaceAll(text, `$\leftarrow$`, "<-")
	text = strings.ReplaceAll(text, `\leftarrow`, "<-")
	text = strings.ReplaceAll(text, `$\Rightarrow$`, "=>")
	text = strings.ReplaceAll(text, `\Rightarrow`, "=>")
	text = strings.ReplaceAll(text, `$\leftrightarrow$`, "<->")
	text = strings.ReplaceAll(text, `\leftrightarrow`, "<->")

	// 2. Remove remaining inline math $...$ wrappers
	mathRegex := regexp.MustCompile(`\$([^\$\n]+)\$`)
	text = mathRegex.ReplaceAllString(text, "$1")

	// 3. Remove bold / italic markdown markers (**text**, *text*, __text__, _text_)
	boldRegex := regexp.MustCompile(`\*\*([^*]+)\*\*`)
	text = boldRegex.ReplaceAllString(text, "$1")

	boldUnderscoreRegex := regexp.MustCompile(`__([^_]+)__`)
	text = boldUnderscoreRegex.ReplaceAllString(text, "$1")

	// 4. Clean up inline backticks around words (e.g. `Indicators` -> Indicators)
	backtickRegex := regexp.MustCompile("`([^`\n]+)`")
	text = backtickRegex.ReplaceAllString(text, "$1")

	// 5. Remove markdown code fences if any
	text = strings.ReplaceAll(text, "```", "")

	// 6. Clean up line by line
	lines := strings.Split(text, "\n")
	var cleanedLines []string
	for _, l := range lines {
		trimmed := strings.TrimRight(l, " \t\r")
		cleanedLines = append(cleanedLines, trimmed)
	}

	return strings.TrimSpace(strings.Join(cleanedLines, "\n"))
}

// ParseCommitCheckResult extracts CHECK_SUMMARY and COMMIT_MESSAGE from agy prompt output.
func ParseCommitCheckResult(output string, userProvidedMessage string) CommitCheckResult {
	cleaned := strings.TrimSpace(output)
	result := CommitCheckResult{
		CommitMessage: strings.TrimSpace(userProvidedMessage),
	}

	// 1. Try extracting CHECK_SUMMARY
	summaryRegex := regexp.MustCompile(`(?i)(?:\*\*|\#\#\s*)?CHECK_SUMMARY:?\s*\*?\*?\s*\n?([\s\S]*?)(?:(?:\*\*|\#\#\s*)?COMMIT_MESSAGE:?|$)`)
	if match := summaryRegex.FindStringSubmatch(cleaned); len(match) > 1 {
		result.CheckSummary = CleanTerminalMarkdown(match[1])
	}

	// 2. Try extracting COMMIT_MESSAGE if user didn't explicitly provide one
	if result.CommitMessage == "" {
		msgRegex := regexp.MustCompile(`(?i)(?:\*\*|\#\#\s*)?COMMIT_MESSAGE:?\s*\*?\*?\s*\n?([\s\S]*?)$`)
		if match := msgRegex.FindStringSubmatch(cleaned); len(match) > 1 {
			msgBlock := strings.TrimSpace(match[1])
			msgCleaned := CleanTerminalMarkdown(msgBlock)

			lines := strings.Split(msgCleaned, "\n")
			var filteredLines []string
			for _, line := range lines {
				tLine := strings.TrimSpace(line)
				tLine = strings.Trim(tLine, "`\"'")
				if strings.HasPrefix(strings.ToLower(tLine), "check_summary") {
					continue
				}
				filteredLines = append(filteredLines, tLine)
			}

			fullMsg := strings.TrimSpace(strings.Join(filteredLines, "\n"))
			if fullMsg != "" {
				result.CommitMessage = fullMsg
			}
		}
	}

	// Fallback parsing if headers weren't found
	if result.CommitMessage == "" {
		lines := strings.Split(cleaned, "\n")
		convRegex := regexp.MustCompile(`(?i)^(feat|fix|refactor|docs|style|test|chore|build|ci|perf)(\(.*\))?: .+`)

		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			line = CleanTerminalMarkdown(line)
			line = strings.Trim(line, "`\"'*")
			if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") {
				line = strings.TrimSpace(line[2:])
			}
			if convRegex.MatchString(line) {
				result.CommitMessage = line
				break
			}
		}

		if result.CommitMessage == "" {
			for i := len(lines) - 1; i >= 0; i-- {
				line := strings.TrimSpace(lines[i])
				line = CleanTerminalMarkdown(line)
				if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "*") && !strings.HasPrefix(line, "`") {
					result.CommitMessage = strings.Trim(line, "\"`'")
					break
				}
			}
		}
	}

	if result.CheckSummary == "" {
		if idx := strings.Index(cleaned, "COMMIT_MESSAGE:"); idx > 0 {
			result.CheckSummary = CleanTerminalMarkdown(cleaned[:idx])
		} else {
			result.CheckSummary = "Clean - No issues reported."
		}
	}

	return result
}

// ExecAgyPrompt runs `agy -p "<prompt>"` non-interactively under the target profile environment.
func ExecAgyPrompt(ctx context.Context, profileDir string, prompt string, extraArgs ...string) (string, error) {
	_ = SyncTrustedWorkspaces()

	args := []string{"-p", prompt}
	if len(extraArgs) > 0 {
		args = append(args, extraArgs...)
	}

	execCmd := BuildCmd(profileDir, args...)

	var stdoutBuf, stderrBuf bytes.Buffer
	execCmd.Stdout = &stdoutBuf
	execCmd.Stderr = &stderrBuf
	execCmd.Stdin = nil

	var expectedRefreshToken string
	baseName := filepath.Base(profileDir)
	if initTok, readErr := ReadToken(baseName); readErr == nil && initTok != nil {
		expectedRefreshToken = initTok.Token.RefreshToken
	}

	err := execCmd.Run()
	SyncKeychainTokenToDisk(profileDir, expectedRefreshToken)

	outStr := stdoutBuf.String()
	if err != nil {
		errStr := strings.TrimSpace(stderrBuf.String())
		if errStr != "" {
			return outStr, fmt.Errorf("agy execution failed: %w (%s)", err, errStr)
		}
		return outStr, fmt.Errorf("agy execution failed: %w", err)
	}

	return outStr, nil
}

// RunAgyCommitCheck performs the AI review and/or commit message generation using the specified profile.
func RunAgyCommitCheck(ctx context.Context, profileDir string, stagedFiles []string, diffContent string, diffStat string, userMsg string, noCheck bool, model string, effort string, customPrompt string) (*CommitCheckResult, error) {
	diffFormatted := FormatDiffForPromptWithStat(stagedFiles, diffContent, diffStat)

	var promptBuilder strings.Builder
	promptBuilder.WriteString("You are an expert software developer and Git assistant.\n")
	promptBuilder.WriteString("Formatting rules:\n")
	promptBuilder.WriteString("- Output in clean plain text suitable for terminal and git log.\n")
	promptBuilder.WriteString("- Do NOT use LaTeX math syntax (e.g. use '->' instead of '$\\rightarrow$').\n")
	promptBuilder.WriteString("- Do NOT use markdown bold tags like **bold** in bullet points.\n\n")

	if noCheck {
		promptBuilder.WriteString("Generate a Conventional Commit message based on the staged changes.\n")
		promptBuilder.WriteString("Commit message structure:\n")
		promptBuilder.WriteString("- Line 1: Concise Conventional Commit title (< 72 chars, e.g. feat(scope): title).\n")
		promptBuilder.WriteString("- If changes touch multiple files or complex logic: add a blank line followed by 2-4 concise bullet points detailing key changes.\n")
		promptBuilder.WriteString("- If changes are small/simple: output only the single-line commit title.\n\n")
		promptBuilder.WriteString(diffFormatted)
		if customPrompt != "" {
			promptBuilder.WriteString("\n\nAdditional user instructions: ")
			promptBuilder.WriteString(customPrompt)
		}
		promptBuilder.WriteString("\n\nOutput strictly in this format:\n")
		promptBuilder.WriteString("COMMIT_MESSAGE:\n<conventional commit title>\n\n- <bullet point 1>\n- <bullet point 2>\n")
	} else if userMsg != "" {
		promptBuilder.WriteString(fmt.Sprintf("The user wants to commit with message: %q.\n\n", userMsg))
		promptBuilder.WriteString(diffFormatted)
		promptBuilder.WriteString("\n\nReview the staged changes for any potential bugs, secrets, API keys, or syntax errors.\n")
		promptBuilder.WriteString("Output strictly in this format:\n")
		promptBuilder.WriteString("CHECK_SUMMARY:\n- <bullet point notes or 'Clean - No issues found'>\n")
	} else {
		promptBuilder.WriteString("Analyze the staged changes, perform a code review check, and generate a Conventional Commit message.\n")
		promptBuilder.WriteString("Commit message structure:\n")
		promptBuilder.WriteString("- Line 1: Concise Conventional Commit title (< 72 chars, e.g. feat(scope): title).\n")
		promptBuilder.WriteString("- If changes touch multiple files or complex logic: add a blank line followed by 2-4 concise bullet points detailing key changes.\n")
		promptBuilder.WriteString("- If changes are small/simple: output only the single-line commit title.\n\n")
		promptBuilder.WriteString(diffFormatted)
		if customPrompt != "" {
			promptBuilder.WriteString("\n\nAdditional user instructions: ")
			promptBuilder.WriteString(customPrompt)
		}
		promptBuilder.WriteString("\n\nOutput strictly in this format:\n")
		promptBuilder.WriteString("CHECK_SUMMARY:\n- <bullet point notes or 'Clean - No issues found'>\n\n")
		promptBuilder.WriteString("COMMIT_MESSAGE:\n<conventional commit title>\n\n- <bullet point 1>\n- <bullet point 2>\n")
	}

	if model == "" {
		model = DefaultCommitModel
	}
	if effort == "" {
		effort = DefaultCommitEffort
	}

	var extraArgs []string
	if model != "" {
		extraArgs = append(extraArgs, "--model", model)
	}
	if effort != "" {
		extraArgs = append(extraArgs, "--effort", effort)
	}

	outStr, err := ExecAgyPrompt(ctx, profileDir, promptBuilder.String(), extraArgs...)
	if err != nil {
		if strings.TrimSpace(outStr) == "" {
			return nil, err
		}
		return nil, fmt.Errorf("%w (output: %s)", err, strings.TrimSpace(outStr))
	}

	res := ParseCommitCheckResult(outStr, userMsg)
	return &res, nil
}

