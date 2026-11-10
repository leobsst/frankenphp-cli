//go:build !windows

package platform

import (
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"syscall"
)

// ChownToSudoUser gives path back to the real user when running under sudo.
//
// Under sudo, HOME may still point to the invoking user's home (macOS), so
// anything created in the data directory would belong to root and be
// unwritable by later runs without sudo. sudo sets SUDO_UID and SUDO_GID:
// the owner is set back to them. Not recursive, errors ignored, no-op
// outside sudo.
func ChownToSudoUser(path string) {
	uid, err := strconv.Atoi(os.Getenv("SUDO_UID"))
	if err != nil {
		return
	}
	gid := -1
	if g, err := strconv.Atoi(os.Getenv("SUDO_GID")); err == nil {
		gid = g
	}
	_ = Chown(path, uid, gid)
}

// UnderSudo reports whether SUDO_UID is set.
func UnderSudo() bool { return os.Getenv("SUDO_UID") != "" }

// AsRealUser makes cmd run as the user who invoked sudo, with an empty
// supplementary group list and RealUserEnv's environment. It does nothing
// outside sudo, or when not root.
func AsRealUser(cmd *exec.Cmd) {
	asRealUser(cmd, os.Getenv, os.Geteuid(), user.LookupId)
}

// asRealUser is AsRealUser with its environment, effective uid and user
// lookup given, for the tests.
func asRealUser(cmd *exec.Cmd, getenv func(string) string, euid int, lookup func(string) (*user.User, error)) {
	if getenv("SUDO_UID") == "" || euid != 0 {
		return
	}
	u, err := lookup(getenv("SUDO_UID"))
	if err != nil {
		return
	}
	gidStr := getenv("SUDO_GID")
	if gidStr == "" {
		gidStr = u.Gid
	}
	uid, err1 := strconv.ParseUint(u.Uid, 10, 32)
	gid, err2 := strconv.ParseUint(gidStr, 10, 32)
	if err1 != nil || err2 != nil {
		return
	}
	env := cmd.Env
	if env == nil {
		env = os.Environ()
	}
	cmd.Env = RealUserEnv(env, u.Username, u.HomeDir)
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid), Groups: []uint32{}}
}
