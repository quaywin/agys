//go:build windows

package updater

import (
	"os"
	"os/exec"
	"syscall"
)

// SpawnDetachedProcess launches a process detached from the console on Windows.
func SpawnDetachedProcess(execPath string, args []string, logFile string) error {
	cmd := exec.Command(execPath, args...)
	// 0x00000200 = CREATE_NEW_PROCESS_GROUP, 0x08000000 = DETACHED_PROCESS
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x00000200 | 0x08000000,
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
