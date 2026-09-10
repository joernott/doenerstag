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
| Sprint 15 | Install version 0.1.0 from a `.deb`, an `.rpm` or a container image.         |

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
| ✅ 14.11 | A third round of design changes from the running application | L | [06](06_ui_ux.md) |
| ✅ 14.11.1 | The menu item's Edit is primary, the category's controls are right-aligned and the heading and its buttons sit in a frame of their own: a menu is a list of lists, and without a border the category headings read as items in the same stream as the dishes under them | S | [06](06_ui_ux.md) |
| ✅ 14.11.2 | "Remove the picture" is red and "Replace picture" primary, like every other pair of that shape | S | [06](06_ui_ux.md) |
| ✅ 14.11.3 | The contact Save buttons and "Add a contact" are primary, and the buttons fill their grid column so that the single Add lines up with the Save and Remove pair above it. Left to their natural widths the pair fell 32px short of the column while the single button did not | S | [06](06_ui_ux.md) |
| ✅ 14.11.4 | The opening hours lost their own Save: the page heading's Save now writes the restaurant and its hours, whichever of the two has changed. Two buttons called Save on one page, each covering a different part of it, is a question nobody should have to answer | M | [06](06_ui_ux.md) |
| ✅ 14.11.5 | The "below the minimum" warning had less space above it than it has inside it, so the total read as part of the warning | S | [06](06_ui_ux.md) |
| ✅ 14.11.6 | The summary is always offered, disabled with error 3004's own text when the viewer is not one of the people it is for. A control that vanishes leaves somebody wondering whether the feature exists; the disabled form is a button rather than a link, because a link that goes nowhere is not a link | S | [06](06_ui_ux.md) |
| ✅ 14.11.7 | The account page is the restaurant page's shape: tabs, with Save and Delete on the title's line. One Save covers the profile and the password, writing whichever changed. The deletion card is gone -- the warning it printed permanently is in the dialog it opens, where it applies | L | [06](06_ui_ux.md) |
| ✅ 14.11.8 | The login and register tabs use the shared component. That page built its own strip with the same classes, a hand-written selection handler and arrow keys that moved without roving the tabindex; it was the older and weaker of the two implementations, and the one flagged as a loose end when the component was written | S | [06](06_ui_ux.md) |
| ✅ 14.11.9 | The version page has the mark behind it and its facts in a card | S | [06](06_ui_ux.md) |
| ✅ 14.11.10 | The API documentation opens in a new tab: it is Swagger UI rather than a page of this application, and following it in place loses whatever the reader was doing | S | [06](06_ui_ux.md) |
| ✅ 14.12 | The order page's controls, and the deadline that could be created in the past | M | [02](02_features.md) |
| ✅ 14.12.1 | The summary stayed locked however many items you added: the heading was built once, so participation was decided once, at load. It is rebuilt on every refresh -- which already runs on every item change and every event -- so adding your first item unlocks it and removing your last locks it again. Reported from the running application | S | [06](06_ui_ux.md) |
| ✅ 14.12.2 | Edit and Delete joined the summary on the title's line. All three act on the order as a whole, and they were the only reason the first card had a row of buttons | S | [06](06_ui_ux.md) |
| ✅ 14.12.3 | An order could be created with a deadline that had already passed, which makes it useless the moment it exists: F6.6 makes an expired order read-only, so nobody can add an item to it and its creator cannot edit it back into life. Refused with the new error 1014, on creation only -- moving an existing order's deadline into the past is how a creator closes one early and stays allowed. Reported with a real order whose deadline was eleven hours gone | M | [02](02_features.md) |
| ✅ 14.12.4 | That order came from the form's own defaults: today at 11:00 and 12:00, a lunch order on the assumption of a morning. Opened at ten in the evening it offered a deadline eleven hours in the past. An hour and two hours from now is right whatever the time is | S | [06](06_ui_ux.md) |
| ✅ 14.12.5 | The summary page's "below the minimum" box got the same spacing as the order page's | S | [06](06_ui_ux.md) |

**Exit criteria:** no serious or critical axe violations. The application is
fully operable by keyboard. The coverage targets are met, in particular 90% on
`internal/auth` and every permission matrix cell exercised.

---

## Sprint 15 — Packaging and release

**Goal:** version 0.1.0, installable four ways.

| ID   | Task                                                                                    | Size | Spec |
| ---- | ----------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 15.1 | `nfpm` configuration producing the `.deb` and the `.rpm` from one description               | L | [10](10_operations.md) |
| ✅ 15.2 | Package contents: unit file, logrotate rule, cron entry, docs, service user, config marking  | M | [10](10_operations.md) |
| ✅ 15.3 | Package install, upgrade and remove tests in throwaway containers                             | M | [12](12_testing.md) |
| ✅ 15.3.1 | The `.rpm` could not be installed at all. `depends: postgresql-client` is a Debian package name and nothing on an RPM distribution provides it, so `dnf install` refused the package outright. The dependency is now declared per format. Found in sprint 15, by running the test that had been written and never executed | S | [10](10_operations.md) |
| ✅ 15.3.2 | The test asserted a removal behaviour neither format has: `dpkg` removes `/var/log/doenerstag` when it is empty, and `rpm` renames a modified configuration file to `.rpmsave` on erase. Both are correct behaviour, and one assertion covering both would be either wrong or too weak to be worth running. Found in sprint 15 | S | [12](12_testing.md) |
| ✅ 15.4 | Three-stage `Dockerfile` ending in `scratch`, for amd64 and arm64                               | M | [10](10_operations.md) |
| ✅ 15.5 | `docker compose` example, verified against the documented first-run sequence                   | M | [10](10_operations.md) |
| ✅ 15.5.1 | The documented compose stack could not start. It preset `POSTGRES_DB` and `POSTGRES_USER`, handing `install` a database and a role it exists to create, and mounted the volume at `/var/lib/postgresql/data`, which the PostgreSQL 18 image refuses because it keeps the cluster in a version-named subdirectory. It is now `packaging/docker-compose.yml`, a file the release is tested with rather than a listing in a document. Found in sprint 15, by running it | S | [10](10_operations.md) |
| ✅ 15.5.2 | `install` wrote `tls_cert: ""` over the declared default `server.crt`, so the server refused to start immediately after an install that had just printed the command to start it. Not a container problem: the package flow had it too. Any setting whose default was non-empty was written empty. Found in sprint 15 | S | [09](09_configuration.md) |
| ✅ 15.5.3 | `update` refused every version below 1.0.0, a rule written when the first release was assumed to be 1.0.0. With 0.1.0 that is the whole 0.x line, and the message told the operator to run `install` instead, on a database whose roles `install` would then try to create a second time. The gate is now "this binary reports 0.0.0". Found in sprint 15 | S | [09](09_configuration.md) |
| ✅ 15.5.4 | `update` runs under the install scope, and configuration was resolved per scope, so the file it rewrote carried the declared default for every setting the verb had no flags for. A tuned port and timeouts reverted silently and `session.jwt_secret` was written empty, leaving a configuration the server refuses to start from at all. Scope now decides which flags exist, not which settings are resolved -- which also makes `install`'s "keep the existing secret so a re-run does not log everyone out" branch reachable for the first time. Found in sprint 15 | M | [09](09_configuration.md) |
| ✅ 15.6 | `make licenses`, `THIRD_PARTY_LICENSES`, the `go-licenses check` allow-list, the font licence   | M | [08](08_technologies.md) |
| ✅ 15.6.1 | `scripts/licenses.sh` was committed `100644` from Windows, so CI ran it and got exit code 126: found, not executable. It works on the test VM only because that checkout is a shared folder reporting every file as `0770`. The release workflow invokes it the same way, so this would have failed the release rather than a pull request. Found in sprint 15, by the licence job on its first run | S | [08](08_technologies.md) |
| ✅ 15.7 | Release CI: a tag builds all four artefacts and runs the package and image checks                | M | [12](12_testing.md) |
| ✅ 15.7.1 | The image build passed `VERSION` and the commit but not `BUILD_DATE`, so the container would have reported "built unknown" while the packages from the same tag carried a real timestamp. All three stamps now come from `make version`. Found in sprint 15 | S | [12](12_testing.md) |
| ✅ 15.8 | Documentation review against what was actually built; correct every drift                        | M | — |
| ✅ 15.8.1 | `10_operations.md` showed the systemd unit with `ExecStart=/usr/local/bin/doenerstag` while claiming the packages install exactly that file; they install `/usr/bin/doenerstag`. Found in sprint 15 | S | [10](10_operations.md) |
| ✅ 15.8.2 | `README.md` said "Specification only. No code yet." -- the first thing anyone reaching the release would read, describing a repository that stopped existing thirteen sprints earlier. Found in sprint 15 | S | — |
| ✅ 15.8.3 | `08_technologies.md` documented a `make migrate` that does not exist, described `make packages` and `make image` as doing things they do not, and called the image a two-stage build. Found in sprint 15 | S | [08](08_technologies.md) |
| ✅ 15.8.4 | `12_testing.md` promised a `go-licenses` check on every push. There was none: the check existed only in the release workflow, so a dependency added without its licence would have been caught after the artefacts were built. It is now a CI job. Found in sprint 15 | S | [12](12_testing.md) |
| ✅ 15.8.5 | `make archives` produced a `.tar.gz` whose single file was named `doenerstag-linux-amd64`. Extracting a release should give you the command you are about to run. Found in sprint 15 | S | [10](10_operations.md) |
| ✅ 15.12 | CI builds the packages on every push and installs each one in its own distribution container, using a job `container:` rather than a nested `docker run` | M | [12](12_testing.md) |
| ✅ 15.13 | CI builds the container image on every push and runs it: `--version`, then `install` and `server` against a PostgreSQL service, answering over HTTPS | M | [12](12_testing.md) |
| 15.9 | Tag 0.1.0. Not 1.0.0: this is the first iteration, it provides the minimal functionality and has had barely any use | S | — |
| 15.10 | Publish the container image to `docker.io/joernott/doenerstag`, as `0.1.0` and `latest`. The credentials are repository secrets; nothing about them is committed | M | [10](10_operations.md) |
| 15.11 | A GitHub release for 0.1.0 carrying the Windows executable in a `.zip`, the Linux executable in a `.tar.gz`, the `.deb`, the `.rpm` and the `Dockerfile` | M | [10](10_operations.md) |

**Exit criteria:** installing the `.deb` on Ubuntu, running `doenerstag install`
and starting the service produces a working application. The container image
runs the same way. Both are proved on every push rather than asserted:
[`ci.yml`](../.github/workflows/ci.yml) installs each package in its own
distribution and starts the image against a real database.
`THIRD_PARTY_LICENSES` is complete and CI fails if a dependency is added
without its licence. Tag 0.1.0 exists, the GitHub release carries all five
artefacts, and `docker.io/joernott/doenerstag:0.1.0` can be pulled.

---

## Sprint 16 — Bug fixes and minor improvements

**Goal:** fix what real use finds. The first release shipped with barely any of
that, so this sprint is driven by what breaks in front of somebody rather than
by a plan written in advance.

| ID   | Task                                                                                    | Size | Spec |
| ---- | ----------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 16.1 | A stale session cookie makes the whole application unreachable: every request, including the page itself and every public endpoint, answers 401 with a JSON error envelope. The browser shows raw JSON and there is no way out of it short of clearing cookies by hand. Reported by the user after leaving a session open overnight | M | [05](05_auth_and_permissions.md) |
| ✅ 16.2 | `mokapi` in `contrib/setup_dev_pipeline.sh` and on the VM, configured as a Mail server (SMTP and IMAP) and an LDAP server, so the mail path can be tested against something that behaves like the real thing | M | [12](12_testing.md) |
| ✅ 16.3 | Outgoing mail: SMTP settings, a sender the application can use, and the operational questions that come with it — what happens when the mail server is down, and what is never put in a message | M | [09](09_configuration.md) |
| ✅ 16.4 | "Forgot password?" behind the login button. A reset identifier held in memory for one hour, a mail carrying a link to a reset page, and a redirect to the login page once the new password is set | L | [05](05_auth_and_permissions.md) |
| ✅ 16.5 | The `user` verb: `list`, `add` (generating a 20-character password and printing it), `delete` and `password` (either setting one interactively or issuing a reset link) | L | [09](09_configuration.md) |
| ✅ 16.6 | A "Users" entry for the administrator: every user with edit, reset-password and delete, the reset doing exactly what the login page's link does | M | [06](06_ui_ux.md) |
| ✅ 16.4.1 | A reset link 404ed instead of loading the application. The token used a dot between its payload and its signature, and 16.1's SPA fallback treats a final path segment containing a dot as a request for a file. Every reset link in every mail would have landed on a JSON error envelope. Found in sprint 16, by walking the journey in a browser -- no unit test on either side could have seen it, because each half was behaving as designed | S | [05](05_auth_and_permissions.md) |
| ✅ 16.7 | The `restaurant` verb: `list`, `delete`, `export` (one, several or `--all`, as YAML or JSON) and `import` (format guessed from the content, `--overwrite` replacing an existing id) | L | [09](09_configuration.md) |
| ✅ 16.7.1 | `restaurant export` wrote the document to standard output and the logger wrote to standard output, so "database connected" was the first line of every exported file and none of them parsed. The log goes to standard error for a verb whose output is data. Found in sprint 16, by exporting a restaurant and reading the file | S | [09](09_configuration.md) |
| ✅ 16.7.2 | `restaurant import --file -f` and `restaurant delete --force -f` both claimed a shorthand that `--log-file` owns globally. pflag refuses that by panicking when the flags are merged, which is when somebody runs the command or asks for help -- so both subcommands were unusable and nothing caught it. Both are long-form only now, and a test walks the tree forcing the merge. Found in sprint 16, by running the command | S | [09](09_configuration.md) |
| ✅ 16.7.3 | An imported contact without a label violated a check constraint: the column is nullable and refuses the empty string, so an absent label has to arrive as NULL rather than as "". Found in sprint 16, by a round trip | S | [03](03_data_model.md) |
| ✅ 16.8 | `--log-file` moves to `-L`, which gives `-f` back to `restaurant import --file` and `restaurant delete --force` | S | [09](09_configuration.md) |
| ✅ 16.9 | The `order` verb: `list` with `--verbose`, and `delete` | M | [09](09_configuration.md) |
| ✅ 16.10 | A reference page for every verb and every flag, so `--help` is not the only place they are written down | M | — |
| ✅ 16.11 | The browser tests clear the orders, restaurants and accounts left by previous runs before they start. The development database had accumulated 500 restaurants, 78 orders and 792 accounts | S | [12](12_testing.md) |
| ✅ 16.11.1 | That cleanup matched test data by the shape of its name -- a word, an underscore, a timestamp -- and deleted an account the user had created that happened to look like one. Every name the fixtures invent now carries an `e2e-` prefix and only that prefix is deleted. Reported by the user, after it had already happened | S | [12](12_testing.md) |
| ✅ 16.12 | A Cleanup button on the order overview, on the heading line and only for the administrator | M | [06](06_ui_ux.md) |
| ✅ 16.13 | A check that the packages install the binary to `/usr/bin` and to nowhere else. They already did; what was in `/usr/local/bin` on the development VM was a hand-installed build, which is the confusion the assertion now prevents | S | [10](10_operations.md) |
| ✅ 16.14 | `contrib/ali_baba.json`: a real menu, transcribed from four photographs, as an import file. The allergen and additive letters had to be translated rather than copied, because the restaurant's numbering and doenerstag's do not agree | M | [09](09_configuration.md) |
| ✅ 16.15 | The order page folded its menu categories with a plus and a cross; the restaurant page's Menu tab uses a disclosure chevron. The order page now uses the chevron too -- on a page where every other plus adds an item and every cross removes one, those two symbols were saying the wrong thing | S | [06](06_ui_ux.md) |
| ✅ 16.16 | A login link carries the page it was pressed on, so logging in returns there. Registering still ends on the account page, which is where a new account has a display name and an address to fill in. The return path is validated as a path on this site, because a login page that navigates wherever a query parameter says is an open redirect | M | [06](06_ui_ux.md) |
| ✅ 16.16.1 | Four page tests and a browser test asserted the login link's address exactly, as `/account`, and 16.16 gave it a return parameter. They were written before there was one, and running the new test rather than the whole suite is what let them reach CI broken. They now put the page at its own address and assert the link that belongs there | S | [12](12_testing.md) |
| ✅ 16.17 | `contrib/ali_baba.json` re-exported after the user filled in the contacts, the opening hours and the notes, and the four photographs removed now that the data is in the file | S | — |
| ✅ 16.18 | `install` appeared to ignore `DOENER_DATABASE_ROOT_PASSWORD` under `docker compose`. It did not: an ordinary question shows its default in brackets, a secret question showed nothing at all, so a prompt for a value already supplied was indistinguishable from one being ignored. Secret prompts now name the variable the value came from. Reported by the user | S | [09](09_configuration.md) |
| ✅ 16.19 | Version references moved to 0.2.0, and the release notes in the workflow stopped calling every release "the first iteration" | S | — |
| ✅ 16.20 | Sprint 16 squashed into `main` and released as 0.2.0 | S | [10](10_operations.md) |

**Exit criteria:** a browser holding a session the server no longer knows about
loads the application, is told once that it was logged out, and can log in
again without clearing anything by hand. Somebody who has forgotten their
password can set a new one from a mail the application sent, and an
administrator can do the same for them from either the command line or the
browser. A restaurant can be carried from one installation to another as a file.
Sprint 16 is on `main` and released as 0.2.0.

---

## Sprint 17 — What a real order found

**Goal:** the first real order was placed at a real restaurant, and it found
things a test never would: an address printed twice, a price hidden behind a
button that changed width with every dish, no way to say who is fetching the
food except by typing a name, and a menu that offers pasta on a Tuesday when
the kitchen only makes it at the weekend. Four of these are data model changes,
so this sprint is the first since 3 to touch the schema in earnest.

| ID   | Task                                                                                    | Size | Spec |
| ---- | ----------------------------------------------------------------------------------------- | :--: | ---- |
| ✅ 17.1 | The password rules under the field list "three of five groups" and then six bullet points, the sixth repeating the ten-character minimum stated above it. Remove it | S | [06](06_ui_ux.md) |
| ✅ 17.2 | A `x/10` counter beside the password field, counting what has been typed and turning green at ten | S | [06](06_ui_ux.md) |
| ✅ 17.3 | A rule that is satisfied already swaps its bullet for a tick; the tick is green as well | S | [06](06_ui_ux.md) |
| ✅ 17.4 | The order page prints a contact address twice when it has no label. An address without a label is the value, shown once | S | [06](06_ui_ux.md) |
| ✅ 17.5 | The summary page prints no address at all. Whoever is collecting the food needs it more than anybody | S | [06](06_ui_ux.md) |
| ✅ 17.4.1 | The order page decided for itself what a contact opens rather than asking contacts.ts, and had drifted: no case for an address at all, spaces left in a `tel:` URI, a website without a scheme becoming a relative link. It asks now, which is what fixed 17.4. The summary page's telephone link had the same spaces and now goes through the same function | S | [06](06_ui_ux.md) |
| ✅ 17.6 | The "Add an item" button changes width with the number of tags, allergens and additives on the dish beside it, so the column of buttons is ragged. One line, always; the price moves above the button and is set bold | M | [06](06_ui_ux.md) |
| ✅ 17.7 | "Fetches the food" and "collects the money" become references to an account rather than free text, chosen on the edit-order page from a list that can be searched | L | [03](03_data_model.md) |
| ✅ 17.8 | When nobody is fetching the food, the order page offers every signed-in visitor a "Me!" button that puts them there | M | [06](06_ui_ux.md) |
| ✅ 17.7.1 | Both people were on the anonymous order shape, as free text. An account is a named person, so ADR-0011 puts them where the creator already is: on the authenticated shape. `GET /users` gained a second tier for the same reason -- an order can only name accounts if there is a list to pick from -- and gives a signed-in caller the public profile it already serves per user, the administrative shape staying administrative | M | [05](05_auth_and_permissions.md) |
| ✅ 17.7.2 | The account list offered the deleted-user placeholder as somebody who might fetch the food. It is a row in `app_user` so that an old order still reads "1x Döner, no onions" without naming anybody, and it is not a person: the form leaves it out and the API refuses it. Found by looking at the dropdown on the development installation | S | [03](03_data_model.md) |
| ✅ 17.9 | A currency needs a minor-unit ratio as well as a number of decimals. The Malagasy ariary and the Mauritanian ouguiya divide into five, not ten, so one decimal place is the wrong way to say it: ten iraimbilanja are two ariary, and arithmetic that assumes powers of ten makes them one | M | [03](03_data_model.md) |
| ✅ 17.10 | A "paid" flag on an order item, which the person who added it, the person who opened the order and the person collecting the money may set. A checkbox beside each price on the summary page, and a paid item is left out of that person's total | L | [03](03_data_model.md) |
| ⬜ 17.11 | Availability: a named, reusable filter combining a date, a weekday and a time of day, tested against the order's pickup or delivery time. Attached to a category or to a single item; several on one element are alternatives, and a category's and an item's are both required. The restaurant page shows the whole menu, the order page only what can be had | L | [03](03_data_model.md) |
| ✅ 17.12 | `update` and `import` as compose services of their own, beside `install`. Both behind profiles, `update` mounting the configuration writable because it rewrites it | S | [10](10_operations.md) |
| ✅ 17.13 | A second compose file putting Traefik in front, with Let's Encrypt certificates, and the application serving plain HTTP behind it | M | [10](10_operations.md) |
| ✅ 17.13.1 | `ValuesFrom` builds the configuration file from a hand-written flag-to-value map, and a loop after it fills in whatever is missing with the declared default. A setting left out of the map is therefore invisible: `DOENER_BEHIND_TLS_PROXY=true` produced a file saying `false`, which for that setting means session cookies stop being marked Secure. Found by running the stack and reading the file it wrote. A test now flips every boolean in the struct and asks for it back | S | [09](09_configuration.md) |
| ✅ 17.14 | An administrative verb run by a person exited FATAL because the configuration file names the server's log and only the service user may write it: `open /var/log/doenerstag/doenerstag.log: permission denied`, before doing anything at all. The verbs a person runs now warn and log to standard error instead. `server` stays fatal: a daemon that cannot write its log would run for months with nobody noticing. Reported by the user | S | [09](09_configuration.md) |

**Exit criteria:** an order at a real restaurant can be placed, fetched, paid
for and settled without anybody typing a name into a text field or working out
which dishes the kitchen is making today. The password field says how far along
the person typing is rather than repeating itself. A currency whose minor unit
is not a tenth of its major one is stored correctly.

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
