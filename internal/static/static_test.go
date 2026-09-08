package static

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	root "github.com/joernott/doenerstag"
)

// assetsIn builds an asset source over a temporary directory holding a minimal
// frontend.
func assetsIn(t *testing.T) (assets *Assets, dir string) {
	t.Helper()

	// An embedded build ignores the directory by design and serves the assets
	// compiled into the binary, so a fixture directory cannot be observed.
	// TestEmbeddedBuildServesItsOwnAssets covers that configuration instead.
	if root.Embedded {
		t.Skip("this build serves its assets from the binary, not from a directory")
	}

	dir = t.TempDir()
	write := func(name, content string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write(IndexFile, "<!doctype html><title>doenerstag</title>")
	write("css/app.css", "body{margin:0}")
	write("js/app.js", "export const x = 1;")
	write("secret.txt", "not an asset anyone should reach through /static")

	assets, err := Open(dir) //nolint:govet // shadowing the named result is clearer here
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return assets, dir
}

func TestServesAssets(t *testing.T) {
	assets, _ := assetsIn(t)
	handler := assets.Handler()

	cases := map[string]string{
		"/static/css/app.css": "body{margin:0}",
		"/static/js/app.js":   "export const x = 1;",
	}
	for path, want := range cases {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))

		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", path, rec.Code)
			continue
		}
		if got := strings.TrimSpace(rec.Body.String()); got != want {
			t.Errorf("%s: body %q, want %q", path, got, want)
		}
	}
}

func TestServesTheIndexForTheApplicationShell(t *testing.T) {
	assets, _ := assetsIn(t)

	rec := httptest.NewRecorder()
	assets.Index().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/orders/abc", http.NoBody))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "doenerstag") {
		t.Errorf("the shell was not served: %s", rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type is %q, want text/html", ct)
	}
}

// A path that climbs out of the asset directory must not reach the filesystem
// above it.
func TestTraversalIsRefused(t *testing.T) {
	assets, _ := assetsIn(t)
	handler := assets.Handler()

	for _, path := range []string{
		"/static/../secret.txt",
		"/static/../../etc/passwd",
		"/static/css/../../secret.txt",
		"/static/",
		"/static",
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))

		if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), "not an asset") {
			t.Errorf("%s reached a file outside the asset directory", path)
		}
	}
}

func TestMissingAssetIsNotFound(t *testing.T) {
	assets, _ := assetsIn(t)

	rec := httptest.NewRecorder()
	assets.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/static/js/missing.js", http.NoBody))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
}

// A development build must never cache: the point of serving from disk is that
// a reload shows the edit. Getting this wrong is an hour of confusion.
func TestDevelopmentBuildDoesNotCache(t *testing.T) {
	if root.Embedded {
		t.Skip("this binary was built with -tags embedstatic")
	}

	assets, _ := assetsIn(t)

	rec := httptest.NewRecorder()
	assets.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/app.css", http.NoBody))

	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control is %q, want no-store in a development build", got)
	}
	if assets.Embedded() {
		t.Error("a development build reports itself as embedded")
	}
	if assets.IgnoresStaticDir() {
		t.Error("a development build claims to ignore --static-dir")
	}
}

// Edits must be visible without restarting, which means the file is read per
// request rather than cached in memory at startup.
func TestEditsAreVisibleWithoutARestart(t *testing.T) {
	if root.Embedded {
		t.Skip("an embedded build cannot see edits, which is the point")
	}

	assets, dir := assetsIn(t)
	handler := assets.Handler()

	serve := func() string {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/css/app.css", http.NoBody))
		return strings.TrimSpace(rec.Body.String())
	}

	if got := serve(); got != "body{margin:0}" {
		t.Fatalf("initial content is %q", got)
	}

	if err := os.WriteFile(filepath.Join(dir, "css", "app.css"),
		[]byte("body{margin:1px}"), 0o600); err != nil {
		t.Fatal(err)
	}

	if got := serve(); got != "body{margin:1px}" {
		t.Errorf("after editing, the handler still serves %q", got)
	}
}

// A missing asset directory must say what to do rather than failing obscurely
// at the first request.
func TestOpenReportsAMissingDirectoryHelpfully(t *testing.T) {
	if root.Embedded {
		t.Skip("an embedded build does not read a directory")
	}

	_, err := Open(filepath.Join(t.TempDir(), "does-not-exist"))
	if err == nil {
		t.Fatal("a missing asset directory was accepted")
	}
	if !strings.Contains(err.Error(), "make frontend") {
		t.Errorf("the error does not say how to fix it: %v", err)
	}
}

func TestOpenRejectsAFile(t *testing.T) {
	if root.Embedded {
		t.Skip("an embedded build does not read a directory")
	}

	path := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(path); err == nil {
		t.Error("a file was accepted as the asset directory")
	}
}

// The two build configurations must agree about which one they are. A binary
// that thinks it is embedded but is not would serve nothing.
func TestBuildTagAndBehaviourAgree(t *testing.T) {
	assets, err := Open("../../static")
	if err != nil {
		if root.Embedded {
			t.Fatalf("an embedded build failed to open its own assets: %v", err)
		}
		t.Skipf("the repository's static directory is not present: %v", err)
	}

	if assets.Embedded() != root.Embedded {
		t.Errorf("Embedded() is %v but the build constant says %v",
			assets.Embedded(), root.Embedded)
	}

	// Whichever build this is, the shell must be servable.
	rec := httptest.NewRecorder()
	assets.Index().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Errorf("the application shell is not servable: status %d", rec.Code)
	}
}

// The embedded build's whole purpose: a release binary serves the frontend with
// no directory beside it. Nothing else in this file can observe that, because
// every other test works through a fixture directory the embedded build
// correctly ignores.
func TestEmbeddedBuildServesItsOwnAssets(t *testing.T) {
	if !root.Embedded {
		t.Skip("this build serves its assets from a directory")
	}

	// Any path is fine: an embedded build reads none of it.
	assets, err := Open("/nonexistent/on/purpose")
	if err != nil {
		t.Fatalf("an embedded build could not open its own assets: %v", err)
	}
	if !assets.Embedded() || !assets.IgnoresStaticDir() {
		t.Error("an embedded build does not report itself as embedded")
	}

	rec := httptest.NewRecorder()
	assets.Index().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("the shell is not servable from the binary: status %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "doenerstag") {
		t.Errorf("the embedded shell looks wrong: %s", rec.Body)
	}

	// Compiled-in assets cannot change without the binary changing, so they may
	// be cached hard.
	rec = httptest.NewRecorder()
	assets.Handler().ServeHTTP(rec,
		httptest.NewRequest(http.MethodGet, "/static/css/app.css", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Fatalf("an embedded asset is not servable: status %d", rec.Code)
	}
	if cc := rec.Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("Cache-Control is %q, want an immutable policy for embedded assets", cc)
	}
}
