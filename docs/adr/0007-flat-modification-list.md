# ADR-0007: Flat modification list instead of option groups

**Status:** Accepted — 2026-09-05

## Context

The original specification referred to "standard modifications specified for the
selected menu item" without defining them. They needed a data structure.

The full model, familiar from commercial ordering systems, is option *groups*
containing option *values*: a "size" group that is required and single-select,
a "sauce" group that allows up to two, a "extras" group that is free
multi-select. Groups carry minimum and maximum selection counts and drive
whether the UI renders radio buttons or checkboxes.

The stated preference was that modifications should be "mostly free text", with
predefined selectable options as a convenience on top.

## Decision

A flat list. `menu_item_modification` holds a name, a price delta that may be
negative, a sort order and a soft-delete marker, attached directly to a menu
item. Any combination may be selected. The UI renders checkboxes.

Free-text modification stays where it already was: `order_item.note`, one field
per order item.

Selected modifications are copied onto the order item as name and price-delta
snapshots, so a later edit to the menu never changes a placed order.

No groups, no minimum or maximum selection counts, no required options, no
mutually exclusive sets.

## Alternatives rejected

- **Full option groups.** Rejected as disproportionate. It doubles the menu
  editing UI — a user adding a missing item mid-order would have to understand
  groups before they could add "no onions" — and the whole point of the open
  editing model is that adding menu data has to be trivial. The complexity buys
  correctness guarantees ("exactly one size must be chosen") that nobody is
  relying on, because the order is placed by a human reading a summary, not by a
  machine.
- **No predefined modifications at all, free text only.** Rejected: "no onions"
  is typed dozens of times a month and priced extras cannot be totalled from
  free text. Predefined options are what make the summary arithmetic correct.

## Consequences

- A restaurant with genuine size variants is modelled as separate menu items —
  "Döner" and "Döner large" — rather than as one item with a size group. That is
  usually how such menus are printed anyway.
- Nothing prevents a user selecting two contradictory modifications. The order
  is read by a human, who will notice.
- Adding groups later is an additive schema change: a nullable `group_id` on
  `menu_item_modification` plus a new table. Existing modifications become an
  implicit ungrouped set. Reversible without a data migration, which is what
  makes starting simple safe.
