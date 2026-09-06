# 08 Technologies and frameworks

doenerstag has a Go backend, a TypeScript/Tailwind frontend and a PostgreSQL 18
database. The built application runs without internet access. Building it does
require internet access, but the toolchain runs locally — no hosted build or
asset service is used, and no asset is fetched from a CDN at runtime.

## Backend

| Concern              | Choice                                            |
| -------------------- | ------------------------------------------------- |
| Language             | Go, minimum 1.24                                   |
| Module path          | `github.com/joernott/doenerstag`                   |
| HTTP routing         | `julienschmidt/httprouter` — for the API, the static assets and the SPA fallback alike |
| CLI and config       | `spf13/cobra` and `spf13/viper`                    |
| Database driver      | `jackc/pgx/v5` with its native pool                |
| Migrations           | `golang-migrate/migrate/v4`, file source, pgx driver |
| Logging              | `rs/zerolog`                                       |
| Password hashing     | `golang.org/x/crypto/argon2`                       |
| JWT                  | `golang-jwt/jwt/v5`                                |
| HTML sanitizing      | `microcosm-cc/bluemonday`                          |
| Image decoding       | Standard library `image/jpeg`, `image/png`, `image/gif`, plus `golang.org/x/image/draw` for scaling |
| UUIDv7               | `google/uuid`                                      |
| Tests                | Standard `testing`, `net/http/httptest`, `testify/require` for assertions |

The backend serves the frontend, a versioned REST API and, unless disabled,
Swagger UI. It uses no ORM; SQL is written by hand.

### Routing

One `httprouter.Router` handles everything the server serves: the API, the
static assets and the single-page-application fallback. There is no second mux
and no per-subtree router.

Routes are registered through `router.Handler` and `router.HandlerFunc`, not
through `router.GET`/`router.POST`. Those methods take an ordinary
`http.Handler` and place the path parameters in the request context, where a
handler reads them with `httprouter.ParamsFromContext(r.Context())`. This keeps
every handler and every piece of middleware a plain `http.Handler`, so the
middleware chain — request ID, logging, authentication, CSRF, security headers —
is written once against the standard interface and does not need adapting to
httprouter's three-argument signature.

Path parameters use httprouter's `:name` syntax. The `{id}` notation in
[04_api.md](04_api.md) is documentation convention; the registered path is
`/api/v1/orders/:id`.

Router configuration:

| Setting                   | Value | Effect                                                              |
| ------------------------- | ----- | ------------------------------------------------------------------- |
| `HandleMethodNotAllowed`  | true  | Returns 405 with an `Allow` header instead of falling through to 404.|
| `HandleOPTIONS`           | true  | Answers `OPTIONS` automatically. `GlobalOPTIONS` adds the CORS headers when `--cors-allowed-origins` is set. |
| `RedirectTrailingSlash`   | true  | `/api/v1/orders/` redirects to `/api/v1/orders`.                     |
| `RedirectFixedPath`       | false | Off deliberately — a case-insensitive redirect would make paths ambiguous and is not wanted for an API. |
| `NotFound`                | set   | The SPA fallback, see below.                                         |
| `PanicHandler`            | set   | Logs at ERROR with the request ID and returns 500 error 9000.        |

Static assets are served by registering `/static/*filepath` against the
`fs.FS` chosen by `internal/static`. Swagger UI is registered at
`/tools/swagger/*filepath` and simply not registered at all when
`--no-swagger` is set.

The SPA fallback is the router's `NotFound` handler: any unmatched path that
does not begin with `/api/` serves `index.html` so that a reload on a deep link
works, and any unmatched path that does begin with `/api/` returns a JSON 404
with error 4000. Using `NotFound` rather than a catch-all route avoids the
wildcard conflicts described below.

**httprouter rejects conflicting routes by panicking at registration time.** It
does not allow a static segment and a wildcard at the same position — `/users/me`
alongside `/users/:id` panics, and a `*filepath` catch-all cannot share a prefix
with any other route. The API in [04_api.md](04_api.md) is designed to be free of
such conflicts, and it must stay that way:

- No `/users/me`-style aliases. The client uses the id from
  `GET /api/v1/auth/session`.
- No catch-all route at the root. The SPA fallback is the `NotFound` handler.
- The catch-alls that do exist, `/static/*filepath` and
  `/tools/swagger/*filepath`, sit under prefixes nothing else uses.

Because the panic happens at registration, a conflict is caught the moment the
server starts rather than at request time. A test that constructs the full
router asserts this, so a conflicting route fails CI instead of production
startup.

## Frontend

| Concern         | Choice                                                      |
| --------------- | ----------------------------------------------------------- |
| Language        | TypeScript, strict mode                                      |
| CSS             | Tailwind CSS, compiled to a static stylesheet at build time   |
| Bundler         | esbuild                                                       |
| Framework       | None. Plain TypeScript modules and the DOM API.               |
| Fonts           | Vendored WOFF2 files under `static/fonts`. No Google Fonts.   |
| Live updates    | `EventSource` (Server-Sent Events)                            |

There is no runtime dependency on any CDN. The Tailwind play CDN script is
explicitly not used — the CSP in
[05_auth_and_permissions.md](05_auth_and_permissions.md) forbids it, and the
offline requirement rules it out.

The application is a single HTML page with client-side routing. Any unknown
non-`/api` path serves that page so a reload on a deep link works.

## Repository layout

```
doenerstag/
├── cmd/doenerstag/          main package, wires cobra commands
├── internal/
│   ├── api/                 HTTP handlers, routing, middleware
│   ├── auth/                sessions, JWT, API tokens, password hashing
│   ├── config/              cobra + viper wiring, config file handling
│   ├── db/                  connection pool, queries, migration runner
│   ├── image/               decode, downscale, thumbnail
│   ├── install/             the install and update verbs: prompts, provisioning, config writing
│   ├── logging/             zerolog setup, correlation IDs
│   ├── model/               domain types shared across packages
│   ├── sse/                 per-order event hubs
│   └── static/              asset serving; chooses embedded vs. on-disk
├── migrations/              golang-migrate SQL, including seed data
├── frontend/
│   ├── src/                 TypeScript sources
│   ├── src/i18n/            en.json, de.json
│   ├── styles/              Tailwind input CSS
│   └── package.json
├── static/                  BUILD OUTPUT — served in development, embedded for release
│   ├── index.html
│   ├── css/
│   ├── js/
│   ├── images/
│   └── fonts/
├── api/openapi.yaml         OpenAPI 3.1 description
├── docs/                    this specification
├── embed.go                 go:embed directive, build tag `embedstatic`
├── embed_disabled.go        counterpart, build tag `!embedstatic`
└── Makefile
```

`static/` is generated. It is checked in so that a release can be built without
running the frontend toolchain, but it is never edited by hand.

## Static asset serving and the embed switch

Two modes, selected by a Go build tag:

**Development (default build).** Assets are read from disk through
`os.DirFS(cfg.StaticDir)`, where `StaticDir` comes from `--static-dir` and
defaults to `./static`. Files are re-read on every request and served with
`Cache-Control: no-store`. Editing a stylesheet or a template and reloading the
browser is enough — the backend does not need to be rebuilt or restarted.

**Release (`-tags embedstatic`).** Assets come from an `embed.FS` compiled into
the binary. `--static-dir` is accepted but ignored, and a warning is logged if
it was set explicitly. Files are served with a long `Cache-Control` and an
`ETag` derived from a build-time content hash.

The `go:embed` directive lives in `embed.go` at the repository root, because an
embed pattern cannot reach outside its own package directory and `static/` is a
subdirectory of the module root:

```go
//go:build embedstatic

package doenerstag

import "embed"

//go:embed all:static
var StaticFS embed.FS

const Embedded = true
```

`embed_disabled.go` carries the `!embedstatic` tag and defines
`const Embedded = false` with an empty FS. `internal/static` selects between the
two at construction time and exposes one `fs.FS` to the rest of the
application. Nothing else in the codebase knows which mode is active.

Releases are always built with `-tags embedstatic`, so a release is a single
binary plus the config file, the TLS certificate and its key.

## Build

The `Makefile` is the entry point. All of it runs locally.

| Target           | Does                                                                    |
| ---------------- | ----------------------------------------------------------------------- |
| `make deps`      | `go mod download` and `npm ci` in `frontend/`.                           |
| `make frontend`  | esbuild the TypeScript, compile Tailwind, copy fonts and images into `static/`. |
| `make dev`       | Frontend in watch mode plus a plain `go build`. Assets served from disk. |
| `make build`     | `make frontend` then `go build` without the embed tag.                   |
| `make release`   | `make frontend` then `go build -tags embedstatic -ldflags "-X main.version=…"`. |
| `make test`      | `go test ./...` and the frontend type check.                             |
| `make lint`      | `go vet`, `golangci-lint`, `tsc --noEmit`, `eslint`.                     |
| `make e2e`       | Playwright against a running server, named by `DOENER_E2E_URL`.          |
| `make migrate`   | Applies migrations against the configured database, for development.     |
| `make packages`  | `make release`, then `nfpm` builds the `.deb` and the `.rpm` from one shared configuration. |
| `make image`     | Builds the container image for `linux/amd64` and `linux/arm64`.          |
| `make licenses`  | Regenerates `THIRD_PARTY_LICENSES` from `go.mod` and `package.json`.     |

`make packages` uses `nfpm` so that both package formats come from a single
declarative description; keeping a `debian/` tree and a `.spec` file in step by
hand is exactly the sort of duplication that drifts. The packaged files are
listed in [10_operations.md](10_operations.md).

The container image is a two-stage build ending in `scratch`, which only works
because the release binary is static and self-contained.

The application version is compiled in with `-ldflags -X` and must match the row
that `install`/`update` writes to `app_version`. A mismatch between the binary's
version and the newest `app_version` row is logged as a WARN at startup.

## Licensing

doenerstag is licensed under the **BSD 3-Clause License**. See
[LICENSE](../LICENSE) at the repository root.

That is compatible with every dependency listed above. Nothing in the set that
is compiled into the binary or shipped in `static/` is copyleft; all of it is
permissive.

### Dependencies distributed with the application

| Dependency                     | Licence      | Compatible with BSD-3-Clause |
| ------------------------------ | ------------ | ---------------------------- |
| Go standard library            | BSD-3-Clause | yes                          |
| `julienschmidt/httprouter`     | BSD-3-Clause | yes                          |
| `spf13/cobra`                  | Apache-2.0   | yes — see the note below     |
| `spf13/pflag`                  | BSD-3-Clause | yes                          |
| `spf13/viper`                  | MIT          | yes                          |
| `jackc/pgx/v5`                 | MIT          | yes                          |
| `golang-migrate/migrate/v4`    | MIT          | yes                          |
| `rs/zerolog`                   | MIT          | yes                          |
| `golang.org/x/crypto`          | BSD-3-Clause | yes                          |
| `golang-jwt/jwt/v5`            | MIT          | yes                          |
| `microcosm-cc/bluemonday`      | BSD-3-Clause | yes                          |
| `golang.org/x/image`           | BSD-3-Clause | yes                          |
| `google/uuid`                  | BSD-3-Clause | yes                          |
| `golang.org/x/net`, `x/text`   | BSD-3-Clause | yes                          |
| Tailwind CSS (generated CSS)   | MIT          | yes                          |

### Build and test tools, not distributed

These never enter the binary or the shipped assets, so their licences place no
obligation on what is distributed. Two of them look alarming and are not:

| Tool                    | Licence      | Note                                                        |
| ----------------------- | ------------ | ----------------------------------------------------------- |
| `golangci-lint`         | GPL-3.0      | A linter that is executed, never linked. No effect on the distributed work. |
| `axe-core`              | MPL-2.0      | Loaded only inside the Playwright test run. Never shipped.   |
| esbuild, eslint, Vitest | MIT          |                                                              |
| TypeScript, Playwright  | Apache-2.0   |                                                              |
| `testify`, `testcontainers-go`, `nfpm` | MIT |                                                    |
| `govulncheck`           | BSD-3-Clause |                                                              |

### Obligations that follow

1. **Apache-2.0 attribution.** `spf13/cobra` is Apache-2.0, which is one-way
   compatible with BSD-3-Clause: it may be combined and redistributed, but its
   licence text and any `NOTICE` file must travel with the distribution. This is
   the only non-trivial obligation in the set.
2. **`THIRD_PARTY_LICENSES`.** A generated file at the repository root
   reproducing the licence text of every distributed dependency. Produced by
   `make licenses` from `go-licenses` and the frontend lockfile, checked in, and
   verified in CI so that adding a dependency without its licence fails the
   build. Shipped in the packages under `/usr/share/doc/doenerstag/` and linked
   from the legal notes page.
3. **Fonts.** The vendored WOFF2 files under `static/fonts` are distributed
   assets and carry their own licence — commonly SIL OFL 1.1, which requires the
   licence to ship alongside and restricts renaming. The chosen font's licence
   must be added to `THIRD_PARTY_LICENSES` when the font is chosen. This is the
   easiest obligation in the list to overlook, because fonts do not appear in
   any dependency manifest.
4. **A licence audit in CI.** `go-licenses check` with an allow-list of
   BSD-2-Clause, BSD-3-Clause, MIT, ISC and Apache-2.0. A new dependency under
   any other licence fails the build rather than being noticed at release time.

The licences above reflect the state of these projects at the time of writing
and should be confirmed by the first `make licenses` run once the dependencies
are actually pinned in `go.mod`.

## Database

PostgreSQL 18 is the only supported version. Nothing older is tested or
supported.

- Connection through `pgx/v5`'s pool. `--max-connection-pool` caps both the
  maximum open and the maximum idle connections.
- `--database-sslmode` is passed through to the connection string. Default
  `prefer`.
- The session time zone is forced to UTC on every connection.
- Schema changes are `golang-migrate` migrations under `migrations/`, applied by
  `doenerstag install` and `doenerstag update`. The server verb never migrates;
  it refuses to start if the schema version does not match what the binary
  expects.
- Seed data — allergens, additives, currencies, contact types, default tags and
  the deleted-user placeholder — ships as `INSERT` statements inside migrations.
  Nothing is downloaded at install time.

## Logging

`zerolog`, JSON output, one object per line, to stdout by default or to the file
given by `--log-file`.

### Levels

| Name    | Number | Contains                                                                          |
| ------- | ------ | --------------------------------------------------------------------------------- |
| `FATAL` | 1      | Errors that force the application to terminate.                                    |
| `ERROR` | 2      | Errors that cannot be compensated for.                                             |
| `WARN`  | 3      | Situations the application recovers from and continues processing.                 |
| `INFO`  | 4      | All API requests, and every data-changing event. This is the audit trail.          |
| `DEBUG` | 5      | Every function entry with its parameters, and every database query with its timing.|

Each level also emits everything from the lower-numbered levels.
`--log-level` accepts any casing and normalizes to upper case.

DEBUG is intentionally very verbose. It is a development and incident tool, not
a level to run in production.

### Standard fields

Every line carries:

| Field        | Notes                                                        |
| ------------ | ------------------------------------------------------------ |
| `time`       | RFC 3339 with milliseconds, UTC.                              |
| `level`      | Lower-case level name.                                        |
| `message`    | Short English text.                                           |
| `component`  | Package or subsystem, e.g. `api`, `db`, `auth`.               |

Request-scoped lines additionally carry:

| Field        | Notes                                                                    |
| ------------ | ------------------------------------------------------------------------ |
| `request_id` | UUIDv7 generated per request, or taken from an inbound `X-Request-Id`.    |
| `user`       | Acting user name, or `-` when anonymous.                                  |
| `method`     | HTTP method.                                                             |
| `path`       | Request path, without the query string.                                   |
| `status`     | Response status code, on the completion line.                             |
| `duration_ms`| Handler duration, on the completion line.                                 |

The request ID is generated by the outermost middleware, put into the request
`context`, echoed in the `X-Request-Id` response header and included in every
API error body. Every log line written while handling that request carries it,
including database and DEBUG lines, so one request can be reconstructed with a
single `grep`.

### Redaction

The DEBUG function-entry logging reflects parameters, so it must never print
secrets. The logger maintains a deny-list of field and parameter names —
`password`, `password_hash`, `token`, `jwt_secret`, `authorization`, `cookie`,
`csrf` — and any matching value is replaced with `***`. Struct fields carry a
`log:"-"` tag where the value must never appear. Request bodies are never logged
in full; only the field names present in them are.

### Rotation

Rotation is external. When `--log-file` is used, the operator is expected to use
`logrotate`. The application reopens its log file on `SIGHUP`, so the
`postrotate` script needs only to send that signal — `copytruncate` is not
required and should not be used.
