# Numeric budgets

Enforced in P7A (resource bounds) and P9A (staging rehearsal). A budget breach
fails the gate; changing a number requires a reviewed commit citing evidence.

| Budget                                   | Target                               | Where enforced |
| ---------------------------------------- | ------------------------------------ | -------------- |
| API p95 latency (read, warm)             | ≤ 150 ms                             | P9A synthetic  |
| API p95 latency (granular PATCH)         | ≤ 250 ms                             | P9A synthetic  |
| Public SSR page p95 (origin, uncached)   | ≤ 400 ms                             | P9A synthetic  |
| PDF render wall time                     | ≤ 20 s hard timeout, p95 ≤ 8 s       | P7A            |
| Concurrent renders                       | 1 (v1)                               | P7A            |
| Render queue depth                       | ≤ 8, then 503 + readiness unhealthy  | P7A            |
| Whole server task memory (Go + Chromium) | ≤ 512 MiB cgroup                     | P7A, P9A       |
| pgx pool size                            | ≤ 20, below Postgres max_connections | P0B config     |
| SSE concurrent connections per task      | ≤ 2000                               | P6A stress     |
| SSE file descriptors headroom            | ≥ 25% below ulimit                   | P6A stress     |
| SSE heartbeat interval                   | 25 s (< CloudFront idle timeout)     | P6A, P9A       |
| Request body                             | ≤ 256 KB                             | P0B middleware |
| Resume document total                    | ≤ 512 KB                             | P2A store      |
| Resume title length                      | ≤ 160 characters                     | P2A DB + store |
| `lng` tag length                         | ≤ 35 characters                      | P2A DB         |
| Idempotency record TTL                   | 24 h                                 | P2A store      |

**Provenance of the P2A rows.** The 512 KB document total is spec §3. The other
three are ratified plan decisions, not spec numbers, landed here 2026-08-02 so
the DDL and the store constant cite one authority instead of forking it: **title
≤ 160** matches the schema's `maxLength` class for the same kind of short
display string (`fullName`, `headline`, `jobTitle`) and is enforced twice —
`resumes_title_length_check` in the database and the store's validation
pipeline; **`lng` ≤ 35** is the BCP 47 tag ceiling, a length bound only (the tag
itself is unvalidated — the documented i18n boundary); **idempotency TTL 24 h**
bounds how long a replayed key returns its stored response, and P2A enforces it
opportunistically: every `Execute` first commits a reap of the calling user's
expired rows, even when key reuse or the later mutation is rejected, because
`response_body` holds user content and a TTL nothing enforces is not a TTL. The
mutation and its new idempotency record remain atomic in a following
transaction. Changing any of these numbers follows this file's rule: a reviewed
commit citing evidence.

## Benchmark protocol (mandatory — a number without this is not a gate)

Added 2026-08-01 after adversarial review: thresholds alone are unenforceable. A
synthetic run over tiny documents can satisfy every latency target while
representative documents fail in production. Every target above is measured
under this protocol, and the evidence is retained with the run.

| Parameter          | Requirement                                                                                                                                                             |
| ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Fixture corpus     | Three sizes measured separately: `minimal.json`, `full.json` (typical), and a **worst-case document at the 512 KB bound** with max sections/entries and 16 KB rich text |
| Hardware / limits  | The **production cgroup and instance class** (ARM64 Graviton), not a developer laptop                                                                                   |
| Concurrency        | Stated per target; SSE measured with connection **churn**, not just steady state                                                                                        |
| Warm-up            | Discarded warm-up period before sampling; cold-start measured separately and reported as its own number                                                                 |
| Duration & samples | Minimum sustained duration and sample count declared per target; single-shot numbers are not accepted                                                                   |
| Percentile method  | Explicit (e.g. HdrHistogram); **queue time is included** in latency, never excluded                                                                                     |
| Repeatability      | Repeat runs; a target passes only if it holds across runs, not on a best-of                                                                                             |
| Evidence           | Raw results retained and linked from the phase's UAT report                                                                                                             |

**Hard safety limits vs SLOs.** Distinguish them: the 512 MiB cgroup, the render
timeout, the queue depth, and the pgx pool ceiling are **hard limits** —
exceeding them is a failure regardless of load. The latency numbers are **SLOs**
— measured against the corpus above and reviewed, not enforced per request.

**Baseline before freeze.** The riskiest limits (512 MiB whole-task memory with
Chromium, 2000 SSE connections) are **baselined with a real measurement in P7A
and P6A respectively**; if the measurement contradicts the number, the number
changes and the change cites the evidence.
