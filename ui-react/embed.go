// Package uireact embeds the Base admin bundle (React 19 + Vite, @hanzo/ui
// true-black design system).
//
// Source lives in ./src. Build with `pnpm --dir ui-react build`, which writes
// the static bundle to ./dist where the //go:embed directive below picks it
// up at compile time of the Base binary. dist/ is committed (CI does not
// rebuild the SPA); refresh it before tagging when the admin changes.
//
// The Go HTTP handler serves the admin UI at the Base server's /_/ path
// — see apis/serve.go (gated behind BASE_ENABLE_ADMIN_UI=1).
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
