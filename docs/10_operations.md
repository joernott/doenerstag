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

## Installation

```sh
# 1. Put the binary and the TLS material in place
install -m 0755 doenerstag /usr/local/bin/
mkdir -p /etc/doenerstag /var/log/doenerstag
cp server.crt server.key /etc/doenerstag/
chmod 0600 /etc/doenerstag/server.key

# 2. Run the interactive installer
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

### Verifying

```sh
doenerstag version                       # what the database says is installed
doenerstag --version                     # what the binary is
curl -sk https://localhost:8443/api/v1/health
```

## Running as a service

A `systemd` unit is the expected deployment:

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

## Log rotation

Rotation is external. The application reopens its log file on `SIGHUP`.

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
unreferenced as a result. Run it from `cron`, or from a systemd timer:

```
17 3 * * *  doenerstag  /usr/local/bin/doenerstag cleanup -c /etc/doenerstag/doenerstag.yaml
```

It is safe to run while the server is running, and safe to run twice. Use
`--dry-run` to see what a run would remove.

Independently of the scheduled run, the creator of an order and the
administrator can delete an order from the UI at any time after it has expired.

## Updating

```sh
systemctl stop doenerstag
install -m 0755 doenerstag-new /usr/local/bin/doenerstag
doenerstag update -c /etc/doenerstag/doenerstag.yaml
systemctl start doenerstag
```

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
