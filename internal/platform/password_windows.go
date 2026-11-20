//go:build windows

package platform

import (
	"bufio"
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// ReadPassword reads a line from stdin without showing it when stdin is a
// console (piped otherwise).
func ReadPassword() (string, error) {
	h := windows.Handle(os.Stdin.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err == nil {
		if err := windows.SetConsoleMode(h, mode&^windows.ENABLE_ECHO_INPUT|windows.ENABLE_LINE_INPUT); err == nil {
			defer func() { _ = windows.SetConsoleMode(h, mode) }()
		}
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
