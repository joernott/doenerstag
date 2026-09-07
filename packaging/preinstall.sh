#!/bin/sh
# Creates the service account the unit runs as.
#
# Idempotent, because this runs on an upgrade as well as on a first install.
# No login shell and no home directory: nothing about this account is meant to
# be logged into.
set -e

if ! getent group doenerstag >/dev/null 2>&1; then
    groupadd --system doenerstag
fi

if ! getent passwd doenerstag >/dev/null 2>&1; then
    useradd --system --gid doenerstag --no-create-home \
        --home-dir /etc/doenerstag --shell /usr/sbin/nologin \
        --comment "doenerstag service account" doenerstag
fi
