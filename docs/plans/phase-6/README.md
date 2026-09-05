# Phase 6: realtime

Status: active. Complete realtime locally before Phase 7 exports and Phase 8
privacy. Deployment remains future planning.

Authorities: [realtime design](../../design/realtime.md),
[resource budgets](../../design/budgets.md),
[ADR 0003](../../adr/0003-sse-over-websocket.md),
[ADR 0022](../../adr/0022-public-artifact-revocation.md), and
[AC-RT](../traceability/ac-rt.md). Autosave, CAS, idempotency, sanitizing, and
the public revocation fence retain their existing contracts.

## Tasks

1. [6.1: transport](task-01-transport.md): bounded local hub, transactional
   PostgreSQL notifications, authenticated and public SSE routes.
2. [6.2: refresh](task-02-refresh.md): reconnect repair, polling fallback,
   editor reconciliation, and public unpublish convergence.

The integration owner owns migrations, SQL generation, OpenAPI generation,
composition, shared configuration, and phase evidence. Workers own disjoint
implementation paths. Run one heavy check at a time on this laptop.

## Wire contract

`GET /api/v1/events` requires the existing session cookie. It observes only the
authenticated account's resumes. `GET /api/v1/live/{slug}` is anonymous and
ignores cookies. It admits only a currently live slug through the public fence.
Both routes reject methods other than GET and use `text/event-stream`,
`Cache-Control: no-store, no-transform`, and immediate flushes.

- `event: revision`: owner data is
  `{"version":1,"resume_id":"<uuid>","revision":"2","deleted":false}`. Public
  data is `{"version":1,"revision":"2"}`. Public events never expose account
  IDs, private resume IDs, document bytes, or unpublished metadata. Revisions
  use decimal strings, matching the existing OpenAPI int64 contract.
- Revision event IDs are `<resume UUID>:<revision>` for owners and the revision
  number for public streams. They are hints for deduplication, never replay
  cursors. A delete uses the last revision plus one, clamped at the int64
  ceiling, and `deleted: true` internally and for the owner. The deletion flag
  always triggers a refetch, including at an equal revision. Public deletion
  closes the stream.
- `event: heartbeat` has data `{"version":1}`. Send one immediately to flush
  headers and every 25 seconds. Clients detect a buffered or silent connection
  after 60 seconds without any delivered frame.
- Every EventSource open and reconnect unconditionally refetches. A revision
  event only schedules a refetch. Refetches coalesce, with one follow-up when an
  event arrives during an active read. Three consecutive failures or a silent
  stream enable conditional polling every 30 seconds; a delivered heartbeat
  restores the streaming path. Unknown event/document versions reload.
- Public refetch `404` reloads the public page to obtain its authoritative 404.
  Transient failures remain retryable. Editor refresh uses the existing
  coordinator's reconciliation and never initializes the store again.

## Resource and failure contract

The hub caps streams at 2,000 total, 100 per canonical client IP, and 20 per
authenticated account. The per-account allowance covers multiple editor tabs;
the larger IP allowance supports shared networks without making keys unbounded.
Each stream queues at most eight metadata events. Full queues disconnect their
consumer. Keys disappear when their last stream leaves. Admission fails closed
when fewer than 25% of process file descriptors remain available.

One LISTEN connection comes from the existing pool capped at 20. Committed
resume insert/update/delete transactions publish metadata with a database
trigger; rollback publishes nothing. A lost listener disconnects subscribers,
rejects new streams until LISTEN succeeds, and reconnects with bounded backoff.
Shutdown cancels and joins the listener and streams before closing the pool.

Owner streams recheck their session before sending each revision or heartbeat.
Public streams hold a metadata-only SSE lease for their lifetime. Revocation
cancels and joins the writer before that lease releases. Writes have a
two-second deadline within the existing five-second revocation deadline.
Notifications cannot bypass the fence or reopen an unpublished slug.

The [exit checklist](exit-criteria.md) records local proof. AWS-shaped resource
and edge measurements remain Phase 10 work.
