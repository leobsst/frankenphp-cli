package platform

import "os/exec"

// ChownToSudoUser does nothing on Windows, which has no sudo.
func ChownToSudoUser(string) {}

// UnderSudo is always false on Windows.
func UnderSudo() bool { return false }

// AsRealUser does nothing on Windows, which has no sudo.
func AsRealUser(*exec.Cmd) {}
