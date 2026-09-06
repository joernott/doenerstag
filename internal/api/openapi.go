package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"gopkg.in/yaml.v3"

	root "github.com/joernott/doenerstag"
)

// OpenAPIPath is where the machine-readable document is served.
const OpenAPIPath = "/openapi.json"

// OpenAPIYAMLPath serves the same document in its source form, which is what a
// person reading it by hand usually wants.
const OpenAPIYAMLPath = "/openapi.yaml"

// SwaggerPath is where the browsable documentation lives.
const SwaggerPath = "/tools/swagger"

// openapiJSON converts the embedded YAML once, on first use.
//
// The source of truth is api/openapi.yaml: YAML is what a person maintains, and
// JSON is what Swagger UI and every generator expect. Converting at runtime
// keeps them from drifting rather than requiring a build step that could be
// forgotten.
var openapiJSON = sync.OnceValues(func() ([]byte, error) {
	source, err := root.OpenAPIYAML()
	if err != nil {
		return nil, err
	}

	var document any
	if err := yaml.Unmarshal(source, &document); err != nil {
		return nil, fmt.Errorf("the embedded OpenAPI document is not valid YAML: %w", err)
	}

	encoded, err := json.Marshal(document)
	if err != nil {
		return nil, fmt.Errorf("converting the OpenAPI document to JSON: %w", err)
	}
	return encoded, nil
})

// OpenAPIJSON returns the document as JSON. Exported so a test can validate it
// without going through a request.
func OpenAPIJSON() ([]byte, error) { return openapiJSON() }

// RegisterOpenAPI adds the document routes.
func (r *Router) RegisterOpenAPI() {
	r.HandleFunc(http.MethodGet, OpenAPIPath, serveOpenAPIJSON)
	r.HandleFunc(http.MethodGet, OpenAPIYAMLPath, serveOpenAPIYAML)
}

func serveOpenAPIJSON(w http.ResponseWriter, req *http.Request) {
	document, err := openapiJSON()
	if err != nil {
		WriteError(w, req, &Error{Code: CodeInternal, Cause: err})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(document)
}

func serveOpenAPIYAML(w http.ResponseWriter, req *http.Request) {
	source, err := root.OpenAPIYAML()
	if err != nil {
		WriteError(w, req, &Error{Code: CodeInternal, Cause: err})
		return
	}
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(source)
}

// SwaggerHandler serves the browsable documentation from the vendored
// Swagger UI in the asset directory.
//
// The page and its assets are served under a relaxed style-src, because
// Swagger UI injects styles at runtime and cannot work under the strict policy.
// script-src is never relaxed: the scripts it loads are files, not inline
// blocks, which is why the build writes init.js rather than a <script> body.
// docs/05_auth_and_permissions.md anticipates exactly this.
func SwaggerHandler(index http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", SwaggerContentSecurityPolicy)
		index.ServeHTTP(w, r)
	})
}

// SwaggerContentSecurityPolicy is the strict policy with inline styles allowed,
// and nothing else changed.
const SwaggerContentSecurityPolicy = "default-src 'self'; " +
	"img-src 'self' data:; " +
	"style-src 'self' 'unsafe-inline'; " +
	"script-src 'self'; " +
	"font-src 'self'; " +
	"connect-src 'self'; " +
	"frame-ancestors 'none'; " +
	"base-uri 'none'; " +
	"form-action 'self'"
