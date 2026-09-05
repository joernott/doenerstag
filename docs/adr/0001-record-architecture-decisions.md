# ADR-0001: Record architecture decisions

**Status:** Accepted — 2026-09-05

## Context

The specification states what the application does. It does not state which
alternatives were considered and rejected, or why. Six months on, a decision
that looks arbitrary gets quietly reversed, and the reason it was made gets
rediscovered the hard way.

Several decisions in this specification are exactly of that kind: they look like
they could go either way and only make sense once you know the constraints.

## Decision

Keep architecture decision records in `docs/adr/`, one file per decision,
numbered sequentially and never renumbered. Each record states its status, the
context that forced the decision, the decision itself and its consequences —
including the ones we do not like.

A decision that is later reversed is not edited or deleted. A new record
supersedes it and the old one is marked accordingly, so the history stays
readable.

Records are written for decisions with a real alternative. Choosing `gofmt` over
manual formatting is not an architecture decision.

## Consequences

- The specification documents stay declarative; the ADRs carry the argument.
- Reviewing a proposal to change something starts by reading why it is the way
  it is.
- One more thing to keep current. The ADR index in
  [README.md](README.md) has to be updated with each new record.
