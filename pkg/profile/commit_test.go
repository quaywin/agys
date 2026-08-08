package profile

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatDiffForPrompt(t *testing.T) {
	stagedFiles := []string{"main.go", "pkg/profile/commit.go"}
	diffContent := "diff --git a/main.go b/main.go\n+ func main() {}\n"

	formatted := FormatDiffForPrompt(stagedFiles, diffContent)
	if !strings.Contains(formatted, "main.go") || !strings.Contains(formatted, "pkg/profile/commit.go") {
		t.Errorf("expected formatted prompt to contain staged file names, got: %s", formatted)
	}
	if !strings.Contains(formatted, "func main()") {
		t.Errorf("expected formatted prompt to contain diff content, got: %s", formatted)
	}

	// Test diff truncation
	largeDiff := strings.Repeat("a", MaxDiffBytesForPrompt+1000)
	truncatedFormatted := FormatDiffForPrompt(stagedFiles, largeDiff)
	if !strings.Contains(truncatedFormatted, "Staged diff truncated") {
		t.Errorf("expected large diff to be truncated")
	}
}

func TestDefaultCommitConstants(t *testing.T) {
	if DefaultCommitModel != "gemini-3.5-flash" {
		t.Errorf("expected DefaultCommitModel to be 'gemini-3.5-flash', got %q", DefaultCommitModel)
	}
	if DefaultCommitEffort != "low" {
		t.Errorf("expected DefaultCommitEffort to be 'low', got %q", DefaultCommitEffort)
	}
}

func TestParseCommitCheckResult(t *testing.T) {
	t.Run("Standard structured output with generated message", func(t *testing.T) {
		output := `
CHECK_SUMMARY:
- Clean - No issues found.
- Proper error handling implemented.

COMMIT_MESSAGE:
feat(commit): add stage commit feature
`
		res := ParseCommitCheckResult(output, "")
		if !strings.Contains(res.CheckSummary, "Clean - No issues found") {
			t.Errorf("unexpected check summary: %q", res.CheckSummary)
		}
		if res.CommitMessage != "feat(commit): add stage commit feature" {
			t.Errorf("unexpected commit message: %q", res.CommitMessage)
		}
	})

	t.Run("User provided message override", func(t *testing.T) {
		output := `
CHECK_SUMMARY:
- Found 1 minor warning.

COMMIT_MESSAGE:
feat: ignore this
`
		userMsg := "fix(core): explicit user commit message"
		res := ParseCommitCheckResult(output, userMsg)
		if res.CommitMessage != userMsg {
			t.Errorf("expected user provided message %q, got: %q", userMsg, res.CommitMessage)
		}
		if !strings.Contains(res.CheckSummary, "Found 1 minor warning") {
			t.Errorf("unexpected check summary: %q", res.CheckSummary)
		}
	})

	t.Run("Fallback parsing for conventional commit without headers", func(t *testing.T) {
		output := `Some analysis notes here.
refactor(auth): simplify token refresh logic
`
		res := ParseCommitCheckResult(output, "")
		if res.CommitMessage != "refactor(auth): simplify token refresh logic" {
			t.Errorf("expected fallback conventional commit message, got: %q", res.CommitMessage)
		}
	})

	t.Run("Markdown bold headers and code blocks", func(t *testing.T) {
		output := `**CHECK_SUMMARY:**
- Clean - No issues found.

**COMMIT_MESSAGE:**
` + "```" + `
fix(auth): persist refreshed OAuth token to disk
` + "```" + `
`
		res := ParseCommitCheckResult(output, "")
		if !strings.Contains(res.CheckSummary, "Clean - No issues found") {
			t.Errorf("unexpected check summary: %q", res.CheckSummary)
		}
		if res.CommitMessage != "fix(auth): persist refreshed OAuth token to disk" {
			t.Errorf("expected commit message 'fix(auth): persist refreshed OAuth token to disk', got: %q", res.CommitMessage)
		}
	})
}

func TestGitRepositoryChecks(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agys-test-repo-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	if IsGitRepository(tmpDir) {
		t.Errorf("expected non-git temp dir to return false for IsGitRepository")
	}

	// Initialize git repo in temp dir for testing
	gitInitCmd := execCommandInDir(tmpDir, "git", "init")
	if err := gitInitCmd.Run(); err != nil {
		t.Skipf("git command not available or failed: %v", err)
	}

	if !IsGitRepository(tmpDir) {
		t.Errorf("expected initialized temp git repo to return true for IsGitRepository")
	}

	// Test empty staged files
	staged, err := GetStagedFiles(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error getting staged files: %v", err)
	}
	if len(staged) != 0 {
		t.Errorf("expected 0 staged files, got %d", len(staged))
	}

	// Create and stage a test file
	testFile := filepath.Join(tmpDir, "test.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	gitAddCmd := execCommandInDir(tmpDir, "git", "add", "test.txt")
	if err := gitAddCmd.Run(); err != nil {
		t.Fatalf("git add failed: %v", err)
	}

	staged, err = GetStagedFiles(tmpDir)
	if err != nil {
		t.Fatalf("failed to get staged files: %v", err)
	}
	if len(staged) != 1 || staged[0] != "test.txt" {
		t.Errorf("expected staged file ['test.txt'], got: %v", staged)
	}

	diff, err := GetStagedDiff(tmpDir)
	if err != nil {
		t.Fatalf("failed to get staged diff: %v", err)
	}
	if !strings.Contains(diff, "hello world") {
		t.Errorf("expected diff to contain 'hello world', got: %s", diff)
	}
}

func execCommandInDir(dir string, name string, args ...string) *exec.Cmd {
	cmd := execCommand(name, args...)
	cmd.Dir = dir
	return cmd
}

var execCommand = func(name string, args ...string) *exec.Cmd {
	return exec.Command(name, args...)
}
