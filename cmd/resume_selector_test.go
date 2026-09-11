package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/quaywin/agys/pkg/profile"
)

func TestTruncatePrompt(t *testing.T) {
	cases := []struct {
		input    string
		maxRunes int
		expected string
	}{
		{
			input:    "hello world",
			maxRunes: 20,
			expected: "hello world",
		},
		{
			input:    "hello world from golang",
			maxRunes: 10,
			expected: "hello w...",
		},
		{
			input:    "Line 1\nLine 2\r\nLine 3\tTab",
			maxRunes: 30,
			expected: "Line 1 Line 2 Line 3 Tab",
		},
		{
			input:    "agys resume bỏ cột project và conversation id để hiển thị cho gọn",
			maxRunes: 30,
			expected: "agys resume bỏ cột project ...",
		},
	}

	for _, c := range cases {
		got := truncatePrompt(c.input, c.maxRunes)
		if got != c.expected {
			t.Errorf("truncatePrompt(%q, %d) = %q, expected %q", c.input, c.maxRunes, got, c.expected)
		}
	}
}

func TestFormatSessionLine(t *testing.T) {
	sess := profile.ConversationSession{
		Profile:     "davidnguyen",
		ConvID:      "conv-12345",
		ModTime:     time.Now(),
		UserPrompt:  "fix login bug",
		ProjectName: "agys",
	}

	// Selected line
	selectedLine := formatSessionLine(1, sess, true, 100)
	if !strings.Contains(selectedLine, "❯") {
		t.Errorf("expected selected line to contain arrow '❯', got: %s", selectedLine)
	}
	if !strings.Contains(selectedLine, "[1]") || !strings.Contains(selectedLine, "[davidnguyen]") {
		t.Errorf("expected selected line to contain [1] and [davidnguyen], got: %s", selectedLine)
	}
	if !strings.Contains(selectedLine, "fix login bug") {
		t.Errorf("expected selected line to contain prompt summary, got: %s", selectedLine)
	}

	// Unselected line
	unselectedLine := formatSessionLine(2, sess, false, 100)
	if strings.Contains(unselectedLine, "❯") {
		t.Errorf("expected unselected line NOT to contain arrow '❯', got: %s", unselectedLine)
	}
	if !strings.Contains(unselectedLine, "[2]") || !strings.Contains(unselectedLine, "[davidnguyen]") {
		t.Errorf("expected unselected line to contain [2] and [davidnguyen], got: %s", unselectedLine)
	}
}

func TestGetTerminalWidth(t *testing.T) {
	w := getTerminalWidth()
	if w <= 0 {
		t.Errorf("expected terminal width > 0, got %d", w)
	}
}

func TestFilterSessions(t *testing.T) {
	sessions := []profile.ConversationSession{
		{Profile: "work", ProjectName: "agys", UserPrompt: "fix login bug", ConvID: "conv-111"},
		{Profile: "personal", ProjectName: "website", UserPrompt: "add dark mode styling", ConvID: "conv-222"},
		{Profile: "work", ProjectName: "backend", UserPrompt: "implement payment api", ConvID: "conv-333"},
	}

	// Empty query returns all
	if got := filterSessions(sessions, ""); len(got) != 3 {
		t.Errorf("expected 3 sessions for empty query, got %d", len(got))
	}

	// Filter by prompt keyword
	matchedPrompt := filterSessions(sessions, "dark mode")
	if len(matchedPrompt) != 1 || matchedPrompt[0].ConvID != "conv-222" {
		t.Errorf("expected conv-222 for 'dark mode', got %v", matchedPrompt)
	}

	// Filter by profile
	matchedProfile := filterSessions(sessions, "work")
	if len(matchedProfile) != 2 {
		t.Errorf("expected 2 sessions for profile 'work', got %d", len(matchedProfile))
	}

	// Filter by project
	matchedProject := filterSessions(sessions, "agys")
	if len(matchedProject) != 1 || matchedProject[0].ConvID != "conv-111" {
		t.Errorf("expected conv-111 for project 'agys', got %v", matchedProject)
	}

	// Filter with no matches
	if got := filterSessions(sessions, "nonexistent query"); len(got) != 0 {
		t.Errorf("expected 0 matches, got %d", len(got))
	}
}

func TestGroupSessions(t *testing.T) {
	sessions := []profile.ConversationSession{
		{ProjectName: "agys", ProjectPath: "/path/agys", UserPrompt: "task 1"},
		{ProjectName: "website", ProjectPath: "/path/web", UserPrompt: "task 2"},
		{ProjectName: "agys", ProjectPath: "/path/agys", UserPrompt: "task 3"},
	}

	groups := groupSessions(sessions)
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	if groups[0].ProjectName != "agys" || len(groups[0].Sessions) != 2 {
		t.Errorf("expected group 'agys' with 2 sessions, got %s with %d", groups[0].ProjectName, len(groups[0].Sessions))
	}
	if groups[1].ProjectName != "website" || len(groups[1].Sessions) != 1 {
		t.Errorf("expected group 'website' with 1 session, got %s with %d", groups[1].ProjectName, len(groups[1].Sessions))
	}
	// Verify Index preserves original position in displayed list
	if groups[0].Sessions[0].Index != 0 || groups[0].Sessions[1].Index != 2 {
		t.Errorf("expected indices 0 and 2 for agys sessions, got %d and %d", groups[0].Sessions[0].Index, groups[0].Sessions[1].Index)
	}
}
