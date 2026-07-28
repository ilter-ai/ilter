//go:build !windows

package mcp

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the child in its own process group so
// killProcessGroup can terminate it and any of its own children together.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends SIGTERM to the process group led by pid.
func killProcessGroup(pid int) error {
	return syscall.Kill(-pid, syscall.SIGTERM)
}
