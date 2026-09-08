#!/bin/sh
# Stops the service before its binary goes away.
#
# Only on a real removal. On an upgrade the package manager calls this too, and
# stopping there would take the application down for every patch release; the
# argument distinguishes the two ($1 is 0 on rpm removal, "remove" on dpkg).
set -e

case "$1" in
    0|remove|purge)
        if command -v systemctl >/dev/null 2>&1; then
            systemctl stop doenerstag >/dev/null 2>&1 || true
            systemctl disable doenerstag >/dev/null 2>&1 || true
        fi
        ;;
esac
