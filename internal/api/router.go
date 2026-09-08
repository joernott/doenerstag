package api

import (
	"net/http"
	"strings"

	"github.com/julienschmidt/httprouter"
)

// APIPrefix is the base path every endpoint lives below.
const APIPrefix = "/api/v1"

// Param returns a path parameter from the request context.
//
// Routes are registered with router.Handler rather than router.GET, which puts
// the parameters in the context and keeps every handler an ordinary
// http.Handler. That is what lets the middleware chain be written once against
// the standard interface. See docs/adr/0010-httprouter-for-routing.md.
func Param(r *http.Request, name string) string {
	return httprouter.ParamsFromContext(r.Context()).ByName(name)
}

// Options configures the router.
type Options struct {
	// Assets serves the frontend. Nil means no static serving, which is what
	// the API tests use.
	Assets http.Handler

	// Index serves the single-page application shell for any unmatched
	// non-API path. Nil means such paths get a plain 404.
	Index http.Handler

	// Swagger serves the API documentation. Nil means --no-swagger.
	Swagger http.Handler

	// CORSOrigins are the origins allowed to call the API cross-origin. Empty
	// sends no CORS headers at all, which is the default and the right answer
	// for a same-origin frontend.
	CORSOrigins []string

	// RouteServed, if set, is called with a route each time a request matches
	// it. Production leaves it nil.
	//
	// It exists for one assertion: docs/12_testing.md expects every API
	// operation to have at least one test, and the only way to know that is to
	// watch which routes the suite actually reaches. Counting operations in the
	// OpenAPI document proves the opposite thing -- that they are described.
	RouteServed func(RouteInfo)
}

// Router builds the application's single router.
//
// One httprouter handles the API, the assets and the SPA fallback. There is no
// second mux: a request is matched once, by one set of rules.
type Router struct {
	mux  *httprouter.Router
	opts Options

	// routes records every API route registered, so that the OpenAPI document
	// can be checked against what the server actually serves rather than
	// against a list somebody maintains by hand.
	routes []RouteInfo
}

// RouteInfo is one registered API route, as the router sees it.
type RouteInfo struct {
	// Method is the HTTP method.
	Method string
	// Path is the path below /api/v1, in httprouter form: /orders/:id.
	Path string
}

// NewRouter creates the router and registers everything that does not depend on
// the database.
func NewRouter(opts Options) *Router {
	mux := httprouter.New()

	// Answer 405 with an Allow header rather than falling through to 404: a
	// client using the wrong method should be told so.
	mux.HandleMethodNotAllowed = true
	mux.HandleOPTIONS = true

	// A trailing slash is a typo, not a different resource.
	mux.RedirectTrailingSlash = true

	// Off deliberately. A case-insensitive redirect would make /Orders and
	// /orders the same path, which is not something an API should promise.
	mux.RedirectFixedPath = false

	r := &Router{mux: mux, opts: opts}

	mux.NotFound = http.HandlerFunc(r.handleNotFound)
	mux.MethodNotAllowed = http.HandlerFunc(r.handleMethodNotAllowed)
	mux.PanicHandler = r.handlePanic

	if len(opts.CORSOrigins) > 0 {
		mux.GlobalOPTIONS = http.HandlerFunc(r.handlePreflight)
	}

	if opts.Assets != nil {
		mux.Handler(http.MethodGet, "/static/*filepath", opts.Assets)
		mux.Handler(http.MethodHead, "/static/*filepath", opts.Assets)
	}
	if opts.Swagger != nil {
		mux.Handler(http.MethodGet, "/tools/swagger/*filepath", opts.Swagger)
		mux.Handler(http.MethodHead, "/tools/swagger/*filepath", opts.Swagger)
	}

	return r
}

// Handle registers an API route below /api/v1.
//
// The path uses httprouter's :name syntax. docs/04_api.md writes {id} for
// readability; the registered path is :id.
func (r *Router) Handle(method, path string, handler http.Handler) {
	info := RouteInfo{Method: method, Path: path}
	r.mux.Handler(method, APIPrefix+path, r.observe(info, handler))
	r.routes = append(r.routes, info)
}

// observe wraps a handler so that reaching it is reported, when anything is
// listening. With no observer the handler is returned unchanged, so the
// shipped router carries no extra frame per request.
func (r *Router) observe(info RouteInfo, next http.Handler) http.Handler {
	if r.opts.RouteServed == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.opts.RouteServed(info)
		next.ServeHTTP(w, req)
	})
}

// Routes lists the API routes registered on this router, in registration
// order.
func (r *Router) Routes() []RouteInfo {
	out := make([]RouteInfo, len(r.routes))
	copy(out, r.routes)
	return out
}

// HandleFunc is Handle for a plain function.
func (r *Router) HandleFunc(method, path string, handler http.HandlerFunc) {
	r.Handle(method, path, handler)
}

// ServeHTTP makes the router an ordinary handler.
func (r *Router) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mux.ServeHTTP(w, req)
}

// handleNotFound is the SPA fallback.
//
// An unmatched path under /api/ is a genuine 404 and gets the JSON envelope. An
// unmatched path anywhere else serves the application shell, so that reloading
// a deep link works with client-side routing.
//
// This is the router's NotFound handler rather than a catch-all route because
// httprouter panics on a catch-all that shares a prefix with anything else.
func (r *Router) handleNotFound(w http.ResponseWriter, req *http.Request) {
	if isAPIPath(req.URL.Path) || looksLikeAFile(req.URL.Path) || r.opts.Index == nil {
		WriteError(w, req, &Error{Code: CodeNotFound})
		return
	}
	r.opts.Index.ServeHTTP(w, req)
}

// looksLikeAFile reports whether a path is asking for a file rather than for a
// page of the application.
//
// The fallback exists so that reloading a deep link works: /orders/{id} has to
// answer with the shell rather than a 404. Nothing the router serves as a page
// has a file extension, and nothing that asks for one wants HTML.
//
// This is not tidiness. Firefox asks every page for /favicon.ico, the fallback
// answered with the application shell -- a 200 with text/html where an icon was
// expected -- and Firefox then held the document's load event open until it
// timed out. Roughly half of all navigations in the browser tests, and a
// spinner that never stops for a real visitor. A 404 is both the correct answer
// and the one that does not hang.
func looksLikeAFile(path string) bool {
	slash := strings.LastIndexByte(path, '/')
	return strings.Contains(path[slash+1:], ".")
}

func (r *Router) handleMethodNotAllowed(w http.ResponseWriter, req *http.Request) {
	// 405 rather than 404: httprouter has already set Allow, and answering
	// "not found" beside a header listing the methods that do work would be
	// contradictory.
	WriteError(w, req, Errorf(CodeMethodNotAllowed,
		"the %s method is not allowed on %s", req.Method, req.URL.Path))
}

// handlePanic exists so that a panic inside a handler produces a JSON 500 with
// the correlation ID rather than a dropped connection.
//
// The Recover middleware catches most panics first; this covers anything that
// happens inside httprouter's own dispatch.
func (r *Router) handlePanic(w http.ResponseWriter, req *http.Request, _ any) {
	WriteError(w, req, &Error{Code: CodeInternal})
}

// handlePreflight answers a CORS preflight for an allowed origin.
//
// Credentials are never allowed cross-origin: cookie sessions are same-origin
// only, and a cross-origin caller must use an API token, which carries no
// ambient authority for another site to abuse.
func (r *Router) handlePreflight(w http.ResponseWriter, req *http.Request) {
	origin := req.Header.Get("Origin")
	if origin == "" || !r.originAllowed(origin) {
		return
	}

	h := w.Header()
	h.Set("Access-Control-Allow-Origin", origin)
	h.Set("Access-Control-Allow-Credentials", "false")
	h.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, "+RequestIDHeader)
	h.Set("Access-Control-Max-Age", "600")
	// Varying on Origin keeps a cache from serving one origin's response to
	// another.
	h.Add("Vary", "Origin")
}

func (r *Router) originAllowed(origin string) bool {
	for _, allowed := range r.opts.CORSOrigins {
		if allowed == origin {
			return true
		}
	}
	return false
}

// isAPIPath reports whether a path belongs to the API rather than the frontend.
func isAPIPath(path string) bool {
	return path == "/api" || strings.HasPrefix(path, "/api/")
}

// ParseCORSOrigins splits the comma-separated --cors-allowed-origins value.
func ParseCORSOrigins(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
