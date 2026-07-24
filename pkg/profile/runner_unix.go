//go:build !windows

package profile

import (
	"os/exec"
	"syscall"
)

func setupProcessGroup(cmd *exec.Cmd) {
	// Do not set Setpgid: true to keep agy in terminal foreground process group
}

func terminateProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd != nil && cmd.Process != nil && cmd.Process.Pid > 0 {
		_ = cmd.Process.Signal(sig)
	}
}

func killProcessGroup(cmd *exec.Cmd) {
	if cmd != nil && cmd.Process != nil && cmd.Process.Pid > 0 {
		_ = cmd.Process.Kill()
	}
}
