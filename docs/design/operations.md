# 9. Privacy, quality, and operations

Operational evidence is part of the product boundary. A configured backup,
alarm, cache rule, or resource limit is not accepted until its failure path is
exercised at the environment that owns the risk.

## Privacy lifecycle

| Concern          | Intended behavior                                                                                                                                    |
| ---------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| Account deletion | Recent reauthentication; transactionally revoke sessions, identities, resumes, public generations, and media references; retain slug tombstones      |
| Media deletion   | Enqueue exact keys with reference revocation; deny access immediately; target physical removal within 24 hours; audit, alert, and retry overdue work |
| Export           | A JSON bundle of the account's resume documents and related portable data                                                                            |
| Session metadata | IP and user-agent data redacted after 90 days                                                                                                        |
| Audit records    | Security and lifecycle audit records retained for 180 days, including delayed and completed physical deletion                                        |
| Orphan media     | Weekly idempotent reconciliation of private objects, live references, and deletion jobs, including crash candidates                                  |
| Idempotency data | Expire after 24 hours; hourly bounded global sweep is authoritative, with request-path cleanup only opportunistic                                    |
| Backups          | Copies expire on the 30-day backup-retention schedule; disclosures distinguish this delay from live access and private-object deletion               |

Account and resume delete APIs may succeed once reference revocation and every
applicable deletion job commit together. Object-storage latency does not extend
the API transaction and cannot restore access. The 24-hour physical-removal
target is measured from reference revocation. A breach creates a lifecycle audit
event and alert and remains queued until a terminal outcome is recorded.
[ADR 0019](../adr/0019-private-media-delivery.md) owns this boundary.

Public delivery and discovery disclosures are product requirements. The final
legal wording and any jurisdiction-specific data-residency obligations require
qualified counsel before production approval. Design documents do not claim that
a named law has been satisfied merely because infrastructure is in one region.

### Account export and deletion

The export is a versioned JSON attachment named `aboutme-export.json` with a
`data` envelope. Version 1 contains `exportedAt`, the account's ID, email, name,
creation/update times and linked provider names, and all of its resumes in
creation-time/ID order. Each resume carries its current-version document and
owner metadata, including hidden/private content and incomplete drafts. A
separate nullable `photo` contains normalized JPEG/PNG media type, standard
base64 bytes and nullable crop; the document's server-owned photo reference is
removed. A missing referenced photo fails the export before any success bytes.
Password, session, CSRF, provider-subject, OAuth, idempotency, storage-key and
cleanup state are excluded. Account avatar storage has no v1 portable-media
contract and is not resolved through the resume-photo backend.

Export and deletion are cookie-only account operations. They accept no body,
query, resume-schema, conditional, or idempotency input. Export emits the
current schema response header and exact `no-store, no-transform`. Deletion
rechecks a live recently reauthenticated session under the account lock, drains
all affected public generations under ADR 0022, and removes account state in one
transaction. A concurrent resume creation invalidates the planned set; three
fresh attempts are allowed before a closed `account_changed` conflict.

A canonical-email advisory lock serializes registration, verification, provider
account creation and deletion. Registration rows precede user rows in the SQL
lock order. Deletion removes same-email pending registration and its mail jobs,
so an old verification token cannot recreate the deleted account. Mail already
accepted before deletion may still arrive, but its token no longer grants
access. No new mail send can pass a deleted scope after success.

Successful deletion returns 204, clears session/OAuth transaction cookies, and
sends `Clear-Site-Data` for cookies and storage. The settings dialog requires
explicit confirmation, including after reauthentication; a callback never
automatically submits deletion.

### Scheduled cleanup and audit

The server exposes one-shot `idempotency-expiry-sweep` and
`media-deletion-sweep` commands hourly, `media-orphan-sweep` weekly, and
`privacy-retention-sweep` daily. These commands use PostgreSQL advisory overlap
locks, bounded runs and fixed numeric result fields. They never start the HTTP
listeners or Chromium. Local tests execute them directly; Phase 10 activates
their schedules and proves heartbeat, failure and overdue alarm delivery.

Session metadata is redacted 90 days after the session's creation. Lifecycle
events contain only a generated event ID, fixed kind, occurrence time and an
optional media-job ID. Account deletion and media overdue/completion events are
durable; audit insertion and the state they describe commit together. Completed
media jobs remain for 180 days from completion to preserve outcome and
ambiguous-commit proof. Audit events expire after 180 days from occurrence.
Pending and overdue jobs never expire. Security diagnostic logs use the same
180-day hosted retention, configured and proved in Phase 10; cleared mail
delivery bookkeeping remains on its existing seven-day cleanup schedule.

Orphan dry runs do not mutate objects, jobs, audit or the stored cursor. Failed
orphan removal creates exact durable retry work before advancing the cursor.
Cleanup always rechecks live references and never derives deletion authority
from object existence or a stored cursor alone.

## Resource and performance budgets

[`budgets.md`](budgets.md) owns numeric limits and benchmark protocol. Hard
limits include request and document sizes, the PostgreSQL pool, render
concurrency, queue depth, timeout, and whole-task memory. Service-level
objectives include API, SSR, and render latency.

A benchmark records the candidate commit, production-shaped hardware and limits,
fixture size, concurrency, warm-up, duration, sample count, percentile method,
and raw results. Queue time remains part of latency. A best-of retry is not
evidence.

## Monitoring

Production alerts cover:

- RDS storage, CPU, connections, backup, and restore verification.
- Task readiness, restart loops, and CloudFront 5xx rate.
- Render queue depth, timeout, and out-of-memory kills.
- SSE connection and file-descriptor headroom.
- TLS expiry and origin-path failures.
- Scheduled retention, orphan cleanup, and audit jobs.
- Media deletion queue age, retry exhaustion, 24-hour target breaches, and
  weekly reconciliation drift.

Every critical alert has a documented trigger and its delivery is proven in
staging. Dashboards without a tested notification path do not satisfy the gate.

## Verification layers

- Unit and integration tests prove local behavior and live database paths.
- Schema, OpenAPI, generator, sqlc, migration, and route checks prevent contract
  drift.
- Hostile-corpus and browser tests cover rich-text and content security policy.
- Golden and visual tests cover renderer determinism.
- Phase acceptance uses one fresh review and a correctable exit checklist under
  ADR 0024; missing required evidence fails the gate.
- Native HTTPS browser checks prove features locally. Phase 10 drives the
  complete product through `https://uat.aboutme.vn` in AWS Singapore.
- The same UAT environment proves infrastructure, restore, rotation, migration,
  rollback, alarms, real email, and edge behavior before production.

The tracked engineering gates are summarized in
[`../standards/engineering.md`](../standards/engineering.md).
