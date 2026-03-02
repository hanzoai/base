// Package uireact embeds the React + Vite 8 admin bundle.
//
// The Go HTTP handler serves the admin UI at the Base server's /_/ path.
// Run `pnpm build` in this directory to produce dist/; the //go:embed
// directive below picks up the resulting static assets.
//
// Deploy as a drop-in replacement for the legacy Svelte UI in ../ui by
// swapping the import in base.go:
//
//	import uibase "github.com/hanzoai/base/ui-react"   // React
//	// instead of
//	import uibase "github.com/hanzoai/base/ui"         // Svelte (legacy)
package uireact

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// DistDirFS returns a rooted fs.FS pointing at dist/ so it can be passed
// directly to http.FileServerFS or the Base router's Static handler.
func DistDirFS() fs.FS {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// dist/ must exist before building the Go binary. Run
		// `pnpm --dir ui-react build` in CI before `go build`.
		panic("ui-react: dist/ missing — run `pnpm --dir ui-react build` first: " + err.Error())
	}
	return sub
}
