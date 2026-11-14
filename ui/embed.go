// Package ui embeds the built SvelteKit app (ui/build, made by `make ui`).
// ui/build/.gitkeep keeps the directory present in Go-only builds; the SPA
// handler shows a stub page when index.html is missing.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:build
var files embed.FS

// FS returns the built app rooted at its index.html.
func FS() fs.FS {
	sub, err := fs.Sub(files, "build")
	if err != nil {
		panic(err)
	}
	return sub
}
