package ui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed dist
var assets embed.FS

func Handler() http.Handler {
	root, _ := fs.Sub(assets, "dist")
	files := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, e := fs.Stat(root, path); e != nil {
			if strings.HasPrefix(path, "assets/") {
				http.NotFound(w, r)
				return
			}
			clone := r.Clone(r.Context())
			clone.URL.Path = "/"
			files.ServeHTTP(w, clone)
			return
		}
		if strings.HasPrefix(path, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
}
