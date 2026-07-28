//go:build windows

package mcp

import (
	"os"
	"os/exec"
)

// setProcessGroup is a no-op on Windows: syscall.SysProcAttr has no Unix
// process-group equivalent wired here, so killProcessGroup falls back to
// killing the process directly instead of a group.
func setProcessGroup(_ *exec.Cmd) {}

// killProcessGroup terminates pid directly (no process-group semantics on
// Windows without a Job Object, which stdio MCP servers don't need here).
func killProcessGroup(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
