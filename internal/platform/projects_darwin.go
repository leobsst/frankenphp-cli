package platform

import "path/filepath"

// defaultProjectsDir is ~/Sites on macOS, the historical web root of the
// user (Apache's UserDir, Laravel Valet).
func defaultProjectsDir(home string) string {
	return filepath.Join(home, "Sites")
}
