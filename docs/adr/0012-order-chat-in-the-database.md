# ADR-0012: The order chat lives in the database, and the stream carries only its count

**Status:** Accepted — 2026-10-26

## Context

Sprint 21 gives every order a chat (F11): the people in it say who is going,
that the food has arrived, who has change. Two questions had to be answered
before any code: where the messages are kept, and how a new one reaches the
other pages.

Keeping them in memory, beside the SSE hub, would have been the smallest
change. It would also lose every chat on a restart -- a package upgrade at
eleven o'clock would wipe the conversation about the twelve o'clock order --
and would be one more piece of state the `cleanup` verb, account deletion and
the order's own deletion knew nothing about.

The stream is the other half. The SSE hub knows one thing about a subscriber:
whether they were signed in when the stream opened (ADR-0003, ADR-0011). The
chat belongs to the order's participants (F11.2), and participation changes
while a page is open -- the first item makes you one, the last one removed
stops you being one. A hub that filtered by participation would have to ask the
database per subscriber per event, or carry a copy of the rule that would
drift from `IsParticipant`.

## Decision

**Messages are rows**, in `order_message`, tied to the order with
`ON DELETE CASCADE` and to their author with `ON DELETE SET DEFAULT` to the
deleted-user placeholder, as `order_item` is. They outlive a restart, go with
the order, and need nothing new from `cleanup` or account deletion.

**The stream carries the count, not the message.** `order.message` goes to
every signed-in subscriber with `{"message_count": n}` and nothing else. A
participant's page then fetches the chat from `GET /orders/{id}/messages`,
which checks participation the same way the summary does. A signed-in visitor
who is not taking part learns how many messages there are, which the Chat
tab's title shows them anyway; an anonymous one learns nothing, because the
event is not sent to them and the anonymous order shape has no count.

The whole chat is fetched each time, not the messages since the last one.
ADR-0006 has no pagination, and a lunch order's chat is a few dozen lines.

## Consequences

- A message costs one extra request per participant page. At the scale this
  application runs at (docs/11), that is nothing.
- The rule for who may read the chat is written once, in `IsParticipant`, and
  the stream cannot leak a line to somebody it does not hold for.
- Messages are not edited or deleted one by one. If that is ever wanted it is
  a route and a permission row, not a schema change.
