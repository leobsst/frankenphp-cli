package platform

import "os"

// DefaultProjectsDir returns the projects root used when DEFAULT_PROJECT_PATH
// is not set: a directory of the user's home following the OS conventions
// (see defaultProjectsDir), "" when the home directory is unknown.
func DefaultProjectsDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return defaultProjectsDir(home)
}
