package platform

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Chown sets the owner of a file (os.Chown), replaced in tests. On
// Windows, nothing calls it: ChownToSudoUser does nothing there.
var Chown = os.Chown

// MakeUserDir creates path and its missing parents (mkdir -p), then hands
// every directory it created back to the user who invoked sudo. Directories
// that already existed are left untouched (e.g. MariaDB data owned by the
// container user).
func MakeUserDir(path string) error {
	var created []string
	for current := path; ; {
		if _, err := os.Stat(current); err == nil || !errors.Is(err, fs.ErrNotExist) {
			break
		}
		created = append(created, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}

	if err := os.MkdirAll(path, 0o777); err != nil {
		return err
	}
	for i := len(created) - 1; i >= 0; i-- {
		ChownToSudoUser(created[i])
	}
	return nil
}

// RealUserEnv returns env for a process run as the real user (name, home)
// instead of root: HOME, USER and LOGNAME are theirs and the SUDO_*
// variables are dropped, like `update` does for `_sync-resources-internal`.
func RealUserEnv(env []string, name, home string) []string {
	out := make([]string, 0, len(env)+3)
	for _, kv := range env {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "HOME", "USER", "LOGNAME", "SUDO_UID", "SUDO_GID", "SUDO_USER", "SUDO_COMMAND":
			continue
		}
		out = append(out, kv)
	}
	return append(out, "HOME="+home, "USER="+name, "LOGNAME="+name)
}
