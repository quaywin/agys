package profile

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// RunCmdWithSignals executes `agy` with the specified profile environment,
// isolated in its own process group, and propagates termination signals
// (SIGINT, SIGTERM, SIGHUP) to prevent orphan processes.
func RunCmdWithSignals(ctx context.Context, profileDir string, args ...string) error {
	execCmd := BuildCmd(profileDir, args...)

	setupProcessGroup(execCmd)

	sigCtx, stopSignal := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stopSignal()

	if err := execCmd.Start(); err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		done <- execCmd.Wait()
	}()

	select {
	case err := <-done:
		return err

	case <-sigCtx.Done():
		// Send graceful termination signal to process group
		terminateProcessGroup(execCmd, syscall.SIGTERM)

		// Give the process group a grace period to exit gracefully
		select {
		case err := <-done:
			return err
		case <-time.After(2 * time.Second):
			// Force kill process group if grace period expires
			killProcessGroup(execCmd)
			return <-done
		}
	}
}
