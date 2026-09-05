# 12 Testing

## Scope

| Layer                        | Approach                                                       |
| ---------------------------- | -------------------------------------------------------------- |
| Domain logic                 | Go unit tests, no database.                                     |
| Data access                  | Go integration tests against a real PostgreSQL 18.               |
| API                          | Go integration tests through `net/http/httptest`, full stack.    |
| Migrations                   | Up-and-down tests plus seed verification.                        |
| Frontend logic               | Vitest unit tests on the pure modules.                           |
| Frontend rendering and flows | Playwright, against a running server with a seeded database.     |
| Accessibility                | `axe-core` inside the Playwright run.                            |

## Go tests

Standard `testing`, with `testify/require` for assertions. No mocking framework
— hand-written fakes behind small interfaces where a fake is genuinely needed.

### Unit tests

Cover the logic that has no business touching a database:

- Order state derivation: active vs. expired around the deadline boundary.
- Computed order title formatting.
- Summary aggregation: grouping by menu item plus modification set plus
  normalized note, counts, per-person totals, grand total, below-minimum flag.
- Line total arithmetic with negative modification price deltas.
- Money formatting and minor-unit conversion.
- Opening hours: multiple ranges per day, ranges crossing midnight, the
  "is the fulfilment time inside opening hours" check.
- Password hashing and verification, including the re-hash-on-outdated-params
  path.
- JWT creation and validation, including tampered signatures and expired tokens.
- CSRF token comparison.
- Configuration precedence: default, then file, then environment, then flag.
- The secret-on-the-command-line rule producing a FATAL for each of the four
  settings.
- Log redaction: none of the deny-listed field names appear in output.
- Image type sniffing and rejection of non-JPEG/PNG/GIF content, including a file
  whose extension lies about its contents.

### Integration tests

Run against a real PostgreSQL 18. No SQLite substitute — the schema uses
PostgreSQL-specific types and constraints, and testing against a different
engine would test the wrong thing.

The database comes from `testcontainers-go` when Docker is available, and
otherwise from `DOENER_TEST_DATABASE_URL` pointing at a database the developer
provides. Tests skip with a clear message when neither is available, so
`go test ./...` never fails merely because Docker is not installed.

Each test runs in a transaction that is rolled back, except migration tests,
which need their own database.

Covered:

- Every migration applies up and rolls back down cleanly, from empty and from
  each intermediate version.
- Seed data is present and correct after migration: 14 allergens, 14 additives,
  the currency list, the contact types, the default tags, the deleted-user
  placeholder with exactly the id `00000000-0000-7000-8000-000000000000`.
- Every constraint actually rejects what it should: `deadline_at <
  fulfilment_at`, `quantity >= 1`, non-negative prices, the per-restaurant
  uniqueness of category and menu item names, the day-of-week range.
- Soft delete hides rows from normal queries.
- The `cleanup` verb removes exactly the right rows in the right order, and
  `--dry-run` changes nothing.
- User deletion: items in expired orders reassigned to the placeholder, items in
  active orders deleted, created orders reassigned, sessions and tokens cascaded.
- Deleting a restaurant referenced by an order is rejected.
- Order item snapshots survive a later change to the menu item's name and price.

### API tests

Every endpoint in [04_api.md](04_api.md) has tests. This is a hard requirement,
enforced by a test that walks the OpenAPI document and fails if an operation has
no corresponding test.

For each endpoint:

- The success path, checking status, body shape and any `Location` header.
- Anonymous access: allowed for public reads, 401 with error 2000 for writes.
- Authorization: a non-owner gets 403 with the right error number; the owner and
  the administrator succeed.
- Validation: each documented error number is actually producible.
- The deadline rule: writes to an expired order return 409 error 4001, for the
  administrator too.
- CSRF: a cookie-authenticated write without `X-CSRF-Token` returns 2005; the
  same request with a Bearer token succeeds.

Cross-cutting API tests:

- Every response carries the security headers from
  [05_auth_and_permissions.md](05_auth_and_permissions.md).
- Every response carries `X-Request-Id`, and it matches the `request_id` in the
  error body and in the log line.
- Login rate limiting triggers at the configured threshold, returns 429 with
  `Retry-After`, and a successful login clears the counter.
- A second login for the same user invalidates the first session, which then
  gets 2003.
- Idle and absolute session timeouts expire sessions at the right moment, with
  time injected rather than slept through.
- The SSE stream delivers `item.created`, `item.updated`, `item.deleted` and
  `order.expired` to a connected client, and closes cleanly on shutdown.
- The OpenAPI document is valid 3.1 and describes every registered route — a
  route with no OpenAPI operation fails the test.

## Frontend tests

**Vitest** for the pure modules: i18n key lookup and interpolation, locale
formatting of dates and money, the client-side validation rules, and the
API-error-code-to-message mapping.

A catalog test fails the build when the German catalog is missing a key present
in the English one.

**Playwright**, against a real server with a seeded database, in Firefox and
Chromium:

- Register, log out, log in, and get logged out of the first browser by a login
  in the second.
- Create a restaurant with contacts, opening hours, a category and a menu item.
- Create an order, add items with predefined and free-text modifications, edit
  them, delete them.
- Watch a second browser context receive a live update through SSE.
- Read the summary page, copy its text, and check the aggregation matches what
  was ordered.
- Cross the deadline and see the page become read-only without a reload.
- Filter a menu by tag, and by allergen exclusion.
- Switch language and theme, reload, and find both preserved.
- Delete an account and see the confirmation modal report the right impact.
- Walk the whole application by keyboard only, reaching every interactive
  control and operating the modals.
- Run `axe-core` on every page in both themes with no serious or critical
  violations.

## Coverage

Coverage is a signal, not a target to game.

| Area                              | Expectation                                    |
| --------------------------------- | ---------------------------------------------- |
| `internal/` overall               | ≥ 75 % statements                               |
| Summary aggregation and totals     | ≥ 95 %                                          |
| `internal/auth`                    | ≥ 90 %                                          |
| Permission checks                  | Every cell of the matrix in [05](05_auth_and_permissions.md) exercised |
| API endpoints                      | 100 % of operations have at least one test      |

The last two matter more than the percentages. A permission bug is the most
likely serious defect in an application whose access rules are this asymmetric.

## Test data

A fixture package builds a realistic seed: three restaurants with full menus in
two currencies, categories, tags, allergens, modifications, a handful of users,
one active order and one expired one, both with items from several people. The
same fixture seeds the Playwright database, so manual exploration and automated
tests see the same world.

Fixtures are built through the API where possible, not by direct `INSERT`, so
that they exercise validation on the way in.

## Continuous integration

Every push and pull request runs, in this order, failing fast:

1. `make lint` — `go vet`, `golangci-lint`, `tsc --noEmit`, `eslint`.
2. `govulncheck ./...`.
3. `make test` — Go unit and integration tests with a PostgreSQL 18 service
   container, plus Vitest.
4. `make release` — proves the embedded build compiles.
5. Playwright against the release binary with a seeded database.

A merge is blocked on all five. Coverage is reported but does not block.
