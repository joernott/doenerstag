# ADR-0003: Server-Sent Events for live order updates

**Status:** Accepted — 2026-09-05

## Context

Several people add items to the same order at the same time, usually in the ten
minutes before the deadline. A participant looking at a stale page adds a
duplicate, or reads a total that is already wrong, or misses that the deadline
has passed.

Order pages therefore have to update themselves. The question is how the browser
learns that something changed.

## Decision

One Server-Sent Events stream per order: `GET /api/v1/orders/{id}/events`,
`text/event-stream`, open to anonymous readers like the rest of the order data.

The server keeps an in-memory hub per order with the connected subscribers. A
write that changes an order or its items publishes an event to that order's hub
after the transaction commits.

Events are `item.created`, `item.updated`, `item.deleted`, `order.updated`,
`order.deleted`, `order.expired` and a `ping` every 30 seconds.

The client uses the browser's built-in `EventSource`, which reconnects on its
own. On reconnect it re-fetches the whole order rather than replaying missed
events, so there is no `Last-Event-Id` support and no server-side event log.

## Alternatives rejected

- **Polling every few seconds.** Simplest to build, and it would work. Rejected
  because it makes a lull between deadlines cost the same as a rush, the latency
  is the poll interval, and the request log fills with noise that makes the
  audit trail harder to read.
- **WebSockets.** Full duplex, which this does not need — every client-to-server
  message is already an ordinary REST call. Adds a second protocol, its own
  framing, its own reconnect logic, and proxy configuration that SSE does not
  need. All cost, no benefit here.
- **Long polling.** Same connection cost as SSE with worse ergonomics and no
  browser primitive.

## Consequences

- The `--http-write-timeout` would kill a long-lived stream, so the event
  handler clears its own write deadline with
  `http.ResponseController.SetWriteDeadline(time.Time{})` after headers are
  sent. This exemption is documented in
  [09_configuration.md](../09_configuration.md) and must survive refactoring.
- The 30-second `ping` exists to keep intermediate proxies from closing an idle
  stream. A proxy that buffers `text/event-stream` breaks live updates entirely;
  the troubleshooting table names this.
- Hubs are in-memory, so the application cannot be scaled to several instances
  without a shared bus. Multi-instance deployment is already out of scope
  ([11_nonfunctional.md](../11_nonfunctional.md)); this is one of the reasons.
- Each open order page holds a connection for as long as it is open. At the
  expected scale — a handful of concurrent orders — this is nothing.
- Re-fetching on reconnect means a client can never be subtly out of date after
  a network blip, at the cost of one extra request. Worth it.
