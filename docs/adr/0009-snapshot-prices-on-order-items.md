# ADR-0009: Snapshot names and prices onto order items

**Status:** Accepted — 2026-09-05

## Context

Menu data is crowdsourced: any logged-in user can correct a price or rename a
dish. That is the point of the open editing model, and it means menu rows change
frequently and at unpredictable moments — including while an order is open.

If an order item only referenced `menu_item_id`, every total in the system would
be computed by joining to the current menu row. Correcting a price from 6.00 to
6.50 would silently change what a colleague owes for a kebab they ate last week.
Renaming a dish would rewrite the history of what people ordered. Neither is
detectable after the fact.

The same applies to modifications: the price delta on "extra cheese" is just as
editable.

## Decision

Copy the display-relevant values onto the order item at the moment it is
created.

`order_item` stores `item_name` and `unit_price_cents` as snapshots of the menu
item at ordering time. `order_item_modification` stores `name` and
`price_delta_cents` as snapshots of the modification.

The snapshots are authoritative for all display and all arithmetic. The line
total is computed entirely from them:

```
quantity * (unit_price_cents + SUM(price_delta_cents))
```

`menu_item_id` and `modification_id` are retained, but only for grouping in the
summary and for checking whether an item is still on the menu. Neither is read
for a price or a name. `modification_id` is nullable with `ON DELETE SET NULL`,
so removing a modification from a menu cannot damage a historical order.

## Alternatives rejected

- **Join to the current menu row.** Rejected: silently rewrites history, and the
  people affected have no way to notice.
- **Version the menu items and reference a version.** A correct and more general
  solution. Rejected as disproportionate: it introduces a versioning scheme,
  makes every menu edit an insert, and complicates every menu query, to achieve
  what two denormalized columns achieve here.
- **Freeze the whole menu into the order at creation time.** Rejected: it would
  prevent adding a missing menu item mid-order, which is a feature people
  actually use.

## Consequences

- Denormalization, deliberately. The name and price of an item exist in two
  places, and they are allowed to diverge — that divergence *is* the record of
  what the price was when the order was placed.
- Editing a menu item never touches existing orders. Correcting a price is
  therefore safe at any time, which is what makes the open editing model
  tolerable.
- The same dish ordered before and after a price change appears twice in the
  summary at two prices, because the aggregation key includes the item and its
  modifications but the rows carry different unit prices. That is correct — they
  cost different amounts — and the summary shows the unit price per line.
- A soft-deleted menu item's order history stays readable and correctly priced,
  which is what allows `cleanup` to remove the menu row once the last referencing
  order has aged out.
- Tests must cover the case explicitly: change a menu item's name and price after
  an order item exists, and assert the order item is unchanged. This is in
  [12_testing.md](../12_testing.md).
