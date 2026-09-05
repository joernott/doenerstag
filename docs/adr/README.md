# Architecture decision records

Short records of decisions that shaped the specification, kept so that the
reasoning survives after the reasoning is forgotten.

One file per decision, numbered sequentially, never renumbered. A decision that
turns out wrong is not edited — a new record supersedes it and the old one is
marked `Superseded by ADR-nnnn`.

Format: Status, Context, Decision, Consequences. Keep them to a page.

| ADR | Title | Status |
| --- | ----- | ------ |
| [0001](0001-record-architecture-decisions.md) | Record architecture decisions | Accepted |
| [0002](0002-static-assets-embed-toggle.md) | Build-tag toggle between embedded and on-disk assets | Accepted |
| [0003](0003-sse-for-order-updates.md) | Server-Sent Events for live order updates | Accepted |
| [0004](0004-jwt-with-server-side-sessions.md) | JWT backed by a server-side session table | Accepted |
| [0005](0005-server-side-argon2id.md) | Hash passwords server-side with Argon2id | Accepted |
| [0006](0006-no-pagination.md) | No pagination on collection endpoints | Accepted |
| [0007](0007-flat-modification-list.md) | Flat modification list instead of option groups | Accepted |
| [0008](0008-images-in-the-database.md) | Store images in the database | Accepted |
| [0009](0009-snapshot-prices-on-order-items.md) | Snapshot names and prices onto order items | Accepted |
| [0010](0010-httprouter-for-routing.md) | httprouter for all routing | Accepted |
| [0011](0011-tiered-order-visibility.md) | Tiered order visibility | Accepted |
