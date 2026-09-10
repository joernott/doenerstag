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
|  `docker.io/joernott/doenerstag:<ver>` | Container image, `amd64` and `arm64`.         |

## Installation

Step 1 has three variants — a distribution package, a plain binary, or a
container. Step 2 is the same in every case.

### Step 1a — Debian and Ubuntu package (preferred)

```sh
apt install ./doenerstag_0.3.0-1_amd64.deb
```

### Step 1b — RHEL, Fedora, Rocky, Alma and openSUSE package (preferred)

```sh
dnf install ./doenerstag-0.3.0-1.x86_64.rpm
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
shell and no home directory, and they depend on the PostgreSQL client tools so
that `psql` and `pg_dump` — which the backup, restore and troubleshooting
sections below tell you to use — are present. That package is
`postgresql-client` on Debian and Ubuntu and `postgresql` on the RPM
distributions; nothing on an RPM distribution provides the Debian name. Neither
package depends on a PostgreSQL **server**: the database usually lives
elsewhere.

Neither package starts or enables the service. It cannot work until step 2 has
run, so the post-install script prints the next command instead of failing a
service start.

Removal never touches the database, and never destroys evidence or a password
you chose. What exactly survives differs between the two formats, because the
two packaging systems differ:

| Removing…                       | `.deb`                                  | `.rpm`                                              |
| ------------------------------- | --------------------------------------- | --------------------------------------------------- |
| A modified `doenerstag.yaml`    | left in place, unchanged                | renamed to `doenerstag.yaml.rpmsave`                |
| An unmodified `doenerstag.yaml` | left in place                           | removed, along with `/etc/doenerstag`               |
| `/var/log/doenerstag` with logs | kept                                    | kept                                                |
| `/var/log/doenerstag` when empty | removed                                | removed                                             |

A real installation always has a modified configuration file, because
`doenerstag install` writes it. So on an RPM distribution, expect to find your
settings in `doenerstag.yaml.rpmsave` after a removal. `apt purge` / `dnf
remove` plus an explicit `rm -rf /etc/doenerstag` is the full uninstall; the
database is never dropped by a package operation.

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
ExecStart=/usr/bin/doenerstag server -c /etc/doenerstag/doenerstag.yaml
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

`ExecStart` points at `/usr/bin/doenerstag`, which is where the packages put
the binary. A plain-binary install ([step 1c](#step-1c--plain-binary)) puts it
in `/usr/local/bin` and has to change that one line.

Port 8443 is above 1024, so no capability to bind a privileged port is needed.
If port 443 is wanted, put a reverse proxy in front rather than granting
`CAP_NET_BIND_SERVICE`.

### Graceful shutdown

On `SIGTERM` or `SIGINT`, and on `POST /api/v1/shutdown`, the server stops
accepting new connections, closes all open SSE streams, waits up to
`--shutdown-grace` (default 30 s) for in-flight requests, closes the database
pool and exits with status 0. A shutdown that exceeds the grace period is logged
at WARN and forced.

`POST /api/v1/shutdown` is administrator only. It answers 2000 to an anonymous
caller and 3000 to a logged-in one, like every other administrator route.

It responds `202 Accepted` first and begins shutting down afterwards, so the
administrator sees a confirmation rather than a dropped connection.

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
| Image            | `docker.io/joernott/doenerstag:<version>`, plus `:latest`         |
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

The stack is [`packaging/docker-compose.yml`](../packaging/docker-compose.yml)
with [`packaging/docker-compose.env`](../packaging/docker-compose.env) next to
it as `.env`. It is not a sketch: the release is tested by running exactly these
files through the sequence below. It defines six services: `db`, the
`doenerstag` server, and four one-shot jobs behind profiles so that none of them
starts with the stack -- `install`, `update`, `import` and `cleanup`.

Two things about it are easy to get wrong and are worth stating:

- **The database container gets no `POSTGRES_DB` or `POSTGRES_USER`.** Only the
  superuser password. `doenerstag install` creates the `doenerstag` database and
  the `doener` runtime role itself, and cannot do that if the image has already
  created them.
- **The volume is mounted at `/var/lib/postgresql`, not `/var/lib/postgresql/data`.**
  From PostgreSQL 18 the official image keeps the cluster in a version-named
  subdirectory, and refuses to start when a volume covers the old path.

#### First run

```sh
mkdir -p /srv/doenerstag && cd /srv/doenerstag
cp …/packaging/docker-compose.yml .
cp …/packaging/docker-compose.env .env
mkdir -p config secrets

# The database superuser password, read by the db container as a secret and
# handed to install as DOENER_DATABASE_ROOT_PASSWORD.
openssl rand -base64 24 | tr -d '\n' > secrets/db_password
chmod 0600 secrets/db_password

# TLS. The defaults are the relative paths server.crt and server.key, and the
# container's working directory is /config, so these names need no configuration
# at all. Replace the self-signed pair with the real certificate when there is
# one.
openssl req -x509 -newkey rsa:2048 -nodes -days 365 \
  -subj "/CN=doenerstag.example" -keyout config/server.key -out config/server.crt
chmod 0644 config/server.crt && chmod 0600 config/server.key

# install runs as 65532 and writes into config/.
chown -R 65532:65532 config

docker compose up -d db
DOENER_DATABASE_ROOT_PASSWORD="$(cat secrets/db_password)" \
  docker compose run --rm -e DOENER_DATABASE_ROOT_PASSWORD install
docker compose up -d doenerstag
```

The `install` service mounts `config/` writable and runs
`install -o /config/doenerstag.yaml -R postgres`; every other service mounts it
read-only. Run interactively it asks for the remaining passwords. To script it,
pass them as environment variables and spell the whole command out:

```sh
docker compose run --rm \
  -e DOENER_DATABASE_ROOT_PASSWORD -e DOENER_DATABASE_ADMIN_PASSWORD \
  -e DOENER_DATABASE_PASSWORD -e DOENER_ROOT_PASSWORD \
  install \
  install -o /config/doenerstag.yaml -R postgres --non-interactive
```

The verb is repeated because `docker compose run SERVICE ARGS…` **replaces** the
service's command rather than appending to it. `docker compose run --rm install
--non-interactive` fails with `unknown flag`, which is confusing until you know
that rule.

#### Importing a restaurant

A menu is an hour of typing and should only be typed once, so `restaurant
export` writes one out and `restaurant import` reads it back — on another
installation, or on this one after a rebuild. The `import` service is that verb
with the file mounted:

```sh
mkdir -p import
cp …/ali_baba.json import/
DOENERSTAG_IMPORT_FILE=ali_baba.json docker compose run --rm import
```

Which file is an environment variable rather than an argument because
`docker compose run SERVICE ARGS…` **replaces** the command rather than
appending to it — the same rule that makes `install` awkward to script.
`DOENERSTAG_IMPORT_ARGS` carries anything else the verb takes:

```sh
DOENERSTAG_IMPORT_FILE=ali_baba.json DOENERSTAG_IMPORT_ARGS=--overwrite \
  docker compose run --rm import
```

**`--overwrite` deletes the existing restaurant and every order placed at it**
before writing the new one. A restaurant whose id is already present is
otherwise skipped and the run continues with the rest of the file.

The file has to be readable by uid 65532, the unprivileged user the container
runs as -- the same rule as `config/`, and the same failure if it is not:
`permission denied` on a file that is plainly there. A file arriving with mode
`0640` from somewhere else is the usual cause.

#### The cleanup job

`cleanup` sits behind a compose profile so it never starts with the stack. Run
it from the host's `cron`:

```
17 3 * * *  cd /srv/doenerstag && docker compose run --rm cleanup
```

Compose has no scheduler of its own; putting the schedule on the host is the
simplest thing that works. On Kubernetes it is a `CronJob` running the same
image with the same command.

### docker compose behind Traefik

[`packaging/docker-compose.traefik.yml`](../packaging/docker-compose.traefik.yml)
is the same stack with [Traefik](https://traefik.io) in front, holding a Let's
Encrypt certificate. Its `.env` is
[`packaging/docker-compose.traefik.env`](../packaging/docker-compose.traefik.env).

The difference is where TLS ends. In the plain file the application holds the
certificate and serves HTTPS on 8443. Here Traefik holds it, answers 443, and
speaks plain HTTP to the application over a network that is not published at
all.

Use it when the host has a public name and port 80 reachable from the internet,
which is what the ACME HTTP challenge needs. Use the plain file when it does
not: an internal network with a self-signed or organisation-issued certificate
is not a worse deployment, it is a different one.

```sh
mkdir -p /srv/doenerstag && cd /srv/doenerstag
cp …/packaging/docker-compose.traefik.yml docker-compose.yml
cp …/packaging/docker-compose.traefik.env .env
$EDITOR .env                     # DOENERSTAG_HOST and ACME_EMAIL are required
mkdir -p config secrets import

openssl rand -base64 24 | tr -d '\n' > secrets/db_password
chmod 0600 secrets/db_password
chown -R 65532:65532 config

docker compose up -d db
DOENER_DATABASE_ROOT_PASSWORD="$(cat secrets/db_password)" \
  docker compose run --rm -e DOENER_DATABASE_ROOT_PASSWORD install
docker compose up -d
```

There is no `openssl req` step: the certificate is Traefik's problem now.

Three things about this file are worth stating, because each is a way to end up
with a site that looks like it works:

- **The application is told twice that it is behind a proxy.** `--no-https`
  stops it looking for a certificate. `--behind-tls-proxy` tells it that the
  browser's side of the connection is encrypted anyway, so the session cookie
  keeps its `Secure` flag and HSTS is still sent. Without the second flag the
  deployment is a working site whose session cookie has quietly stopped being
  marked `Secure`, which nothing in a browser will tell you.

  Set `--behind-tls-proxy` **without** a proxy in front and the opposite
  happens: the browser refuses to return a `Secure` cookie over plain HTTP, so
  logging in appears to succeed and the next request is anonymous.

- **`DOENERSTAG_HOST` has to resolve to this host from the public internet
  before the first `docker compose up`**, and port 80 has to reach Traefik. The
  ACME HTTP challenge is answered there. It is also the `Host()` rule of the
  router and the base of every link in a password-reset mail, so a wrong value
  produces a 404 from Traefik rather than a certificate error.

- **Let's Encrypt rate-limits certificates per registered domain per week.**
  While setting this up, uncomment the `caserver` line in the compose file to
  use the staging endpoint. Its certificates are not trusted by browsers, which
  is the point: you are testing the plumbing, not the certificate. Comment it
  out and delete the `acme` volume to switch to the real one.

The `install`, `update`, `import` and `cleanup` jobs are the same as in the
plain file and are run the same way. `install` here also writes the base URL and
the two TLS settings into the configuration file, taking them from the
environment, because `install` is a different verb from `server` and does not
carry the server's flags.

### Things to get right

- **`config/` must belong to uid 65532.** The container runs unprivileged, and
  `install` writes the configuration file as that user. Without the `chown` the
  install step fails with a permission error and nothing else works.
- **The config file must be mode `0600` or `0400`**, or the application exits
  FATAL ([09_configuration.md](09_configuration.md)). `install` writes it that
  way; a file copied in by hand may not be.
- **The config file is mounted read-only** for `doenerstag` and `cleanup`, and
  writable only for `install`. That is the whole difference between the service
  definitions.
- **TLS.** The certificate and key go in `config/` as `server.crt` and
  `server.key`, the names the defaults already use. `--no-https` is acceptable
  only when a TLS-terminating proxy sits in front, and it logs a WARN.
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

The configuration file is readable only by the service user, and so is the log
it names, so `update` is run as `root` or with `sudo -u doenerstag`. Run as
somebody else it stops at the configuration file, which is the security rule
doing its job. It no longer stops at the **log** file: an administrative verb
that cannot write the log warns and writes to standard error instead, because
refusing to migrate a database over a log line helps nobody.

Plain binary:

```sh
systemctl stop doenerstag
install -m 0755 doenerstag-new /usr/local/bin/doenerstag
doenerstag update -c /etc/doenerstag/doenerstag.yaml
systemctl start doenerstag
```

Container:

```sh
# The tag lives in .env, so an upgrade starts by editing one line there.
sed -i 's/^DOENERSTAG_VERSION=.*/DOENERSTAG_VERSION=0.3.0/' .env
docker compose pull
# update is a service of its own, behind a profile, with the writable config
# mount it needs. It asks for the password of the role that owns the schema,
# which is never stored; set DOENER_DATABASE_ADMIN_PASSWORD and add
# --non-interactive to script it.
docker compose run --rm update
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
