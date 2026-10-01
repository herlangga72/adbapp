// Package webui menanam berkas tampilan ke dalam biner aplikasi.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed static
var staticFS embed.FS

// FS mengembalikan berkas tampilan siap disajikan.
func FS() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
