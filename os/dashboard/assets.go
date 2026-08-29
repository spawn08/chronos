package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed ui
var uiFS embed.FS

// UI returns an http.Handler serving the dashboard's single-page web UI (no
// external CDN or build step — plain HTML/CSS/JS embedded in the binary).
// Mount it under a path prefix with http.StripPrefix.
func UI() http.Handler {
	sub, _ := fs.Sub(uiFS, "ui") // uiFS embeds the fixed "ui" directory; this cannot fail.
	return http.FileServer(http.FS(sub))
}
