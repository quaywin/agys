package cmd

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/quaywin/agys/pkg/profile"
	"golang.org/x/term"
)

// getTerminalWidth returns the current terminal column width, or default 100.
func getTerminalWidth() int {
	fd := int(os.Stdout.Fd())
	if term.IsTerminal(fd) {
		w, _, err := term.GetSize(fd)
		if err == nil && w > 30 {
			return w
		}
	}
	return 100
}

// truncatePrompt strips newlines/excess spaces and truncates to maxRunes cleanly.
func truncatePrompt(prompt string, maxRunes int) string {
	prompt = strings.TrimSpace(prompt)
	prompt = strings.ReplaceAll(prompt, "\r\n", " ")
	prompt = strings.ReplaceAll(prompt, "\n", " ")
	prompt = strings.ReplaceAll(prompt, "\r", " ")
	prompt = strings.ReplaceAll(prompt, "\t", " ")
	for strings.Contains(prompt, "  ") {
		prompt = strings.ReplaceAll(prompt, "  ", " ")
	}

	runes := []rune(prompt)
	if len(runes) <= maxRunes {
		return prompt
	}
	if maxRunes <= 3 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-3]) + "..."
}

// formatSessionLine formats a single session in compact 1-line style with ANSI colors.
func formatSessionLine(num int, s profile.ConversationSession, selected bool, termWidth int) string {
	relTime := profile.FormatRelativeTime(s.ModTime)

	// Fixed width calculation:
	// prefix: 4 ("  ❯ " or "    ")
	// num: e.g. "[1] " (4-5 chars)
	// profile: e.g. "[davidnguyen] " (len + 3 chars)
	// time: e.g. "(just now)" (len + 2 chars)
	// spacing: 2 chars
	numStrLen := utf8.RuneCountInString(fmt.Sprintf("[%d] ", num))
	profStrLen := utf8.RuneCountInString(fmt.Sprintf("[%s] ", s.Profile))
	timeStrLen := utf8.RuneCountInString(fmt.Sprintf("(%s)", relTime))
	metaWidth := 4 + numStrLen + profStrLen + timeStrLen + 2

	maxPromptWidth := termWidth - metaWidth
	if maxPromptWidth < 15 {
		maxPromptWidth = 15
	}

	promptClean := truncatePrompt(s.UserPrompt, maxPromptWidth)

	if selected {
		// Active highlighted item
		return fmt.Sprintf("\033[1;36m  ❯\033[0m \033[1;36m[%d]\033[0m \033[1;37m%s\033[0m \033[1;35m[%s]\033[0m \033[90m(%s)\033[0m",
			num, promptClean, s.Profile, relTime)
	}

	// Inactive item
	return fmt.Sprintf("    \033[90m[%d]\033[0m %s \033[35m[%s]\033[0m \033[90m(%s)\033[0m",
		num, promptClean, s.Profile, relTime)
}

// selectSessionInteractive launches an interactive arrow-key selector in the terminal.
func selectSessionInteractive(sessions []profile.ConversationSession) (*profile.ConversationSession, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil, errors.New("stdin is not a terminal")
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}

	restored := false
	restore := func() {
		if !restored {
			_ = term.Restore(fd, oldState)
			fmt.Print("\033[?25h") // Ensure cursor is visible
			restored = true
		}
	}
	defer restore()

	// Hide cursor during interactive menu
	fmt.Print("\033[?25l")

	cursor := 0
	numSessions := len(sessions)
	totalLines := numSessions + 2 // Sessions + blank line + footer

	render := func(firstTime bool) {
		termWidth := getTerminalWidth()
		if !firstTime {
			// Move cursor up to overwrite previous render
			fmt.Printf("\033[%dA", totalLines)
		}

		for i, s := range sessions {
			selected := (i == cursor)
			line := formatSessionLine(i+1, s, selected, termWidth)
			fmt.Printf("\r\033[2K%s\r\n", line)
		}

		// Blank spacer line
		fmt.Print("\r\033[2K\r\n")

		// Footer navigation hint
		var footer string
		if numSessions <= 9 {
			footer = fmt.Sprintf("\033[90m  Navigate: ↑/↓, j/k • Select: Enter, Space • Jump: 1-%d • Quit: Esc, q\033[0m", numSessions)
		} else {
			footer = "\033[90m  Navigate: ↑/↓, j/k • Select: Enter, Space • Quit: Esc, q\033[0m"
		}
		fmt.Printf("\r\033[2K%s\r\n", footer)
	}

	render(true)

	// Cleanly handle Ctrl+C signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	go func() {
		<-sigChan
		restore()
		os.Exit(0)
	}()

	buf := make([]byte, 16)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			return nil, nil
		}
		if n == 0 {
			continue
		}

		// Enter or Space -> confirm selection
		if buf[0] == '\r' || buf[0] == '\n' || (buf[0] == ' ' && n == 1) {
			// Clear interactive menu block for clean terminal state
			fmt.Printf("\033[%dA", totalLines)
			for i := 0; i < totalLines; i++ {
				fmt.Print("\r\033[2K\r\n")
			}
			fmt.Printf("\033[%dA", totalLines)
			restore()
			return &sessions[cursor], nil
		}

		// Quit: 'q', 'Q', Ctrl+C (3), Ctrl+D (4)
		if (buf[0] == 'q' || buf[0] == 'Q' || buf[0] == 3 || buf[0] == 4) && n == 1 {
			restore()
			return nil, nil
		}

		// Escape alone
		if buf[0] == 27 && n == 1 {
			restore()
			return nil, nil
		}

		// Escape sequences
		if buf[0] == 27 && n >= 3 {
			if buf[1] == '[' || buf[1] == 'O' {
				switch buf[2] {
				case 'A': // Up
					cursor = (cursor - 1 + numSessions) % numSessions
					render(false)
					continue
				case 'B': // Down
					cursor = (cursor + 1) % numSessions
					render(false)
					continue
				case 'H', '1', '7': // Home
					cursor = 0
					render(false)
					continue
				case 'F', '4', '8': // End
					cursor = numSessions - 1
					render(false)
					continue
				case '5': // Page Up
					cursor -= 5
					if cursor < 0 {
						cursor = 0
					}
					render(false)
					continue
				case '6': // Page Down
					cursor += 5
					if cursor >= numSessions {
						cursor = numSessions - 1
					}
					render(false)
					continue
				}
			}
		}

		// Single character inputs
		if n == 1 {
			switch buf[0] {
			case 'k', 'K', 16: // k, K, Ctrl+P (Up)
				cursor = (cursor - 1 + numSessions) % numSessions
				render(false)
				continue
			case 'j', 'J', 14: // j, J, Ctrl+N (Down)
				cursor = (cursor + 1) % numSessions
				render(false)
				continue
			case 'g': // Jump to top
				cursor = 0
				render(false)
				continue
			case 'G': // Jump to bottom
				cursor = numSessions - 1
				render(false)
				continue
			}

			// Number jump 1..9
			if buf[0] >= '1' && buf[0] <= '9' {
				num := int(buf[0] - '1')
				if num < numSessions {
					cursor = num
					render(false)
					continue
				}
			}
		}
	}
}
