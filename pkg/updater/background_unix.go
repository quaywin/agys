//go:build !windows

package updater

import (
	"os"
	"os/exec"
	"syscall"
)

// SpawnDetachedProcess launches a process detached from the current terminal session.
func SpawnDetachedProcess(execPath string, args []string, logFile string) error {
	cmd := exec.Command(execPath, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
	cmd.Stdin = nil

	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
		if err == nil {
			cmd.Stdout = f
			cmd.Stderr = f
		} else {
			cmd.Stdout = nil
			cmd.Stderr = nil
		}
	} else {
		cmd.Stdout = nil
		cmd.Stderr = nil
	}

	return cmd.Start()
}
