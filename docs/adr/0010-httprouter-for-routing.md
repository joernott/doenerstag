# ADR-0010: httprouter for all routing

**Status:** Accepted — 2026-09-05

Supersedes the routing choice in the first draft of
[08_technologies.md](../08_technologies.md), which specified the standard
library's method-and-pattern routing.

## Context

The server routes three different kinds of thing: the REST API with path
parameters, the static assets, and the single-page-application fallback that
must serve `index.html` for any unmatched non-API path.

Go 1.22's `net/http` mux gained method matching and path wildcards and would
have covered this. `julienschmidt/httprouter` was specified instead.

httprouter is a radix-tree router: matching is allocation-free and does not
depend on the number of registered routes. That performance argument is
irrelevant here — at three orders a day, routing is never the bottleneck. The
reasons that do apply are that it is small, stable, has no dependencies of its
own, is BSD-3-Clause like this project, and behaves predictably.

## Decision

One `httprouter.Router` handles everything: API, static assets and the SPA
fallback. There is no second mux.

Routes are registered through `router.Handler` and `router.HandlerFunc` rather
than `router.GET`/`router.POST`. Those take an ordinary `http.Handler` and put
the path parameters into the request context, read back with
`httprouter.ParamsFromContext`. Every handler and every middleware therefore
stays a plain `http.Handler`, and the middleware chain — request ID, logging,
authentication, CSRF, security headers — is written once against the standard
interface.

The SPA fallback is the router's `NotFound` handler, not a catch-all route.
`HandleMethodNotAllowed` and `HandleOPTIONS` are on; `RedirectFixedPath` is off.
`PanicHandler` logs at ERROR with the request ID and returns 500 error 9000.

## The constraint this imposes

httprouter **panics at registration time** on conflicting routes. A static
segment cannot share a position with a wildcard: registering `/users/me`
alongside `/users/:id` is a panic, not a precedence rule. A `*filepath`
catch-all cannot share a prefix with anything else.

The API in [04_api.md](../04_api.md) is designed to be conflict-free, and has to
stay that way:

- No `/users/me`-style aliases. The client uses the id from
  `GET /api/v1/auth/session`.
- No catch-all at the root — hence the `NotFound` handler for the SPA fallback.
- The two catch-alls that exist, `/static/*filepath` and
  `/tools/swagger/*filepath`, sit under prefixes nothing else uses.

This is the real cost of the choice, and it is a design constraint on the API
rather than an implementation detail.

## Alternatives rejected

- **`net/http` (Go 1.22+).** No dependency at all, and it resolves overlapping
  patterns by specificity instead of panicking, so `/users/me` and `/users/:id`
  would both be possible. Rejected because httprouter was specified. Worth
  recording that the standard library would have been adequate: if httprouter is
  ever dropped, this is what replaces it, and the `ParamsFromContext` boundary is
  narrow enough to make that a contained change.
- **chi, gorilla/mux, gin.** More features than this application needs.
  gorilla/mux is also markedly slower, and gin brings a whole handler idiom that
  would infect every handler signature.

## Consequences

- Route conflicts fail loudly and immediately, at startup rather than at request
  time. A test that constructs the full production router turns that into a CI
  failure — see [12_testing.md](../12_testing.md).
- Handlers read parameters from the context rather than from a third argument,
  which keeps them ordinary `http.Handler`s and keeps the middleware portable.
- `{id}` in [04_api.md](../04_api.md) is documentation notation; the registered
  path is `:id`. The two must be kept in step by hand, which the OpenAPI
  coverage test catches when they drift.
- One more dependency, BSD-3-Clause, with no transitive dependencies of its own.
- httprouter is mature and changes rarely. That is a virtue for this use and a
  risk if it is ever abandoned; the `net/http` fallback above is the mitigation.
