// Package web embeds the dashboard, built by `make web` into
// web/embedded/dist and served by the daemon.
package web

import (
	"embed"
	"io/fs"
)

// The dist directory always holds .gitkeep, so that the module builds
// without the web build; the pages come with `make web`.
//
//go:embed all:embedded/dist
var dist embed.FS

// Dashboard returns the pages of the dashboard, nil when they were not
// built.
func Dashboard() fs.FS {
	sub, err := fs.Sub(dist, "embedded/dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
