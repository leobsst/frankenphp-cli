//go:build !windows

package platform

import (
	"bufio"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// ReadPassword reads a line from stdin without showing it when stdin is a
// terminal (piped otherwise).
func ReadPassword() (string, error) {
	fd := int(os.Stdin.Fd())
	old, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err == nil {
		raw := *old
		raw.Lflag &^= unix.ECHO
		raw.Lflag |= unix.ICANON | unix.ISIG
		if err := unix.IoctlSetTermios(fd, ioctlSetTermios, &raw); err == nil {
			defer func() { _ = unix.IoctlSetTermios(fd, ioctlSetTermios, old) }()
		}
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
