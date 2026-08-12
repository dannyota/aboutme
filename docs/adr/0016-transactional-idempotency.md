# 0016 — Resume mutations store idempotency results transactionally

Status: Proposed (2026-08-12)

## Context

Clients retry writes after timeouts and CSRF refresh. A successful mutation can
therefore be submitted more than once. Recording a result outside the mutation
transaction leaves a crash window in which the data commits but a retry runs the
mutation again.

## Decision

Every resume mutation carries a UUID idempotency key. Records are scoped by
user, canonical operation identity, and key. The operation identity contains the
HTTP method, registered operation, and canonical concrete target values such as
the resume, section, or entry ID. It never uses only a route template.

The request fingerprint is SHA-256 over an unambiguous length-prefixed tuple of
the resolved wire version, normalized `If-Match` revision or an explicit absent
marker, every other operation-declared semantic query or header value, and the
bounded payload bytes. JSON operations hash the exact accepted body bytes. A
photo upload hashes the raw `file` part bytes; multipart framing, transfer
metadata, and filename are not semantic inputs. Records store this fingerprint,
status, JSON response, approved deterministic response headers, and expiry.
Request-scoped headers such as `Date` and `X-Request-ID` are never stored.

Both operation identity and request fingerprint use one binary tuple encoding.
Write the UTF-8 domain tag followed by one zero byte, the unsigned 32-bit
big-endian field count, then each field as an unsigned 32-bit big-endian byte
length followed by its bytes. Operation tuples use the domain
`aboutme.idempotency.operation.v1` and alternating canonical field names and
values: uppercase method, exact OpenAPI `operationId`, then route-declared
target names and values. UUID values are lowercase hyphenated strings; a path
value is decoded and validated once and is not Unicode-normalized. The stored
operation identity is the lowercase SHA-256 hex digest of that tuple.

Request tuples use `aboutme.idempotency.request.v1` and alternating field names
and values: resolved wire-version decimal, normalized precondition decimal or
`absent`, each operation-declared semantic input in its registered order, then
the exact bounded payload. Field names are part of the tuple. This fixes byte
order, field order, absence, and domain separation across implementations.

The service serializes a user's contenders before reading the record. A live
record with the same fingerprint replays its stored result without running the
mutation. The same key with a different fingerprint returns a conflict and
writes nothing. A key used for another concrete target has a different operation
identity and cannot replay that target's response. With no live record, the
callback performs all database writes through the supplied transaction, then
inserts the result before the same commit. External side effects are forbidden
inside that callback.

Records expire after 24 hours. Each mutation deletes at most 200 of the calling
user's expired records, oldest first by `(expires_at, id)`. The query uses a
`(user_id, expires_at, id)` index and leaves any backlog for later requests or a
scheduled sweep. A transactionally maintained per-user usage row counts every
physically retained record and its stored body and approved headers, including
expired backlog not yet deleted. A new key is admitted only when its insert
would keep the account at or below 50,000 retained records and 1 GiB of stored
response bytes. Existing unexpired keys still replay at the limit. The check
runs under the existing user lock without scanning response bodies. Capacity
rejection writes neither the mutation nor a replay record. Its `Retry-After` is
one second while expired backlog remains; otherwise it is the rounded-up time
until the earliest retained record expires.

The hourly privacy sweep is the expiry authority for inactive accounts. It
deletes at most 1,000 rows per page and 10,000 per run, updates the same usage
counters, and emits backlog, oldest-expired-age, and failure metrics. The
request batch is opportunistic latency protection, not the retention guarantee.

## Consequences

- Mutation rollback also rolls back its response record.
- A retry after an uncertain commit either finds the committed result or runs
  against a transaction that did not commit.
- Media upload bytes are an external side effect and need the compensation and
  orphan-sweep rules in ADR 0019; the database idempotency transaction cannot
  make object storage atomic.
- P8 privacy owns the scheduled expiry sweep; it is a launch prerequisite, not
  an optional cleanup.
