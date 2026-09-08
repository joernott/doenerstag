# 10 Operations

## Prerequisites

| Component  | Requirement                                                   |
| ---------- | ------------------------------------------------------------- |
| PostgreSQL | Version 18. No other version is supported.                     |
| OS         | Any platform Go cross-compiles to. Linux is the reference.     |
| TLS        | A certificate and key, from the organization's internal CA or self-signed. |
| Network    | Reachable from the intranet. No outbound internet access needed. |

A release is one static binary. `static/` is embedded, migrations are embedded,
seed data is inside the migrations. Nothing is downloaded at install or run time.

Every release is published in four forms, all built from the same binary:

| Artefact                          | For                                            |
| --------------------------------- | ---------------------------------------------- |
| `doenerstag_<ver>_<arch>.deb`     | Debian, Ubuntu and derivatives.                 |
| `doenerstag-<ver>-1.<arch>.rpm`   | RHEL, Fedora, Rocky, Alma, openSUSE.            |
| `doenerstag-<ver>-<os>-<arch>`    | Bare binary for everything else.                |
| `ghcr.io/joernott/doenerstag:<ver>` | Container image, `amd64` and `arm64`.         |

## Installation

Step 1 has three variants — a distribution package, a plain binary, or a
container. Step 2 is the same in every case.

### Step 1a — Debian and Ubuntu package (preferred)

```sh
apt install ./doenerstag_1.0.0_amd64.deb
```

### Step 1b — RHEL, Fedora, Rocky, Alma and openSUSE package (preferred)

```sh
dnf install ./doenerstag-1.0.0-1.x86_64.rpm
```

Both packages are built from the same source with `nfpm`, so their contents stay
in step with each other, and both are produced by `make packages` in CI for
every release.

They install:

| Path                                          | Contents                                                     |
| --------------------------------------------- | ------------------------------------------------------------ |
| `/usr/bin/doenerstag`                          | The binary, built with `-tags embedstatic`.                   |
| `/etc/doenerstag/doenerstag.yaml`              | Config template, mode `0600`, marked as a config file so an upgrade never overwrites a modified one. |
| `/etc/logrotate.d/doenerstag`                  | The rotation rule from the section below.                     |
| `/etc/cron.d/doenerstag`                       | The nightly `cleanup` job.                                    |
| `/usr/lib/systemd/system/doenerstag.service`   | The unit from the section below.                              |
| `/usr/share/doc/doenerstag/`                   | `README`, `LICENSE`, `THIRD_PARTY_LICENSES` and the sample imprint and legal notes snippets. |
| `/var/log/doenerstag/`                         | Log directory, owned by the service user.                     |

The packages also create the system user and group `doenerstag` with no login
shell and no home directory, and they declare a dependency on
`postgresql-client` for the convenience of `psql` on the host. They do **not**
depend on a PostgreSQL server — the database usually lives elsewhere.

Neither package starts or enables the service. It cannot work until step 2 has
run, so the post-install script prints the next command instead of failing a
service start.

Removing the package leaves `/etc/doenerstag/doenerstag.yaml`, the log directory
and the database untouched. `apt purge` / `dnf remove` plus an explicit
`rm -rf /etc/doenerstag` is the full uninstall; the database is never dropped by
a package operation.

### Step 1c — plain binary

For platforms with neither package format:

```sh
install -m 0755 doenerstag /usr/local/bin/
mkdir -p /etc/doenerstag /var/log/doenerstag
cp server.crt server.key /etc/doenerstag/
chmod 0600 /etc/doenerstag/server.key
useradd --system --no-create-home --shell /usr/sbin/nologin doenerstag
chown -R doenerstag:doenerstag /var/log/doenerstag
```

The systemd unit, the `logrotate` rule and the cron entry then have to be
installed by hand from the sections below.

### Step 1d — container

See [Running in Docker](#running-in-docker).

### Step 2 — run the installer

Identical for every variant:

```sh
cd /etc/doenerstag
DOENER_DATABASE_ROOT_PASSWORD='…' doenerstag install -o /etc/doenerstag/doenerstag.yaml
```

The installer asks for the database connection, the privileged database
identities, the `root` administrator password, and the paths to the imprint and
legal notes HTML snippets. It then creates the database and its runtime user,
applies all migrations including the seed data, writes the configuration file
with mode `0600`, and appends a row to `app_version`.

The privileged database credentials are used once and never stored. See
[09_configuration.md](09_configuration.md) for why they may not be passed on the
command line.

Leaving the two snippet paths empty is fine: the migration seeds a placeholder
for each, and the `root` account can replace both from the imprint and legal
notes pages in the browser afterwards. Whichever way they arrive, the HTML goes
through the same allow-list.

### Step 3 — start it

```sh
systemctl enable --now doenerstag
```

### Verifying

```sh
doenerstag version                       # what the database says is installed
doenerstag --version                     # what the binary is
curl -sk https://localhost:8443/api/v1/health
```

## Running as a service

A `systemd` unit is the expected deployment. The `.deb` and `.rpm` packages
install exactly this file as
`/usr/lib/systemd/system/doenerstag.service`; with the plain binary it has to be
created by hand.

```ini
[Unit]
Description=doenerstag food order coordination
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=doenerstag
Group=doenerstag
WorkingDirectory=/etc/doenerstag
ExecStart=/usr/local/bin/doenerstag server -c /etc/doenerstag/doenerstag.yaml
ExecReload=/bin/kill -HUP $MAINPID
Restart=on-failure
RestartSec=5s

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=/var/log/doenerstag
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
```

Port 8443 is above 1024, so no capability to bind a privileged port is needed.
If port 443 is wanted, put a reverse proxy in front rather than granting
`CAP_NET_BIND_SERVICE`.

### Graceful shutdown

On `SIGTERM` or `SIGINT`, and on `POST /api/v1/shutdown`, the server stops
accepting new connections, closes all open SSE streams, waits up to
`--shutdown-grace` (default 30 s) for in-flight requests, closes the database
pool and exits with status 0. A shutdown that exceeds the grace period is logged
at WARN and forced.

`POST /api/v1/shutdown` responds `202 Accepted` first and begins shutting down
afterwards, so the administrator sees a confirmation rather than a dropped
connection.

## Running in Docker

A container image is published alongside the packages for every release. It
suits a quick evaluation, a development environment, or a host where installing
packages is not wanted.

### The image

Built from a two-stage `Dockerfile`. The build stage compiles the frontend and
then the Go binary with `-tags embedstatic`; the runtime stage is `scratch` with
nothing but the binary, `ca-certificates` and `/etc/passwd` for the unprivileged
user. The result is one static binary in an otherwise empty image, which is
possible precisely because the assets are embedded and nothing is fetched at
runtime.

| Property         | Value                                                          |
| ---------------- | -------------------------------------------------------------- |
| Image            | `ghcr.io/joernott/doenerstag:<version>`, plus `:latest`         |
| Architectures    | `linux/amd64`, `linux/arm64`                                    |
| User             | `65532:65532`, non-root                                          |
| Entrypoint       | `/doenerstag`                                                    |
| Default command  | `server`                                                         |
| Exposed port     | `8443`                                                           |
| Working directory| `/config`                                                        |
| Volumes          | `/config` for the configuration file and the TLS material        |
| Health check     | `doenerstag --version`, since `scratch` has no `curl` or shell   |

The image contains no shell. `docker exec … sh` will not work, which is the
point; run another container from the same image with a different command
instead.

### docker compose

```yaml
services:
  db:
    image: postgres:18-alpine
    environment:
      POSTGRES_DB: doenerstag
      POSTGRES_USER: doener
      POSTGRES_PASSWORD_FILE: /run/secrets/db_password
    secrets: [db_password]
    volumes:
      - pgdata:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U doener -d doenerstag"]
      interval: 5s
      retries: 10

  doenerstag:
    image: ghcr.io/joernott/doenerstag:1.0.0
    depends_on:
      db: { condition: service_healthy }
    command: ["server", "-c", "/config/doenerstag.yaml"]
    environment:
      DOENER_DATABASE_SERVER: db
    ports:
      - "8443:8443"
    volumes:
      - ./config:/config:ro
    restart: unless-stopped

  cleanup:
    image: ghcr.io/joernott/doenerstag:1.0.0
    depends_on:
      db: { condition: service_healthy }
    command: ["cleanup", "-c", "/config/doenerstag.yaml"]
    environment:
      DOENER_DATABASE_SERVER: db
    volumes:
      - ./config:/config:ro
    profiles: ["cleanup"]

secrets:
  db_password:
    file: ./secrets/db_password

volumes:
  pgdata:
```

First run, once the database is up:

```sh
docker compose up -d db
docker compose run --rm \
  -e DOENER_DATABASE_ROOT_PASSWORD \
  -v "$PWD/config:/config" \
  doenerstag install -o /config/doenerstag.yaml
docker compose up -d doenerstag
```

The `cleanup` service sits behind a compose profile so it never starts with the
stack. Run it from the host's `cron`:

```
17 3 * * *  cd /srv/doenerstag && docker compose run --rm cleanup
```

Compose has no scheduler of its own; putting the schedule on the host is the
simplest thing that works. On Kubernetes it is a `CronJob` running the same
image with the same command.

### Things to get right

- **The config file must be mode `0600` or `0400`**, or the application exits
  FATAL ([09_configuration.md](09_configuration.md)). Mounting `./config` from a
  host directory means the host's permissions are what count. `chmod 0600
  config/doenerstag.yaml` before the first start.
- **The config file is mounted read-only** for `server` and `cleanup`, and must
  be writable for `install` and `update`. The commands above differ in exactly
  that.
- **`install` is interactive.** Use `docker compose run` — not `up` — so that
  stdin is attached, or pass every value through `DOENER_*` variables and
  `--non-interactive`.
- **TLS.** Put the certificate and key in the mounted `/config` directory and
  point `--tls-cert` / `--tls-key` at them. `--no-https` is acceptable only when
  a TLS-terminating proxy sits in front, and it logs a WARN.
- **Logs go to stdout.** Leave `--log-file` unset in a container so the runtime's
  logging driver collects them; `logrotate` has no place here.
- **Database data lives in the `pgdata` volume**, so back it up with
  `pg_dump` from a one-off container exactly as described below. A container
  restart is not a backup.
- **`docker compose down -v` destroys the database.** The volume holds every
  order, user and image.

## Log rotation

Rotation is external, and applies to package and plain-binary installs. In a
container, logs go to stdout and this section does not apply.

The `.deb` and `.rpm` packages install the rule below as
`/etc/logrotate.d/doenerstag`. The application reopens its log file on `SIGHUP`.

```
/var/log/doenerstag/doenerstag.log {
    daily
    rotate 14
    compress
    delaycompress
    missingok
    notifempty
    create 0640 doenerstag doenerstag
    postrotate
        systemctl reload doenerstag > /dev/null 2>&1 || true
    endscript
}
```

Do **not** use `copytruncate`. The `SIGHUP` reopen is the supported mechanism
and loses no lines.

## Cleanup

`cleanup` deletes orders past the retention period and the data that becomes
unreferenced as a result.

The packages install this as `/etc/cron.d/doenerstag`:

```
17 3 * * *  doenerstag  /usr/bin/doenerstag cleanup -c /etc/doenerstag/doenerstag.yaml
```

With the plain binary, add the equivalent cron entry or a systemd timer by hand.
The path is `/usr/local/bin/doenerstag` in that case.

It is safe to run while the server is running, and safe to run twice. Use
`--dry-run` to see what a run would remove.

Scheduling this is not optional housekeeping. An installation where `cleanup`
never runs keeps every order forever, which quietly breaks the retention period
promised in the privacy notice — see
[13_legal_and_privacy.md](13_legal_and_privacy.md). Shipping the cron entry in
the packages is how that is made hard to forget.

Independently of the scheduled run, the creator of an order and the
administrator can delete an order from the UI at any time after it has expired.

## Updating

Package install:

```sh
systemctl stop doenerstag
apt install ./doenerstag_1.1.0_amd64.deb      # or: dnf upgrade ./doenerstag-1.1.0-1.x86_64.rpm
doenerstag update -c /etc/doenerstag/doenerstag.yaml
systemctl start doenerstag
```

Plain binary:

```sh
systemctl stop doenerstag
install -m 0755 doenerstag-new /usr/local/bin/doenerstag
doenerstag update -c /etc/doenerstag/doenerstag.yaml
systemctl start doenerstag
```

Container:

```sh
docker compose pull
docker compose run --rm -v "$PWD/config:/config" doenerstag update -c /config/doenerstag.yaml
docker compose up -d doenerstag
```

The package upgrade replaces the binary and the unit file but never the
configuration file — it is marked as a config file in both formats, so a
modified `doenerstag.yaml` survives. `doenerstag update` is what brings that file
forward, and it must be run in every variant.

`update` applies outstanding migrations and rewrites the configuration file,
adding new settings with their defaults and comments while preserving every
value already set. Settings that no longer exist are commented out with a note
rather than dropped.

The server verb refuses to start when the schema version does not match what the
binary expects, so a forgotten `update` fails loudly at startup rather than
producing subtle errors later.

Take a backup before updating. `update` does not roll back on its own; recovery
from a failed migration is a restore.

## Backup and restore

Everything worth keeping is in PostgreSQL, including the images and the imprint
and legal notes pages. The configuration file holds the JWT secret and the
database password and belongs in the backup too.

```sh
pg_dump --format=custom --file=doenerstag-$(date +%F).dump doenerstag
cp /etc/doenerstag/doenerstag.yaml /backup/
```

```sh
systemctl stop doenerstag
dropdb doenerstag && createdb -O doenerstag_admin doenerstag
pg_restore --dbname=doenerstag doenerstag-2026-09-05.dump
systemctl start doenerstag
```

Restoring a backup with a different `jwt_secret` than the running configuration
logs everyone out. That is harmless.

Because images live in the database, the dump grows with them. With the expected
scale — a few hundred menu items — this stays in the low tens of megabytes.

## Monitoring

| Check                              | Endpoint / method                                       |
| ---------------------------------- | ------------------------------------------------------- |
| Is it up and is the database up?   | `GET /api/v1/health` — 200 `ok`, or 503 `error`.         |
| Object counts, pool usage          | `GET /api/v1/metrics`                                    |
| Version drift                      | `GET /api/v1/version` against the expected release.      |
| Errors                             | Log lines with `"level":"error"` or `"fatal"`.           |

Both `health` and `metrics` are public — they expose no personal data, only
counts, and requiring a credential would make them harder to wire into a
monitoring system than the information is worth.

Alerting worth setting up: `health` non-200 for more than a minute;
`db_connections_open` at `db_connections_max` for a sustained period; any
`fatal` line.

## The audit trail

There is no separate audit table. The INFO-level log is the audit trail.

Every data-changing operation logs one line carrying the acting user, the
request ID, the object type and id, and what changed. Because the request ID
also appears in the response header and in any error body, a user's report of
"it went wrong just now" can be traced to the exact request.

Retaining that trail is therefore a matter of retaining logs. The `logrotate`
example above keeps 14 days, matching the default order retention.

The `created_by` and `updated_by` columns on every table are a convenience for
the UI, not the audit trail — they hold only the most recent writer and are
nulled when that user is deleted.

## Troubleshooting

| Symptom                                          | Cause and remedy                                                            |
| ------------------------------------------------ | --------------------------------------------------------------------------- |
| Server exits FATAL at startup, schema mismatch    | Run `doenerstag update`.                                                     |
| Server exits FATAL, "jwt-secret unset"            | The configuration file was regenerated or the secret was removed. Restore it, or set a new one — everyone will be logged out. |
| Everyone is logged out after a restart            | `jwt_secret` differs from the previous run. Check the configuration file.     |
| A user cannot log in from a second browser        | Working as designed. One session per user; the newer login wins.              |
| Images fail to upload                             | Larger than `--max-image-size`, or not JPEG/PNG/GIF. The error body names which. |
| Order page stops updating live                    | The SSE stream was cut by an intermediate proxy. The browser reconnects on its own; if a proxy buffers `text/event-stream`, configure it not to. |
| Frontend changes do not appear                    | The binary was built with `-tags embedstatic`, so `--static-dir` is ignored. Rebuild without the tag for development. |
| Log floods after a level change                   | `DEBUG` logs every function call and every query. It is a diagnostic level, not an operational one. |
