#!/bin/bash
#
# Remove what the browser tests left in a development installation.
#
# frontend/e2e/cleanup.ts does the same at the start of every Playwright run,
# through the API -- but only when it can log in as root, and a development
# install whose root password is not CI's skips it with a warning nobody
# reads. The development VM had collected some 900 restaurants and 1,500
# accounts that way. This works from the machine itself, through the
# administrator verbs, and needs no web password.
#
# It removes, and only removes:
#   - restaurants named "e2e-..." (E2E_PREFIX in frontend/e2e/support.ts), with
#     their menus and orders, and the legacy "Neu <base36>" restaurants that
#     shell.spec.ts created before it used the prefix;
#   - the votes that offer any of them;
#   - accounts named "e2e-...".
# Anything else -- a restaurant somebody entered, a person's own account,
# root and the deleted-user placeholder -- is not touched.
#
# Usage, on the machine running the installation:
#   sudo ./contrib/cleanup_e2e_data.sh [/etc/doenerstag/doenerstag.yaml]

set -euo pipefail

CONFIG="${1:-/etc/doenerstag/doenerstag.yaml}"
DATABASE="${DOENER_DATABASE_NAME:-doenerstag}"

if [ "$(id -u)" -ne 0 ]; then
  exec sudo "$0" "$@"
fi

psql_q() {
  sudo -u postgres psql -d "$DATABASE" -Atq -c "$1"
}

# The test restaurants, given the column that holds the name.
test_restaurant() {
  echo "($1 LIKE 'e2e-%' OR $1 ~ '^Neu [0-9a-z]{6,12}\$')"
}

# Votes first. Until HardDeleteRestaurant removes the votes that offer a
# restaurant itself, a vote holding one refuses the restaurant's deletion.
votes=$(psql_q "DELETE FROM vote WHERE id IN (
  SELECT c.vote_id FROM vote_choice c JOIN restaurant r ON r.id = c.restaurant_id
  WHERE $(test_restaurant r.name)) RETURNING id" | wc -l)

removed=0
failed=0
while read -r id; do
  [ -n "$id" ] || continue
  if doenerstag restaurant delete -c "$CONFIG" --id "$id" --force >/dev/null 2>&1; then
    removed=$((removed + 1))
  else
    failed=$((failed + 1))
    echo "could not delete restaurant $id" >&2
  fi
done < <(psql_q "SELECT id FROM restaurant WHERE deleted_at IS NULL AND $(test_restaurant name)")

# Restaurants the tests soft-deleted are invisible to the verb above, and the
# cleanup verb, which would collect them, also deletes every order older than
# the retention period -- real ones included. Their menus and contacts cascade;
# an order still pointing at one makes the statement fail, which is right.
softdeleted=$(psql_q "DELETE FROM restaurant
  WHERE deleted_at IS NOT NULL AND $(test_restaurant name)
    AND NOT EXISTS (SELECT 1 FROM food_order o WHERE o.restaurant_id = restaurant.id)
  RETURNING id" | wc -l)
removed=$((removed + softdeleted))

accounts=0
while read -r id; do
  [ -n "$id" ] || continue
  if doenerstag user delete -c "$CONFIG" --id "$id" >/dev/null 2>&1; then
    accounts=$((accounts + 1))
  else
    failed=$((failed + 1))
    echo "could not delete account $id" >&2
  fi
done < <(psql_q "SELECT id FROM app_user WHERE name LIKE 'e2e-%'")

echo "removed ${votes} vote(s), ${removed} restaurant(s) with their orders, ${accounts} account(s); ${failed} failure(s)"
echo "left: $(psql_q "SELECT string_agg(name, ', ' ORDER BY name) FROM restaurant") / $(psql_q "SELECT string_agg(name, ', ' ORDER BY name) FROM app_user")"
