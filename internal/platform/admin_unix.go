//go:build !windows

package platform

import "os"

// IsAdmin reports whether the process runs as root.
func IsAdmin() bool { return os.Geteuid() == 0 }
