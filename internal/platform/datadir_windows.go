package platform

import "path/filepath"

// defaultDataDir is <home>\AppData\Local\frankenmanager on Windows: built
// from the home directory, not from %LOCALAPPDATA%, so that it stays the
// directory earlier releases used and existing installations keep their
// data.
func defaultDataDir(home string) string {
	return filepath.Join(home, "AppData", "Local", "frankenmanager")
}
