package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The version endpoint says whether Swagger UI is served.
//
// It is the only way the frontend can find out. --no-swagger omits the route
// rather than answering 404, and an omitted route falls through to the SPA
// fallback, which answers every unknown path with the application shell -- so
// probing /tools/swagger would report a Swagger UI that is not there. The menu
// entry for the API documentation depends on this being right.
func TestTheVersionEndpointReportsWhetherSwaggerIsServed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		swagger bool
	}{
		{name: "served", swagger: true},
		{name: "disabled", swagger: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := NewRouter(Options{})
			(&SystemHandlers{Swagger: tc.swagger, MaxImageSize: 5 << 20}).Register(r)

			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/version", http.NoBody))

			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
			}

			var body struct {
				Version      string `json:"version"`
				Swagger      bool   `json:"swagger"`
				MaxImageSize int64  `json:"max_image_size"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("decoding the response: %v", err)
			}
			if body.Swagger != tc.swagger {
				t.Errorf("swagger = %v, want %v", body.Swagger, tc.swagger)
			}
			if body.Version == "" {
				t.Error("version is empty")
			}
			// The upload control refuses an oversized file before sending it,
			// and the limit is the operator's to set.
			if body.MaxImageSize != 5<<20 {
				t.Errorf("max_image_size = %d, want %d", body.MaxImageSize, 5<<20)
			}
		})
	}
}
