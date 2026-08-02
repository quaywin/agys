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

// FormatDiffForPrompt truncates the staged diff if it exceeds MaxDiffBytesForPrompt.
func FormatDiffForPrompt(stagedFiles []string, diffContent string) string {
	var sb strings.Builder
	sb.WriteString("Staged Files:\n")
	for _, f := range stagedFiles {
		sb.WriteString("- ")
		sb.WriteString(f)
		sb.WriteString("\n")
	}
	sb.WriteString("\nStaged Diff:\n")

	if len(diffContent) > MaxDiffBytesForPrompt {
		sb.WriteString(diffContent[:MaxDiffBytesForPrompt])
		sb.WriteString(fmt.Sprintf("\n\n... [Staged diff truncated: showing first %d of %d bytes] ...\n", MaxDiffBytesForPrompt, len(diffContent)))
	} else {
		sb.WriteString(diffContent)
	}

	return sb.String()
}

// ParseCommitCheckResult extracts CHECK_SUMMARY and COMMIT_MESSAGE from agy prompt output.
func ParseCommitCheckResult(output string, userProvidedMessage string) CommitCheckResult {
	cleaned := strings.TrimSpace(output)
	result := CommitCheckResult{
		CommitMessage: strings.TrimSpace(userProvidedMessage),
	}

	// Try extracting CHECK_SUMMARY
	summaryRegex := regexp.MustCompile(`(?i)CHECK_SUMMARY:\s*\n?([\s\S]*?)(?:COMMIT_MESSAGE:|$)`)
	if match := summaryRegex.FindStringSubmatch(cleaned); len(match) > 1 {
		result.CheckSummary = strings.TrimSpace(match[1])
	}

	// Try extracting COMMIT_MESSAGE if user didn't explicitly provide one
	if result.CommitMessage == "" {
		msgRegex := regexp.MustCompile(`(?i)COMMIT_MESSAGE:\s*\n?([^\n]+)`)
		if match := msgRegex.FindStringSubmatch(cleaned); len(match) > 1 {
			result.CommitMessage = strings.TrimSpace(match[1])
			// Strip leading/trailing quotes if agy wrapped message in quotes
			result.CommitMessage = strings.Trim(result.CommitMessage, "\"`'")
		}
	}

	// Fallback parsing if headers weren't found
	if result.CommitMessage == "" {
		// Look for conventional commit prefix in output lines
		lines := strings.Split(cleaned, "\n")
		convRegex := regexp.MustCompile(`^(feat|fix|refactor|docs|style|test|chore|build|ci|perf)(\(.*\))?: .+`)

		for i := len(lines) - 1; i >= 0; i-- {
			line := strings.TrimSpace(lines[i])
			line = strings.Trim(line, "\"`'")
			if convRegex.MatchString(line) {
				result.CommitMessage = line
				break
			}
		}

		// Ultimate fallback: take last non-empty line
		if result.CommitMessage == "" {
			for i := len(lines) - 1; i >= 0; i-- {
				line := strings.TrimSpace(lines[i])
				if line != "" && !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "*") {
					result.CommitMessage = strings.Trim(line, "\"`'")
					break
				}
			}
		}
	}

	if result.CheckSummary == "" {
		if idx := strings.Index(cleaned, "COMMIT_MESSAGE:"); idx > 0 {
			result.CheckSummary = strings.TrimSpace(cleaned[:idx])
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
func RunAgyCommitCheck(ctx context.Context, profileDir string, stagedFiles []string, diffContent string, userMsg string, noCheck bool, model string, customPrompt string) (*CommitCheckResult, error) {
	diffFormatted := FormatDiffForPrompt(stagedFiles, diffContent)

	var promptBuilder strings.Builder
	promptBuilder.WriteString("You are an expert software developer and Git assistant.\n")

	if userMsg != proposedMessagePlaceholder(userMsg) {
		promptBuilder.WriteString(fmt.Sprintf("The user wants to commit with message: %q.\n\n", userMsg))
		promptBuilder.WriteString(diffFormatted)
		promptBuilder.WriteString("\n\nReview the staged changes for any potential bugs, secrets, API keys, or syntax errors.\n")
		promptBuilder.WriteString("Output strictly in this format:\n")
		promptBuilder.WriteString("CHECK_SUMMARY:\n- <bullet point notes or 'Clean - No issues found'>\n")
	} else {
		promptBuilder.WriteString("Analyze the staged changes, perform a code review check, and generate a concise Conventional Commit message.\n\n")
		promptBuilder.WriteString(diffFormatted)
		if customPrompt != "" {
			promptBuilder.WriteString("\n\nAdditional user instructions: ")
			promptBuilder.WriteString(customPrompt)
		}
		promptBuilder.WriteString("\n\nOutput strictly in this format:\n")
		promptBuilder.WriteString("CHECK_SUMMARY:\n- <bullet point notes or 'Clean - No issues found'>\n\n")
		promptBuilder.WriteString("COMMIT_MESSAGE:\n<conventional commit message>\n")
	}

	var extraArgs []string
	if model != "" {
		extraArgs = append(extraArgs, "--model", model)
	}

	outStr, err := ExecAgyPrompt(ctx, profileDir, promptBuilder.String(), extraArgs...)
	if err != nil && strings.TrimSpace(outStr) == "" {
		return nil, err
	}

	res := ParseCommitCheckResult(outStr, userMsg)
	return &res, nil
}

func proposedMessagePlaceholder(userMsg string) string {
	if userMsg != "" {
		return userMsg
	}
	return ""
}
