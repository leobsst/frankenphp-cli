package alias

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newInstaller(t *testing.T, goos string) *Installer {
	t.Helper()
	dir := t.TempDir()
	i := &Installer{GOOS: goos, BinDir: dir, Target: CommandFile(goos)}
	if err := os.WriteFile(filepath.Join(dir, i.Target), []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return i
}

func TestInstallAndRemoveSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	i := newInstaller(t, runtime.GOOS)
	if err := i.Install("fkm"); err != nil {
		t.Fatal(err)
	}
	// Relative: the alias follows the command when it is replaced
	if link, err := os.Readlink(i.Path("fkm")); err != nil || link != "frankenmanager" {
		t.Errorf("link %q, %v", link, err)
	}
	if !i.Installed("fkm") || i.Install("fkm") != nil {
		t.Error("installed alias not recognized")
	}
	if removed, err := i.Remove("fkm"); !removed || err != nil {
		t.Errorf("remove: %v, %v", removed, err)
	}
	if _, err := os.Lstat(i.Path("fkm")); err == nil {
		t.Error("alias still there")
	}
}

// A file of the alias name that is not ours is never replaced nor removed.
func TestInstallConflict(t *testing.T) {
	i := newInstaller(t, runtime.GOOS)
	if err := os.WriteFile(i.Path("fkm"), []byte("other tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	var conflict *ConflictError
	if err := i.Install("fkm"); !errors.As(err, &conflict) {
		t.Errorf("err = %v", err)
	}
	if removed, err := i.Remove("fkm"); removed || err != nil {
		t.Errorf("remove: %v, %v", removed, err)
	}
	if data, _ := os.ReadFile(i.Path("fkm")); string(data) != "other tool" {
		t.Errorf("other file changed: %q", data)
	}
}

func TestWindowsShim(t *testing.T) {
	i := newInstaller(t, "windows")
	if err := i.Install("fkm"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(i.BinDir, "fkm.cmd"))
	if err != nil || !strings.Contains(string(data), `"%~dp0frankenmanager.exe" %*`) {
		t.Errorf("shim %q, %v", data, err)
	}
	if !i.Owns("fkm") {
		t.Error("shim not recognized")
	}
	if removed, _ := i.Remove("fkm"); !removed {
		t.Error("shim not removed")
	}
}
