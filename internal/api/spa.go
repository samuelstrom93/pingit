package api

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
)

func SPAHandler(root fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if clean == "." || clean == "" {
			serveIndex(w, r, root)
			return
		}
		if _, err := fs.Stat(root, clean); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		serveIndex(w, r, root)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, root fs.FS) {
	index, err := fs.ReadFile(root, "index.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(index)
}
