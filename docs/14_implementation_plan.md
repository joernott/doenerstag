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
| 1.1 | `go mod init`, repository layout, `.gitignore`, `.editorconfig`, `golangci-lint` config        | S | [08](08_technologies.md) |
| 1.2 | `Makefile` with `deps`, `build`, `test`, `lint`; frontend targets stubbed                       | S | [08](08_technologies.md) |
| 1.3 | cobra command tree: root plus `server`, `install`, `update`, `cleanup`, `version` as stubs      | S | [09](09_configuration.md) |
| 1.4 | viper wiring: defaults → config file → `DOENER_*` → flags, with the nested-key mapping           | M | [09](09_configuration.md) |
| 1.5 | Global flags; `--version` with the build-time version injected via `-ldflags`                   | S | [09](09_configuration.md) |
| 1.6 | Secret-on-the-command-line rule: FATAL for each of the four settings                            | S | [09](09_configuration.md) |
| 1.7 | Config file permission check: `0600`/`0400` pass, anything else FATAL, skipped on Windows        | S | [09](09_configuration.md) |
| 1.8 | zerolog: five levels, JSON output, `--log-file`, `SIGHUP` reopen, standard fields                | M | [08](08_technologies.md) |
| 1.9 | Redaction deny-list for the DEBUG parameter logging                                             | S | [08](08_technologies.md) |
| 1.10| Unit tests: precedence, secret rule, permission check, redaction, level mapping                  | M | [12](12_testing.md) |
| 1.11| CI pipeline: `lint`, `vet`, `test`, `govulncheck`                                                | M | [12](12_testing.md) |

**Exit criteria:** `doenerstag --version` prints the version. Every verb runs and
exits with a clear "not implemented". Passing a password on the command line is
fatal. A `0644` config file is fatal. CI is green.

---

## Sprint 2 — Database schema and seed data

**Goal:** the complete schema exists as migrations, applies and rolls back
cleanly against PostgreSQL 18, and the seeded reference data is verified.

No application logic reads or writes it yet. Getting the schema right before
anything depends on it is the cheapest ordering there is.

| ID   | Task                                                                                       | Size | Spec |
| ---- | ------------------------------------------------------------------------------------------ | :--: | ---- |
| 2.1  | pgx pool: connection string assembly, `sslmode`, `--max-connection-pool`, UTC session         | M | [08](08_technologies.md) |
| 2.2  | golang-migrate integration with embedded migration files                                     | M | [08](08_technologies.md) |
| 2.3  | Audit column convention and the `updated_at` trigger                                          | S | [03](03_data_model.md) |
| 2.4  | Migration: `app_user`, `session`, `api_token`; deleted-user placeholder with its fixed UUID    | M | [03](03_data_model.md) |
| 2.5  | Migration: `currency`, `contact_type`, `tag`, `allergen`, `additive` seeds with source comments | M | [03](03_data_model.md) |
| 2.6  | Migration: `restaurant`, `restaurant_contact`, `opening_hours`                                | M | [03](03_data_model.md) |
| 2.7  | Migration: `menu_category`, `menu_item`, `menu_item_modification`, the three link tables       | M | [03](03_data_model.md) |
| 2.8  | Migration: `food_order`, `order_item`, `order_item_modification`                              | M | [03](03_data_model.md) |
| 2.9  | Migration: `image`, `content_page`, `app_version`                                             | S | [03](03_data_model.md) |
| 2.10 | Indexes, `CHECK` constraints, partial unique indexes, foreign key actions                     | M | [03](03_data_model.md) |
| 2.11 | Test harness: testcontainers-go, `DOENER_TEST_DATABASE_URL` fallback, clean skip when neither   | M | [12](12_testing.md) |
| 2.12 | Migration tests: up and down from empty and from each intermediate version                     | M | [12](12_testing.md) |
| 2.13 | Seed verification tests, asserting each `code` individually                                    | M | [12](12_testing.md) |
| 2.14 | Constraint tests: `deadline_at < fulfilment_at`, `quantity >= 1`, uniqueness, day-of-week range | M | [12](12_testing.md) |

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
| 3.1  | Interactive prompt framework, offering values from an existing config file as defaults         | M | [09](09_configuration.md) |
| 3.2  | Safe output handling: writability probe that cannot truncate the input file, temp file + rename | M | [09](09_configuration.md) |
| 3.3  | Root and admin identities: create database, create runtime role, grant DML only                 | L | [09](09_configuration.md) |
| 3.4  | Apply migrations using the admin identity                                                       | S | [10](10_operations.md) |
| 3.5  | Password package: Argon2id hashing in PHC format, and the three-of-five complexity rules with NFC | L | [05](05_auth_and_permissions.md) |
| 3.6  | Create the `root` administrator interactively                                                    | S | [05](05_auth_and_permissions.md) |
| 3.7  | Generate `jwt_secret` and write it to the config file                                            | S | [05](05_auth_and_permissions.md) |
| 3.8  | Load imprint and legal notes snippets from files, sanitize with bluemonday, insert                | M | [02](02_features.md) |
| 3.9  | Config file writer: every setting with its explanatory comment, mode `0600`                       | M | [09](09_configuration.md) |
| 3.10 | Append the `app_version` row                                                                     | S | [03](03_data_model.md) |
| 3.11 | `--non-interactive` mode                                                                         | S | [09](09_configuration.md) |
| 3.12 | `version` verb reading the newest `app_version` row                                               | S | [09](09_configuration.md) |
| 3.13 | Installer integration tests against a throwaway database                                          | L | [12](12_testing.md) |

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
| 4.1  | httprouter setup, `router.Handler` registration, `ParamsFromContext` helper                 | M | [08](08_technologies.md) |
| 4.2  | `NotFound` SPA fallback, JSON 404 under `/api/`, `MethodNotAllowed`, `OPTIONS`, `PanicHandler` | M | [08](08_technologies.md) |
| 4.3  | Router construction test proving no route conflicts                                          | S | [12](12_testing.md) |
| 4.4  | Middleware chain: request ID, INFO request logging, recovery, security headers                | M | [05](05_auth_and_permissions.md) |
| 4.5  | Error envelope and the error number registry                                                  | M | [04](04_api.md) |
| 4.6  | `internal/static` with the `embedstatic` build tag; `embed.go` and `embed_disabled.go`        | M | [adr/0002](adr/0002-static-assets-embed-toggle.md) |
| 4.7  | Minimal frontend build: esbuild, Tailwind, `index.html` skeleton; `make frontend`/`dev`/`release` | L | [08](08_technologies.md) |
| 4.8  | TLS listener, `--no-https`, bind address, HTTP timeouts, all five startup checks                | M | [09](09_configuration.md) |
| 4.9  | Graceful shutdown on `SIGTERM`/`SIGINT` with `--shutdown-grace`                                | M | [10](10_operations.md) |
| 4.10 | `/health`, `/metrics`, `/version` endpoints                                                    | M | [04](04_api.md) |
| 4.11 | OpenAPI document skeleton, Swagger UI serving, `--no-swagger`                                   | M | [04](04_api.md) |
| 4.12 | CORS handling for `--cors-allowed-origins`                                                      | S | [05](05_auth_and_permissions.md) |
| 4.13 | API tests for all of the above, including the security header assertions                        | M | [12](12_testing.md) |

**Exit criteria:** `doenerstag server` serves HTTPS. `/health` returns 200 with
the database up and 503 without it. Assets serve from disk in a default build and
from the binary in a `-tags embedstatic` build. Swagger UI loads. Both builds are
exercised in CI.

---

## Sprint 5 — Accounts, sessions, authorization

**Goal:** registration, login, sessions, tokens and the complete permission model.

| ID   | Task                                                                                    | Size | Spec |
| ---- | ----------------------------------------------------------------------------------------- | :--: | ---- |
| 5.1  | `POST /auth/register` with the complexity rules from 3.5                                    | M | [05](05_auth_and_permissions.md) |
| 5.2  | `POST /auth/login`: session row, JWT issue, single-session replacement, both cookies          | L | [adr/0004](adr/0004-jwt-with-server-side-sessions.md) |
| 5.3  | Session validation middleware: signature, `exp`, session row, idle and absolute timeouts, `last_seen_at` throttle | L | [05](05_auth_and_permissions.md) |
| 5.4  | `POST /auth/logout` and `GET /auth/session`                                                  | S | [04](04_api.md) |
| 5.5  | CSRF double-submit enforcement with constant-time comparison                                  | M | [05](05_auth_and_permissions.md) |
| 5.6  | Login rate limiting per user name and per client address                                      | M | [05](05_auth_and_permissions.md) |
| 5.7  | API tokens: create, list, revoke, Bearer authentication, CSRF exemption                        | M | [05](05_auth_and_permissions.md) |
| 5.8  | Authorization helpers: authenticated, owner, creator, participant, admin                       | M | [05](05_auth_and_permissions.md) |
| 5.9  | `GET`/`PATCH /users/{id}` and the admin user list                                              | M | [04](04_api.md) |
| 5.10 | Account deletion and `GET /users/{id}/deletion-impact` with the remap and delete rules          | L | [02](02_features.md) |
| 5.11 | Permission matrix test suite, one case per cell                                                | L | [12](12_testing.md) |

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
| 6.1 | Restaurant create, read, list, update                                                    | M | [04](04_api.md) |
| 6.2 | Contacts subresource, with the at-least-one rule                                          | M | [04](04_api.md) |
| 6.3 | Opening hours: whole-set `PUT`, validation, midnight crossing, multiple ranges per day     | M | [03](03_data_model.md) |
| 6.4 | Reference endpoints `/currencies` and `/contact-types`                                    | S | [04](04_api.md) |
| 6.5 | Restaurant delete: admin only, 409 while referenced by an order                            | S | [04](04_api.md) |
| 6.6 | Image pipeline: content sniffing, size cap, decode, downscale, thumbnail, SHA-256 dedupe   | L | [adr/0008](adr/0008-images-in-the-database.md) |
| 6.7 | `POST /images`, `GET /images/{id}`, `GET /images/{id}/thumbnail` with `ETag` and caching   | M | [04](04_api.md) |
| 6.8 | Logo association on restaurants                                                            | S | [03](03_data_model.md) |
| 6.9 | Tests, including a file whose extension lies about its contents                            | M | [12](12_testing.md) |

**Exit criteria:** a restaurant with a logo, several contacts and a lunch-break
opening-hours pattern can be created and read back through the API. Uploading a
non-image with a `.jpg` name is rejected.

---

## Sprint 7 — Menus

**Goal:** categories, menu items, modifications and classification.

| ID  | Task                                                                                     | Size | Spec |
| --- | ------------------------------------------------------------------------------------------ | :--: | ---- |
| 7.1 | Category create, update, reorder, soft delete                                               | M | [04](04_api.md) |
| 7.2 | Menu item create, read, update, soft delete, `available` flag                                | M | [04](04_api.md) |
| 7.3 | Ordering rule: numeric-aware `external_id`, `NULLS LAST`, then `name`                        | M | [03](03_data_model.md) |
| 7.4 | Modifications: create, update, soft delete                                                   | M | [adr/0007](adr/0007-flat-modification-list.md) |
| 7.5 | `/tags` list and create; `/allergens` and `/additives` read-only lists                        | S | [04](04_api.md) |
| 7.6 | Tag, allergen and additive assignment to menu items                                           | M | [03](03_data_model.md) |
| 7.7 | Menu item filtering: `category`, `tag`, `exclude_allergen`, `exclude_additive`, `available`    | M | [04](04_api.md) |
| 7.8 | Soft delete semantics: hidden from every normal query                                          | S | [03](03_data_model.md) |
| 7.9 | Tests, including the ordering rule and the filter combinations                                 | M | [12](12_testing.md) |

**Exit criteria:** a full menu can be built through the API, ordered as a printed
menu would be, and filtered by tag and by allergen exclusion.

---

## Sprint 8 — Orders and order items

**Goal:** the core domain. Orders, items, snapshots and the visibility split.

| ID   | Task                                                                                      | Size | Spec |
| ---- | ------------------------------------------------------------------------------------------- | :--: | ---- |
| 8.1  | Order create, copying currency, minimum order value and delivery fee from the restaurant      | M | [03](03_data_model.md) |
| 8.2  | Order read in both shapes: anonymous header plus `item_count`, authenticated full             | L | [adr/0011](adr/0011-tiered-order-visibility.md) |
| 8.3  | Order update: creator only, restaurant locked once items exist, deadline before fulfilment     | M | [02](02_features.md) |
| 8.4  | Order delete, cascading                                                                       | S | [04](04_api.md) |
| 8.5  | Order list, active first then expired, never with item detail                                  | M | [06](06_ui_ux.md) |
| 8.6  | Order items: create with name and price snapshots, update, delete                              | L | [adr/0009](adr/0009-snapshot-prices-on-order-items.md) |
| 8.7  | Order item modifications with their own snapshots; line total arithmetic                        | M | [03](03_data_model.md) |
| 8.8  | Ownership and deadline rules, including read-only after the deadline for the administrator      | M | [05](05_auth_and_permissions.md) |
| 8.9  | Derived status and computed title                                                               | S | [02](02_features.md) |
| 8.10 | Re-verify account deletion against real orders                                                  | M | [02](02_features.md) |
| 8.11 | Tests, including the anonymous leak test that scans the whole response body                     | L | [12](12_testing.md) |

**Exit criteria:** an order can be created, filled by several users and read
back. Changing a menu item's name and price afterwards leaves the order
untouched. An anonymous `GET` of an order contains no user name and no menu item
name anywhere in the response body.

---

## Sprint 9 — Summary, live updates, cleanup

**Goal:** the backend is feature-complete.

| ID  | Task                                                                                          | Size | Spec |
| --- | ----------------------------------------------------------------------------------------------- | :--: | ---- |
| 9.1 | Summary aggregation, per-person breakdown, totals, below-minimum flag, plain-text rendering        | L | [04](04_api.md) |
| 9.2 | Participant authorization with error 3004, creator included without items                         | M | [adr/0011](adr/0011-tiered-order-visibility.md) |
| 9.3 | Per-order SSE hub with subscriber management                                                       | L | [adr/0003](adr/0003-sse-for-order-updates.md) |
| 9.4 | Event publication after commit, including `order.expired`                                          | M | [04](04_api.md) |
| 9.5 | Per-subscriber filtering: anonymous receives `order.item_count`, always republished                 | M | [02](02_features.md) |
| 9.6 | Write-deadline exemption, 30-second `ping`, stream closure on shutdown                              | M | [09](09_configuration.md) |
| 9.7 | `cleanup` verb: the six ordered steps plus `--dry-run`                                              | L | [09](09_configuration.md) |
| 9.8 | Tests: aggregation, the SSE split, and a stream surviving past `--http-write-timeout`               | L | [12](12_testing.md) |

**Exit criteria:** a complete food order can be run end to end through the API.
Two subscribers on one order, one anonymous and one authenticated, each receive
only their own event set. `cleanup --dry-run` reports what a real run would
remove, and a real run removes exactly that.

---

## Sprint 10 — Frontend foundation

**Goal:** the shell every screen is built on.

| ID   | Task                                                                                  | Size | Spec |
| ---- | --------------------------------------------------------------------------------------- | :--: | ---- |
| 10.1 | Full esbuild and Tailwind pipeline with a watch mode                                     | M | [08](08_technologies.md) |
| 10.2 | Client-side router, page shell, dark and light themes with the cookie                     | M | [06](06_ui_ux.md) |
| 10.3 | i18n runtime: catalog loading, the `_meta` registry generated at build time, precedence     | L | [07](07_i18n.md) |
| 10.4 | `en.json` and `de.json` including every reference-data key                                 | M | [07](07_i18n.md) |
| 10.5 | Catalog completeness, registry and reference-data coverage tests                            | M | [12](12_testing.md) |
| 10.6 | `Intl` formatting helpers for dates, times, relative times, numbers and money               | M | [07](07_i18n.md) |
| 10.7 | API client: fetch wrapper, CSRF header, error code to message mapping, session state         | M | [04](04_api.md) |
| 10.8 | Title bar, dropdown menu, language selector driven by the registry, login/logout states       | M | [06](06_ui_ux.md) |
| 10.9 | Base components: tile grid, modal with focus trap, form controls, confirmation dialog         | L | [06](06_ui_ux.md) |
| 10.10| Responsive breakpoints and the print stylesheet foundation                                     | M | [06](06_ui_ux.md) |
| 10.11| Vitest setup and the first unit tests                                                          | S | [12](12_testing.md) |

**Exit criteria:** the shell renders in both themes and both languages, the
language selector is populated from the catalogs present in the build rather than
from a hardcoded list, and money formats correctly for a CHF restaurant viewed in
German.

---

## Sprint 11 — Frontend: accounts, restaurants, menus

| ID   | Task                                                                                | Size | Spec |
| ---- | ------------------------------------------------------------------------------------- | :--: | ---- |
| 11.1 | Login and register page with the live complexity indicator                             | M | [06](06_ui_ux.md) |
| 11.2 | User page: profile, password change, API token management                              | M | [06](06_ui_ux.md) |
| 11.3 | Account deletion with the impact modal and the type-your-name confirmation              | M | [06](06_ui_ux.md) |
| 11.4 | Restaurant overview tiles and the plus-tile empty state                                 | S | [06](06_ui_ux.md) |
| 11.5 | Restaurant page: data, currency, minimum order value, delivery fee, logo upload          | M | [06](06_ui_ux.md) |
| 11.6 | Contacts editor and opening hours editor with the crosses-midnight hint                  | M | [06](06_ui_ux.md) |
| 11.7 | Menu editing: categories with keyboard-accessible reordering, items, modifications        | L | [06](06_ui_ux.md) |
| 11.8 | Tag, allergen and additive assignment UI                                                  | M | [06](06_ui_ux.md) |
| 11.9 | Image upload component with client-side size feedback                                     | M | [06](06_ui_ux.md) |

**Exit criteria:** a restaurant and its complete menu can be entered in the
browser, by a user who is not an administrator.

---

## Sprint 12 — Frontend: orders

| ID   | Task                                                                                   | Size | Spec |
| ---- | ---------------------------------------------------------------------------------------- | :--: | ---- |
| 12.1 | Order overview tiles: active and expired, deadline with relative hint, counts, totals      | M | [06](06_ui_ux.md) |
| 12.2 | Create order flow with client-side validation of the deadline and opening hours warning     | M | [02](02_features.md) |
| 12.3 | Order page left column: order data, creator editing, delete with confirmation               | L | [06](06_ui_ux.md) |
| 12.4 | Order page right column: menu, collapsible categories, the three filters                    | L | [06](06_ui_ux.md) |
| 12.5 | Add-item dialog: quantity, modification checkboxes, free text, live line total               | M | [06](06_ui_ux.md) |
| 12.6 | Add a missing menu item from inside the order                                                | M | [02](02_features.md) |
| 12.7 | The anonymous order view: item count, explanatory line, login link                            | M | [06](06_ui_ux.md) |
| 12.8 | SSE subscription, automatic reconnect, re-fetch on reconnect, reconnect on login               | L | [04](04_api.md) |
| 12.9 | Deadline transition to read-only while the page is open                                        | M | [02](02_features.md) |
| 12.10| Playwright coverage of the core flows, including two browser contexts seeing a live update      | L | [12](12_testing.md) |

**Exit criteria:** two people in two browsers can fill one order and see each
other's items appear without reloading.

---

## Sprint 13 — Frontend: summary, admin, content pages

| ID   | Task                                                                            | Size | Spec |
| ---- | --------------------------------------------------------------------------------- | :--: | ---- |
| 13.1 | Summary page: the four sections, with the aggregated list visually dominant        | L | [06](06_ui_ux.md) |
| 13.2 | Copy-as-text, and the print stylesheet                                             | M | [06](06_ui_ux.md) |
| 13.3 | The non-participant explanatory page, with a login link when anonymous              | S | [06](06_ui_ux.md) |
| 13.4 | Administration: user list with the impact-confirmed delete                          | M | [06](06_ui_ux.md) |
| 13.5 | Administration: imprint and legal notes replacement                                 | M | [02](02_features.md) |
| 13.6 | Version, imprint and legal notes pages                                              | S | [06](06_ui_ux.md) |
| 13.7 | Playwright coverage for these screens                                               | M | [12](12_testing.md) |

**Exit criteria:** the whole product works in a browser. A person can create an
order, others can join it, and the creator can read the summary down the phone.

---

## Sprint 14 — Accessibility, hardening, the update verb

**Goal:** make it correct and safe rather than merely working.

| ID   | Task                                                                                       | Size | Spec |
| ---- | -------------------------------------------------------------------------------------------- | :--: | ---- |
| 14.1 | WCAG 2.1 AA pass: focus indicators, contrast in both themes, semantics, skip link, live regions | L | [06](06_ui_ux.md) |
| 14.2 | Keyboard-only operation of every control, including category reordering and every modal          | L | [06](06_ui_ux.md) |
| 14.3 | `axe-core` in the Playwright run across every page in both themes                                | M | [12](12_testing.md) |
| 14.4 | `update` verb: the pre-release message, the config rewrite machinery, migration application       | L | [09](09_configuration.md) |
| 14.5 | Performance check against the targets, and the concurrency headroom check                         | M | [11](11_nonfunctional.md) |
| 14.6 | Security review: CSP in practice, upload handling, an audit that every query is parameterized      | L | [11](11_nonfunctional.md) |
| 14.7 | Close the coverage gaps against the targets                                                        | M | [12](12_testing.md) |

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
