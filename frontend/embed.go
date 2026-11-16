// Package frontend embeds the built UI (frontend/dist) into the binary.
// Run `make frontend` (or `npm run build` in this folder) before `go build`.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets returns the built UI rooted at dist/.
func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(err)
	}
	return sub
}
