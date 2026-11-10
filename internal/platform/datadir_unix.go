//go:build !windows

package platform

import "path/filepath"

// defaultDataDir is ~/.frankenmanager on macOS and Linux.
func defaultDataDir(home string) string {
	return filepath.Join(home, ".frankenmanager")
}
