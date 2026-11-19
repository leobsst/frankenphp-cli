package platform

import "path/filepath"

// defaultProjectsDir is <home>\Projects on Windows.
func defaultProjectsDir(home string) string {
	return filepath.Join(home, "Projects")
}
