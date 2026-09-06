package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"github.com/joernott/doenerstag/internal/config"
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
// half-truth.
//
// The routes come from the production wiring rather than from a list kept here
// by hand. The list version of this test passed for nine sprints while the
// document described four endpoints out of sixty, because the list was the
// same four.
func TestEveryRegisteredRouteIsDocumented(t *testing.T) {
	documented := documentedPaths(t)

	for _, route := range productionRoutes(t) {
		path := documentedForm(route.Path)
		operations, present := documented[path]
		if !present {
			t.Errorf("%s %s is served but not described in api/openapi.yaml",
				route.Method, path)
			continue
		}

		// The path being present is not enough: the method has to be there
		// too, or DELETE could be undocumented under a documented GET.
		methods, ok := operations.(map[string]any)
		if !ok {
			t.Errorf("%s is not an object in the document", path)
			continue
		}
		if _, described := methods[strings.ToLower(route.Method)]; !described {
			t.Errorf("%s %s is served but only other methods are documented",
				route.Method, path)
		}
	}
}

// productionRoutes is what NewServer registers, which is the only list that
// cannot drift from what is served.
//
// The server is built without a database: nothing here touches one, because
// registering a route does not.
func productionRoutes(t *testing.T) []RouteInfo {
	t.Helper()

	cfg := &config.Config{}
	cfg.Session.JWTSecret = strings.Repeat("k", MinJWTSecretLength)
	logger := zerolog.Nop()

	server, err := NewServer(ServerOptions{Config: cfg, Logger: &logger})
	if err != nil {
		t.Fatalf("building the server: %v", err)
	}

	routes := server.Routes()
	if len(routes) == 0 {
		t.Fatal("the server registered no routes")
	}
	return routes
}

// documentedForm turns httprouter's /orders/:id into the document's
// /orders/{id}.
func documentedForm(path string) string {
	var out []string
	for _, segment := range strings.Split(path, "/") {
		if strings.HasPrefix(segment, ":") {
			segment = "{" + segment[1:] + "}"
		}
		out = append(out, segment)
	}
	return strings.Join(out, "/")
}

// And the other way: a documented path that nothing serves would send a client
// to a 404, which is worse than no documentation at all.
func TestEveryDocumentedPathIsServed(t *testing.T) {
	served := map[string]map[string]bool{}
	for _, route := range productionRoutes(t) {
		path := documentedForm(route.Path)
		if served[path] == nil {
			served[path] = map[string]bool{}
		}
		served[path][strings.ToLower(route.Method)] = true
	}

	for path, operations := range documentedPaths(t) {
		methods, ok := served[path]
		if !ok {
			t.Errorf("documented path %s is not served by anything", path)
			continue
		}

		described, ok := operations.(map[string]any)
		if !ok {
			continue
		}
		for method := range described {
			// Keys that are not operations: a path item may also carry
			// parameters, a summary or a description.
			if !isHTTPMethod(method) {
				continue
			}
			if !methods[method] {
				t.Errorf("%s %s is documented but not served",
					strings.ToUpper(method), path)
			}
		}
	}
}

func isHTTPMethod(name string) bool {
	switch name {
	case "get", "put", "post", "delete", "patch", "head", "options", "trace":
		return true
	default:
		return false
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
