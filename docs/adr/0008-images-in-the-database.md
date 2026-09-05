# ADR-0008: Store images in the database

**Status:** Accepted — 2026-09-05

## Context

Restaurant logos and menu item photos are uploaded by users and have to live
somewhere. The candidates are the filesystem, an object store, or the database.

An object store is out immediately: it would be an external service, and the
application must run without internet access and without additional
infrastructure.

That leaves the filesystem and the database.

## Decision

Store images in PostgreSQL as `bytea`, in an `image` table that also carries the
metadata: media type, dimensions, byte size, SHA-256, original filename, and a
separate thumbnail with its own type and dimensions.

Uploads are constrained and normalized on the way in:

- Only JPEG, PNG and GIF, determined by sniffing the content rather than
  trusting the extension or the declared `Content-Type`.
- Size capped by `--max-image-size`, default 5 MiB, enforced with
  `http.MaxBytesReader` before the body is read into memory.
- Every image is decoded and re-encoded, downscaled to at most 800×800 for the
  main image and 200×200 for the thumbnail, aspect ratio preserved, never
  upscaled. Animated GIFs keep their first frame only.

Images are referenced by id and served from `GET /api/v1/images/{id}` with a
long `Cache-Control` and a strong `ETag` derived from the SHA-256.

## Alternatives rejected

- **Filesystem with a path column.** Rejected mainly because it splits the
  backup. `pg_dump` would no longer capture the whole application state, so a
  restore would need two coordinated restores from two sources with two
  retention policies — and a mismatch shows up as broken images, silently. It
  also introduces orphan files that no transaction cleans up, a writable
  directory to configure and secure, and a second thing to get right in the
  systemd unit. The single-binary deployment story is better without it.
- **Storing the original alongside the downscaled copy.** Rejected: nothing
  displays it, and it would multiply the database size for no benefit.

## Consequences

- The backup is `pg_dump` plus the configuration file. Nothing else. This is the
  main reason for the decision.
- Deleting an image is transactional. Orphans are impossible in the sense that
  matters — an unreferenced row is found and removed by `cleanup`, not left as a
  file nobody knows about.
- The database grows with the images. At the expected scale — a few hundred
  items, a few hundred KiB each — this is tens of megabytes, and the downscaling
  is what keeps it there.
- Serving an image is a database round trip rather than a `sendfile`. On a LAN
  with `ETag` revalidation this is not measurable.
- Re-encoding strips EXIF metadata, including GPS coordinates, as a side effect.
  That is a genuine privacy benefit and is noted in
  [11_nonfunctional.md](../11_nonfunctional.md).
- If image volume ever grows by an order of magnitude, this is the first
  decision to revisit. PostgreSQL large objects or a filesystem store would both
  become reasonable, and the `image` table's metadata columns would survive
  either move.
