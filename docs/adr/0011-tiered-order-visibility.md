# ADR-0011: Tiered order visibility

**Status:** Accepted — 2026-09-05

Supersedes the "reading is entirely open" position in the first draft of
[05_auth_and_permissions.md](../05_auth_and_permissions.md).

## Context

The first draft made all reading anonymous: any visitor could see every order,
every item, who ordered it and the summary. That followed from the deployment
context — a trusted internal network — and from the application's purpose, which
is to make an order visible to the group.

It also meant that anything able to reach the port could read what each named
colleague eats. On a network where that is a badly-scoped proxy, a vulnerability
scanner or an intranet search crawler, "the group" is doing more work in that
sentence than it can bear. Food choices support inferences about religion and
health ([13_legal_and_privacy.md](../13_legal_and_privacy.md)), and per-person
totals are close to a record of what an individual spends.

Against that, requiring a login to see anything would make the application worse
at its job. Somebody glancing at the intranet to see whether a lunch order is
still open should not have to log in first.

## Decision

Three tiers.

| Tier                     | Sees                                                                    |
| ------------------------ | ----------------------------------------------------------------------- |
| Anonymous                | Restaurants and menus in full. The order list, each order's header, and its **item count**. |
| Any logged-in user       | Additionally, the item list of any order: who ordered what, with modifications and totals. |
| Participants of an order | Additionally, that order's summary page with per-person totals.          |

A participant is the order's creator, anyone holding at least one item in the
order, or the administrator. Participation is derived from the data on every
request, never stored.

The creator counts as a participant with no items of their own, because the
creator is usually the person who phones the restaurant and the summary is what
they read from. Omitting them would have broken the application's main workflow.

The split is enforced in the handlers. The anonymous response shape does not
contain the item data at all — the frontend is not trusted to hide fields it was
sent.

## Consequences

- `GET /orders/{id}` returns two different shapes. `item_count` is present in
  both so the frontend does not branch on its absence.
- The SSE stream has to split the same way, or the live updates would leak what
  the REST endpoint withholds. An anonymous subscriber gets `order.item_count`
  instead of `item.*` events. The count is republished even when a change leaves
  it unchanged, so nothing can be inferred from an event not arriving (F7.4).
- Authentication state is captured when a stream opens, so logging in requires a
  reconnect to receive the fuller event set. The frontend does this
  automatically.
- Losing your last item in an order loses you access to its summary. This is
  correct and slightly surprising; it is called out in
  [05_auth_and_permissions.md](../05_auth_and_permissions.md).
- The protection is real but shallow: registration is open, so anyone on the
  network can become a logged-in user in a minute. The network remains the
  actual boundary. What this buys is that food choices are not served to
  anything that merely reaches the port. That limit is stated plainly in
  [13_legal_and_privacy.md](../13_legal_and_privacy.md) rather than being
  overclaimed.
- Three data-leak paths now need dedicated tests — the REST shape, the summary
  authorization and the SSE split. They are listed in
  [12_testing.md](../12_testing.md).
