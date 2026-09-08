#!/bin/sh
# Ownership and the next step.
#
# The service is deliberately neither started nor enabled: it cannot work until
# `doenerstag install` has created the database and written a real configuration
# file, and a unit that fails on boot teaches an operator to ignore it. Printing
# the next command is more use than a failed start.
set -e

chown doenerstag:doenerstag /var/log/doenerstag
chmod 0750 /var/log/doenerstag

# The template ships as a placeholder; the installer replaces it. Either way it
# holds a database password eventually, so it is never world-readable.
if [ -f /etc/doenerstag/doenerstag.yaml ]; then
    chown root:doenerstag /etc/doenerstag/doenerstag.yaml
    chmod 0640 /etc/doenerstag/doenerstag.yaml
fi

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
fi

cat <<'MESSAGE'

doenerstag is installed but not yet configured.

  1. cd /etc/doenerstag
  2. DOENER_DATABASE_ROOT_PASSWORD='...' doenerstag install -o /etc/doenerstag/doenerstag.yaml
  3. systemctl enable --now doenerstag

The installer creates the database, its runtime user and the administrator
account. See /usr/share/doc/doenerstag/ for the full instructions.

MESSAGE
