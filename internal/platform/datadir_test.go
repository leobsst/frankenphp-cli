package platform

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestDataDirOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "data")
	t.Setenv(DataDirEnv, want)

	got, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
}

func TestDataDirDefault(t *testing.T) {
	home := t.TempDir()
	t.Setenv(DataDirEnv, "")
	t.Setenv("HOME", home)        // Unix
	t.Setenv("USERPROFILE", home) // Windows

	want := filepath.Join(home, ".frankenmanager")
	if runtime.GOOS == "windows" {
		want = filepath.Join(home, "AppData", "Local", "frankenmanager")
	}

	got, err := DataDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("DataDir() = %q, want %q", got, want)
	}
}
