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
| HTTP routing         | `net/http` with the standard library's method-and-pattern routing (Go 1.22+). No third-party router. |
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
| `make migrate`   | Applies migrations against the configured database, for development.     |

The application version is compiled in with `-ldflags -X` and must match the row
that `install`/`update` writes to `app_version`. A mismatch between the binary's
version and the newest `app_version` row is logged as a WARN at startup.

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
