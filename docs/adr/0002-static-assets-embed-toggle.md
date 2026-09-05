# ADR-0002: Build-tag toggle between embedded and on-disk assets

**Status:** Accepted — 2026-09-05

## Context

Two requirements pull in opposite directions.

A release should be a single binary. The application has to run without internet
access and be easy to deploy; shipping a binary plus a directory tree of assets
that must stay in sync with it invites the failure where they do not.

Development wants the opposite. Editing a stylesheet or a template and then
rebuilding and restarting the backend to see the change makes frontend work
miserable. The assets should be readable from disk and re-read on every request.

`go:embed` gives the first and forbids the second: what is embedded is fixed at
compile time.

## Decision

Select between the two with a Go build tag.

- **Default build** — assets are served from `os.DirFS(cfg.StaticDir)`, where
  `StaticDir` comes from `--static-dir` and defaults to `./static`. Files are
  re-read per request and served `Cache-Control: no-store`.
- **`-tags embedstatic`** — assets come from an `embed.FS` compiled into the
  binary. `--static-dir` is accepted and ignored, with a warning when it was set
  explicitly. Files are served with a long cache lifetime and a content-hash
  `ETag`.

The `go:embed` directive lives in `embed.go` at the repository root, because an
embed pattern cannot reach outside its own package directory and `static/` is a
subdirectory of the module root. `embed_disabled.go` carries the `!embedstatic`
tag and provides the same symbols with an empty filesystem.

`internal/static` picks one at construction and exposes a single `fs.FS`.
Nothing else in the codebase knows which mode is active.

Releases are always built with the tag.

## Alternatives rejected

- **Always embed, and rebuild for every frontend change.** Rejected: it makes
  the frontend edit-reload cycle a full Go build, which is the problem.
- **Never embed, always ship a directory.** Rejected: deployment gets a class of
  failure where the binary and the assets disagree, with no way to detect it.
- **A runtime flag rather than a build tag.** Rejected: the assets would be
  compiled into every build regardless, so a development binary would carry a
  stale copy of the very files being edited. Confusing in exactly the situation
  the flag exists to help with.

## Consequences

- Two build configurations exist, so CI must build both. `make release` runs in
  CI for this reason.
- A developer who builds with the tag and then wonders why their CSS edits do
  nothing will be confused once. The startup log states which mode is active,
  and the troubleshooting table in [10_operations.md](../10_operations.md) names
  the symptom.
- `static/` is generated output that is nevertheless committed, so a release can
  be built without the Node toolchain. It must never be edited by hand.
