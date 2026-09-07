# 14 Implementation plan

A sequential plan: one sprint at a time, no two sprints running in parallel.
Each sprint depends only on the ones before it, so a single developer or a
single team can work straight down the list without ever needing to stub out
something that a concurrent sprint is supposed to be building.

The database schema and the installer come first, as requested. Everything after
that follows dependency order.

## Assumptions

- One team working on one sprint at a time.
- Sprints are sized roughly equally, but no calendar length is asserted here —
  calibrate after Sprint 1 and adjust.
- Sizes are relative: **S** small, **M** medium, **L** large.
- A task is done when it is implemented, tested to the standard in
  [12_testing.md](12_testing.md), and CI is green.

## Branching

**Every sprint gets its own branch, named `sprint-<number>`.** A sprint's branch
starts from the tip of the previous sprint's branch and ends at that sprint's
last commit, so each branch is exactly the work of one sprint and the whole
history reads as a sequence of them.

| Branch     | Contains                                              |
| ---------- | ----------------------------------------------------- |
| `sprint-0` | The specification and this plan. No code.              |
| `sprint-1` | Tasks 1.1 – 1.15.                                      |
| `sprint-2` | Tasks 2.1 – 2.14.                                      |
| `sprint-n` | The tasks of sprint n, branched from `sprint-(n-1)`.   |

Commit messages carry the task number they implement, in the form
`Task <n.m>: <description>`, so a commit can be traced to the plan entry it
satisfies. A commit that closes several tasks names all of them.

Work that belongs to no task — a defect found while running a later sprint's
code, for instance — is committed on the branch of the sprint that found it,
not retrofitted into the earlier one, and says in its message which sprint's
artefacts it corrects.

## Milestones

| After     | You can                                                                     |
| --------- | --------------------------------------------------------------------------- |
| Sprint 3  | Install the application on a clean machine: provisioned database, `root` account, valid config file. Nothing serves yet. |
| Sprint 4  | Run the server over HTTPS, hit `/health`, browse Swagger UI, serve assets.   |
| Sprint 5  | Register, log in, manage accounts and tokens through the API.                |
| Sprint 9  | Run a complete food order end to end **through the API**. The backend is feature-complete. |
| Sprint 13 | Run a complete food order end to end in a browser. The product works.        |
| Sprint 15 | Install version 1.0.0 from a `.deb`, an `.rpm` or a container image.         |

## A note on the sequencing

This plan is backend-first: the API is finished (Sprint 9) before the real
frontend starts (Sprint 10). That is what "no parallel sprints" plus "database
and installer first" produces, and it has a real cost — **there is nothing an
end user can look at until around Sprint 12.**

Two things take the edge off it. Swagger UI is available from Sprint 4, so the
API is explorable and demonstrable throughout the middle of the plan. And a
minimal frontend build lands in Sprint 4 as well — enough to prove the esbuild,
Tailwind and `go:embed` pipeline works, which is the part of the frontend most
likely to produce nasty surprises late.

If seeing the product earlier matters more than strict layering, the alternative
is vertical slices: build restaurants end to end (API and UI), then menus, then
orders. That trades some rework on the frontend foundation for much earlier
feedback. Say so and this plan can be resequenced that way — but it is a
different plan, not a tweak to this one.

---

## Sprint 1 — Skeleton, configuration, logging

**Goal:** the binary exists, reads configuration from all four sources in the
right order, logs correctly, and every verb is a stub that fails cleanly.

Nothing here touches the database. It exists first because the installer in
Sprint 3 needs configuration and logging to already work.

| ID  | Task                                                                                          | Size | Spec |
| --- | --------------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 1.1 | `go mod init`, repository layout, `.gitignore`, `.editorconfig`, `golangci-lint` config     | S | [08](08_technologies.md) |
| ✅ 1.1.1 | `.gitattributes` normalising line endings to LF. Added because development happens on Windows, where a checkout would otherwise reintroduce CRLF and break `gofmt -l` in CI. | S | — |
| ✅ 1.2 | `Makefile` with `deps`, `build`, `test`, `lint`; frontend targets stubbed                    | S | [08](08_technologies.md) |
| ✅ 1.3 | cobra command tree: root plus `server`, `install`, `update`, `cleanup`, `version` as stubs   | S | [09](09_configuration.md) |
| ✅ 1.4 | viper wiring: defaults → config file → `DOENER_*` → flags, with the nested-key mapping        | M | [09](09_configuration.md) |
| ✅ 1.4.1 | Duration parser extending `time.ParseDuration` with day and week units. Added because the documented defaults `7d` and `14d` are rejected outright by the standard library, which stops at hours. | S | [09](09_configuration.md) |
| ✅ 1.4.2 | Byte-size parser for `--max-image-size`. Added because `5MiB` has no parser in the standard library. | S | [09](09_configuration.md) |
| ✅ 1.5 | Global flags; `--version` with the build-time version injected via `-ldflags`                | S | [09](09_configuration.md) |
| ✅ 1.6 | Secret-on-the-command-line rule: FATAL for each of the four settings                         | S | [09](09_configuration.md) |
| ✅ 1.7 | Config file permission check: `0600`/`0400` pass, anything else FATAL, skipped on Windows     | S | [09](09_configuration.md) |
| ✅ 1.8 | zerolog: five levels, JSON output, `--log-file`, `SIGHUP` reopen, standard fields             | M | [08](08_technologies.md) |
| ✅ 1.9 | Redaction deny-list for the DEBUG parameter logging                                          | S | [08](08_technologies.md) |
| ✅ 1.10 | Unit tests: precedence, secret rule, permission check, redaction, level mapping             | M | [12](12_testing.md) |
| ✅ 1.11 | CI pipeline: `lint`, `vet`, `test`, `govulncheck`                                           | M | [12](12_testing.md) |
| ✅ 1.12 | Extend `contrib/setup_dev_pipeline.sh` with everything the build and test pipeline needs at this stage | M | [08](08_technologies.md) |
| ✅ 1.13 | Add the tools later sprints are already known to need to the same script                    | S | [08](08_technologies.md) |
| ✅ 1.14 | Run the build and test pipeline on the Linux VM                                             | M | [12](12_testing.md) |
| ✅ 1.15 | Keep the Windows and Linux coverage results side by side as `<os>-coverage.out`, and render each to `<os>-coverage.html` | S | [12](12_testing.md) |

**Exit criteria:** `doenerstag --version` prints the version. Every verb runs and
exits with a clear "not implemented". Passing a password on the command line is
fatal. A `0644` config file is fatal. CI is green. The pipeline runs on a Linux
machine provisioned solely by `contrib/setup_dev_pipeline.sh`.

### The development VM

`contrib/setup_dev_pipeline.sh` provisions a Debian machine with everything the
project needs. It is the definition of the development environment: a tool the
pipeline needs and the script does not install is a defect in the script.

It is deliberately close to what CI installs. The Windows workstation cannot run
`go test -race`, which needs cgo, so the Linux VM is where the race detector and
the Docker-dependent database tests from sprint 2 onwards actually run before
they reach CI.

### Coverage is per platform

The two platforms do not execute the same code. The configuration file
permission check is skipped on Windows and the `SIGHUP` log reopen does not
exist there, so a single coverage number is an average of two different runs and
hides which lines are actually unexercised on each.

Coverage is therefore kept separately as `linux-coverage.out` and
`windows-coverage.out`, each rendered to a matching `.html`, and both uploaded
by CI. When a line looks uncovered, the question worth asking is *on which
platform* — and that is only answerable if the two are never merged.

---

## Sprint 2 — Database schema and seed data

**Goal:** the complete schema exists as migrations, applies and rolls back
cleanly against PostgreSQL 18, and the seeded reference data is verified.

No application logic reads or writes it yet. Getting the schema right before
anything depends on it is the cheapest ordering there is.

| ID   | Task                                                                                       | Size | Spec |
| ---- | ------------------------------------------------------------------------------------------ | :--: | ---- |
| ✅ 2.1 | pgx pool: connection string assembly, `sslmode`, `--max-connection-pool`, UTC session       | M | [08](08_technologies.md) |
| ✅ 2.2 | golang-migrate integration with embedded migration files                                    | M | [08](08_technologies.md) |
| ✅ 2.3 | Audit column convention and the `updated_at` trigger                                        | S | [03](03_data_model.md) |
| ✅ 2.4 | Migration: `app_user`, `session`, `api_token`; deleted-user placeholder with its fixed UUID | M | [03](03_data_model.md) |
| ✅ 2.5 | Migration: `currency`, `contact_type`, `tag`, `allergen`, `additive` seeds with source comments | M | [03](03_data_model.md) |
| ✅ 2.6 | Migration: `restaurant`, `restaurant_contact`, `opening_hours`                              | M | [03](03_data_model.md) |
| ✅ 2.7 | Migration: `menu_category`, `menu_item`, `menu_item_modification`, the three link tables    | M | [03](03_data_model.md) |
| ✅ 2.8 | Migration: `food_order`, `order_item`, `order_item_modification`                            | M | [03](03_data_model.md) |
| ✅ 2.9 | Migration: `image`, `content_page`, `app_version`                                           | S | [03](03_data_model.md) |
| ✅ 2.10 | Indexes, `CHECK` constraints, partial unique indexes, foreign key actions                  | M | [03](03_data_model.md) |
| ✅ 2.11 | Test harness: testcontainers-go, `DOENER_TEST_DATABASE_URL` fallback, clean skip when neither | M | [12](12_testing.md) |
| ✅ 2.12 | Migration tests: up and down from empty and from each intermediate version                | M | [12](12_testing.md) |
| ✅ 2.13 | Seed verification tests, asserting each `code` individually                               | M | [12](12_testing.md) |
| ✅ 2.14 | Constraint tests: `deadline_at < fulfilment_at`, `quantity >= 1`, uniqueness, day-of-week range | M | [12](12_testing.md) |
| ✅ 2.15 | Fix the CI failures the sprint 2 dependencies introduced: `govulncheck` and the Windows test job | M | [12](12_testing.md) |

**Exit criteria:** migrations apply and roll back cleanly at every version. The
14 allergens, 14 additives, currencies, contact types and default tags are
present with the exact codes the catalogs will key on. Every documented
constraint is proven to reject what it should.

---

## Sprint 3 — Installer

**Goal:** a clean machine goes from nothing to a provisioned database, a `root`
administrator and a valid configuration file.

| ID   | Task                                                                                        | Size | Spec |
| ---- | -------------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 3.1 | Interactive prompt framework, offering values from an existing config file as defaults       | M | [09](09_configuration.md) |
| ✅ 3.2 | Safe output handling: writability probe that cannot truncate the input file, temp file + rename | M | [09](09_configuration.md) |
| ✅ 3.3 | Root and admin identities: create database, create runtime role, grant DML only               | L | [09](09_configuration.md) |
| ✅ 3.4 | Apply migrations using the admin identity                                                     | S | [10](10_operations.md) |
| ✅ 3.5 | Password package: Argon2id hashing in PHC format, and the three-of-five complexity rules with NFC | L | [05](05_auth_and_permissions.md) |
| ✅ 3.6 | Create the `root` administrator interactively                                                    | S | [05](05_auth_and_permissions.md) |
| ✅ 3.7 | Generate `jwt_secret` and write it to the config file                                            | S | [05](05_auth_and_permissions.md) |
| ✅ 3.8 | Load imprint and legal notes snippets from files, sanitize with bluemonday, insert              | M | [02](02_features.md) |
| ✅ 3.9 | Config file writer: every setting with its explanatory comment, mode `0600`                     | M | [09](09_configuration.md) |
| ✅ 3.10 | Append the `app_version` row                                                                  | S | [03](03_data_model.md) |
| ✅ 3.11 | `--non-interactive` mode                                                                      | S | [09](09_configuration.md) |
| ✅ 3.12 | `version` verb reading the newest `app_version` row                                           | S | [09](09_configuration.md) |
| ✅ 3.13 | Installer integration tests against a throwaway database                                       | L | [12](12_testing.md) |

**Exit criteria:** `doenerstag install` against an empty PostgreSQL 18 produces a
working database, a usable `root` login and a `0600` config file containing every
setting. `doenerstag version` reports what was installed. Re-running `install`
offers the existing values as defaults and does not corrupt the file it is
reading.

Password complexity lands here rather than in Sprint 5 because the installer is
the first thing that has to accept a password.

---

## Sprint 4 — HTTP server foundation

**Goal:** the server runs over HTTPS with the full middleware chain, serves
static assets in both modes, and answers the operational endpoints.

| ID   | Task                                                                                     | Size | Spec |
| ---- | ------------------------------------------------------------------------------------------ | :--: | ---- |
| ✅ 4.1 | httprouter setup, `router.Handler` registration, `ParamsFromContext` helper                 | M | [08](08_technologies.md) |
| ✅ 4.2 | `NotFound` SPA fallback, JSON 404 under `/api/`, `MethodNotAllowed`, `OPTIONS`, `PanicHandler` | M | [08](08_technologies.md) |
| ✅ 4.3 | Router construction test proving no route conflicts                                          | S | [12](12_testing.md) |
| ✅ 4.4 | Middleware chain: request ID, INFO request logging, recovery, security headers                | M | [05](05_auth_and_permissions.md) |
| ✅ 4.5 | Error envelope and the error number registry                                                  | M | [04](04_api.md) |
| ✅ 4.6 | `internal/static` with the `embedstatic` build tag; `embed.go` and `embed_disabled.go`        | M | [adr/0002](adr/0002-static-assets-embed-toggle.md) |
| ✅ 4.7 | Minimal frontend build: esbuild, Tailwind, `index.html` skeleton; `make frontend`/`dev`/`release` | L | [08](08_technologies.md) |
| ✅ 4.7.1 | CI: build the frontend, type-check it, and fail if the committed `static/` no longer matches its sources | S | [12](12_testing.md) |
| ✅ 4.8 | TLS listener, `--no-https`, bind address, HTTP timeouts, all five startup checks                | M | [09](09_configuration.md) |
| ✅ 4.9 | Graceful shutdown on `SIGTERM`/`SIGINT` with `--shutdown-grace`                                | M | [10](10_operations.md) |
| ✅ 4.10 | `/health`, `/metrics`, `/version` endpoints                                                    | M | [04](04_api.md) |
| ✅ 4.11 | OpenAPI document skeleton, Swagger UI serving, `--no-swagger`                                   | M | [04](04_api.md) |
| ✅ 4.11.1 | Document error code 4006 (405) in [04](04_api.md); `handleMethodNotAllowed` was answering 405 with the 404 code | S | [04](04_api.md) |
| ✅ 4.12 | CORS handling for `--cors-allowed-origins`                                                      | S | [05](05_auth_and_permissions.md) |
| ✅ 4.13 | API tests for all of the above, including the security header assertions                        | M | [12](12_testing.md) |
| ✅ 4.14 | Deploy on the VM exactly as [10](10_operations.md) documents it, to test the document as well as the build | S | [10](10_operations.md) |
| ✅ 4.14.1 | `install` validates the binary's version before touching the database; the Makefile stops stamping a bare commit hash | S | [10](10_operations.md) |

**Exit criteria:** `doenerstag server` serves HTTPS. `/health` returns 200 with
the database up and 503 without it. Assets serve from disk in a default build and
from the binary in a `-tags embedstatic` build. Swagger UI loads. Both builds are
exercised in CI.

---

## Sprint 5 — Accounts, sessions, authorization

**Goal:** registration, login, sessions, tokens and the complete permission model.

| ID   | Task                                                                                    | Size | Spec |
| ---- | ----------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 5.1.1 | `internal/model` domain types, the user/session/token queries in `internal/db`, JWT signing and API token generation in `internal/auth` | L | [03](03_data_model.md) |
| ✅ 5.1 | `POST /auth/register` with the complexity rules from 3.5                                    | M | [05](05_auth_and_permissions.md) |
| ✅ 5.2 | `POST /auth/login`: session row, JWT issue, single-session replacement, both cookies          | L | [adr/0004](adr/0004-jwt-with-server-side-sessions.md) |
| ✅ 5.3 | Session validation middleware: signature, `exp`, session row, idle and absolute timeouts, `last_seen_at` throttle | L | [05](05_auth_and_permissions.md) |
| ✅ 5.3.1 | Make the request log line see the acting user. `WithUserName` returned a context the outer logging middleware never saw, so the user field would have read `-` on every authenticated request | S | [08](08_technologies.md) |
| ✅ 5.3.2 | Give session and token timestamps one clock. `last_seen_at` was written by the database and judged against the application clock | S | [05](05_auth_and_permissions.md) |
| ✅ 5.4 | `POST /auth/logout` and `GET /auth/session`                                                  | S | [04](04_api.md) |
| ✅ 5.5 | CSRF double-submit enforcement with constant-time comparison                                  | M | [05](05_auth_and_permissions.md) |
| ✅ 5.6 | Login rate limiting per user name and per client address                                      | M | [05](05_auth_and_permissions.md) |
| ✅ 5.7 | API tokens: create, list, revoke, Bearer authentication, CSRF exemption                        | M | [05](05_auth_and_permissions.md) |
| ✅ 5.8 | Authorization helpers: authenticated, owner, creator, participant, admin                       | M | [05](05_auth_and_permissions.md) |
| ✅ 5.9 | `GET`/`PATCH /users/{id}` and the admin user list                                              | M | [04](04_api.md) |
| ✅ 5.9.1 | Document error code 3005, a general "not the owner". A profile request was being refused with 3002, "only the owner may change this order item" | S | [04](04_api.md) |
| ✅ 5.10 | Account deletion and `GET /users/{id}/deletion-impact` with the remap and delete rules          | L | [02](02_features.md) |
| ✅ 5.11 | Permission matrix test suite, one case per cell                                                | L | [12](12_testing.md) |

**Exit criteria:** the full account lifecycle works through the API. Logging in
twice invalidates the first session. A cookie-authenticated write without the
CSRF header is rejected; the same request with a Bearer token succeeds. Every
cell of the permission matrix has a passing test.

Task 5.10 depends on `order_item`, which exists as a table from Sprint 2 but has
no API until Sprint 8. Its tests insert order fixtures directly; Sprint 8
re-verifies the behaviour against real orders.

---

## Sprint 6 — Restaurants and images

**Goal:** restaurants, their contacts, their opening hours, and image upload.

| ID  | Task                                                                                  | Size | Spec |
| --- | --------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 6.1 | Restaurant create, read, list, update                                                    | M | [04](04_api.md) |
| ✅ 6.2 | Contacts subresource, with the at-least-one rule                                          | M | [04](04_api.md) |
| ✅ 6.3 | Opening hours: whole-set `PUT`, validation, midnight crossing, multiple ranges per day     | M | [03](03_data_model.md) |
| ✅ 6.4 | Reference endpoints `/currencies` and `/contact-types`                                    | S | [04](04_api.md) |
| ✅ 6.5 | Restaurant delete: admin only, 409 while referenced by an order                            | S | [04](04_api.md) |
| ✅ 6.6 | Image pipeline: content sniffing, size cap, decode, downscale, thumbnail, SHA-256 dedupe   | L | [adr/0008](adr/0008-images-in-the-database.md) |
| ✅ 6.7 | `POST /images`, `GET /images/{id}`, `GET /images/{id}/thumbnail` with `ETag` and caching   | M | [04](04_api.md) |
| ✅ 6.8 | Logo association on restaurants                                                            | S | [03](03_data_model.md) |
| ✅ 6.9 | Tests, including a file whose extension lies about its contents                            | M | [12](12_testing.md) |

**Exit criteria:** a restaurant with a logo, several contacts and a lunch-break
opening-hours pattern can be created and read back through the API. Uploading a
non-image with a `.jpg` name is rejected.

---

## Sprint 7 — Menus

**Goal:** categories, menu items, modifications and classification.

| ID  | Task                                                                                     | Size | Spec |
| --- | ------------------------------------------------------------------------------------------ | :--: | ---- |
| ✅ 7.1 | Category create, update, reorder, soft delete                                               | M | [04](04_api.md) |
| ✅ 7.2 | Menu item create, read, update, soft delete, `available` flag                                | M | [04](04_api.md) |
| ✅ 7.3 | Ordering rule: numeric-aware `external_id`, `NULLS LAST`, then `name`                        | M | [03](03_data_model.md) |
| ✅ 7.4 | Modifications: create, update, soft delete                                                   | M | [adr/0007](adr/0007-flat-modification-list.md) |
| ✅ 7.5 | `/tags` list and create; `/allergens` and `/additives` read-only lists                        | S | [04](04_api.md) |
| ✅ 7.6 | Tag, allergen and additive assignment to menu items                                           | M | [03](03_data_model.md) |
| ✅ 7.7 | Menu item filtering: `category`, `tag`, `exclude_allergen`, `exclude_additive`, `available`    | M | [04](04_api.md) |
| ✅ 7.8 | Soft delete semantics: hidden from every normal query                                          | S | [03](03_data_model.md) |
| ✅ 7.9 | Tests, including the ordering rule and the filter combinations                                 | M | [12](12_testing.md) |

**Exit criteria:** a full menu can be built through the API, ordered as a printed
menu would be, and filtered by tag and by allergen exclusion.

---

## Sprint 8 — Orders and order items

**Goal:** the core domain. Orders, items, snapshots and the visibility split.

| ID   | Task                                                                                      | Size | Spec |
| ---- | ------------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 8.1 | Order create, copying currency, minimum order value and delivery fee from the restaurant      | M | [03](03_data_model.md) |
| ✅ 8.2 | Order read in both shapes: anonymous header plus `item_count`, authenticated full             | L | [adr/0011](adr/0011-tiered-order-visibility.md) |
| ✅ 8.3 | Order update: creator only, restaurant locked once items exist, deadline before fulfilment     | M | [02](02_features.md) |
| ✅ 8.4 | Order delete, cascading                                                                       | S | [04](04_api.md) |
| ✅ 8.5 | Order list, active first then expired, never with item detail                                  | M | [06](06_ui_ux.md) |
| ✅ 8.6 | Order items: create with name and price snapshots, update, delete                              | L | [adr/0009](adr/0009-snapshot-prices-on-order-items.md) |
| ✅ 8.7 | Order item modifications with their own snapshots; line total arithmetic                        | M | [03](03_data_model.md) |
| ✅ 8.8 | Ownership and deadline rules, including read-only after the deadline for the administrator      | M | [05](05_auth_and_permissions.md) |
| ✅ 8.9 | Derived status and computed title                                                               | S | [02](02_features.md) |
| ✅ 8.10 | Re-verify account deletion against real orders                                                  | M | [02](02_features.md) |
| ✅ 8.11 | Tests, including the anonymous leak test that scans the whole response body                     | L | [12](12_testing.md) |
| ✅ 8.11.1 | Fix a sprint-6 test that deduplication and Go's map iteration order had made vacuous | S | [12](12_testing.md) |
| ✅ 8.12 | Remove the creator from the anonymous order shape, so no user is named to an anonymous caller at all | S | [adr/0011](adr/0011-tiered-order-visibility.md) |

**Exit criteria:** an order can be created, filled by several users and read
back. Changing a menu item's name and price afterwards leaves the order
untouched. An anonymous `GET` of an order contains no user name and no menu item
name anywhere in the response body.

---

## Sprint 9 — Summary, live updates, cleanup

**Goal:** the backend is feature-complete.

| ID  | Task                                                                                          | Size | Spec |
| --- | ----------------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 9.1 | Summary aggregation, per-person breakdown, totals, below-minimum flag, plain-text rendering        | L | [04](04_api.md) |
| ✅ 9.2 | Participant authorization with error 3004, creator included without items                         | M | [adr/0011](adr/0011-tiered-order-visibility.md) |
| ✅ 9.3 | Per-order SSE hub with subscriber management                                                       | L | [adr/0003](adr/0003-sse-for-order-updates.md) |
| ✅ 9.4 | Event publication after commit, including `order.expired`                                          | M | [04](04_api.md) |
| ✅ 9.5 | Per-subscriber filtering: anonymous receives `order.item_count`, always republished                 | M | [02](02_features.md) |
| ✅ 9.6 | Write-deadline exemption, 30-second `ping`, stream closure on shutdown                              | M | [09](09_configuration.md) |
| ✅ 9.7 | `cleanup` verb: the six ordered steps plus `--dry-run`                                              | L | [09](09_configuration.md) |
| ✅ 9.8 | Tests: aggregation, the SSE split, and a stream surviving past `--http-write-timeout`               | L | [12](12_testing.md) |
| ✅ 9.8.1 | Restore `Flush` on the request-logging wrapper. Embedding an `http.ResponseWriter` hid it, so no handler beneath the middleware could stream | S | [adr/0003](adr/0003-sse-for-order-updates.md) |

**Exit criteria:** a complete food order can be run end to end through the API.
Two subscribers on one order, one anonymous and one authenticated, each receive
only their own event set. `cleanup --dry-run` reports what a real run would
remove, and a real run removes exactly that.

---

## Sprint 10 — Frontend foundation

**Goal:** the shell every screen is built on.

| ID   | Task                                                                                  | Size | Spec |
| ---- | --------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 10.1 | Full esbuild and Tailwind pipeline with a watch mode                                     | M | [08](08_technologies.md) |
| ✅ 10.2 | Client-side router, page shell, dark and light themes with the cookie                     | M | [06](06_ui_ux.md) |
| ✅ 10.3 | i18n runtime: catalog loading, the `_meta` registry generated at build time, precedence     | L | [07](07_i18n.md) |
| ✅ 10.4 | `en.json` and `de.json` including every reference-data key                                 | M | [07](07_i18n.md) |
| ✅ 10.5 | Catalog completeness, registry and reference-data coverage tests                            | M | [12](12_testing.md) |
| ✅ 10.6 | `Intl` formatting helpers for dates, times, relative times, numbers and money               | M | [07](07_i18n.md) |
| ✅ 10.7 | API client: fetch wrapper, CSRF header, error code to message mapping, session state         | M | [04](04_api.md) |
| ✅ 10.8 | Title bar, dropdown menu, language selector driven by the registry, login/logout states       | M | [06](06_ui_ux.md) |
| ✅ 10.8.1 | `/version` reports whether Swagger UI is served. The menu must hide the API documentation entry under `--no-swagger`, and nothing told the frontend: the route is omitted rather than answering 404, so a probe gets the SPA fallback and sees a Swagger UI that is not there | S | [04](04_api.md) |
| ✅ 10.9 | Base components: tile grid, modal with focus trap, form controls, confirmation dialog         | L | [06](06_ui_ux.md) |
| ✅ 10.10| Responsive breakpoints and the print stylesheet foundation                                     | M | [06](06_ui_ux.md) |
| ✅ 10.11| Vitest setup and the first unit tests                                                          | S | [12](12_testing.md) |
| ✅ 10.11.1 | eslint with typescript-eslint, wired into `make lint` and CI. [08](08_technologies.md) and [11](11_nonfunctional.md) both promise the frontend passes eslint; until this sprint there was no frontend to lint | S | [08](08_technologies.md) |

**Exit criteria:** the shell renders in both themes and both languages, the
language selector is populated from the catalogs present in the build rather than
from a hardcoded list, and money formats correctly for a CHF restaurant viewed in
German.

---

## Sprint 11 — Frontend: accounts, restaurants, menus

| ID   | Task                                                                                | Size | Spec |
| ---- | ------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 11.1 | Login and register page with the live complexity indicator                             | M | [06](06_ui_ux.md) |
| ✅ 11.2 | User page: profile, password change, API token management                              | M | [06](06_ui_ux.md) |
| ✅ 11.3 | Account deletion with the impact modal and the type-your-name confirmation              | M | [06](06_ui_ux.md) |
| ✅ 11.4 | Restaurant overview tiles and the plus-tile empty state                                 | S | [06](06_ui_ux.md) |
| ✅ 11.5 | Restaurant page: data, currency, minimum order value, delivery fee, logo upload          | M | [06](06_ui_ux.md) |
| ✅ 11.6 | Contacts editor and opening hours editor with the crosses-midnight hint                  | M | [06](06_ui_ux.md) |
| ✅ 11.7 | Menu editing: categories with keyboard-accessible reordering, items, modifications        | L | [06](06_ui_ux.md) |
| ✅ 11.8 | Tag, allergen and additive assignment UI                                                  | M | [06](06_ui_ux.md) |
| ✅ 11.9 | Image upload component with client-side size feedback                                     | M | [06](06_ui_ux.md) |
| ✅ 11.9.1 | `/version` reports `--max-image-size`. The upload control refuses an oversized file before spending a minute sending it, and the limit is an operator's setting rather than a constant the frontend can hold | S | [04](04_api.md) |
| ✅ 11.10 | Frontend unit tests for the sprint: the password rules mirrored from the Go implementation, money parsing, and the account and restaurant pages rendered against a stubbed server | M | [12](12_testing.md) |
| ✅ 11.10.1 | Read the collection envelope. Every collection endpoint answers `{"<plural>": [...]}` rather than a bare array; the frontend assumed arrays, and so did its stubs, so the tests agreed with the mistake. Documented as a convention in [04](04_api.md) and unwrapped in one place | S | [04](04_api.md) |
| ✅ 11.10.2 | Restaurant tiles carry what [06](06_ui_ux.md) asks of them: the first contact, the menu item count and an "open now" state computed from the opening hours, crossed midnights included | S | [06](06_ui_ux.md) |

**Exit criteria:** a restaurant and its complete menu can be entered in the
browser, by a user who is not an administrator.

---

## Sprint 12 — Frontend: orders

| ID   | Task                                                                                   | Size | Spec |
| ---- | ---------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 12.1 | Order overview tiles: active and expired, deadline with relative hint, counts, totals      | M | [06](06_ui_ux.md) |
| ✅ 12.2 | Create order flow with client-side validation of the deadline and opening hours warning     | M | [02](02_features.md) |
| ✅ 12.3 | Order page left column: order data, creator editing, delete with confirmation               | L | [06](06_ui_ux.md) |
| ✅ 12.4 | Order page right column: menu, collapsible categories, the three filters                    | L | [06](06_ui_ux.md) |
| ✅ 12.5 | Add-item dialog: quantity, modification checkboxes, free text, live line total               | M | [06](06_ui_ux.md) |
| ✅ 12.6 | Add a missing menu item from inside the order                                                | M | [02](02_features.md) |
| ✅ 12.7 | The anonymous order view: item count, explanatory line, login link                            | M | [06](06_ui_ux.md) |
| ✅ 12.8 | SSE subscription, automatic reconnect, re-fetch on reconnect, reconnect on login               | L | [04](04_api.md) |
| ✅ 12.9 | Deadline transition to read-only while the page is open                                        | M | [02](02_features.md) |
| ✅ 12.10| Playwright coverage of the core flows, including two browser contexts seeing a live update      | L | [12](12_testing.md) |
| ✅ 12.10.1 | Playwright browsers in `contrib/setup_dev_pipeline.sh`, `make e2e`, and a CI job that installs, starts and exercises the release binary against a PostgreSQL service container | M | [12](12_testing.md) |
| ✅ 12.11 | Router teardown: a page can register work to undo when it is replaced, because an event stream outlives the DOM it feeds unless somebody closes it | S | [08](08_technologies.md) |
| ✅ 12.12 | Move the menu item editor into a component. The order page opens the same dialog to add a dish the menu is missing, and two copies of that form would have drifted | S | [06](06_ui_ux.md) |

**Exit criteria:** two people in two browsers can fill one order and see each
other's items appear without reloading.

---

## Sprint 13 — Frontend: summary, admin, content pages

| ID   | Task                                                                            | Size | Spec |
| ---- | --------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 13.1 | Summary page: the four sections, with the aggregated list visually dominant        | L | [06](06_ui_ux.md) |
| ✅ 13.2 | Copy-as-text, and the print stylesheet                                             | M | [06](06_ui_ux.md) |
| ✅ 13.3 | The non-participant explanatory page, with a login link when anonymous              | S | [06](06_ui_ux.md) |
| ✅ 13.4 | Administration: user list with the impact-confirmed delete                          | M | [06](06_ui_ux.md) |
| ✅ 13.5 | Administration: imprint and legal notes replacement                                 | M | [02](02_features.md) |
| ✅ 13.6 | Version, imprint and legal notes pages                                              | S | [06](06_ui_ux.md) |
| ✅ 13.7 | Playwright coverage for these screens                                               | M | [12](12_testing.md) |
| ✅ 13.8 | `GET` and `PUT /pages/{key}`. [04](04_api.md) has documented them since sprint 0 and nothing ever built them: the installer filled the table and no endpoint read it, so 13.5 and 13.6 had nothing to call | M | [04](04_api.md) |
| ✅ 13.9 | Move the HTML sanitiser into `internal/htmlsafe`, so the installer and the API apply one policy rather than two that can drift apart | S | [11](11_nonfunctional.md) |
| ✅ 13.10 | The summary buttons deferred from sprint 12, on the order page and on the order tile, for participants only | S | [06](06_ui_ux.md) |
| ✅ 13.11 | Date formatting survives a value that is not a date. A development build reports its build date as `unknown`, `Intl` throws on it, and the version page rendered nothing at all | S | [07](07_i18n.md) |

**Exit criteria:** the whole product works in a browser. A person can create an
order, others can join it, and the creator can read the summary down the phone.

---

## Sprint 14 — Accessibility, hardening, the update verb

**Goal:** make it correct and safe rather than merely working.

| ID   | Task                                                                                       | Size | Spec |
| ---- | -------------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 14.1 | WCAG 2.1 AA pass: focus indicators, contrast in both themes, semantics, skip link, live regions | L | [06](06_ui_ux.md) |
| ✅ 14.1.1 | An expired order tile was drawn with `opacity: 0.72`, which applies to the whole subtree and cannot be undone by a child, so it dimmed the "expired" badge too. 72% of the danger colour on the dark theme's raised surface is below the AA ratio. The recessed surface says the same thing and leaves every foreground colour at full strength. Found in sprint 14, by axe, once enough expired orders had accumulated for one to appear on the list | S | [06](06_ui_ux.md) |
| ✅ 14.1.2 | Hovering a primary or danger button replaced its background with the sunken surface while the label kept the colour chosen to sit on the accent: near-black on near-black in the dark theme, white on light grey in the light one. `.button:hover` outranks `.button-primary` three selectors to one. The label now turns the colour the button just was, which keeps 9.2:1 and 4.9:1 for the accent and 6.9:1 and 5.5:1 for the danger colour. axe never saw it, because it scans a page at rest and nothing is hovered at rest. Reported by the user in sprint 14 review | S | [06](06_ui_ux.md) |
| ✅ 14.2 | Keyboard-only operation of every control, including category reordering and every modal          | L | [06](06_ui_ux.md) |
| ✅ 14.2.1 | Roughly one navigation in sixty, Firefox never completes `page.goto`: the server answers in under a millisecond and the failure report shows the page fully rendered behind the stalled navigation. Not the per-host connection limit and not the `load` event -- `domcontentloaded` stalls identically -- so it is a browser-level stall, answered with one retry and a 20 s navigation timeout. Found in sprint 14, by CI | S | [12](12_testing.md) |
| ✅ 14.3 | `axe-core` in the Playwright run across every page in both themes                                | M | [12](12_testing.md) |
| ✅ 14.3.1 | `@axe-core/playwright` was installed on the test VM and never added to `package.json`, so the type check passed locally and would have failed in CI. The same mistake as `@playwright/test` in sprint 12. Found in sprint 14 | S | [12](12_testing.md) |
| ✅ 14.4 | `update` verb: the pre-release message, the config rewrite machinery, migration application       | L | [09](09_configuration.md) |
| ✅ 14.5 | Performance check against the targets, and the concurrency headroom check                         | M | [11](11_nonfunctional.md) |
| ✅ 14.6 | Security review: CSP in practice, upload handling, an audit that every query is parameterized      | L | [11](11_nonfunctional.md) |
| ✅ 14.6.1 | `POST /shutdown` checked nothing at all. It was written in sprint 3 with a comment promising that sprint 5 would wrap it in the administrator check; sprint 5 wrapped every other route and not this one. Anonymous requests are deliberately exempt from the CSRF check, so for nine sprints any unauthenticated caller on the network could stop the server. Found in sprint 14, by writing the matrix row for it | S | [05](05_auth_and_permissions.md) |
| ✅ 14.6.2 | Registering while already logged in was accepted, and replaced the caller's session with one for the new account. The matrix has always said registration is anonymous only. Refused with the new error 2006. Found in sprint 14 | S | [05](05_auth_and_permissions.md) |
| ✅ 14.7 | Close the coverage gaps against the targets                                                        | M | [12](12_testing.md) |
| ✅ 14.7.1 | `make cover` ran without `-coverpkg`, so a package was credited only for what its own tests executed. `internal/db` reported 22.6% while the API integration tests were exercising three quarters of it, and the overall figure read 60.8% against a 75% target that was in fact already met. Found in sprint 14 | S | [12](12_testing.md) |
| ✅ 14.7.2 | Twenty-one rows of the permission matrix had never been exercised. They were listed in a `pendingRows` map naming the sprint that would deliver each; those sprints landed, the map did not change, and nothing read it. The matrix rows now come from the document itself | M | [05](05_auth_and_permissions.md) |
| ✅ 14.7.3 | `GET /health`, `GET /metrics` and `PATCH /restaurants/{id}/categories/{cid}` were registered, documented and reached by no test. Found by counting which routes the suite actually serves rather than by counting operations in the document | S | [12](12_testing.md) |
| ✅ 14.8 | Finish the OpenAPI document. It describes the four system endpoints and nothing else, while [04](04_api.md) promises it describes the API and [12](12_testing.md) promises a test that fails when a registered route has no operation. That test currently checks a hardcoded list of four paths, so both promises are unkept. Found in sprint 13, which added two more routes it could not honestly document | L | [04](04_api.md) |
| ✅ 14.9 | Design changes from the first look at the running application. The list below is one round of review by the user against the deployed build | L | [06](06_ui_ux.md) |
| ✅ 14.9.1 | The real mark in the title bar. Two files were supplied differing only in the frame stroke and the calendar text; they are one asset with those in `currentColor`, inlined rather than referenced so an `<img>`'s separate document cannot swallow the inheritance. The same asset is the tile placeholder and the watermark. Drawn once into the document as a `<symbol>` and referenced with `<use>`: the first version cloned all fifty-seven of its elements per copy, which put seven thousand SVG nodes on an overview of a hundred and twenty-five orders and stopped axe-core finishing its walk of the document inside a minute in Firefox | M | [06](06_ui_ux.md) |
| ✅ 14.9.2 | The plus on the add tile sized as a share of the card rather than in fixed units, so it stays about three quarters of the tile from one column to four | S | [06](06_ui_ux.md) |
| ✅ 14.9.3 | The mark behind both overviews, at a few percent opacity against the viewport height. Against the page height it would have been several screens tall on a list of a hundred orders | S | [06](06_ui_ux.md) |
| ✅ 14.9.4 | The summary button on an order tile was rendered in a footer *outside* the card, which the grid then drew the next row over: a button half-hidden behind a tile. A tile was one big anchor, so a control could not live inside it -- a link inside a link is not something a browser can make sense of. The tile is now a card whose title is the link, stretched over it, which is what lets the summary, the pencil and the bin sit inside | M | [06](06_ui_ux.md) |
| ✅ 14.9.5 | The item count, the participant count, the total and the creator are off the tiles: a grid is for choosing between orders, not for reading them | S | [06](06_ui_ux.md) |
| ✅ 14.9.6 | A restaurant with no logo gets the mark rather than an empty dashed box, on both overviews | S | [06](06_ui_ux.md) |
| ✅ 14.9.7 | The restaurant page is four tabs rather than four stacked cards, with the menu first: it is what somebody almost always came for and it used to be the section they scrolled past three others to reach. The component uses the classes the account page already had, so there is one tab look rather than two | M | [06](06_ui_ux.md) |
| ✅ 14.9.8 | Save and Delete moved to the restaurant title's line, aligned with the card below. Save is disabled until the form differs from what was loaded, compared against a snapshot so that typing a character and deleting it leaves it disabled; Delete is disabled with a tooltip naming which of its two obstacles applies | M | [06](06_ui_ux.md) |
| ✅ 14.9.9 | The opening hours are compact: each control sized to its own content instead of sharing the row, and the "crosses midnight" note keeping its column whether or not it has anything to say, so typing a time no longer shoves the remove button sideways. That button is red now, like every other remove in the application | S | [06](06_ui_ux.md) |
| ✅ 14.10 | A second round of design changes from the running application | L | [06](06_ui_ux.md) |
| ✅ 14.10.1 | The summary moved off the order's first card onto the title's line, right-aligned and primary: it opens a different page rather than doing something to this one, and it is the control somebody arrives wanting | S | [06](06_ui_ux.md) |
| ✅ 14.10.2 | Each tab panel repeated its tab's name as a heading inside itself. A `card` without a heading, since the tab already names the panel and the tabs component labels it from there | S | [06](06_ui_ux.md) |
| ✅ 14.10.3 | Contact rows are one grid with fixed columns, so the new row's fields are the width of the ones above it. They were flex rows sharing their leftover space, and the new row has one button where the others have two, which was enough to make every field a different size. The type dropdown is sized to its own widest option | S | [06](06_ui_ux.md) |
| ✅ 14.10.4 | A button behind each contact's value that opens what the contact is: `tel:` for a telephone, a mobile or a fax, `mailto:` for e-mail, the site for a website, a Maps query for an address. The icon says which kind before it is pressed. "Other" is a free string with nothing sensible to open, so it gets no button but keeps its column | M | [06](06_ui_ux.md) |
| ✅ 14.10.5 | The remove button on an opening-hours row sits directly after the "crosses midnight" note rather than at the far end of the row | S | [06](06_ui_ux.md) |
| ✅ 14.10.6 | Renaming a restaurant changed the browser tab and left the heading above the form showing the old name until a reload, which reads as a save that did not take | S | [06](06_ui_ux.md) |
| ✅ 14.10.7 | "Add an item" and "Add a category" at the top of the menu tab, both primary. The category form that lived at the bottom is a dialog: one field and a button is a dialog's worth of interface, not a permanent row, and a control below two hundred items is a control nobody finds | M | [06](06_ui_ux.md) |
| ✅ 14.10.8 | A menu category is a heading with a disclosure rather than a text box, and its Save is an Edit that opens the rename dialog. A page of input fields reads as a form to fill in. Which categories are folded is remembered across the reloads a rename or a reorder triggers | M | [06](06_ui_ux.md) |
| ✅ 14.10.9 | The menu tab now has an Edit button on every category and every item, so the visible word is no longer a name on its own. `button()` gained an `ariaLabel` and the category's names the category: a dozen controls all called "Edit" is ambiguous to a screen reader and unusable by voice. Found while fixing the test that had been finding the item's Edit button by its text | S | [06](06_ui_ux.md) |
| ✅ 14.10.10 | Two browser tests drove the add-category form that became a dialog, and one read category names out of input fields that are headings now. Rewritten for the dialog. And the axe scans were failing on the clock rather than on a violation: a scan is proportional to the size of the document, the order overview draws a tile per order, and a development database that has been in use for a while has hundreds of them. They have a long timeout of their own now -- an accessibility test has no business also being a performance assertion | S | [12](12_testing.md) |

**Exit criteria:** no serious or critical axe violations. The application is
fully operable by keyboard. The coverage targets are met, in particular 90% on
`internal/auth` and every permission matrix cell exercised.

---

## Sprint 15 — Packaging and release

**Goal:** version 1.0.0, installable four ways.

| ID   | Task                                                                                    | Size | Spec |
| ---- | ----------------------------------------------------------------------------------------- | :--: | ---- |
| 15.1 | `nfpm` configuration producing the `.deb` and the `.rpm` from one description               | L | [10](10_operations.md) |
| 15.2 | Package contents: unit file, logrotate rule, cron entry, docs, service user, config marking  | M | [10](10_operations.md) |
| 15.3 | Package install, upgrade and remove tests in throwaway containers                             | M | [12](12_testing.md) |
| 15.4 | Two-stage `Dockerfile` ending in `scratch`, for amd64 and arm64                               | M | [10](10_operations.md) |
| 15.5 | `docker compose` example, verified against the documented first-run sequence                   | M | [10](10_operations.md) |
| 15.6 | `make licenses`, `THIRD_PARTY_LICENSES`, the `go-licenses check` allow-list, the font licence   | M | [08](08_technologies.md) |
| 15.7 | Release CI: a tag builds all four artefacts and runs the package and image checks                | M | [12](12_testing.md) |
| 15.8 | Documentation review against what was actually built; correct every drift                        | M | — |
| 15.9 | Tag 1.0.0                                                                                        | S | — |

**Exit criteria:** installing the `.deb` on Ubuntu, running `doenerstag install`
and starting the service produces a working application. The container image runs
the same way. `THIRD_PARTY_LICENSES` is complete and CI fails if a dependency is
added without its licence.

---

## Deliberately not in this plan

These are specified as out of scope in [01_overview.md](01_overview.md) and are
listed here so their absence reads as a decision rather than an oversight:
payment handling, deadline notifications, search and filtering of restaurants and
orders, sharing links, multi-instance deployment, and translations beyond English
and German.

Two items are deferred rather than excluded, and both are additive when they come:

- **Modification groups** — mutually exclusive option sets. Adding them later is
  a nullable `group_id` plus one table
  ([adr/0007](adr/0007-flat-modification-list.md)).
- **Client-side password pre-hashing** as defence in depth on top of the
  server-side Argon2id ([adr/0005](adr/0005-server-side-argon2id.md)). No schema
  change required.

## Risks worth tracking

| Risk                                                                                  | Sprint | Mitigation                                                        |
| ------------------------------------------------------------------------------------- | ------ | ----------------------------------------------------------------- |
| Nothing user-visible until Sprint 12                                                   | 1–11   | Swagger UI from Sprint 4; resequence into vertical slices if this bites. |
| Frontend build pipeline surprises discovered late                                       | 4      | The minimal build is pulled forward into Sprint 4 for this reason. |
| The role and grant work in 3.3 varies with how the target PostgreSQL is administered    | 3      | Test against a stock PostgreSQL 18 early; the privileged identities are separated so a restricted environment can be accommodated. |
| SSE behind an unknown intranet proxy                                                    | 9      | The `ping` event and the documented proxy requirement; test against the real deployment before 1.0. |
| The permission matrix is the most likely place for a serious defect                     | 5, 8   | One test per matrix cell, plus the anonymous response body scan.   |
