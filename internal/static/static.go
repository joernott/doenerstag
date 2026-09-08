// Package static serves the frontend, from disk during development and from
// the binary in a release.
//
// Which one is decided by the embedstatic build tag, not at runtime. See
// docs/adr/0002-static-assets-embed-toggle.md for why.
package static

import (
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strings"

	root "github.com/joernott/doenerstag"
)

// IndexFile is the single-page application shell, served for any unmatched
// non-API path so that reloading a deep link works.
const IndexFile = "index.html"

// Assets serves the frontend from whichever source this build uses.
type Assets struct {
	fsys fs.FS

	// embedded says where the files came from, which decides how aggressively
	// they may be cached.
	embedded bool

	// dir is the on-disk directory, for the message when it is missing.
	dir string
}

// Open prepares the asset source.
//
// In an embedded build the directory argument is ignored, and the caller is
// told so rather than being left wondering why edits have no effect.
func Open(dir string) (*Assets, error) {
	if root.Embedded {
		sub, err := fs.Sub(root.StaticFS, "static")
		if err != nil {
			return nil, fmt.Errorf("reading the embedded frontend: %w", err)
		}
		return &Assets{fsys: sub, embedded: true}, nil
	}

	info, err := os.Stat(dir)
	if err != nil {
		// The remedy is part of the message: a missing asset directory is
		// almost always "make frontend has not been run", and saying so beats
		// leaving the operator with a bare stat error.
		return nil, fmt.Errorf(
			"cannot serve the frontend from %s: %w "+
				"(run make frontend to build it, or point --static-dir elsewhere)",
			dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("cannot serve the frontend: %s is not a directory", dir)
	}

	return &Assets{fsys: os.DirFS(dir), dir: dir}, nil
}

// Embedded reports whether this build carries its assets.
func (a *Assets) Embedded() bool { return a.embedded }

// Source describes where the assets come from, for the startup log line.
func (a *Assets) Source() string {
	if a.embedded {
		return "embedded in the binary"
	}
	return a.dir
}

// IgnoresStaticDir reports whether --static-dir has no effect in this build.
//
// The server warns when it was set explicitly and cannot be honoured, because
// "my CSS changes do nothing" is otherwise a genuinely confusing hour.
func (a *Assets) IgnoresStaticDir() bool { return a.embedded }

// Handler serves files under /static/.
func (a *Assets) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/static/")
		if name == "" || name == "." || strings.HasPrefix(name, "..") {
			http.NotFound(w, r)
			return
		}
		a.serveFile(w, r, name)
	})
}

// Index serves the application shell.
func (a *Assets) Index() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.serveFile(w, r, IndexFile)
	})
}

func (a *Assets) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	file, err := a.fsys.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = file.Close() }()

	info, err := file.Stat()
	if err != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}

	seeker, ok := file.(io.ReadSeeker)
	if !ok {
		// An fs.File is not required to be seekable. Falling back to a copy
		// loses range requests, which no asset here needs.
		w.Header().Set("Content-Type", contentType(name))
		a.setCaching(w)
		_, _ = io.Copy(w, file)
		return
	}

	a.setCaching(w)
	// ServeContent sets the content type from the name and handles conditional
	// requests, which is what makes the ETag worth setting.
	http.ServeContent(w, r, name, info.ModTime(), seeker)
}

// setCaching applies the caching policy for this build.
//
// A development build must never cache: the whole point of serving from disk is
// that a reload shows the edit. A release build carries assets that cannot
// change without the binary changing, so they may be cached hard.
func (a *Assets) setCaching(w http.ResponseWriter) {
	if a.embedded {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
}

// contentType maps an extension to a media type for the non-seekable path.
func contentType(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	default:
		return "application/octet-stream"
	}
}

// SwaggerDir is where the frontend build vendors Swagger UI.
const SwaggerDir = "swagger"

// SwaggerIndex serves the vendored Swagger UI page.
//
// The assets it loads come through the ordinary /static/ handler; only the
// entry document needs its own route, because it is mounted at /tools/swagger
// rather than under /static.
func (a *Assets) SwaggerIndex() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.serveFile(w, r, path.Join(SwaggerDir, IndexFile))
	})
}
