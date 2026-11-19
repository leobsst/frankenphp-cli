//go:build !darwin && !windows

package platform

import "path/filepath"

// defaultProjectsDir is ~/projects on Linux and the other Unix systems.
func defaultProjectsDir(home string) string {
	return filepath.Join(home, "projects")
}
