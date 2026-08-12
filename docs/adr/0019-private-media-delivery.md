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

Upload writes a new candidate first. The storage interface distinguishes a
proved create, a proved non-create, and an unknown remote outcome. Only a proved
create may enter the normal revision and idempotency boundary. A proved-created
candidate has a hard five-minute create-to-commit lifetime measured from the
successful `Put` return. The database mutation must start, finish, and classify
its commit within the remaining lifetime; after the deadline it cannot start or
resume a commit. A timeout during commit is ambiguous and retains the object.
Every proved-created candidate not referenced by a definite database result is
best-effort deleted, including a replay or concurrent loser, conflict, stale
precondition, or definite rollback. An ambiguous database commit retains the
candidate because it may be live. An unknown object-write outcome stops before
database mutation and retains the key because a lost response cannot prove
whether this call created it or collided with existing bytes. Reconciliation
later deletes it only after proving it unreferenced.

Replacement, photo deletion, resume deletion, and account deletion remove the
old reference and enqueue an exact-key deletion job in the same PostgreSQL
transaction. The durable queue is a work ledger, not a second source of media
ownership. If enqueue fails, the transaction and mutation fail together. Once
that transaction commits, Go's reference and live-state gates make the old bytes
inaccessible even though private storage may still contain them. The API may
return success without waiting for object storage.

The deletion worker validates the canonical key, treats an already-absent object
as success, and retries bounded failures. Physical removal has a 24-hour target
from reference revocation. A job that exceeds that target raises an alert and a
lifecycle audit event and keeps retrying until it reaches a terminal recorded
outcome. Product deletion disclosures distinguish immediate access revocation,
the 24-hour object-removal target, and the separate backup-retention schedule.

A weekly orphan reconciliation lists objects old enough to be outside the
request window, compares them with current database references and outstanding
deletion jobs, and deletes only unreferenced objects. It repairs crash
candidates and any queue/accounting gap; it is not the normal deletion path. Its
age threshold, page bounds, retry policy, metrics, and dry-run mode are fixed
before implementation. The 48-hour minimum age is far beyond the five-minute
candidate lifetime, so a request cannot later resume and reference an object
that reconciliation has proved unreferenced and removed.

## Consequences

- Object existence alone grants no public access.
- A crash may leave unreachable bytes, but cannot expose an unpublished photo.
- A successful delete means access is revoked and cleanup is durably scheduled;
  it does not claim that the object-store delete already finished.
- Ordinary object deletion removes the unversioned bytes; the orphan sweep does
  not need version-list or version-delete permissions.
- Source metadata, trailing payloads, and decoder-specific errors do not cross
  the storage or disclosure boundary.
- Public photo responses share public-resume absence, live-state revalidation,
  and entity-tag behavior.
- P2B cannot dispatch until this ADR, its durable deletion-job contract, the
  image-processing bounds, the orphan-sweep bounds, and the media acceptance
  criteria are approved.
