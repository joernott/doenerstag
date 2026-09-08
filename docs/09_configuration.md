# 09 Configuration

Configuration comes from four sources. Later sources win:

1. Built-in defaults.
2. The configuration file — `doenerstag.yaml` in the working directory unless
   `--config` says otherwise.
3. Environment variables prefixed `DOENER_`.
4. Command line flags.

The environment variable for a setting is `DOENER_` plus the flag name in upper
case with dashes replaced by underscores: `--database-server` becomes
`DOENER_DATABASE_SERVER`.

## Secrets on the command line

Five settings hold secrets. Passing any of them as a command line flag makes the
application log a **FATAL** security error and exit immediately, because command
lines are visible to every user on the machine through the process list and end
up in shell history.

| Setting                     | Accepted from                                          |
| --------------------------- | ------------------------------------------------------ |
| `database-password`         | Config file, environment, interactive prompt            |
| `database-root-password`    | Environment, interactive prompt                         |
| `database-admin-password`   | Environment, interactive prompt                         |
| `root-password`             | Environment, interactive prompt                         |
| `jwt-secret`                | Config file, environment                                |

`root-password` is the doenerstag `root` administrator's password, not a
database one. Every database identity carries a `database-` prefix; this is the
application's own account, and it is the one setting here whose absence of that
prefix is load-bearing.

The two privileged database passwords and the administrator password are never
written to a generated configuration file. Neither are the privileged user
names.

---

## Global settings

Understood by every verb.

| Flag                    | Short | Default        | Description                                                       |
| ----------------------- | :---: | -------------- | ----------------------------------------------------------------- |
| `--config`              | `-c`  | `doenerstag.yaml` | Path to the configuration file, relative to the working directory. |
| `--database-server`     | `-S`  | `localhost`    | PostgreSQL host name.                                              |
| `--database-port`       | `-P`  | `5432`         | PostgreSQL port.                                                   |
| `--database-name`       | `-N`  | `doenerstag`   | Database name.                                                     |
| `--database-user`       | `-U`  | `doener`       | Database user used at runtime.                                     |
| `--database-password`   |       | *(none)*       | **Never on the command line.** See above.                          |
| `--database-sslmode`    |       | `prefer`       | One of `disable`, `allow`, `prefer`, `require`, `verify-ca`, `verify-full`. |
| `--max-connection-pool` |       | `10`           | Upper bound for open and for idle database connections.            |
| `--log-level`           | `-l`  | `INFO`         | `FATAL`, `ERROR`, `WARN`, `INFO` or `DEBUG`. Case-insensitive.     |
| `--log-file`            | `-f`  | *(stdout)*     | Log destination. Reopened on `SIGHUP` for `logrotate`.             |
| `--help`                | `-h`  |                | Usage for the verb.                                                |
| `--version`             | `-v`  |                | Print the compiled-in version and exit, without touching the database. |

`--version` is the offline counterpart of the `version` verb: the flag reports
what the binary is, the verb reports what the database says is installed.

---

## Verb `server`

Runs the web server. HTTPS by default.

| Flag                        | Short | Default        | Description                                                  |
| --------------------------- | :---: | -------------- | ------------------------------------------------------------ |
| `--port`                    | `-p`  | `8443`         | TCP port to listen on.                                        |
| `--bind-address`            | `-b`  | *(all)*        | Address to bind to. Empty binds to all addresses, IPv4 and IPv6. |
| `--no-https`                |       | `false`        | Serve plain HTTP. Disables the `Secure` cookie flag and HSTS.  |
| `--tls-cert`                | `-t`  | `server.crt`   | PEM certificate chain, relative to the working directory.      |
| `--tls-key`                 | `-T`  | `server.key`   | PEM private key, relative to the working directory.            |
| `--no-swagger`              |       | `false`        | Do not serve `/tools/swagger` and do not link it in the menu.  |
| `--static-dir`              | `-s`  | `static`       | Directory to serve assets from. Ignored in embedded builds.    |
| `--http-read-timeout`       |       | `30s`          | `http.Server.ReadTimeout`. Covers reading the request body, so it bounds an image upload. |
| `--http-write-timeout`      |       | `60s`          | `http.Server.WriteTimeout`. The SSE route is exempt — see below. |
| `--http-idle-timeout`       |       | `120s`         | `http.Server.IdleTimeout` for keep-alive connections.          |
| `--idle-timeout`            |       | `6h`           | Session idle timeout. A session unused for longer is dropped.  |
| `--absolute-timeout`        |       | `7d`           | Session lifetime regardless of activity.                       |
| `--jwt-secret`              |       | *(generated)*  | **Never on the command line.** Written by `install`.           |
| `--max-image-size`          |       | `5MiB`         | Largest accepted image upload. Accepts `KiB`, `MiB` suffixes.  |
| `--cors-allowed-origins`    |       | *(empty)*      | Comma-separated origins allowed to call the API cross-origin. Empty sends no CORS headers. |
| `--login-rate-limit-user`   |       | `10`           | Failed logins per user name per window.                        |
| `--login-rate-limit-ip`     |       | `60`           | Failed logins per client address per window.                   |
| `--login-rate-limit-window` |       | `15m`          | Rate limit window.                                             |
| `--shutdown-grace`          |       | `30s`          | How long to wait for in-flight requests during shutdown.       |

### Timeouts and the SSE exemption

The defaults are 30 s read, 60 s write and 120 s idle. The previous 300 s values
were far longer than anything this application legitimately does — the slowest
real request is a 5 MiB image upload with a downscale, budgeted at 2 seconds in
[11_nonfunctional.md](11_nonfunctional.md) — and a long write timeout mostly
just holds resources open for a stalled client.

Shortening the write timeout does **not** on its own make the Server-Sent Events
streams safe; it makes them fail sooner. No finite `WriteTimeout` can
accommodate a stream that is meant to stay open for hours, because
`http.Server.WriteTimeout` bounds the whole response, not the gap between
writes.

What protects the stream is the exemption: the handler for
`GET /api/v1/orders/{id}/events` clears its own write deadline with
`http.ResponseController.SetWriteDeadline(time.Time{})` once the response
headers are sent, so the configured value never applies to it. The 30-second
`ping` event keeps intermediate proxies from closing an idle connection. Every
other route observes the configured timeout.

The exemption is therefore load-bearing, and it is covered by a test: a stream
held open past `--http-write-timeout` must still be delivering events.

### Startup checks

Before it begins listening, the server:

0. Checks the configuration file's permissions and fails FATAL if they are
   neither `0600` nor `0400`. See the section at the end of this document.
1. Connects to the database and fails FATAL if it cannot.
2. Compares the schema version in `app_version` against what the binary expects
   and refuses to start on a mismatch, pointing at `doenerstag update`.
3. Reads the TLS certificate and key unless `--no-https` is set, and fails FATAL
   if either is missing or unreadable.
4. Fails FATAL if `jwt-secret` is unset or shorter than 32 bytes.
5. Logs a WARN when `--no-https` is set, naming the security implications.

---

## Verb `install`

Sets up the database and writes a configuration file, interactively.

Behaviour:

- Reads any existing configuration file and offers each value found there as the
  default for the corresponding question. Where nothing is configured, the
  built-in default is offered.
- **Before asking anything**, tests that the output file is writable. If the
  output path equals the input path, the test must not truncate or corrupt the
  existing file — it writes to a temporary file in the same directory and
  renames only at the very end.
- Creates the database, the runtime database user and the schema, applies all
  migrations and the seed data.
- Creates the `root` administrator with a password chosen interactively, or
  supplied unattended through `DOENER_ROOT_PASSWORD`, and the deleted-user
  placeholder row. Re-running `install` resets a forgotten `root` password. The
  password must satisfy the rules in
  [05_auth_and_permissions.md](05_auth_and_permissions.md), which are applied
  here rather than only to accounts created later.
- Generates a 32-byte `jwt-secret` and writes it to the configuration file.
- Loads the imprint and legal notes snippets from the files whose paths the
  operator supplies, or inserts a placeholder if a path is left empty.
- Writes a configuration file containing **every** setting with a comment
  explaining it, so the file doubles as documentation.
- Appends a row to `app_version`.

| Flag                        | Short | Default             | Description                                                   |
| --------------------------- | :---: | ------------------- | ------------------------------------------------------------- |
| `--output`                  | `-o`  | value of `--config` | Configuration file to write.                                   |
| `--database-root-user`      | `-R`  | *(prompted)*        | User permitted to create databases and roles. Never stored.    |
| `--database-root-password`  |       | *(prompted)*        | **Never on the command line.** Never stored.                   |
| `--database-admin-user`     | `-A`  | *(prompted)*        | Owner of the database and its objects. Never stored.           |
| `--database-admin-password` |       | *(prompted)*        | **Never on the command line.** Never stored.                   |
| `--root-password`           |       | *(prompted)*        | Password for the doenerstag `root` administrator, not a database one. **Never on the command line.** Never stored. |
| `--imprint-file`            |       | *(prompted)*        | HTML snippet loaded into the imprint page.                     |
| `--legal-notes-file`        |       | *(prompted)*        | HTML snippet loaded into the legal notes page.                 |
| `--non-interactive`         |       | `false`             | Ask nothing. Fails if a required value is missing.             |

The three database identities are distinct on purpose:

| Identity                     | Used for                                              | Stored in the config |
| ---------------------------- | ----------------------------------------------------- | :------------------: |
| root (`-R`)                  | Creating the database and the roles.                   | no                   |
| admin (`-A`)                 | Owning and migrating the schema.                       | no                   |
| runtime (`-U`, global)       | Everything the running server does.                    | yes                  |

The runtime user gets `SELECT`, `INSERT`, `UPDATE`, `DELETE` on the application
tables and nothing else — no `CREATE`, no `DROP`.

---

## Verb `update`

Accepts every flag of `install`.

An untagged development build reports `0.0.0`, has no released version it could
be updating from, and so prints a message saying that and exits with status 1.
The test is `0.0.0` exactly, not "below 1.0.0": the first release is `0.1.0`, and
a major-version test would refuse every upgrade within the 0.x line.

From a released build on, it:

- Applies outstanding `golang-migrate` migrations using the admin identity.
- Rewrites the configuration file, adding settings introduced by the new
  version with their defaults and their explanatory comments, and preserving
  every value the operator has set. Removed settings are commented out rather
  than deleted, with a note.
- Appends a row to `app_version`.
- Can reset the `root` password when asked to.
- Refuses to run against a database whose schema is newer than the binary.

---

## Verb `cleanup`

Removes expired data. Intended to be run from `cron` and safe to run
concurrently with the server.

| Flag          | Short | Default | Description                                                    |
| ------------- | :---: | ------- | -------------------------------------------------------------- |
| `--retention` | `-r`  | `14d`   | Orders whose deadline is older than this are deleted.            |
| `--dry-run`   |       | `false` | Report what would be deleted and change nothing.                 |

Order of operations, each in its own transaction:

1. Delete orders whose `deadline_at` is older than the retention period,
   cascading to their items and item modifications.
2. Physically delete soft-deleted `menu_item_modification` rows no longer
   referenced by any `order_item_modification`.
3. Physically delete soft-deleted `menu_item` rows no longer referenced by any
   `order_item`.
4. Physically delete soft-deleted `menu_category` and `restaurant` rows no
   longer referenced by anything.
5. Delete `image` rows referenced by no restaurant and no menu item.
6. Delete `session` rows past their idle or absolute expiry, and `api_token`
   rows past their expiry.

Every deletion is logged at INFO with the object type and count.

---

## Verb `version`

Prints the newest `app_version` row: the semantic version, the applied schema
version and when it was applied. Connects to the database; use the `--version`
flag instead when the database is unavailable.

---

## Configuration file

YAML. The file `install` generates carries a comment above every setting. An
abridged example:

```yaml
# doenerstag configuration
# Generated by doenerstag install on 2026-09-05T10:14:22Z

database:
  # PostgreSQL host name. Default: localhost
  server: db.example.internal
  # PostgreSQL port. Default: 5432
  port: 5432
  # Database name. Default: doenerstag
  name: doenerstag
  # Database user used at runtime. Default: doener
  user: doener
  # Password for the runtime database user.
  # May also be supplied as DOENER_DATABASE_PASSWORD.
  password: "…"
  # TLS mode for the database connection. Default: prefer
  sslmode: require
  # Upper bound for open and idle database connections. Default: 10
  max_connection_pool: 10

server:
  port: 8443
  bind_address: ""
  no_https: false
  tls_cert: /etc/doenerstag/server.crt
  tls_key: /etc/doenerstag/server.key
  no_swagger: false
  static_dir: static
  http_read_timeout: 30s
  http_write_timeout: 60s
  http_idle_timeout: 120s
  shutdown_grace: 30s
  max_image_size: 5MiB
  cors_allowed_origins: ""

session:
  # Signing key for session tokens. Rotating it logs everyone out.
  # May also be supplied as DOENER_JWT_SECRET.
  jwt_secret: "…"
  idle_timeout: 6h
  absolute_timeout: 7d
  login_rate_limit_user: 10
  login_rate_limit_ip: 60
  login_rate_limit_window: 15m

log:
  level: INFO
  file: /var/log/doenerstag/doenerstag.log

cleanup:
  retention: 14d
```

Nested keys map to flags by joining the path with a dash: `database.server`
is `--database-server`, `session.idle_timeout` is `--idle-timeout`. The
`server.` and `log.` prefixes are dropped for flags that already carry the word
— `server.port` is `--port`, `log.level` is `--log-level`. The generated file is
the authoritative example of the mapping.

## Configuration file permissions

The configuration file holds the database password and the JWT secret. Its
permissions are checked on **every** run of **every** verb, before anything else
happens.

`install` creates the file with mode `0600`. If an existing file has any mode
other than `0600` or `0400`, the application logs a **FATAL** error naming the
file and its actual mode, and exits without reading it.

This is a fatal error rather than a warning on purpose. A warning about a
world-readable file containing a database password is a warning that scrolls
past in a log nobody reads, and the file stays world-readable for years. A
startup failure gets fixed in the thirty seconds it takes to type `chmod 0600`.

`0400` is accepted alongside `0600` because a read-only configuration file is
strictly safer, and failing on it would push operators toward loosening
permissions to satisfy a permissions check.

The check applies to the POSIX permission bits and is therefore skipped on
Windows, where they do not carry the same meaning. A DEBUG line records that it
was skipped.
