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
- Phase acceptance runs an immutable criterion catalog and fails closed on
  missing evidence.
- Local UAT drives the complete HTTPS Podman deployment through the browser.
- Staging proves production infrastructure, restore, rotation, migration,
  rollback, alarms, and edge behavior.

The tracked engineering gates are summarized in
[`../standards/engineering.md`](../standards/engineering.md).
