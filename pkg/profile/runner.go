package profile

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// RunWithFailover executes execCmd. If effectiveFailover is true, it captures output to inspect for quota errors.
// When isInteractive is true and standard I/O is attached to a terminal, it uses a PTY to preserve interactive TUI features.
func RunWithFailover(execCmd *exec.Cmd, outWriter, errWriter *QuotaInterceptorWriter, effectiveFailover bool, isInteractive bool) error {
	if !effectiveFailover {
		return execCmd.Run()
	}

	stdinFd := int(os.Stdin.Fd())
	stdoutFd := int(os.Stdout.Fd())

	// Use PTY if interactive TUI mode and standard I/O is a terminal
	if isInteractive && term.IsTerminal(stdinFd) && term.IsTerminal(stdoutFd) {
		ptmx, err := pty.Start(execCmd)
		if err != nil {
			return fmt.Errorf("failed to start pty: %w", err)
		}
		defer func() { _ = ptmx.Close() }()

		// Handle terminal resize (SIGWINCH)
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGWINCH)
		go func() {
			for range ch {
				_ = pty.InheritSize(os.Stdout, ptmx)
			}
		}()
		ch <- syscall.SIGWINCH // Send initial resize signal
		defer func() {
			signal.Stop(ch)
			close(ch)
		}()

		// Set stdin in raw mode so key combinations pass through directly
		oldState, err := term.MakeRaw(stdinFd)
		if err == nil {
			defer func() { _ = term.Restore(stdinFd, oldState) }()
		}

		// Copy user's stdin into PTY master
		go func() {
			_, _ = io.Copy(ptmx, os.Stdin)
		}()

		// Copy PTY master output into both real os.Stdout AND interceptor writers
		multiWriter := io.MultiWriter(os.Stdout, outWriter, errWriter)
		_, _ = io.Copy(multiWriter, ptmx)

		return execCmd.Wait()
	}

	// Non-interactive or non-terminal mode: use standard Pipe interceptors
	execCmd.Stdout = outWriter
	execCmd.Stderr = errWriter
	return execCmd.Run()
}
