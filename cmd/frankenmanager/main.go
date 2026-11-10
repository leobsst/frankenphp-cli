// Command frankenmanager manages a FrankenPHP development environment in
// Docker.
package main

import (
	"os"

	"github.com/leobsst/frankenphp-cli/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
