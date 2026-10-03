package inspector

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"dev.atomyx/internal/session"
)

const (
	// assetsPrefix is the static path every page asset is served under.
	assetsPrefix = "/assets/"
	pageFile     = "index.html"
)

//go:embed assets
var embeddedAssets embed.FS

// assetContentTypes maps an asset extension to its content type. An
// extension not listed here is not served, so a stray file in the
// assets folder can never be sent with a guessed type.
var assetContentTypes = map[string]string{
	".html": "text/html; charset=utf-8",
	".css":  "text/css; charset=utf-8",
	".js":   "text/javascript; charset=utf-8",
}

// PageRoutes is the route group that serves the embedded inspector
// page at GET / and its scripts and stylesheet under /assets/. The
// page and its assets are exempt from the token, because the page is
// what exchanges the bootstrap code for one. The group takes no part
// in the holder's activity accounting and answers while the device is
// released.
func PageRoutes(_ session.Controller) []Route {
	assets := mustSub(embeddedAssets, "assets")
	return []Route{
		{Method: http.MethodGet, Path: pagePath, Handler: assetHandler(assets, func(*http.Request) string { return pageFile })},
		{Method: http.MethodGet, Path: assetsPrefix, Handler: assetHandler(assets, func(r *http.Request) string {
			return strings.TrimPrefix(r.URL.Path, assetsPrefix)
		})},
	}
}

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic("inspector: embedded assets folder is missing: " + err.Error())
	}
	return sub
}

func assetHandler(assets fs.FS, name func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		file := name(r)
		contentType, known := assetContentTypes[path.Ext(file)]
		if !known || !fs.ValidPath(file) {
			http.NotFound(w, r)
			return
		}
		body, err := fs.ReadFile(assets, file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write(body)
	})
}
