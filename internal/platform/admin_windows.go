package platform

import "golang.org/x/sys/windows"

// IsAdmin reports whether the process runs elevated (administrator
// terminal).
func IsAdmin() bool { return windows.GetCurrentProcessToken().IsElevated() }
