// Package alias installs the short alias of the frankenmanager command
// (fm by default), next to it: a relative symlink on macOS and Linux, a
// .cmd shim on Windows. frankenmanager itself is never touched.
package alias

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/platform"
)

// Command is the name of the main command, always available.
const Command = "frankenmanager"

// CommandFile returns the file name of the main command on goos.
func CommandFile(goos string) string {
	if goos == "windows" {
		return Command + ".exe"
	}
	return Command
}

// shimMarker identifies a Windows shim written by FrankenManager.
const shimMarker = "REM FrankenManager alias"

// ConflictError reports a file of the alias name that is not ours.
type ConflictError struct{ Path string }

func (e *ConflictError) Error() string {
	return fmt.Sprintf("%s already exists and is not a FrankenManager alias", e.Path)
}

// Installer installs aliases in BinDir, pointing to Target.
type Installer struct {
	GOOS   string
	BinDir string // directory of the frankenmanager command
	Target string // file name of the command in BinDir (frankenmanager[.exe])
	// LookPath finds a command in PATH (exec.LookPath when nil)
	LookPath func(name string) (string, error)
}

// Locate returns the installer of the running binary: next to the
// frankenmanager found in PATH when it is this binary, next to the binary
// otherwise (e.g. a development build).
func Locate() (*Installer, error) {
	exe, err := platform.Executable()
	if err != nil {
		return nil, err
	}
	i := &Installer{GOOS: runtime.GOOS, BinDir: filepath.Dir(exe), Target: filepath.Base(exe)}
	if p, err := exec.LookPath(Command); err == nil {
		if real, err := filepath.EvalSymlinks(p); err == nil && real == exe {
			if abs, err := filepath.Abs(p); err == nil {
				p = abs
			}
			i.BinDir, i.Target = filepath.Dir(p), filepath.Base(p)
		}
	}
	return i, nil
}

// Path returns the file of the alias name.
func (i *Installer) Path(name string) string {
	if i.GOOS == "windows" {
		return filepath.Join(i.BinDir, name+".cmd")
	}
	return filepath.Join(i.BinDir, name)
}

// shim is the content of a Windows alias.
func (i *Installer) shim() string {
	return "@echo off\r\n" + shimMarker + "\r\n\"%~dp0" + i.Target + "\" %*\r\n"
}

// Owns reports whether the file of the alias name is an alias of ours.
func (i *Installer) Owns(name string) bool {
	p := i.Path(name)
	if i.GOOS == "windows" {
		data, err := os.ReadFile(p)
		return err == nil && strings.Contains(string(data), shimMarker)
	}
	link, err := os.Readlink(p)
	if err != nil {
		return false
	}
	if link == i.Target {
		return true
	}
	// An absolute link to the same binary
	real, err1 := filepath.EvalSymlinks(p)
	self, err2 := filepath.EvalSymlinks(filepath.Join(i.BinDir, i.Target))
	return err1 == nil && err2 == nil && real == self
}

// Installed reports whether the alias name is in place.
func (i *Installer) Installed(name string) bool { return name != "" && i.Owns(name) }

// Install creates the alias name, doing nothing when it is already ours.
// A file of that name that is not ours is a *ConflictError.
func (i *Installer) Install(name string) error {
	if name == "" {
		return nil
	}
	p := i.Path(name)
	if _, err := os.Lstat(p); err == nil {
		if i.Owns(name) {
			return nil
		}
		return &ConflictError{Path: p}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if i.GOOS == "windows" {
		return os.WriteFile(p, []byte(i.shim()), 0o755)
	}
	return os.Symlink(i.Target, p)
}

// Remove removes the alias name when it is ours, and reports whether it
// did.
func (i *Installer) Remove(name string) (bool, error) {
	if name == "" || !i.Owns(name) {
		return false, nil
	}
	if err := os.Remove(i.Path(name)); err != nil {
		return false, err
	}
	return true, nil
}

// ManualCommand is the command that creates the alias name by hand, for
// when the directory is not writable.
func (i *Installer) ManualCommand(name string) string {
	if i.GOOS == "windows" {
		return fmt.Sprintf(`Set-Content -Path "%s" -Value '@"%%~dp0%s" %%*'`, i.Path(name), i.Target)
	}
	return fmt.Sprintf("sudo ln -s %s %s", i.Target, i.Path(name))
}

// Shadowed returns the command PATH runs for name when it is not the alias
// of BinDir (another program of that name comes first), "" otherwise.
func (i *Installer) Shadowed(name string) string {
	lookPath := i.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	p, err := lookPath(name)
	if err != nil {
		return ""
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if strings.EqualFold(p, i.Path(name)) {
		return ""
	}
	return p
}
