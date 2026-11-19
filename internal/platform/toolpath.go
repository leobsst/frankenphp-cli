package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// toolDirs are where the tools FrankenManager runs (mkcert, docker) are
// usually installed, missing from the PATH of a service manager (launchd,
// systemd) or of sudo.
func toolDirs() []string {
	switch runtime.GOOS {
	case "darwin":
		return []string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", "/usr/local/sbin",
			"/Applications/Docker.app/Contents/Resources/bin", "/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	case "linux":
		return []string{"/home/linuxbrew/.linuxbrew/bin", "/usr/local/bin", "/usr/local/sbin", "/snap/bin",
			"/usr/bin", "/bin", "/usr/sbin", "/sbin"}
	}
	return nil
}

// ToolPath returns path followed by the usual tool directories it lacks.
func ToolPath(path string) string {
	sep := string(os.PathListSeparator)
	var dirs []string
	seen := map[string]bool{}
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	for _, d := range filepath.SplitList(path) {
		add(d)
	}
	for _, d := range toolDirs() {
		add(d)
	}
	return strings.Join(dirs, sep)
}
