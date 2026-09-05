# ADR-0006: No pagination on collection endpoints

**Status:** Accepted — 2026-09-05

## Context

Every collection endpoint could be paginated. The usual argument for doing it
from the start is that retrofitting pagination is a breaking API change and a
frontend rewrite, so it is cheaper to build it in.

The counter-argument is the expected scale, stated in
[01_overview.md](../01_overview.md): 3 to 10 restaurants, a few hundred menu
items, around three active orders a day, tens of users, all on a LAN. The
largest response in the system is a restaurant's full menu — a few hundred rows
of small JSON, well under a megabyte, delivered over gigabit ethernet.

## Decision

Collection endpoints return the complete collection. No `limit`, no `offset`, no
cursors, no `next` links.

Menu items accept filter parameters — category, tags, allergen and additive
exclusion, availability — because those exist for the user's benefit, not to
manage response size.

Large menus are made navigable by grouping items into categories in the UI, not
by paging them.

## Alternatives rejected

- **Paginate everything from the start.** Rejected: it adds a cursor to every
  endpoint, paging state to every frontend list, and the possibility of a
  partially-loaded menu, in exchange for solving a problem that the deployment
  scale says will not occur.
- **Paginate only menu items.** Rejected as the worst of both: the inconsistency
  costs more in comprehension than the pagination saves in bytes.

## Consequences

- Simpler API, simpler frontend, simpler tests. The frontend can hold the full
  menu in memory and filter it client-side without a round trip, which is why
  the filters feel instant.
- The summary and the order totals are always computed over the complete set,
  with no risk of totalling a page instead of the whole.
- If the scale assumption ever breaks — a restaurant with a five-thousand item
  menu, or an installation that keeps orders for years — this decision breaks
  with it. The response would get slow before it got broken, which is a
  survivable failure mode.
- Reversing it is a breaking API change, which is exactly why the assumption is
  written down in [01_overview.md](../01_overview.md) and listed among the
  accepted risks in [11_nonfunctional.md](../11_nonfunctional.md) rather than
  left implicit.
