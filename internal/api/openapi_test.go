package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The document has to be machine-readable, because Swagger UI and every
// generator consume the JSON rather than the YAML source.
func TestOpenAPIDocumentIsValidJSON(t *testing.T) {
	document, err := OpenAPIJSON()
	if err != nil {
		t.Fatalf("converting the embedded document: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("the converted document is not JSON: %v", err)
	}

	if version, _ := decoded["openapi"].(string); !strings.HasPrefix(version, "3.1") {
		t.Errorf("openapi is %q, want 3.1.x", version)
	}
	for _, section := range []string{"info", "paths", "components"} {
		if _, present := decoded[section]; !present {
			t.Errorf("the document has no %s section", section)
		}
	}
}

// Every route the server registers must be described, or the document is a
// half-truth. Sprint 4 registers only the system endpoints; this test grows
// with the API rather than being rewritten.
func TestEveryRegisteredRouteIsDocumented(t *testing.T) {
	documented := documentedPaths(t)

	// The routes SystemHandlers registers, as the document should spell them.
	want := []string{"/health", "/metrics", "/version", "/shutdown"}

	for _, path := range want {
		if _, present := documented[path]; !present {
			t.Errorf("route %s is served but not described in api/openapi.yaml", path)
		}
	}
}

// And the other way: a documented path that nothing serves would send a client
// to a 404.
func TestEveryDocumentedPathIsServed(t *testing.T) {
	r := NewRouter(Options{})
	(&SystemHandlers{Shutdown: func() {}}).Register(r)
	r.RegisterOpenAPI()

	for path := range documentedPaths(t) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, APIPrefix+path, http.NoBody)
		r.ServeHTTP(rec, req)

		// A documented path must resolve to something. What it answers depends
		// on the method and on whether a database is present; the only wrong
		// answer here is "this path does not exist".
		if rec.Code == http.StatusNotFound {
			t.Errorf("documented path %s is not served", path)
		}
	}
}

func documentedPaths(t *testing.T) map[string]any {
	t.Helper()

	document, err := OpenAPIJSON()
	if err != nil {
		t.Fatalf("converting the embedded document: %v", err)
	}

	var decoded struct {
		Paths map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(document, &decoded); err != nil {
		t.Fatalf("reading the paths: %v", err)
	}
	if len(decoded.Paths) == 0 {
		t.Fatal("the document describes no paths")
	}
	return decoded.Paths
}

func TestOpenAPIIsServedAsJSONAndYAML(t *testing.T) {
	r := NewRouter(Options{})
	r.RegisterOpenAPI()

	cases := map[string]string{
		APIPrefix + OpenAPIPath:     "application/json",
		APIPrefix + OpenAPIYAMLPath: "application/yaml",
	}
	for path, wantType := range cases {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, http.NoBody))

		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", path, rec.Code)
			continue
		}
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, wantType) {
			t.Errorf("%s: content type %q, want %s", path, ct, wantType)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s: empty document", path)
		}
	}
}

// --no-swagger omits the route rather than answering 404 on it, so an operator
// who turned it off cannot tell it apart from a path that never existed.
func TestSwaggerCanBeTurnedOff(t *testing.T) {
	off := NewRouter(Options{})
	rec := httptest.NewRecorder()
	off.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, SwaggerPath+"/", http.NoBody))
	if rec.Code != http.StatusNotFound {
		t.Errorf("with Swagger off, status %d, want 404", rec.Code)
	}

	on := NewRouter(Options{Swagger: http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<!doctype html><title>doenerstag API</title>"))
		})})
	rec = httptest.NewRecorder()
	on.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, SwaggerPath+"/", http.NoBody))
	if rec.Code != http.StatusOK {
		t.Errorf("with Swagger on, status %d, want 200", rec.Code)
	}
}

// Swagger UI injects styles at runtime and cannot work under the strict policy.
// The relaxation is scoped to its own handler and touches style-src only:
// relaxing script-src would give up the protection the CSP exists for.
func TestSwaggerRelaxesOnlyStyleSrc(t *testing.T) {
	handler := SwaggerHandler(http.HandlerFunc(
		func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ui")) }))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, SwaggerPath+"/", http.NoBody))

	policy := rec.Header().Get("Content-Security-Policy")
	if !strings.Contains(policy, "style-src 'self' 'unsafe-inline'") {
		t.Errorf("Swagger's policy does not allow inline styles: %s", policy)
	}
	if !strings.Contains(policy, "script-src 'self'") ||
		strings.Contains(policy, "script-src 'self' 'unsafe-inline'") {
		t.Errorf("Swagger's policy relaxes script-src: %s", policy)
	}
	if !strings.Contains(policy, "frame-ancestors 'none'") {
		t.Errorf("Swagger's policy allows framing: %s", policy)
	}

	// And it must differ from the strict one in exactly that one clause.
	strict := strings.Replace(ContentSecurityPolicy,
		"style-src 'self'", "style-src 'self' 'unsafe-inline'", 1)
	if policy != strict {
		t.Errorf("Swagger's policy differs from the strict one by more than style-src:\n got: %s\nwant: %s",
			policy, strict)
	}
}

// The rest of the application must keep the strict policy: a relaxation that
// leaked past its own route would silently weaken every page.
func TestTheStrictPolicyIsUnaffectedBySwagger(t *testing.T) {
	if strings.Contains(ContentSecurityPolicy, "unsafe-inline") {
		t.Error("the application-wide CSP allows inline content")
	}
}
