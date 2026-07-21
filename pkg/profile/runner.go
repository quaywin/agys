package profile

import (
	"os"
	"os/exec"
)

// RunWithFailover executes execCmd.
// For interactive TUI mode, it runs directly with standard OS descriptors for 100% terminal compatibility and zero SIGKILL.
// For non-interactive mode with failover enabled, it uses pipe interceptors to capture quota errors.
func RunWithFailover(execCmd *exec.Cmd, outWriter, errWriter *QuotaInterceptorWriter, effectiveFailover bool, isInteractive bool) error {
	if !effectiveFailover || isInteractive {
		// Direct execution: native terminal TTY, zero PTY issues, zero SIGKILL
		execCmd.Stdout = os.Stdout
		execCmd.Stderr = os.Stderr
		execCmd.Stdin = os.Stdin
		return execCmd.Run()
	}

	// Non-interactive mode: use pipe interceptors
	execCmd.Stdout = outWriter
	execCmd.Stderr = errWriter
	execCmd.Stdin = os.Stdin
	return execCmd.Run()
}
