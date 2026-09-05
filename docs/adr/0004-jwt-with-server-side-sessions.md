# ADR-0004: JWT backed by a server-side session table

**Status:** Accepted — 2026-09-05

## Context

Sessions were specified as JWT. Three further requirements interact with that
choice:

- An idle timeout of 6 hours by default.
- An absolute timeout of 7 days.
- One active session per user, so that logging in elsewhere ends the previous
  session. This is what keeps a single user from editing the same order item
  from two browsers, which is why the API has no optimistic locking.

A stateless JWT can express the absolute timeout, through `exp`. It can express
neither of the other two. An idle timeout needs to know when the token was last
used, and single-session enforcement needs to revoke a token that has not
expired — both are server-side state by definition.

## Decision

Keep the JWT, and back it with a `session` row.

The JWT carries `sub` (user id), `jti` (session id), `iat`, `exp` and an
advisory `adm` flag, signed HS256 with a 32-byte secret generated at install
time. It travels in the `doener_session` cookie: `HttpOnly`, `Secure`,
`SameSite=Strict`.

Every authenticated request verifies the signature and `exp`, then loads the
`session` row by `jti` and checks `absolute_expires` and `last_seen_at` against
the idle timeout. `last_seen_at` is updated, skipping the write when it is less
than 60 seconds old so that a burst of requests costs one write rather than
many.

`session` has a unique constraint on `user_id`. Logging in deletes any existing
row for that user and inserts a new one; the previously logged-in browser gets
error 2003 on its next request.

`is_admin` is re-read from the database on every request. The `adm` claim is a
hint for the frontend, never an authorization decision.

## Alternatives rejected

- **Plain server-side session cookies with an opaque random id.** Honestly the
  better fit for what this actually needs, and would have been the choice
  without the JWT requirement. Kept the JWT because it costs little here and
  gives API-token-style verification for free if a second service ever needs to
  validate a session.
- **Stateless JWT with short expiry and refresh tokens.** Rejected: refresh
  tokens are server-side state under another name, with more moving parts and a
  worse failure mode. It would also not give single-session enforcement.
- **Stateless JWT, dropping the idle timeout and single-session rule.** Rejected:
  the single-session rule is what makes the absence of optimistic locking safe.

## Consequences

- The JWT is effectively a signed reference to a database row. It is not
  stateless, and calling it stateless would be wrong.
- One extra `SELECT` per authenticated request, on a primary key. Negligible.
- Rotating `jwt_secret` logs everyone out. Documented as the recovery path when
  the secret is compromised, and as a surprise after restoring a backup with a
  different configuration file.
- Logging in on a phone logs you out on the desktop. This is intended, and is
  the most likely thing for a user to report as a bug. The troubleshooting table
  says so.
- Expired session rows accumulate until `cleanup` removes them, in addition to
  being dropped lazily when encountered.
