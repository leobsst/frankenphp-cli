//go:build !windows

package platform

import (
	"errors"
	"os/exec"
	"syscall"
)

// ProcessAlive reports whether pid runs. A process of another user
// (EPERM) is alive too.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

// Terminate sends SIGTERM to pid.
func Terminate(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

// Detach makes cmd outlive this process, in its own session (setsid).
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}
