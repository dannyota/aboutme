# 0019 — Resume media stays private behind live-gated reads

Status: Proposed (2026-08-12)

## Context

A stable public object URL can keep a photo reachable after a resume is
unpublished, renamed, or deleted. Database updates and object-store operations
cannot share a transaction, so upload and replacement also have crash windows.

## Decision

Resume objects live in private storage under server-derived immutable keys. Go
authorizes owner reads and serves public photos only after checking the current
resume slug and live state. V1 has no direct public `/assets` origin. The media
bucket is unversioned: immutable keys make replacement-at-key unnecessary, and
deletion must remove bytes rather than retain hidden noncurrent versions. Object
creation is conditional and fails on an existing key; no request may overwrite a
supposedly immutable object.

Photo intake fully decodes JPEG, PNG, or WebP under the image and time budgets,
applies Exif orientation, rejects animation and invalid structure, strips all
metadata, and stores only normalized JPEG or PNG pixels. The original container
never enters object storage. The output format determines the key extension and
served media type.

Upload writes a new candidate first. The resume update then commits through the
normal revision and idempotency boundary. Every candidate not referenced by the
committed result is best-effort deleted, including a replay or concurrent loser,
conflict, stale precondition, or definite rollback. An ambiguous commit retains
the candidate for the orphan sweep. Replacement and deletion remove the old
reference before deleting old bytes.

A scheduled orphan sweep lists objects old enough to be outside the request
window, compares them with current database references, and deletes only
unreferenced objects. Its age threshold, page bounds, retry policy, metrics, and
dry-run mode are fixed before implementation.

## Consequences

- Object existence alone grants no public access.
- A crash may leave unreachable bytes, but cannot expose an unpublished photo.
- Ordinary object deletion removes the unversioned bytes; the orphan sweep does
  not need version-list or version-delete permissions.
- Source metadata, trailing payloads, and decoder-specific errors do not cross
  the storage or disclosure boundary.
- Public photo responses share public-resume absence rules, cache invalidation,
  and entity-tag behavior.
- P2B cannot dispatch until this ADR, the image-processing bounds, the
  orphan-sweep bounds, and the media acceptance criteria are approved.
