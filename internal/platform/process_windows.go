package platform

import (
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// stillActive is the exit code of a process that has not exited.
const stillActive = 259

// ProcessAlive reports whether pid runs. It only queries the process
// (OpenProcess, GetExitCodeProcess): a kill(pid, 0) check, as on Unix, can
// terminate the process on Windows instead of probing it.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer func() { _ = windows.CloseHandle(h) }()
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// Terminate ends pid (Windows has no SIGTERM).
func Terminate(pid int) error {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return err
	}
	defer func() { _ = windows.CloseHandle(h) }()
	return windows.TerminateProcess(h, 1)
}

// Detach makes cmd outlive this process, without console.
func Detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS,
	}
}
