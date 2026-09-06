# 11 Non-functional requirements

## Browser support

Firefox and Chrome first. Those two are what the target audience uses, and they
are the ones the application is developed and tested against.

| Browser         | Support level                                                    |
| --------------- | ---------------------------------------------------------------- |
| Firefox         | Current and previous ESR. Fully tested.                            |
| Chrome / Edge   | Current and previous major. Fully tested.                          |
| Safari (macOS)  | Current major. Expected to work; fixed when broken, not pre-tested.|
| Safari (iOS)    | Current major. Same.                                               |
| Anything else   | Not supported. No polyfills, no transpilation below ES2022.        |

Required browser features, all of which the above provide natively: ES2022
modules, `EventSource`, `Intl.DateTimeFormat` / `NumberFormat` /
`RelativeTimeFormat` / `PluralRules`, CSS nesting, `:has()`, CSS logical
properties, `loading="lazy"`.

## Devices and layout

Desktop is the primary environment — Windows and macOS. Tablets and phones must
be fully usable, but they are adapted to rather than designed for. The
breakpoints are listed in [06_ui_ux.md](06_ui_ux.md).

No native application, no PWA install prompt, no offline mode. The application
is useless without the server anyway.

## Accessibility

**WCAG 2.1 level AA**, as specified in detail in [06_ui_ux.md](06_ui_ux.md).
The two requirements that are easiest to lose and most important to keep:
everything is operable by keyboard alone, and the contrast minima hold in the
dark theme as well as the light one.

## Performance

The numbers below follow from the expected scale in
[01_overview.md](01_overview.md) — 3 to 10 restaurants, a few hundred menu
items, around three active orders a day, tens of users on a LAN. They are
deliberately unambitious; the point is to notice if something goes badly wrong,
not to optimize.

| Operation                                     | Target (server-side, 95th percentile) |
| --------------------------------------------- | ------------------------------------- |
| `GET /orders`                                  | < 50 ms                               |
| `GET /orders/{id}` with 50 items               | < 100 ms                              |
| `GET /orders/{id}/summary`                     | < 100 ms                              |
| `GET /restaurants/{id}/menu-items`, 300 items  | < 150 ms                              |
| `POST /orders/{id}/items`                      | < 100 ms                              |
| Login (dominated by Argon2id)                  | < 500 ms                              |
| Image upload with downscale, 5 MiB             | < 2 s                                 |
| SSE event delivered after the causing write    | < 500 ms                              |

Client-side: first contentful paint under one second on the LAN; the JavaScript
bundle under 200 KiB and the stylesheet under 100 KiB, both compressed. The two
budgets are checked by `TestTheClientBudgetsAreMet` in `internal/static`, against
the files a release embeds.

The server-side targets are checked by the performance tests in `internal/api`,
which seed the data volumes below and measure the 95th percentile. They are off
by default -- a timing assertion on a shared CI runner is a flake generator --
and are run deliberately:

```
DOENERSTAG_PERFORMANCE=1 go test ./internal/api/ -run TestPerformance -v
```

Measured on the two-core development VM in sprint 14, every target held with at
least an order of magnitude to spare except login, which is dominated by Argon2id
as intended: `GET /orders` 2.8 ms, the 50-item order 4.4 ms, its summary 5.2 ms,
the 300-item menu 12.1 ms, adding an item 5.3 ms, login 106 ms, a 5 MiB upload
with downscale 219 ms, and an event delivered 7.3 ms after the write that caused
it. Under 50 concurrent readers with 100 streams open, a list plus a detail read
took 137 ms.

Concurrency: 50 simultaneous users and 100 concurrent SSE streams without
degradation. That is far above the expected load and exists as a headroom check.

Login is the slowest operation by design — Argon2id at 64 MiB and 3 iterations
costs roughly 100–200 ms of CPU. This also bounds practical login throughput,
which is fine and is one reason the login endpoint is rate limited.

## Availability

No high-availability requirement. A single instance against a single PostgreSQL
server is the supported topology. Planned downtime for updates is acceptable.
The application is a convenience, and the failure mode — people phone the
restaurant the way they did before — is tolerable.

Nothing in the design prevents running several instances behind a load balancer
except the in-memory pieces: the login rate limiter counts per instance, and SSE
hubs are per instance, so a write on instance A does not reach a stream on
instance B. Multi-instance deployment is out of scope.

## Security

The threat model is stated in
[05_auth_and_permissions.md](05_auth_and_permissions.md): a trusted internal
network, low-value data, measures aimed at accidents and browser-level attack
classes rather than a determined attacker.

Requirements that hold regardless:

- **Transport.** HTTPS by default. `--no-https` exists for development and for
  deployments behind a TLS-terminating reverse proxy; using it logs a WARN at
  startup naming the consequences.
- **Passwords.** Argon2id, parameters in
  [05_auth_and_permissions.md](05_auth_and_permissions.md). Never logged, never
  returned by the API, never written to a configuration file.
- **Secrets.** The database password and the JWT secret may not be passed on the
  command line; attempting it is a FATAL error. The configuration file is
  created mode `0600`.
- **SQL.** Every query uses parameter binding. String-concatenated SQL is a
  review blocker. Identifiers are never taken from user input. Checked rather
  than remembered: `internal/db/sqlsafety_test.go` parses this package and
  fails on a SQL fragment formatted with anything but `%d`, or on a statement
  built from a variable. The handful of places that legitimately assemble a
  statement -- a PATCH sets only the fields that were sent -- are named there
  with the reason each is safe, and an entry that stops being needed fails too.
- **XSS.** The frontend builds the DOM through `textContent` and explicit
  element creation. `innerHTML` is used in exactly one place — rendering the
  imprint and legal notes snippets — and that content is sanitized on write with
  `bluemonday` and constrained on read by the CSP.
- **CSRF.** Double-submit token on every cookie-authenticated write. See
  [05_auth_and_permissions.md](05_auth_and_permissions.md).
- **Uploads.** Media type determined by sniffing content, not by extension or
  declared type. Only JPEG, PNG and GIF are accepted. Every image is decoded and
  re-encoded, which strips EXIF metadata — including GPS coordinates — as a side
  effect. Size limited by `--max-image-size` and enforced by
  `http.MaxBytesReader` before the body is read into memory.
- **Headers.** The response headers listed in
  [05_auth_and_permissions.md](05_auth_and_permissions.md) are sent on every
  response.
- **Dependencies.** `govulncheck` in CI. `go.mod` and `package-lock.json` are
  committed and dependencies are pinned. New dependencies need a reason; the
  standard library is preferred.
- **Error messages.** API errors never expose SQL text, stack traces, file paths
  or internal host names. The request ID is the link between a user-visible
  error and the log line that has the detail.
- **Enumeration.** Login returns error 2001, "invalid user name or password",
  identically for an unknown user and a wrong password, and takes comparable
  time in both cases. Registration necessarily reveals whether a name is taken;
  that is accepted for an open-registration intranet tool.

### Explicitly accepted risks

Recorded so they are decisions rather than oversights:

| Risk                                                                 | Why it is accepted                                            |
| -------------------------------------------------------------------- | ------------------------------------------------------------- |
| Any logged-in user can read every order and every participant name | The application exists to make orders visible to the group. Anonymous visitors see only item counts (F1.2), and summaries are limited to participants (F1.3). |
| Anyone can register                                                   | Access to the network is the access control.                    |
| Any logged-in user can edit any restaurant or menu                    | Crowdsourcing the data is the point; deletion is restricted.    |
| No e-mail verification                                                | Nothing is ever sent by e-mail.                                 |
| No pagination, so a large collection is one large response            | The expected scale makes it a non-issue. Revisit if it changes. |
| `/health` and `/metrics` are unauthenticated                          | They expose counts only, and monitoring should be easy to wire. |
| No optimistic locking on concurrent edits                             | One session per user, and users only edit their own items.      |

## Data volume

| Table                | Expected rows after a year | Notes                              |
| -------------------- | -------------------------- | ---------------------------------- |
| `restaurant`         | 3 – 10                     |                                     |
| `menu_item`          | 100 – 500                  |                                     |
| `food_order`         | ≤ 40 live                  | Retention is 14 days by default.    |
| `order_item`         | ≤ 500 live                 |                                     |
| `app_user`           | tens                       |                                     |
| `image`              | 100 – 500                  | Downscaled; a few hundred KiB each. |

Total database size stays in the tens of megabytes. No partitioning, no
archiving, no read replicas.

## Maintainability

- Go code passes `go vet` and `golangci-lint` with the project configuration.
  TypeScript compiles under `strict` and passes `eslint`.
- Formatting is not debated: `gofmt` and Prettier decide.
- Public functions carry doc comments. Comments explain why, not what.
- No ORM and no code generation for the data layer. SQL is written and reviewed
  as SQL.
- Every dependency is listed in [08_technologies.md](08_technologies.md) with the
  reason it is there.
- The specification in `docs/` is kept current with the code. A change that
  makes a document wrong is incomplete until the document is fixed.
