//go:build !darwin

package main

import (
	"fmt"
	"io"
)

// run fails: the app, and so its launcher, only exists on macOS.
func run(_ []string, stderr io.Writer) int {
	fmt.Fprintln(stderr, "frankenmanager-agent only runs on macOS")
	return 1
}
