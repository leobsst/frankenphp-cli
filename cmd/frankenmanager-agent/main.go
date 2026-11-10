// Command frankenmanager-agent is the executable of FrankenManager.app, the
// macOS app shipped next to the CLI:
//
//	frankenmanager/
//	    frankenmanager                      <- the real CLI
//	    FrankenManager.app/Contents/MacOS/
//	        frankenmanager-agent            <- this program
//
// The background items started by FrankenManager (the LAN DNS watcher's
// LaunchAgent) run through it so that macOS attributes them to the app and
// shows "FrankenManager" in Login Items, instead of the name on the code
// signing certificate. It only replaces itself with the CLI, with the same
// arguments.
package main

import (
	"errors"
	"os"
	"path/filepath"
)

// cliName is the CLI next to the app.
const cliName = "frankenmanager"

// cliPath returns the CLI of the bundle holding the app whose executable is
// exe: four levels up from its real path (MacOS, Contents, the app, then the
// bundle directory), like the C launcher.
func cliPath(exe string) (string, error) {
	real, err := filepath.EvalSymlinks(exe)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(real) {
		return "", errors.New("executable path is not absolute: " + real)
	}
	dir := real
	for range 4 {
		dir = filepath.Dir(dir)
	}
	return filepath.Join(dir, cliName), nil
}

func main() {
	os.Exit(run(os.Args, os.Stderr))
}
