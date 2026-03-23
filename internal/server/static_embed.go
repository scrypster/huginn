//go:build embed_frontend

package server

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed dist
var distFS embed.FS

func staticFS() http.FileSystem {
	sub, _ := fs.Sub(distFS, "dist")
	return http.FS(sub)
}
