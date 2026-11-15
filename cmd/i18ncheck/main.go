// Command i18ncheck checks the translations against the code, from the
// root of the repository:
//
//	go run ./cmd/i18ncheck
//
// It fails when a translatable message misses a translation, when a
// translation is left without its message, or when a translation does not
// use the printf verbs of its message.
package main

import (
	"fmt"
	"os"

	"github.com/leobsst/frankenphp-cli/internal/i18n"
	"github.com/leobsst/frankenphp-cli/internal/i18n/i18ncheck"
)

func main() {
	msgs, problems, err := i18ncheck.Scan(".")
	if err != nil {
		fmt.Fprintln(os.Stderr, "i18ncheck:", err)
		os.Exit(1)
	}
	problems = append(problems, i18ncheck.Check(msgs, i18n.Catalogs())...)
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, p)
	}
	if len(problems) > 0 {
		fmt.Fprintf(os.Stderr, "i18ncheck: %d problem(s)\n", len(problems))
		os.Exit(1)
	}
	fmt.Printf("i18ncheck: %d messages, every one translated\n", len(msgs))
}
