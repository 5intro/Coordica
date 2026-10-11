// Package web exposes the built workbench to the center binary.
// Run npm run build in web/ before building the center.
package web

import (
	"embed"
	"io/fs"
)

//go:embed dist
var bundle embed.FS

// FS returns the production assets rooted at dist, with no disk dependency.
func FS() fs.FS {
	assets, err := fs.Sub(bundle, "dist")
	if err != nil {
		panic(err) // An invalid embedded directory is a build-time packaging defect.
	}
	return assets
}
