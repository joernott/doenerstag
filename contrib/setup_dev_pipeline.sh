#!/bin/bash
#
# Provision a Debian machine for doenerstag development.
#
# This script is the definition of the development environment: if the build and
# test pipeline needs a tool and this script does not install it, that is a bug
# in this script. It installs what the pipeline needs today and what the later
# sprints in docs/14_implementation_plan.md are already known to need.
#
# Safe to run more than once. Every step checks before it acts, so a re-run
# after adding a tool installs only the new one.
#
# Usage:
#   sudo ./contrib/setup_dev_pipeline.sh
#
# Environment overrides:
#   GO_VERSION          Go toolchain to install            (default 1.26.6)
#   NODE_MAJOR          Node.js major version              (default 22)
#   DEV_USER            user to grant docker access to     (default the invoking user)
#   POSTGRES_PASSWORD   password for the postgres superuser
#   TEST_DB_PASSWORD    password for the doenerstag test role
#   SKIP_UPGRADE=1      skip apt upgrade, for a faster re-run

set -euo pipefail

GO_VERSION="${GO_VERSION:-1.26.6}"
NODE_MAJOR="${NODE_MAJOR:-22}"
DEV_USER="${DEV_USER:-${SUDO_USER:-$(id -un)}}"
TEST_DB_NAME="${TEST_DB_NAME:-doenerstag_test}"
TEST_DB_USER="${TEST_DB_USER:-doener}"
TEST_DB_PASSWORD="${TEST_DB_PASSWORD:-doener}"

export DEBIAN_FRONTEND=noninteractive

# Go tools are installed into GOBIN and symlinked here so that every user, and
# any non-login shell such as a CI runner, finds them.
GO_ROOT=/usr/local/go
GO_TOOL_PATH=/usr/local/go-tools
SHARED_BIN=/usr/local/bin

log()  { printf '\n\033[1;34m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
have() { command -v "$1" >/dev/null 2>&1; }

if [ "$(id -u)" -ne 0 ]; then
  echo "This script must run as root. Re-running under sudo." >&2
  exec sudo --preserve-env=GO_VERSION,NODE_MAJOR,DEV_USER,POSTGRES_PASSWORD,TEST_DB_PASSWORD,SKIP_UPGRADE "$0" "$@"
fi

# -----------------------------------------------------------------------------
# Base system
# -----------------------------------------------------------------------------

install_base() {
  log "Base packages"
  apt update
  if [ "${SKIP_UPGRADE:-0}" != "1" ]; then
    apt -y upgrade
  fi

  # build-essential is not optional: cgo, and therefore `go test -race`, needs a
  # C toolchain. The race detector is a large part of why this VM exists, since
  # the Windows workstation cannot run it.
  apt install -y \
    build-essential \
    ca-certificates \
    curl \
    git \
    gnupg \
    jq \
    logrotate \
    make \
    openssl \
    pkg-config \
    rsync \
    tar \
    unzip
}

# -----------------------------------------------------------------------------
# Go toolchain
# -----------------------------------------------------------------------------

install_go() {
  local current=""
  if [ -x "$GO_ROOT/bin/go" ]; then
    current="$("$GO_ROOT/bin/go" version | awk '{print $3}' | sed 's/^go//')"
  fi

  if [ "$current" = "$GO_VERSION" ]; then
    log "Go $GO_VERSION already installed"
    return
  fi

  log "Installing Go $GO_VERSION (found: ${current:-none})"

  # Debian's golang-go lags the release the project targets, and
  # docs/08_technologies.md pins a minimum. Take the upstream tarball so the VM
  # and CI agree on the toolchain.
  local arch tarball url
  case "$(dpkg --print-architecture)" in
    amd64) arch=amd64 ;;
    arm64) arch=arm64 ;;
    *) echo "unsupported architecture: $(dpkg --print-architecture)" >&2; exit 1 ;;
  esac

  tarball="go${GO_VERSION}.linux-${arch}.tar.gz"
  url="https://go.dev/dl/${tarball}"

  curl -fsSL --retry 3 -o "/tmp/${tarball}" "$url"
  rm -rf "$GO_ROOT"
  tar -C /usr/local -xzf "/tmp/${tarball}"
  rm -f "/tmp/${tarball}"

  # A profile entry for interactive shells, plus symlinks so that non-login
  # shells and `sudo go` find it without one.
  cat >/etc/profile.d/go.sh <<EOF
export PATH="\$PATH:${GO_ROOT}/bin:${GO_TOOL_PATH}/bin"
export GOPATH="\${HOME}/go"
export PATH="\$PATH:\${GOPATH}/bin"
EOF
  chmod 0644 /etc/profile.d/go.sh

  ln -sf "$GO_ROOT/bin/go" "$SHARED_BIN/go"
  ln -sf "$GO_ROOT/bin/gofmt" "$SHARED_BIN/gofmt"
}

# go_install installs a Go tool into a shared location and links it onto PATH.
# Called with the binary name and the module path.
go_install() {
  local name="$1" module="$2"

  if have "$name"; then
    log "$name already installed"
    return
  fi

  log "Installing $name"
  mkdir -p "$GO_TOOL_PATH"
  if ! GOPATH="$GO_TOOL_PATH" GOBIN="$GO_TOOL_PATH/bin" \
       GOFLAGS=-mod=mod "$GO_ROOT/bin/go" install "$module"; then
    warn "could not install $name from $module; continuing"
    return
  fi
  ln -sf "$GO_TOOL_PATH/bin/$name" "$SHARED_BIN/$name"
}

install_go_tools_now() {
  log "Go tools needed by the current pipeline"

  # golangci-lint v2 lives under a /v2 module path. The pinned version keeps the
  # VM and the CI workflow reporting the same findings.
  go_install golangci-lint "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest"
  go_install govulncheck "golang.org/x/vuln/cmd/govulncheck@latest"
}

install_go_tools_later() {
  log "Go tools later sprints already need"

  # Sprint 2 and 3: the migration CLI. The application uses golang-migrate as a
  # library, but the CLI is what a developer reaches for to inspect or force a
  # schema version by hand.
  go_install migrate \
    "github.com/golang-migrate/migrate/v4/cmd/migrate@latest"

  # Sprint 15: .deb and .rpm from one description, and the licence audit that
  # gates a release.
  go_install nfpm "github.com/goreleaser/nfpm/v2/cmd/nfpm@latest"
  go_install go-licenses "github.com/google/go-licenses@latest"
}

# -----------------------------------------------------------------------------
# PostgreSQL 18
# -----------------------------------------------------------------------------

install_postgresql() {
  if have psql && psql --version | grep -q ' 18\.'; then
    log "PostgreSQL 18 already installed"
  else
    log "Installing PostgreSQL 18"
    apt install -y postgresql-common
    /usr/share/postgresql-common/pgdg/apt.postgresql.org.sh -y
    apt install -y postgresql-18 postgresql-client-18
  fi

  configure_postgresql
}

configure_postgresql() {
  local conf=/etc/postgresql/18/main/postgresql.conf
  local hba=/etc/postgresql/18/main/pg_hba.conf
  local changed=0

  if [ ! -f "$conf" ]; then
    warn "$conf not found; skipping PostgreSQL configuration"
    return
  fi

  log "Configuring PostgreSQL"

  if [ -n "${POSTGRES_PASSWORD:-}" ]; then
    sudo -u postgres psql -qc \
      "ALTER USER postgres PASSWORD '${POSTGRES_PASSWORD}';"
  fi

  # Listen on all interfaces so the workstation can reach the database.
  #
  # The pattern must include the quotes around localhost. Writing this with
  # nested single quotes inside a single-quoted sed expression silently strips
  # them, leaving a pattern that matches nothing and a server still bound to
  # loopback. Double quotes around the expression avoid that.
  if grep -qE "^#[[:space:]]*listen_addresses" "$conf"; then
    sed -i -E "s|^#[[:space:]]*listen_addresses[[:space:]]*=.*|listen_addresses = '*'\t\t# set by setup_dev_pipeline.sh|" "$conf"
    changed=1
  fi

  # Appending unconditionally would add a duplicate rule on every run.
  local rule="host    all             all             192.168.178.0/24        scram-sha-256"
  if ! grep -qF "192.168.178.0/24" "$hba"; then
    echo "$rule" >>"$hba"
    changed=1
  fi

  if [ "$changed" -eq 1 ]; then
    systemctl restart postgresql
  fi
  systemctl enable postgresql >/dev/null 2>&1 || true
}

# create_test_database provisions the role and database that
# DOENER_TEST_DATABASE_URL points at.
#
# Sprint 2's test harness prefers testcontainers and falls back to this when
# Docker is unavailable. Having it present means the fallback path is exercised
# rather than assumed to work.
create_test_database() {
  log "Test role and database"

  local role_exists db_exists
  role_exists="$(sudo -u postgres psql -tAc \
    "SELECT 1 FROM pg_roles WHERE rolname = '${TEST_DB_USER}'" || echo "")"
  if [ "$role_exists" != "1" ]; then
    sudo -u postgres psql -qc \
      "CREATE ROLE ${TEST_DB_USER} LOGIN PASSWORD '${TEST_DB_PASSWORD}' CREATEDB;"
  else
    sudo -u postgres psql -qc \
      "ALTER ROLE ${TEST_DB_USER} LOGIN PASSWORD '${TEST_DB_PASSWORD}' CREATEDB;"
  fi

  db_exists="$(sudo -u postgres psql -tAc \
    "SELECT 1 FROM pg_database WHERE datname = '${TEST_DB_NAME}'" || echo "")"
  if [ "$db_exists" != "1" ]; then
    sudo -u postgres createdb -O "${TEST_DB_USER}" "${TEST_DB_NAME}"
  fi

  cat >/etc/profile.d/doenerstag-test-db.sh <<EOF
# Used by the integration tests when Docker is unavailable.
# See docs/12_testing.md.
export DOENER_TEST_DATABASE_URL="postgres://${TEST_DB_USER}:${TEST_DB_PASSWORD}@localhost:5432/${TEST_DB_NAME}?sslmode=disable"
EOF
  chmod 0644 /etc/profile.d/doenerstag-test-db.sh
}

# -----------------------------------------------------------------------------
# Docker
# -----------------------------------------------------------------------------

install_docker() {
  if have docker; then
    log "Docker already installed"
  else
    log "Installing Docker"
    install -m 0755 -d /etc/apt/keyrings
    curl -fsSL https://download.docker.com/linux/debian/gpg \
      -o /etc/apt/keyrings/docker.asc
    chmod a+r /etc/apt/keyrings/docker.asc

    cat >/etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/debian
Suites: $(. /etc/os-release && echo "$VERSION_CODENAME")
Components: stable
Architectures: $(dpkg --print-architecture)
Signed-By: /etc/apt/keyrings/docker.asc
EOF

    apt update
    apt install -y docker-ce docker-ce-cli containerd.io \
      docker-buildx-plugin docker-compose-plugin
  fi

  systemctl enable --now docker

  # testcontainers-go talks to the Docker socket as the invoking user. Without
  # group membership every database test in sprint 2 would need root.
  if [ -n "$DEV_USER" ] && ! id -nG "$DEV_USER" | tr ' ' '\n' | grep -qx docker; then
    log "Adding $DEV_USER to the docker group"
    usermod -aG docker "$DEV_USER"
    warn "$DEV_USER must log out and back in before docker works without sudo"
  fi
}

# -----------------------------------------------------------------------------
# Node.js, for the frontend from sprint 4 onwards
# -----------------------------------------------------------------------------

install_node() {
  local current=""
  if have node; then
    current="$(node --version | sed 's/^v//' | cut -d. -f1)"
  fi

  if [ "$current" = "$NODE_MAJOR" ]; then
    log "Node.js $NODE_MAJOR already installed"
    return
  fi

  log "Installing Node.js $NODE_MAJOR (found: ${current:-none})"

  # Debian's nodejs trails the LTS that esbuild and Tailwind are tested
  # against, so take it from NodeSource.
  install -m 0755 -d /etc/apt/keyrings
  curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key \
    | gpg --dearmor --yes -o /etc/apt/keyrings/nodesource.gpg
  chmod a+r /etc/apt/keyrings/nodesource.gpg

  cat >/etc/apt/sources.list.d/nodesource.sources <<EOF
Types: deb
URIs: https://deb.nodesource.com/node_${NODE_MAJOR}.x
Suites: nodistro
Components: main
Signed-By: /etc/apt/keyrings/nodesource.gpg
EOF

  apt update
  apt install -y nodejs
}

# -----------------------------------------------------------------------------
# Playwright browsers, for the end-to-end tests from sprint 12 onwards
# -----------------------------------------------------------------------------

install_playwright() {
  local dev_home
  dev_home="$(getent passwd "$DEV_USER" | cut -d: -f6)"

  if [ -d "${dev_home}/.cache/ms-playwright" ] && \
     [ -n "$(ls -A "${dev_home}/.cache/ms-playwright" 2>/dev/null)" ]; then
    log "Playwright browsers already installed for $DEV_USER"
    return
  fi

  log "Installing the Playwright browsers (Chromium and Firefox)"

  # The system libraries the browsers link against. Installed as root; the
  # browsers themselves belong to the user who runs the tests, because
  # Playwright looks for them under that user's cache.
  #
  # Chromium and Firefox, and not WebKit: docs/12_testing.md names those two,
  # they are what this application is designed for, and a third browser is
  # another 300 MB on a machine that does not have it to spare.
  ( cd "${dev_home}/doenerstag/frontend" 2>/dev/null || cd /tmp
    npx --yes playwright@latest install-deps chromium firefox
    sudo -u "$DEV_USER" npx --yes playwright@latest install chromium firefox
  ) || log "Playwright install failed; run 'npx playwright install' by hand"
}

# -----------------------------------------------------------------------------
# Summary
# -----------------------------------------------------------------------------
# mokapi, the development mail and directory server (sprint 16)
# -----------------------------------------------------------------------------
#
# doenerstag sends one kind of mail, and until sprint 16 there was no way to
# watch it do so without a real mail server and a real address. mokapi is an SMTP
# and IMAP server that accepts everything and delivers nothing, with a dashboard
# for reading what arrived, plus an LDAP server for the directory work that an
# intranet tool eventually gets asked for.
#
# Nothing in the test suite requires it: `make test` uses an in-process fake
# (internal/mail/mail_test.go). This is for looking at the result by hand, and
# for the end-to-end test of the reset flow.
#
# The .deb installs one file, /usr/bin/mokapi, and nothing else -- no unit, no
# configuration directory, no service account. All of that is created here.

MOKAPI_VERSION="${MOKAPI_VERSION:-0.51.2}"
MOKAPI_CONF_DIR=/etc/mokapi/conf.d
MOKAPI_SMTP_PORT=2525
MOKAPI_IMAP_PORT=1143
MOKAPI_LDAP_PORT=3389
MOKAPI_DASHBOARD_PORT=8080

install_mokapi() {
  local want="$MOKAPI_VERSION" have=""

  if have mokapi; then
    # Bounded: mokapi prints its version and then does not exit, so an
    # unbounded call here would hang the whole provisioning run.
    have="$(timeout 5 mokapi --version 2>/dev/null | grep -oE '[0-9]+[.][0-9]+[.][0-9]+' | head -1)"
  fi

  if [ "$have" = "$want" ]; then
    log "mokapi $want already installed"
  else
    log "Installing mokapi $want"
    local arch deb tmp
    arch="$(dpkg --print-architecture)"
    case "$arch" in
      amd64|arm64) ;;
      *) warn "mokapi publishes no package for $arch; skipping"; return 0 ;;
    esac

    tmp="$(mktemp -d)"
    deb="$tmp/mokapi.deb"
    if ! curl -fsSL -o "$deb" \
      "https://github.com/marle3003/mokapi/releases/download/v${want}/mokapi_${want}_linux_${arch}.deb"; then
      warn "could not download mokapi ${want}; skipping"
      rm -rf "$tmp"
      return 0
    fi
    apt install -y "$deb"
    rm -rf "$tmp"
  fi

  configure_mokapi
}

configure_mokapi() {
  log "Configuring mokapi for SMTP, IMAP and LDAP"

  install -d -m 0755 /etc/mokapi "$MOKAPI_CONF_DIR"

  # The mail server. The schema is mokapi's own rather than AsyncAPI, and the
  # `mail: '1.0'` line is what tells mokapi so. info.title is required: without
  # it the file is rejected.
  #
  # mokapi authenticates by matching username and password across every mailbox,
  # not by the address, so the credentials under doenerstag@doener.test are
  # exactly what goes in mail-username and mail-password.
  cat >"$MOKAPI_CONF_DIR/mail.yaml" <<'EOF'
mail: '1.0'

info:
  title: doenerstag development mail
  description: SMTP in, IMAP out, nothing delivered anywhere.
  version: 1.0.0

servers:
  smtp:
    host: :2525
    protocol: smtp
    description: Where doenerstag sends. Unprivileged, so mokapi needs no root.
  imap:
    host: :1143
    protocol: imap
    description: Where a test reads back what was sent.

settings:
  allowUnknownSenders: true
  autoCreateMailbox: true
  # The default is unlimited, whatever the documentation says: mokapi applies
  # its 100 only when the settings block is absent entirely, and this one is
  # not. Stated so a long-running VM does not accumulate for ever.
  maxInboxMails: 500

mailboxes:
  doenerstag@doener.test:
    username: doenerstag
    password: doenerstag-development-only
    description: The sender. Its credentials are the SMTP credentials.
  probe@doener.test:
    username: probe
    password: probe-development-only
    description: A recipient a test can read over IMAP.
    folders:
      Sent:
        flags: ['\Sent']
EOF

  # The directory. doenerstag does not authenticate against LDAP today; this is
  # here so that the question can be answered by trying rather than by guessing.
  cat >"$MOKAPI_CONF_DIR/ldap.yaml" <<'EOF'
ldap: '1.0'

info:
  title: doenerstag development directory
  description: A directory to develop against. No account here is real.

host: :3389

files:
  - ./doener.ldif
EOF

  # mokapi compares userPassword as plaintext, and an entry without one binds
  # successfully with any password at all. Every entry here has one.
  cat >"$MOKAPI_CONF_DIR/doener.ldif" <<'EOF'
dn:
namingContexts: dc=doener,dc=test

dn: dc=doener,dc=test
objectClass: top
objectClass: domain
dc: doener

dn: ou=people,dc=doener,dc=test
objectClass: organizationalUnit
ou: people

dn: ou=groups,dc=doener,dc=test
objectClass: organizationalUnit
ou: groups

dn: uid=probe,ou=people,dc=doener,dc=test
objectClass: inetOrgPerson
cn: Probe Person
sn: Person
uid: probe
mail: probe@doener.test
userPassword: probe-development-only

dn: uid=hungry,ou=people,dc=doener,dc=test
objectClass: inetOrgPerson
cn: Hungry Colleague
sn: Colleague
uid: hungry
mail: hungry@doener.test
userPassword: hungry-development-only

dn: cn=doenerstag-users,ou=groups,dc=doener,dc=test
objectClass: groupOfNames
cn: doenerstag-users
member: uid=probe,ou=people,dc=doener,dc=test
member: uid=hungry,ou=people,dc=doener,dc=test
EOF

  # An unprivileged account. mokapi binds only ports above 1024, so it never
  # needs root, and a development tool that accepts connections should not have
  # more than it needs.
  if ! getent passwd mokapi >/dev/null; then
    useradd --system --no-create-home --shell /usr/sbin/nologin mokapi
  fi
  chown -R mokapi:mokapi /etc/mokapi

  cat >/etc/systemd/system/mokapi.service <<EOF
[Unit]
Description=mokapi, the development mail and directory server for doenerstag
Documentation=https://mokapi.io/docs
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=mokapi
Group=mokapi
ExecStart=/usr/bin/mokapi --providers-file-directory $MOKAPI_CONF_DIR
Restart=on-failure
RestartSec=5s

NoNewPrivileges=true
PrivateTmp=true
ProtectSystem=strict
ProtectHome=true
CapabilityBoundingSet=
AmbientCapabilities=

[Install]
WantedBy=multi-user.target
EOF

  systemctl daemon-reload
  systemctl enable --now mokapi

  cat <<EOF

mokapi is listening:
  SMTP       localhost:$MOKAPI_SMTP_PORT   (user doenerstag, password doenerstag-development-only)
  IMAP       localhost:$MOKAPI_IMAP_PORT
  LDAP       localhost:$MOKAPI_LDAP_PORT   (dc=doener,dc=test)
  Dashboard  http://localhost:$MOKAPI_DASHBOARD_PORT

To point a development doenerstag at it, in doenerstag.yaml:
  mail:
    host: "localhost"
    port: $MOKAPI_SMTP_PORT
    username: "doenerstag"
    password: "doenerstag-development-only"
    from: "doenerstag@doener.test"
    encryption: "none"

encryption is none because mokapi offers STARTTLS with a certificate from its
own self-signed authority, which nothing here trusts. That is acceptable for a
loopback development server and for nothing else.
EOF
}

# -----------------------------------------------------------------------------

# tool_version prints one line describing an installed tool.
#
# Not every tool takes --version: `go --version` is an error, and gofmt has no
# version flag at all. Getting this wrong aborts the script under `set -e` with
# pipefail, which is exactly what happened the first time.
tool_version() {
  case "$1" in
    go)     go version ;;
    gofmt)  go version | sed 's/^go version/gofmt (from/;s/$/)/' ;;
    npm)    echo "npm $(npm --version)" ;;
    # nfpm answers with an ASCII-art banner; pick the version out of it.
    nfpm)   echo "nfpm $(nfpm --version 2>&1 | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)" ;;
    # go-licenses has no version flag at all.
    go-licenses) echo "installed (reports no version)" ;;
    # mokapi answers with an ASCII-art banner, like nfpm; pick the version out.
    # mokapi answers with an ASCII-art banner and then does not exit, so
    # the version is picked out of it and the call is bounded.
    mokapi) echo "mokapi $(timeout 5 mokapi --version 2>&1 | grep -oE '[0-9]+[.][0-9]+[.][0-9]+' | head -1)" ;;
    *)      "$1" --version ;;
  esac
}

report() {
  log "Installed versions"
  local tools=(go gofmt make gcc git golangci-lint govulncheck migrate nfpm mokapi
               go-licenses node npm psql docker jq)
  local missing=0

  for tool in "${tools[@]}"; do
    printf '  %-16s' "$tool"
    if have "$tool"; then
      # Never let the summary abort the run it is summarising.
      tool_version "$tool" 2>&1 | head -1 || echo "(version unavailable)"
    else
      echo "MISSING"
      missing=1
    fi
  done

  echo
  if [ "$missing" -eq 1 ]; then
    warn "some tools are missing; the pipeline may not run"
  fi

  cat <<'EOF'

Next steps:
  - Log out and back in, or run `newgrp docker`, so docker works without sudo
    and the PATH picks up /etc/profile.d/go.sh.
  - Then, from a checkout:
      make lint
      make test
      make build

Not installed here, deliberately:
  - Playwright browsers (sprint 12). They are around a gigabyte and the project
    installs them itself with `npx playwright install --with-deps` once
    frontend/package.json exists.
EOF
}

main() {
  install_base
  install_go
  install_go_tools_now
  install_go_tools_later
  install_postgresql
  create_test_database
  install_docker
  install_node
  install_playwright
  install_mokapi
  report
}

main "$@"
