# 05 Authentication and permissions

## Threat model

doenerstag runs on a trusted internal network. Everyone who can reach it is a
colleague or club member, and the data it holds — who wants which kebab — is of
low value. The security measures below exist to prevent accidents, casual
mischief and browser-level attack classes, not to withstand a determined
attacker with network access.

Two things are nevertheless treated seriously, because getting them wrong has
consequences outside the application: **passwords** (people reuse them) and
**personal data** (see [13_legal_and_privacy.md](13_legal_and_privacy.md)).

## Anonymous access

Reading is mostly open, but not entirely. An unauthenticated visitor can see:

- Every restaurant, its contacts, opening hours and full menu.
- The list of orders, and each order's header: restaurant, fulfilment type and
  time, deadline and status. **No user is named**, not even the order's
  creator.
- **How many** items an order has — but not the items themselves.
- The version, imprint and legal notes pages.

An unauthenticated visitor **cannot** see:

- The order items: what was ordered, by whom, with which modifications, or any
  total (F1.2).
- Who created an order. The header carries the restaurant and the times, not a
  person.
- Any order's summary page. That is restricted further still, to participants
  (F1.3).

The line is drawn so that the application is useful before you log in — you can
see that a Döner Palast order closes at 11:30 and that nine items are already in
it — without publishing to every passer-by on the network what each named
colleague eats. That data is where the privacy sensitivity actually sits; see
[13_legal_and_privacy.md](13_legal_and_privacy.md).

This is enforced server-side. The anonymous response shape simply does not
contain the item data; the frontend is not trusted to hide it.

An unauthenticated visitor can change nothing. The frontend hides or disables
every control that would write, and the API rejects every non-`GET` request from
an anonymous caller with error 2000.

## Participants

Several rules turn on whether a user is a **participant** of an order. A
participant is:

- the order's creator, **or**
- any user holding at least one `order_item` in that order, **or**
- the administrator.

The creator counts even with no items of their own, because the creator is
normally the person who phones the restaurant and reads from the summary.

Participation is derived, never stored: it is a query against `food_order.creator_id`
and `order_item.user_id`. Adding an item makes you a participant immediately;
removing your last item stops you being one. A user who is removed from an
order therefore loses access to its summary, which is intended.

## Registration

Open self-registration. Anyone who can reach the application can create an
account.

| Rule                | Value                                                              |
| ------------------- | ------------------------------------------------------------------ |
| User name           | 3–64 characters, unique case-insensitively.                         |
| Display name        | Optional, 1–64 characters. Falls back to the user name.             |
| E-mail              | Optional. Format-checked, never verified, never used to send mail.  |
| Password length     | Minimum 10, maximum 256 characters.                                 |
| Password complexity | At least three of the five character classes below.                 |

There is no password expiry — passwords are changed when there is a reason to
change them, not on a calendar.

### Password complexity rules

A password must satisfy **at least three** of these five rules:

| # | Rule                             | Character set                                              |
| - | -------------------------------- | ---------------------------------------------------------- |
| 1 | At least one upper case letter   | `A`–`Z`                                                     |
| 2 | At least one lower case letter   | `a`–`z`                                                     |
| 3 | At least one digit               | `0`–`9`                                                     |
| 4 | At least one special character   | ``<>|-_.:,;#'!"§$%&/()[]{}?@``                              |
| 5 | At least one language-specific character, currency symbol or other non-ASCII printable character | e.g. `äöüÄÖÜß`, `áàâéèêíìîóòôúùû`, `ñçøåæ`, `€`, `£`, `¥` |

Implementation notes:

- Rule 5 is defined by exclusion, not by a fixed list: any printable character
  that is not covered by rules 1–4 and is not a plain ASCII space satisfies it.
  Enumerating accented characters would leave out somebody's alphabet, and the
  examples above are examples, not the specification.
- Classification runs over Unicode code points after NFC normalization, so a
  precomposed `ä` and a decomposed `a` + combining diaeresis count the same.
- The password is normalized to NFC before hashing as well, so a password typed
  on a Mac verifies on Windows.
- Space is permitted anywhere and satisfies no rule on its own; passphrases pass
  easily on rules 1, 2 and 5 or 4.
- The maximum of 256 characters is applied to the code point count, not the byte
  count.

The frontend shows which rules are currently satisfied as the password is typed,
and the backend enforces the same check independently — a password set through
the API is subject to identical rules. Failing the check returns error 1010,
naming how many rules were met but never echoing the password.

### A note on this choice

Composition rules of this kind are no longer what NIST SP 800-63B recommends;
length and a check against known-breached passwords are the current guidance,
because forced classes push people toward `Password1!` and its relatives.

The three-of-five form used here is a reasonable compromise: it is permissive
enough that a long passphrase passes without contortion, and it is what most
organizations' password policies expect to see. Combined with the 10-character
minimum it is comfortably strong enough for an intranet tool. Keeping it means
the application does not need to ship and maintain an offline breached-password
list, which the no-internet requirement would otherwise force.

## Password storage

Passwords are hashed **server-side with Argon2id**, using
`golang.org/x/crypto/argon2`.

| Parameter   | Value                                    |
| ----------- | ---------------------------------------- |
| Variant     | Argon2id                                 |
| Memory      | 65536 KiB (64 MiB)                       |
| Iterations  | 3                                        |
| Parallelism | 2                                        |
| Salt        | 16 random bytes per password             |
| Key length  | 32 bytes                                 |

The hash is stored as a PHC-format string, so the parameters travel with the
hash and can be raised later without invalidating existing passwords. On a
successful login with an outdated parameter set, the password is re-hashed with
the current parameters and the row is updated.

### Why not hash in the browser

Hashing with Argon2id inside the browser is technically possible through a WASM
build, but it was rejected:

- If the browser sends `Argon2id(password)` and the server stores that value
  directly, the stored hash *is* a password equivalent. Anyone who reads the
  database can log in without cracking anything — the exact property password
  hashing exists to prevent. Avoiding that requires hashing again on the server,
  which is where the real protection would come from anyway.
- Per-user salting in the browser requires an unauthenticated endpoint that
  hands out a salt for a given user name, which is a clean user-enumeration
  oracle.
- The transport is already TLS, so the plaintext password is not exposed on the
  wire.
- A WASM Argon2 build adds a runtime dependency that must be vendored to satisfy
  the offline requirement.

Client-side pre-hashing may still be added later as defence in depth *on top of*
server-side Argon2id. It is not a substitute for it.

If Argon2id ever has to be abandoned, the replacement is bcrypt with a **cost
factor of at least 12**, and passwords must then be pre-hashed with SHA-256 and
base64-encoded to work around bcrypt's 72-byte input limit.

## Sessions

Sessions use a JWT carried in a cookie, backed by a `session` row in the
database. The database row is what makes idle timeouts and single-session
enforcement possible; a purely stateless JWT can express neither.

### The token

| Property   | Value                                                    |
| ---------- | -------------------------------------------------------- |
| Algorithm  | HS256                                                     |
| Secret     | 32 random bytes, generated at install, stored in the config file as `jwt_secret`. Rotating it invalidates all sessions. |
| `sub`      | User id.                                                  |
| `jti`      | Session id — the primary key of the `session` row.         |
| `iat`      | Issued at.                                                |
| `exp`      | `iat` + `absolute-timeout` (default 7 days).               |
| `adm`      | Boolean, true for the administrator. Advisory only; the server re-reads `is_admin` from the database on every request. |

### The cookie

| Attribute  | Value                                                        |
| ---------- | ------------------------------------------------------------ |
| Name       | `doener_session`                                              |
| `HttpOnly` | yes                                                           |
| `Secure`   | yes, unless the server runs with `--no-https`                 |
| `SameSite` | `Strict`                                                      |
| `Path`     | `/`                                                           |
| `Max-Age`  | Matches the absolute timeout                                  |

### Validation on every authenticated request

1. Verify the JWT signature and `exp`.
2. Load the `session` row by `jti`. Missing row → 2003 (superseded or logged
   out).
3. Reject if `now > absolute_expires` → 2002.
4. Reject if `now - last_seen_at > idle-timeout` (default 6 hours) → 2002, and
   delete the row.
5. Update `last_seen_at`. To avoid a write on every request, the update is
   skipped when `last_seen_at` is less than 60 seconds old.

### What a failed credential does to the request

The code above says which error a credential earns. What happens to the request
depends on **which** credential it was, and the two are not the same:

| Credential          | Fails how                                                                 |
| ------------------- | ------------------------------------------------------------------------- |
| `Authorization: Bearer` | The request is refused, with the code above. A caller that names a token asked for it to be used; downgrading it to anonymity would turn a clear 401 into a confusing 403 or an empty list. |
| Session cookie      | The request continues **anonymously**. The cookie is expired, the reason is recorded, and authorization then decides the outcome exactly as it would for any anonymous caller. |

The cookie is ambient: the browser attaches it to the page, the stylesheet, the
script and every public endpoint without anyone choosing to. Refusing those
requests meant one stale cookie made the whole application unreachable —
navigating to `/` produced a JSON error envelope rather than the application,
and since the same cookie rode along on everything there was no way out of it
short of clearing cookies by hand.

The reason is written into a short-lived `doener_session_ended` cookie
(`HttpOnly`, five minutes) by the request that detects it. It has to be recorded
there and then rather than worked out again later: the request that finds a
session past its idle timeout also deletes the row, so a second look would find
no row and conclude "superseded by a newer login" — the wrong thing to tell
somebody whose session simply lapsed overnight.

`GET /auth/session` reports it, in the `ended` object described in
[04_api.md](04_api.md), and clears the note. The frontend shows it once, as a
dialog over a working anonymous application, offering a login. Telling somebody
they were logged out is the part worth keeping; making them read it as JSON was
not.

### One session per user

`session` has a unique constraint on `user_id`. A successful login deletes any
existing row for that user and inserts a new one. The previously logged-in
browser is told 2003 the next time it asks who it is, and is offered a login.

This deliberately prevents the same account being used from two browsers at
once, which also removes most of the scope for concurrent edits of the same
order item (see [04_api.md](04_api.md); there is no optimistic locking).

### Logout

`POST /api/v1/auth/logout` deletes the session row and clears both cookies.

## CSRF protection

Cookie-borne credentials plus a REST API require CSRF protection even with
`SameSite=Strict`.

- On session creation the server also sets a `doener_csrf` cookie containing 32
  random bytes, base64url-encoded. This cookie is **not** `HttpOnly`, so the
  frontend can read it.
- Every `POST`, `PUT`, `PATCH` and `DELETE` request authenticated by cookie must
  send that value in the `X-CSRF-Token` header. Mismatch or absence → 2005.
- The comparison is constant-time.
- Requests authenticated by `Authorization: Bearer` are exempt — they carry no
  ambient browser credential, so there is nothing for a foreign site to abuse.
- `GET` requests are exempt and must never change state.

## API tokens

For third-party and scripted access. Third-party integration is not a focus of
the first release; the mechanism exists so that it does not have to be retrofitted.

- A user creates a token from their user page, giving it a name and an optional
  expiry date.
- The server generates 32 random bytes, encodes them as base64url, and returns
  the value **once**. Only its SHA-256 hash and an 8-character prefix are
  stored.
- The token is sent as `Authorization: Bearer <token>`.
- A token inherits the permissions of its owner, including administrator rights
  if the owner is `root`.
- Token requests do not create or touch `session` rows and are unaffected by the
  single-session rule and the idle timeout.
- `last_used_at` is updated on use, subject to the same 60-second throttle as
  sessions.
- Revocation is immediate.

## The administrator

Exactly one administrator account exists: `root`, created during
`doenerstag install`. It cannot be renamed, cannot be deleted, and no other
account can be granted `is_admin`. Promoting a second administrator is a
deliberate non-feature; if the password is lost, the operator resets it with
`doenerstag update`.

## Permission matrix

| Action                                        | Anonymous | User | Owner / creator | Admin |
| --------------------------------------------- | :-------: | :--: | :-------------: | :---: |
| View the order list and each order's item count | ✓ | ✓ | ✓ | ✓ |
| View an order's items and totals              | – | ✓ | ✓ | ✓ |
| View an order's summary page                  | – | participants only | ✓ | ✓ |
| View restaurants, menus, opening hours        | ✓ | ✓ | ✓ | ✓ |
| View imprint, legal notes, version            | ✓ | ✓ | ✓ | ✓ |
| Register an account                           | ✓ | – | – | – |
| Create an order                               | – | ✓ | ✓ | ✓ |
| Edit an order's fields                        | – | – | ✓ | ✓ |
| Take on fetching the food, when nobody has    | – | ✓ | ✓ | ✓ |
| Change an order's restaurant (no items yet)   | – | – | ✓ | ✓ |
| Delete an order                               | – | – | ✓ | ✓ |
| Add an order item to an active order          | – | ✓ | ✓ | ✓ |
| Edit or delete an order item                  | – | – | ✓ | ✓ |
| Edit or delete an order item after deadline   | – | – | – | – |
| Create a restaurant                           | – | ✓ | ✓ | ✓ |
| Edit a restaurant, contacts, opening hours    | – | ✓ | ✓ | ✓ |
| Delete a restaurant                           | – | – | – | ✓ |
| Create a menu category or menu item           | – | ✓ | ✓ | ✓ |
| Edit a menu item, mark it unavailable         | – | ✓ | ✓ | ✓ |
| Delete a menu category, item or modification  | – | – | – | ✓ |
| Create a free tag                             | – | ✓ | ✓ | ✓ |
| Upload an image                               | – | ✓ | ✓ | ✓ |
| Edit own profile, manage own API tokens       | – | – | ✓ | ✓ |
| Delete own account                            | – | – | ✓ | ✓ |
| List the accounts (id, name, display name)    | – | ✓ | ✓ | ✓ |
| List all users with addresses and login times | – | – | – | ✓ |
| Replace imprint / legal notes                 | – | – | – | ✓ |
| Shut the application down                     | – | – | – | ✓ |

"Owner / creator" means the acting user owns the resource. Wherever that column
is ticked, the administrator can act as well.

Note the deliberate asymmetry: **creating and editing** menu data is open to
every logged-in user so the database can be crowdsourced, while **deleting** it
is restricted to the administrator so that a misclick cannot remove a
restaurant's whole menu.

After the deadline an order and its items are read-only for everyone, including
the administrator. The administrator may still delete the order as a whole.

## Rate limiting

Only the login endpoint is rate limited. Everything else is unprotected, which
is appropriate for the deployment context.

| Scope         | Limit                                    |
| ------------- | ---------------------------------------- |
| Per user name | 10 failed attempts per 15 minutes         |
| Per client IP | 60 failed attempts per 15 minutes         |

Counters are held in memory, reset on restart, and cleared on a successful
login. Exceeding a limit returns 429 with error 5000 and a `Retry-After` header.
Successful logins are not counted.

Both limits are configurable — see [09_configuration.md](09_configuration.md).

## CORS

By default the server sends no CORS headers at all: the frontend is served from
the same origin as the API, so none are needed, and their absence keeps other
origins from calling the API with the user's cookies.

`--cors-allowed-origins` accepts a comma-separated list of origins for
third-party API consumers. When set, the server answers preflight requests for
those origins and sends `Access-Control-Allow-Credentials: false`. Cookie-based
sessions are never usable cross-origin; cross-origin callers must use API
tokens.

## Security response headers

Sent on every response:

| Header                      | Value                                                          |
| --------------------------- | -------------------------------------------------------------- |
| `Content-Security-Policy`   | `default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; font-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'` |
| `X-Content-Type-Options`    | `nosniff`                                                       |
| `Referrer-Policy`           | `same-origin`                                                   |
| `Cross-Origin-Opener-Policy`| `same-origin`                                                   |
| `Permissions-Policy`        | `geolocation=(), camera=(), microphone=(), payment=()`          |
| `Strict-Transport-Security` | `max-age=31536000` — only when running with TLS                 |

The CSP forbids inline scripts and inline styles. This is achievable because
Tailwind is compiled to a static stylesheet and TypeScript to static bundles at
build time — nothing is generated at runtime.

Swagger UI is served from the same origin with the same policy. If the bundled
Swagger UI cannot run under it, Swagger UI is served from `/tools/swagger` with
a relaxed `style-src 'self' 'unsafe-inline'` scoped to that path only, and never
with a relaxed `script-src`.

The imprint and legal-notes snippets are administrator-supplied HTML. They are
sanitized on write with an allow-list sanitizer (`bluemonday`'s UGC policy,
extended to permit `id` and `class`), which strips scripts, event handlers and
`javascript:` URLs. The CSP is the second line of defence.
