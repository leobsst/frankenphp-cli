package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCLIPath(t *testing.T) {
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "lib", "frankenmanager")
	agent := filepath.Join(bundle, "FrankenManager.app", "Contents", "MacOS", "frankenmanager-agent")
	if err := os.MkdirAll(filepath.Dir(agent), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(agent, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(bundle, "frankenmanager")
	if got, err := cliPath(agent); got != want || err != nil {
		t.Errorf("cliPath = %q, %v; want %q", got, err, want)
	}

	// Reached through a symlink: the real path decides, like realpath()
	link := filepath.Join(root, "agent-link")
	if err := os.Symlink(agent, link); err != nil {
		t.Skip("symlinks not available:", err) // Windows without the privilege
	}
	if got, err := cliPath(link); got != want || err != nil {
		t.Errorf("through a symlink: %q, %v", got, err)
	}
	if _, err := cliPath(filepath.Join(root, "missing")); err == nil {
		t.Error("missing executable accepted")
	}
}
