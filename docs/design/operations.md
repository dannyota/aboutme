# 9. Privacy, quality, and operations

Operational evidence is part of the product boundary. A configured backup,
alarm, cache rule, or resource limit is not accepted until its failure path is
exercised at the environment that owns the risk.

## Privacy lifecycle

| Concern          | Intended behavior                                                                                                                   |
| ---------------- | ----------------------------------------------------------------------------------------------------------------------------------- |
| Account deletion | Recent reauthentication; revoke sessions; delete identities, resumes, and media; retain slug tombstones; invalidate public surfaces |
| Export           | A JSON bundle of the account's resume documents and related portable data                                                           |
| Session metadata | IP and user-agent data redacted after 90 days                                                                                       |
| Audit records    | Security and lifecycle audit records retained for 180 days                                                                          |
| Orphan media     | Weekly idempotent sweep, including objects left by a crash between object and database operations                                   |
| Idempotency data | Expire after 24 hours; hourly bounded global sweep is authoritative, with request-path cleanup only opportunistic                   |
| Backups          | Copies expire on the 30-day backup-retention schedule and this delay is disclosed to users                                          |

Public delivery and discovery disclosures are product requirements. The final
legal wording and any jurisdiction-specific data-residency obligations require
qualified counsel before production approval. Design documents do not claim that
a named law has been satisfied merely because infrastructure is in one region.

## Resource and performance budgets

[`../plans/budgets.md`](../plans/budgets.md) owns numeric limits and benchmark
protocol. Hard limits include request and document sizes, the PostgreSQL pool,
render concurrency, queue depth, timeout, and whole-task memory. Service-level
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
[`../standards/engineering.md`](../standards/engineering.md). Phase-specific
evidence and ownership live in [`../plans/`](../plans/).
