# ADR-0014: static/ is build output and is not committed

**Status:** Accepted — 2026-11-21. Supersedes the consequence in
[ADR-0002](0002-static-assets-embed-toggle.md) that committed `static/`.

## Context

ADR-0002 embeds the built frontend in the release binary with
`-tags embedstatic`, and as a consequence committed `static/` -- the bundle,
the stylesheet, the shell and the vendored Swagger UI -- "so a release can be
built without the Node toolchain". The embed itself is unaffected by this
record; only where `static/` comes from changes.

The committed copy cost more than it saved. Every change to the frontend's
sources, and every npm update that changed the bundle, needed `static/`
rebuilt and committed by hand, and CI failed a pull request that forgot. For
most of the project's life only the development VM had Node, so that rebuild
was a trip to the VM; sprint 22's dependency update needed it once per
package. Sprint 23 adds Dependabot (23.4), whose npm pull requests would
each have needed the same, by hand or by a workflow committing to them.

And the saving never applied. Releases, packages and the image are built on
GitHub, which has Node; the Dockerfile already rebuilt the frontend rather
than trusting the committed copy; and the Makefile's embedded targets
(`release`, `dist-linux`, `dist-windows`) already depended on `frontend`.
Nobody builds a release without Node.

## Decision

**`static/` is build output.** It is listed in `.gitignore`, built by
`make frontend` (`npm run build` in `frontend/`), and built again wherever a
binary embeds it: the Makefile's embedded targets, CI, the release workflow
and the image.

A plain `go build` or `make build` is unchanged: it serves the frontend from
`--static-dir` and needs no `static/` to compile. `go build -tags embedstatic`
without a built frontend fails, because go:embed requires the directory. The
failure is the compiler's -- "pattern static: no matching files found" -- and
`make release` never meets it, because it builds the frontend first.

## Consequences

- A change to the frontend or its dependencies is one commit of sources, and
  CI's check that the committed `static/` matched them goes, with nothing
  left to compare.
- Building a release locally needs Node, which `make release` already did.
- The development VM stops being the one place `static/` can be made; any
  machine with Node can run `make frontend`.
- The build is still reproducible (`npm ci`, no timestamps or content hashes
  in build.mjs), so two builds of one commit embed identical assets.
