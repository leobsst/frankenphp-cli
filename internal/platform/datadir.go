// Package platform holds what differs from one OS to another: paths,
// elevation, process handling. OS-specific code lives in files suffixed
// with the OS name (_windows.go) or guarded by build tags.
package platform

import "os"

// DataDirEnv overrides the data directory location.
const DataDirEnv = "FRANKENMANAGER_DATA_DIR"

// DataDir returns the FrankenManager data directory: $FRANKENMANAGER_DATA_DIR
// when set, the OS default otherwise (see defaultDataDir).
func DataDir() (string, error) {
	if dir := os.Getenv(DataDirEnv); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return defaultDataDir(home), nil
}
