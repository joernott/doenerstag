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
- Password complexity: the three-of-five rule, with a table-driven case per
  class and per combination, including a passphrase that passes on classes 1, 2
  and 5, a password that meets exactly two classes and is rejected, NFC
  normalization making a decomposed `ä` count as class 5, and the code-point
  length limit.
- Configuration precedence: default, then file, then environment, then flag.
- The configuration file permission check: `0600` and `0400` pass, `0640`,
  `0644` and `0666` produce a FATAL, and the check is skipped on Windows.
- Menu item ordering: numeric IDs sorting numerically (2 before 10), mixed IDs
  sorting as text, missing IDs last, name as the tie-break.
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

Setting `DOENER_TEST_SKIP_DOCKER` to a non-empty value stops the harness
attempting a container at all. The Windows CI job sets it: that runner has a
Docker daemon, but one serving Windows containers, so a Linux PostgreSQL image
can never run there.

The harness must never fail a suite because the environment lacks a usable
database — only provide one or skip. A panic raised while a third-party library
probes for Docker is therefore caught and reported as unavailability, with the
panic value carried into the skip message.

Each test runs in a transaction that is rolled back, except migration tests,
which need their own database.

Covered:

- Every migration applies up and rolls back down cleanly, from empty and from
  each intermediate version.
- Seed data is present and correct after migration: 14 allergens with their
  Annex II `reference` numbers 1–14, 14 additives, the currency list with its
  symbols and minor units, the contact types, the default tags, the deleted-user
  placeholder with exactly the id `00000000-0000-7000-8000-000000000000`.
  Codes are asserted individually, since they are now the only identifier these
  rows carry and renaming one silently breaks its translation.
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

Three rules get their own dedicated tests, because each is a place where a
refactor could silently leak data:

- **Anonymous order visibility (F1.2).** `GET /orders/{id}` and `GET /orders` as
  an anonymous caller must return `item_count` and no `items` key at all. The
  test asserts on the serialized JSON, not on a struct, so a field that reappears
  through an embedded type is caught. A companion test walks the whole anonymous
  response body for any user name or menu item name present in the fixture and
  fails if one appears.
- **Summary access (F1.3).** The creator with no items of their own succeeds; a
  user with an item succeeds; the administrator succeeds; a logged-in
  non-participant gets 403 error 3004; an anonymous caller gets 403. A user whose
  last item is removed loses access on the next request.
- **SSE payload split (F7.4).** An anonymous subscriber receives
  `order.item_count` and header events only, never `item.*`. An authenticated
  subscriber on the same order receives the item events. Both subscribe to one
  order simultaneously and the test asserts each got its own event set.

Two further router-level tests follow from using `httprouter`:

- Constructing the full production router must not panic. httprouter rejects
  conflicting routes at registration time, so this catches a wildcard collision
  in CI rather than at server startup.
- The SPA fallback: an unmatched path not starting with `/api/` serves
  `index.html`; an unmatched path under `/api/` returns JSON 404 error 4000.

And one that guards the timeout configuration:

- An SSE stream held open for longer than `--http-write-timeout` is still
  delivering events. This proves the write-deadline exemption is in place; without
  it the stream dies silently at the timeout and live updates simply stop.

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

Two of those deserve naming, because they are the ones where being wrong is
expensive and invisible:

- **The password rules exist twice**, in `internal/auth/complexity.go` and in
  `frontend/src/password.ts`, because asking the server on every keystroke would
  send an unfinished password over the wire repeatedly. The two test suites
  therefore assert the same cases — NFC normalisation, the space that earns no
  class, a passphrase that fails on two classes — so that a change to one and
  not the other shows up as a disagreement rather than as an indicator that
  quietly lies.
- **Money is parsed from text, not through a float.**
  `Math.round(parseFloat("19.99") * 100)` is 1998.9999999999998, and rounding
  hides it for most numbers but not for all of them. The parser works on the
  digits and is tested on the amounts where the float version goes wrong.

Pages are tested by rendering them against a stubbed fetch and asserting on the
DOM they produce — that the last contact's delete button is disabled and says
why, that an unavailable item is marked in words and not only by a strike, that
a non-administrator is not offered the deletions. What the test sees is what a
browser would show.

Five catalog tests, none of which names a language, so all keep working as
translations are added:

- Any catalog in the registry missing a key present in the English one fails the
  build. Plural keys count as present when the catalog supplies every category
  *its own* language has, which `Intl.PluralRules` is asked for rather than
  assumed.
- Every catalog declares a well-formed `_meta` block.
- The generated registry lists exactly the catalogs in the directory, so a
  language cannot appear in the selector without a catalog behind it, or be
  shipped without appearing.
- Every error number documented in [04_api.md](04_api.md) has an `error.<code>`
  message. The codes are read out of the document's own table, so a new error
  number fails CI until it can be shown to a user in their language.
- **Reference data completeness.** Every `code` seeded by a migration has a
  matching catalog key in every catalog: `allergen.<code>`, `additive.<code>`,
  `currency.<code>`, `contact_type.<code>` and `tag.<code>` for the seeded tags.
  The codes are read from the migration files, not from a list maintained
  alongside the test, so adding a seeded row without its translations fails CI.
  Since these tables no longer carry name columns, this check is the only thing
  standing between a new seed row and an allergen rendered to a user as
  `sulphites`.

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

### How the suite runs

The suite does not start the server. The server needs a database, and where
that database is depends on the machine: `make e2e` runs the tests against
whatever `DOENER_E2E_URL` names, defaulting to `https://localhost:8443`, and the
CI job builds the release binary, runs the installer against a PostgreSQL
service container and starts it on `--no-https` before calling the same target.
Running the documented installation on every push is a second benefit: the
install path is exercised continuously rather than only when somebody installs.

Each test builds the world it needs — an account, a restaurant, a menu, an
order — through the API, with a name no other run will have used. That is not
the shared fixture described below; it is what exists until the fixture package
does, and it has the advantage that a test states its own preconditions instead
of depending on a seed somebody else maintains.

The browsers are Chromium and Firefox. `contrib/setup_dev_pipeline.sh` installs
them, so a machine provisioned by that script can run the suite.

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

### Coverage is measured per platform

The two supported development platforms do not execute the same code. The
configuration file permission check is skipped on Windows, and the `SIGHUP` log
reopen does not exist there at all. A single merged coverage number would
average two different runs and hide which lines are unexercised on which
platform.

Coverage is therefore kept separately:

| File                     | From                                    |
| ------------------------ | --------------------------------------- |
| `linux-coverage.out`     | The Linux job, run with `-race`.         |
| `linux-coverage.html`    | Rendered from it.                        |
| `windows-coverage.out`   | The Windows job.                         |
| `windows-coverage.html`  | Rendered from it.                        |

`make cover` writes the pair for whichever platform it runs on, naming them
from `go env GOOS`. CI runs it on both and uploads each pair as its own
artifact, `coverage-linux` and `coverage-windows`.

The profiles are never merged. When a line looks uncovered the useful question
is *on which platform*, and merging is exactly what makes that unanswerable.

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
3. `go-licenses check` against the allow-list in
   [08_technologies.md](08_technologies.md#licensing), plus a check that
   `THIRD_PARTY_LICENSES` is up to date. A dependency added without its licence
   fails here rather than at release time.
4. `make test` — Go unit and integration tests with a PostgreSQL 18 service
   container, plus Vitest.
5. `make release` — proves the embedded build compiles.
6. Playwright against the release binary with a seeded database.

A merge is blocked on all six. Coverage is reported but does not block.

On a release tag, two further steps run and must also pass:

7. `make packages` — builds the `.deb` and `.rpm`, then installs each in a
   throwaway container and asserts that the unit file, the `logrotate` rule and
   the cron entry landed where [10_operations.md](10_operations.md) says they do,
   that the config file is mode `0600`, and that `doenerstag --version` reports
   the tag.
8. `make image` — builds the container image for both architectures and runs
   `doenerstag --version` inside it.
