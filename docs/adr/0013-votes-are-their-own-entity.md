# ADR-0013: A vote is a thing of its own, and the server decides it

**Status:** Accepted — 2026-11-21

## Context

Sprint 22 lets a group choose the restaurant before anybody orders (F12). A
vote offers two to five restaurants, every signed-in user picks one, and at
the decision time the restaurant ahead becomes an order. Two questions had to
be answered before the schema: where a vote lives, and who turns it into an
order.

A vote looks like an order that has not got its restaurant yet: it has a
creator, a deadline and a fulfilment time, and it ends up as one. Making it
that -- a `food_order` with a null `restaurant_id` and a `vote` flag -- would
have reused the overview, the stream and the deletion rules. It would also
have put a nullable column under every rule that assumes an order has a
restaurant: the copied currency, minimum value and fee (F5.11), the menu on
the order page, the summary, the "restaurant changes only while empty" check
(F5.8), the restaurant-deletion check, and every query that joins the two.
Each of those would have needed a "unless it is a vote" branch, and the first
one forgotten would be a 500 on a page somebody opened at lunch.

The decision has the same shape as 21.3's draw: something must happen at a
time, exactly once, whether or not anybody has the page open. A browser cannot
be trusted with it -- there may be none, or five, open at that moment.

## Decision

**A vote is its own three tables**, `vote`, `vote_choice` and `vote_ballot`
(docs/03), with its own routes and its own stream. It shares no row with an
order until it is decided; then the order it became is an ordinary order, made
by the same code path as `POST /orders`, and the vote points at it. An order
never knows anything about votes beyond the `vote_id` its shape reports.

**The server decides**, in a pass that runs beside the draw, every 15 seconds:
it takes the undecided votes whose decision time has passed and, per vote, in
one transaction, counts the ballots, picks the restaurant ahead (F12.7),
creates the order and marks the vote decided with an
`UPDATE ... WHERE NOT decided`. A second process or a second pass finds
nothing to update and rolls back, so no vote ever becomes two orders. A
restart that spans the decision time is caught by the next pass. The decision
is up to one interval late, which nobody waiting for lunch will notice.

**The rules live in the schema where they can.** Two to five choices is a
deferred constraint trigger, one ballot per voter is the primary key, a ballot
naming another vote's choice is a composite foreign key, and a chosen
restaurant cannot be removed because the ballot's foreign key restricts it.
The API checks the same rules first, for the error numbers; the schema is what
holds when two requests race.

## Consequences

- Nothing that handles an order changes, except that its shape gains
  `vote_id`. The start page, which lists both, is the one place the two meet.
- A vote and its order are deleted together (`ON DELETE CASCADE` from the
  order), so a decided vote needs nothing new from `cleanup`. A vote that became
  no order does: `cleanup` removes it after the retention period.
- The tie rule needs the time each ballot was cast or last changed, which
  `vote_ballot.cast_at` stores. `updated_at` would hold the same value today,
  but it is bookkeeping maintained by a trigger with rules of its own (20.13),
  and a result that decides where twenty people eat should not depend on them.
- Secret ballots are secret in what the application says, not in what it
  stores (docs/13).
