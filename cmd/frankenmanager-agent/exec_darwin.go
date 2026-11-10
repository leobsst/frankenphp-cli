package main

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// run replaces this process with the CLI, argv[0] included, and only
// returns on failure (127, like the C launcher's failed execv).
func run(args []string, stderr io.Writer) int {
	exe, err := os.Executable()
	if err == nil {
		var cli string
		if cli, err = cliPath(exe); err == nil {
			err = syscall.Exec(cli, append([]string{cli}, args[1:]...), os.Environ())
		}
	}
	fmt.Fprintf(stderr, "frankenmanager-agent: %v\n", err)
	return 127
}
