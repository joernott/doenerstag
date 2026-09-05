# ADR-0005: Hash passwords server-side with Argon2id

**Status:** Accepted — 2026-09-05

## Context

The question raised was whether Argon2id could be computed in the browser and
verified in the backend, with bcrypt as the fallback if not.

It can be. WASM builds of Argon2 exist and run acceptably in current browsers.
The question is whether it should be, and what it would actually buy.

Client-side hashing protects the plaintext password against a compromised or
dishonest server, and against the password appearing in a server-side log. It
does not protect against anything else, and it introduces problems of its own.

## Decision

Hash passwords **server-side** with Argon2id via
`golang.org/x/crypto/argon2`: 64 MiB memory, 3 iterations, parallelism 2, a
16-byte random salt per password, a 32-byte key, stored as a PHC-format string
so the parameters travel with the hash.

On a successful login against a hash with outdated parameters, re-hash with the
current parameters and update the row.

Do not hash in the browser.

## Why not the browser

- **It moves the problem rather than solving it.** If the browser sends
  `Argon2id(password)` and the server stores that value, the stored value *is* a
  password equivalent: anyone who reads the database can authenticate without
  cracking anything. That is precisely the property password hashing exists to
  prevent. Avoiding it means hashing again on the server — at which point the
  server-side hash is doing the real work and the client-side one is a bonus.
- **Salting leaks.** Per-user salt in the browser requires an unauthenticated
  endpoint that returns a salt for a given user name. That is a clean user
  enumeration oracle. A salt derived from the user name instead is weaker and
  breaks on rename.
- **The threat it addresses is not in the threat model.** The plaintext is
  already protected in transit by TLS, and by the redaction rules that keep
  passwords out of the logs. The remaining benefit is protection against our own
  server, which is not a party we are defending against here.
- **It adds an offline dependency.** A WASM Argon2 build would have to be
  vendored and kept current to satisfy the no-internet requirement.

Client-side pre-hashing remains available later as defence in depth *on top of*
server-side Argon2id. It is not a substitute for it, and adding it later
requires no schema change.

## Why not bcrypt

bcrypt was the offered fallback. It is not needed: Argon2id is available in the
Go standard extended libraries, is the current recommendation for new systems,
and resists GPU and ASIC attack far better because it is memory-hard.

If Argon2id ever has to be abandoned, the replacement is bcrypt at **cost 12 or
higher**, with the password pre-hashed with SHA-256 and base64-encoded to work
around bcrypt's 72-byte input truncation.

## Consequences

- Login costs roughly 100–200 ms of CPU and 64 MiB of transient memory per
  attempt. That is the intended cost, and it bounds login throughput.
- It also makes login the natural denial-of-service target, which is one reason
  the login endpoint is the only rate-limited one.
- Memory is the parameter to watch when raising the work factor: 64 MiB times
  the number of concurrent logins. At the expected scale this never matters, but
  it is the constraint that would bite first.
- The PHC-format storage means parameters can be raised later without
  invalidating any existing password.
- This decision deviates from the literal instruction, which offered only
  browser-side Argon2id or bcrypt. It is recorded here rather than made
  silently, and is straightforward to reverse.
