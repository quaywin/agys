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
