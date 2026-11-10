package platform

import (
	"slices"
	"testing"
)

func TestRealUserEnv(t *testing.T) {
	env := []string{"PATH=/usr/bin", "HOME=/var/root", "USER=root", "LOGNAME=root",
		"SUDO_UID=501", "SUDO_GID=20", "SUDO_USER=me", "SUDO_COMMAND=/x", "LANG=fr_FR.UTF-8"}
	got := RealUserEnv(env, "me", "/Users/me")
	want := []string{"PATH=/usr/bin", "LANG=fr_FR.UTF-8", "HOME=/Users/me", "USER=me", "LOGNAME=me"}
	if !slices.Equal(got, want) {
		t.Errorf("got %q", got)
	}
}
