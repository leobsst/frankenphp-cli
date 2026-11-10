// Command release builds the release assets into a directory:
//
//	go run ./cmd/release all -version 1.2.3 [-out dist] [-targets darwin/arm64,linux/amd64]
//
// all is stage then pack then checksums. CI runs them apart to sign and
// notarize the macOS stage in between:
//
//	stage      build each bundle into <out>/stage/<goos>-<goarch>/frankenmanager
//	pack       archive the staged bundles into <out>/assets, with the bare executables
//	checksums  write <out>/assets/SHA256SUMS.txt
//
// The archive timestamps come from SOURCE_DATE_EPOCH when set (CI: the
// commit time), otherwise now.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/leobsst/frankenphp-cli/internal/release"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "release:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: release all|stage|pack|checksums [-version V] [-out DIR] [-targets LIST]")
	}
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	version := fs.String("version", "", "version to stamp, e.g. 1.2.3 (a leading v is ignored)")
	out := fs.String("out", "dist", "output directory")
	list := fs.String("targets", "", "comma-separated goos/goarch (default: all 7)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	targets := release.Targets
	if *list != "" {
		targets = nil
		for _, s := range strings.Split(*list, ",") {
			t, err := release.ParseTarget(strings.TrimSpace(s))
			if err != nil {
				return err
			}
			targets = append(targets, t)
		}
	}
	v := strings.TrimPrefix(*version, "v")
	needVersion := args[0] == "all" || args[0] == "stage"
	if needVersion && v == "" {
		return errors.New("-version is required")
	}
	mtime, err := sourceDate()
	if err != nil {
		return err
	}

	ctx := context.Background()
	stage := func() error {
		for _, t := range targets {
			bundle, err := release.Stage(ctx, release.GoBuild, t, v, *out)
			if err != nil {
				return err
			}
			fmt.Println("staged", bundle)
		}
		return nil
	}
	pack := func() error {
		for _, t := range targets {
			if err := release.Pack(release.StagedBundle(*out, t), t, *out, mtime); err != nil {
				return err
			}
			fmt.Println("packed", filepath.Join(*out, "assets", t.Archive()))
		}
		return nil
	}
	checksums := func() error { return release.Checksums(filepath.Join(*out, "assets")) }

	switch args[0] {
	case "stage":
		return stage()
	case "pack":
		return pack()
	case "checksums":
		return checksums()
	case "all":
		for _, step := range []func() error{stage, pack, checksums} {
			if err := step(); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("unknown command %q", args[0])
}

func sourceDate() (time.Time, error) {
	s := os.Getenv("SOURCE_DATE_EPOCH")
	if s == "" {
		return time.Now().UTC().Truncate(time.Second), nil
	}
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("SOURCE_DATE_EPOCH: %w", err)
	}
	return time.Unix(sec, 0).UTC(), nil
}
