//go:build !windows

package platform

import (
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"testing"
)

// fakeChown records the files handed back instead of changing owners.
func fakeChown(t *testing.T) *[]string {
	t.Helper()
	var owned []string
	old := Chown
	Chown = func(path string, uid, gid int) error {
		if uid != 501 || gid != 20 {
			t.Errorf("chown %s to %d:%d", path, uid, gid)
		}
		owned = append(owned, path)
		return nil
	}
	t.Cleanup(func() { Chown = old })
	return &owned
}

// Under `sudo frankenmanager ...`, the directories created for the data
// directory go back to the real user, not those that existed: a database
// directory owned by the container user stays its own.
func TestMakeUserDirHandsBackOnlyWhatItCreates(t *testing.T) {
	owned := fakeChown(t)
	t.Setenv("SUDO_UID", "501")
	t.Setenv("SUDO_GID", "20")
	root := t.TempDir()
	existing := filepath.Join(root, "existing")
	if err := os.Mkdir(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(existing, "a", "b")
	if err := MakeUserDir(target); err != nil {
		t.Fatal(err)
	}
	if want := []string{filepath.Join(existing, "a"), target}; !slices.Equal(*owned, want) {
		t.Errorf("chowned %v, want %v", *owned, want)
	}
	if info, err := os.Stat(target); err != nil || !info.IsDir() {
		t.Errorf("not created: %v", err)
	}
}

func TestNoChownOutsideSudo(t *testing.T) {
	owned := fakeChown(t)
	t.Setenv("SUDO_UID", "")
	if err := MakeUserDir(filepath.Join(t.TempDir(), "a")); err != nil {
		t.Fatal(err)
	}
	ChownToSudoUser(t.TempDir())
	if len(*owned) != 0 {
		t.Errorf("chowned %v outside sudo", *owned)
	}
}

// AsRealUser runs `update`'s resync as the user of SUDO_UID/SUDO_GID, with
// no supplementary group and their HOME; outside sudo, or when not root,
// the command is left alone.
func TestAsRealUser(t *testing.T) {
	env := map[string]string{"SUDO_UID": "501", "SUDO_GID": "20"}
	getenv := func(k string) string { return env[k] }
	lookup := func(id string) (*user.User, error) {
		if id != "501" {
			t.Errorf("lookup %q", id)
		}
		return &user.User{Uid: "501", Gid: "20", Username: "me", HomeDir: "/Users/me"}, nil
	}

	cmd := exec.Command("true")
	cmd.Env = []string{"HOME=/var/root", "SUDO_UID=501", "PATH=/usr/bin"}
	asRealUser(cmd, getenv, 0, lookup)
	c := cmd.SysProcAttr.Credential
	if c == nil || c.Uid != 501 || c.Gid != 20 || c.Groups == nil || len(c.Groups) != 0 || c.NoSetGroups {
		t.Errorf("credential = %+v", c)
	}
	if want := []string{"PATH=/usr/bin", "HOME=/Users/me", "USER=me", "LOGNAME=me"}; !slices.Equal(cmd.Env, want) {
		t.Errorf("env = %q", cmd.Env)
	}

	for name, run := range map[string]func(*exec.Cmd){
		"not root":     func(c *exec.Cmd) { asRealUser(c, getenv, 501, lookup) },
		"outside sudo": func(c *exec.Cmd) { asRealUser(c, func(string) string { return "" }, 0, lookup) },
	} {
		c := exec.Command("true")
		run(c)
		if c.SysProcAttr != nil || c.Env != nil {
			t.Errorf("%s: changed %+v %v", name, c.SysProcAttr, c.Env)
		}
	}
}
