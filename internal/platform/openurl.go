package platform

import (
	"os/exec"
	"runtime"
)

// OpenURLCommand is the command that opens url in the default browser.
func OpenURLCommand(url string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url)
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		return exec.Command("xdg-open", url)
	}
}
