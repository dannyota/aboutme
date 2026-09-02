# Numeric budgets

Each owning component enforces its rows. Staging rehearsal repeats
production-shaped latency and resource measurements. A hard-budget breach fails
the gate; changing a number requires a reviewed change with evidence.

| Budget                                    | Target                                              | Where enforced                                        |
| ----------------------------------------- | --------------------------------------------------- | ----------------------------------------------------- |
| API p95 latency (read, warm)              | ≤ 150 ms                                            | Staging synthetic benchmark                           |
| API p95 latency (granular PATCH)          | ≤ 250 ms                                            | Staging synthetic benchmark                           |
| Public SSR page p95 (origin, uncached)    | ≤ 400 ms                                            | Staging synthetic benchmark                           |
| PDF render wall time                      | ≤ 20 s hard timeout, p95 ≤ 8 s                      | Print worker (planned)                                |
| Concurrent renders                        | 1 (v1)                                              | Print worker (planned)                                |
| Render queue depth                        | ≤ 8, then 503 + readiness unhealthy                 | Print worker (planned)                                |
| Whole server task memory (Go + Chromium)  | ≤ 512 MiB cgroup                                    | Print worker (planned); staging benchmark             |
| pgx pool size                             | ≤ 20, below Postgres max_connections                | Server pool configuration                             |
| SSE concurrent connections per task       | ≤ 2000                                              | SSE transport (planned); stress test                  |
| SSE file descriptors headroom             | ≥ 25% below ulimit                                  | SSE transport (planned); stress test                  |
| SSE heartbeat interval                    | 25 s (< CloudFront idle timeout)                    | SSE transport (planned); staging benchmark            |
| Request body                              | ≤ 256 KB                                            | API middleware                                        |
| Global API requests per client IP         | ≤ 300/min                                           | API middleware                                        |
| In-memory rate-limiter keys per instance  | ≤ 10,000                                            | Every rate-limiter instance                           |
| Anonymous login starts per client IP      | ≤ 30/min                                            | Auth service                                          |
| Privileged OAuth starts/account and IP    | ≤ 30/min                                            | Auth service                                          |
| Public revocation drain                   | ≤ 5 s hard, then fail before mutation               | Publish transitions                                   |
| Public render `canonicalOrigin`           | ≤ 512 ASCII bytes                                   | Public render boundary (Go and Nuxt)                  |
| Internal public-render JSON               | ≤ 532,480 bytes                                     | Public render boundary (Go and Nuxt)                  |
| Internal public-render HTML               | ≤ 2,097,152 bytes; provisional gate                 | Public render corpus test                             |
| Direct public-render wall time            | ≤ 5 s hard; cancel and join                         | Direct-render client                                  |
| Slug claim/rename attempts                | ≤ 30/account/hour                                   | Slug route limiter                                    |
| OAuth cleanup batch                       | ≤ 200 rows/start                                    | Auth service                                          |
| Resume photo multipart body               | ≤ 2,162,688 bytes                                   | Photo route                                           |
| Resume photo file / normalized object     | ≤ 2,097,152 bytes                                   | Media package                                         |
| Resume photo source edge / pixels         | ≤ 8,192 px / 16,777,216 pixels                      | Media package                                         |
| Resume photo normalized edge              | ≤ 2,048 px opaque / 1,024 px alpha                  | Media package                                         |
| Concurrent photo intake                   | 1 per server task; wait ≤ 1 s                       | Media package                                         |
| Photo body read / object write            | ≤ 60 s / 5 s hard deadline                          | Media package                                         |
| Photo candidate create-to-commit lifetime | ≤ 5 min hard deadline                               | Media package                                         |
| Photo normalization                       | ≤ 5 s; synchronous                                  | Media package; controlled-host and staging benchmarks |
| Photo normalization peak RSS delta        | ≤ 192 MiB                                           | Media package; controlled-host and staging benchmarks |
| Resume document total                     | ≤ 512 KB                                            | Resume store                                          |
| Resume title length                       | ≤ 160 characters                                    | Database constraint and resume store                  |
| `lng` tag length                          | ≤ 35 characters                                     | Database constraint                                   |
| Idempotency record TTL                    | 24 h                                                | Resume store                                          |
| Idempotency cleanup batch                 | ≤ 200 rows/mutation                                 | Idempotency store                                     |
| Retained idempotency records/account      | ≤ 50,000 after new-key insert                       | Idempotency store                                     |
| Retained idempotency stored bytes/account | ≤ 1 GiB after new-key insert                        | Idempotency store                                     |
| Global idempotency expiry sweep           | hourly; 1,000/page; 10,000/run                      | Privacy sweep (planned)                               |
| Resume reads per account and IP           | ≤ 600/min                                           | Resume route limiters                                 |
| Resume writes per account and IP          | ≤ 240/min                                           | Resume route limiters                                 |
| Photo uploads per account and IP          | ≤ 20/h                                              | Resume route limiters                                 |
| Structure commands per request            | ≤ 100                                               | Resume handlers                                       |
| Customization deltas per request          | ≤ 100                                               | Resume handlers                                       |
| Media orphan minimum age                  | ≥ 48 h                                              | Media jobs (planned)                                  |
| Media orphan sweep page / run             | 1,000 / 10,000 objects                              | Media jobs (planned)                                  |
| Media orphan delete concurrency           | ≤ 4                                                 | Media jobs (planned)                                  |
| Media deletion physical-removal target    | ≤ 24 h from reference revocation                    | Media jobs (planned)                                  |
| Media deletion queue page / run           | 200 / 2,000 jobs                                    | Media jobs (planned)                                  |
| Media deletion retry / concurrency        | 1/run, ≤ 6 h backoff / ≤ 4                          | Media jobs (planned)                                  |
| Password route body                       | ≤ 4,096 bytes                                       | Password routes                                       |
| Canonical account email                   | 5–254 ASCII bytes, stored lowercase                 | Account email parser                                  |
| Registration name                         | 1–100 code points after NFC; ≤ 400 raw UTF-8 bytes  | Account email parser                                  |
| Password input                            | ≤ 1,024 UTF-8 bytes; 15–128 code points after NFC   | Password policy                                       |
| Argon2id cost                             | 64 MiB, 3 iterations, parallelism 1                 | Password hasher                                       |
| Argon2id encoding                         | 16 B salt, 32 B result, PHC ≤ 192 ASCII bytes       | Password hasher                                       |
| Hash admission                            | 2 running, 16 queued                                | Password hasher                                       |
| Bundled blocklist                         | 99,840 lines; pinned commit and SHA-256             | Password blocklist                                    |
| HIBP range lookup                         | 5 s deadline, 128 KiB response cap                  | HIBP lookup                                           |
| HIBP prefix cache                         | 256 prefixes, 16 MiB entries, 24 h                  | HIBP lookup                                           |
| Bearer token                              | 32 random bytes → 43-char base64url; 32-byte digest | Password tokens                                       |
| Registration token expiry                 | 24 h                                                | Password store                                        |
| Reset token expiry                        | 30 min                                              | Password store                                        |
| Encoded password hash                     | 1–192 bytes                                         | Password store                                        |
| Outbox plaintext / ciphertext             | ≤ 4,096 / ≤ 4,112 bytes                             | Auth mail outbox                                      |
| Outbox key ring                           | 1 active + ≤ 1 previous 32-byte key                 | Auth mail outbox                                      |
| Mail claim batch / concurrency            | ≤ 10 / ≤ 2 sends                                    | Auth mail worker                                      |
| Mail lease / send deadline                | 30 s / 10 s                                         | Auth mail worker                                      |
| Mail retry backoff                        | min(30 s × 2^(n−1), 1 h); terminal at attempt 8     | Auth mail worker                                      |
| Expired auth cleanup                      | ≤ 200 rows/tick                                     | Auth mail worker                                      |
| Login admission                           | 30/min/IP                                           | Password rate policies                                |
| Login failures                            | 10/15 min per email HMAC                            | Password rate policies                                |
| Register/forgot                           | 5/h per email, 20/h per IP                          | Password rate policies                                |
| Verify/reset token consumption            | 10/h per IP                                         | Password rate policies                                |
| Add/change/reauth                         | 10/h per (account, IP)                              | Password rate policies                                |
| Local mail capture                        | ≤ 50 messages, ≤ 256 KiB total, ≤ 16 KiB/message    | Local mail capture                                    |
| Capture ports                             | 127.0.0.1:20091 native; 127.0.0.1:20444 HTTPS       | Local mail capture                                    |
| `/oauth/register` per IP                  | ≤ 5/hour                                            | OAuth and MCP rate policies                           |
| `/oauth/token` per IP                     | ≤ 30/min                                            | OAuth and MCP rate policies                           |
| Failed grants per client                  | ≤ 10 per 15 min                                     | OAuth and MCP rate policies                           |
| `/mcp` tool calls per token               | ≤ 120/min                                           | OAuth and MCP rate policies                           |
| `/mcp` tool calls per user                | ≤ 240/min                                           | OAuth and MCP rate policies                           |
| Concurrent `/mcp` requests/user           | ≤ 4                                                 | OAuth and MCP rate policies                           |
| Live grants per user                      | ≤ 10 (11th consent refused)                         | Consent handler                                       |
| `/mcp` request body                       | ≤ 4,194,304 bytes                                   | OAuth and MCP routes                                  |
| OAuth request bodies                      | ≤ 4,096 bytes                                       | OAuth and MCP routes                                  |
| Idle-client GC                            | 24 h idle; ≤ 200 rows/sweep                         | OAuth server                                          |

**Document and idempotency rows.** The
[data design](data.md#bounds-and-invariants) owns the 512 KiB document limit.
Title length is enforced in both PostgreSQL and the aggregate validator. The
`lng` row is the database's coarse stored-length bound. The resume HTTP boundary
validates and canonicalizes non-empty BCP 47 tags, then rejects a canonical form
over 35 characters before persistence; read projection maps invalid or overlong
legacy data to `und`. The 24-hour idempotency lifetime bounds the replay window.
Request-path cleanup removes at most 200 rows for the caller in deterministic
oldest-first order without making one request pay for an unbounded backlog. A
per-user usage row caps every retained record and its stored body and approved
deterministic headers, so an expired backlog cannot escape the storage bound.
The hourly privacy sweep is the retention guarantee for inactive users; it
deletes at most 10,000 expired rows per run in 1,000-row pages and alarms on
backlog or age. [ADR 0016](../adr/0016-transactional-idempotency.md) owns
mutation and replay-record atomicity.

**Photo and resume-route rows.** The photo part remains at exactly 2 MiB; the
request gets 64 KiB of multipart framing allowance. Header inspection precedes
full decode. Each source edge is at most 8,192 pixels and the overflow-safe
pixel product is at most 16,777,216. That admits common 4,032×3,024 photos while
bounding an eight-byte-per-pixel working image at 128 MiB. One task-wide permit
and a measured 192 MiB peak-RSS delta keep normalization within the 512 MiB
whole-task limit. Opaque output is at most 2,048 pixels on its longest edge;
alpha output is at most 1,024. Both remain at or below 2 MiB after canonical
encoding.

The body-read and object-write limits have cancellable I/O boundaries and are
request deadlines. The in-process image decoders are synchronous and have no
general cancellation boundary. Five seconds is therefore measured, not a timer
that returns while a decoder still runs. The media intake applies it
provisionally to the frozen hostile and boundary image corpus on a pinned local
controlled-cgroup profile. Staging rehearsal repeats the same corpus on the
selected ARM64 Graviton instance and 512 MiB task cgroup as a launch gate. A
request returns only after normalization stops and releases the task-wide
permit. A failing fixture blocks its phase or launch, or requires a reviewed
isolated-worker design; it never creates detached work.

Reads at 600/min and writes at 240/min permit several editor tabs above the
expected one-second autosave cadence without making the limit inert. Photo
uploads at 20/h contain binary abuse without blocking normal crop and
replacement work. All three route policies use the account-and-client-IP
composite key.

The global API limiter permits 300 requests per client IP each minute. Each
independent limiter instance keeps at most 10,000 ordinary keys and applies the
shared overflow behavior from ADR 0018. Anonymous login starts add a
30-per-minute client-IP policy. Authenticated provider-link and reauthentication
starts use a separate 30-per-minute `(account, client IP)` policy. Resume read,
write, and upload policies are also separate instances. Each start reaps at most
200 expired transactions. Structure and customization requests accept at most
100 ordered operations each; the 256 KiB transport ceiling remains a separate
byte bound.

**Public render and revocation rows.** `canonicalOrigin` is one normalized ASCII
`http` or `https` origin with no userinfo, non-root path, query, or fragment.
Production requires `https`; local development may use configured `http`. The
532,480-byte request bound is the 524,288-byte document ceiling plus an
8,192-byte envelope. A boundary test must serialize the largest valid canonical
document with the exact 512-byte origin and prove the closed request fits. The
projection contains one document-shaped value, and its only possible growth is
replacement of one bounded photo key by its authorized absolute URL.

The 2,097,152-byte HTML bound is provisional until minimal, full, and 512 KiB
renderer fixtures pass beneath it without truncation. A breach blocks the owning
phase or changes the budget with measured evidence. The direct-render
five-second hard deadline cancels and joins Nuxt work; it never returns while
detached rendering continues. The dedicated slug limit applies only when a
requested slug differs from the stored value. The existing shared five-second
revocation bound remains one wall-clock deadline across all affected fence
drains and fails before the mutation transaction.

An orphan object is never public, but retained bytes still need a bound. The
weekly sweep ignores objects younger than 48 hours, reads at most 1,000 keys per
page and 10,000 per run, and deletes with concurrency four. It retries each
object up to three times with capped exponential backoff, exposes backlog and
failure metrics, and supports dry-run mode. A backlog at the run ceiling alerts
and continues from a stored cursor on the next run.

Reference-removal transactions enqueue exact-key deletion jobs. The hourly
worker reads at most 200 jobs per page and 2,000 per run with concurrency four.
It makes one delete attempt per due job per run and doubles retry delay up to
six hours. Already-absent objects complete successfully. Jobs older than 24
hours alert and remain queued. The weekly sweep reconciles objects against both
live references and outstanding jobs; it is not the normal deletion path.

**Password authentication rows.** The password route body is 4,096 bytes so
strict JSON, token, and hash inputs all fit with headroom. The canonical email
accepts 5–254 ASCII bytes and stores lowercase; the migration preflight proves
every existing row already matches. A password is rejected above 1,024 UTF-8
bytes before normalization, then must be 15–128 code points after NFC. Argon2id
uses 64 MiB, 3 iterations, and parallelism 1 at or above the OWASP minimum; the
release gate benchmarks it on the deployment CPU. Two hashes run and sixteen
wait; the seventeenth waiter fails closed, and unknown/provider-only login uses
the same admitted dummy verification.

The bundled blocklist is the pinned NCSC 100k list normalized to sorted SHA-256
digests; runtime lookup compares digests only. HIBP sends the first five hex
characters of the SHA-1 digest with padding, caches at most 256 prefixes and 16
MiB of entries for 24 hours, and fails closed when needed but unavailable.
Tokens are 32 random bytes stored only as SHA-256 digests; registration expires
in 24 hours and reset in 30 minutes.

The outbox plaintext is at most 4,096 bytes and encrypts to at most 4,112 bytes
with AES-256-GCM under a one-active-plus-one-previous key ring. The worker polls
once per second, claims at most 10 jobs, sends at most two at once with a
10-second deadline, leases for 30 seconds, retries temporary failures with
jittered exponential backoff, and marks terminal on expiry or the eighth failed
attempt. The local capture retains at most 50 messages and 256 KiB total,
rejects a message over 16 KiB, and binds loopback on the fixed native and HTTPS
ports. Password rate policies share the 10,000-key bounded store and add the
login, failure, register/forgot, token, and account-mutation budgets.

**Agent access rows.** This table is the enforcement authority for agent access.
Registration is unauthenticated, so five per hour per IP admits a genuine first
connection while making bulk client creation useless; garbage collection then
deletes any client that is 24 hours old with no live grant and no live token, at
most 200 rows per sweep, so an abandoned registration cannot accumulate. Thirty
token requests per minute per IP covers refresh rotation for several agents
behind one address, and the separate ten-failed-grants-per-15-minutes bucket per
client makes code and verifier guessing expensive without letting one hostile
client throttle another. Tool calls are capped at 120 per minute per token and
240 per minute per user so a second agent cannot be starved by the first, with
at most four concurrent `/mcp` requests per user bounding in-flight database and
render work. Ten live grants per user is a visible, revocable ceiling; the
eleventh consent is refused with a closed error rather than silently evicting an
existing grant.

The OAuth request bodies stay at 4,096 bytes so a strict registration JSON
document with five 512-byte redirect URIs, and every form-encoded token or
revocation request, fit with headroom. `/mcp` needs a larger cap than the global
256 KiB ceiling because `upload_photo` carries base64 content: 4,194,304 bytes
is the 2 MiB photo bound plus base64 expansion and JSON-RPC envelope, following
the photo-route precedent. The decoded image is still bounded by the unchanged
media limits. All rate policies compose the ADR 0018 bounded limiter, share its
10,000-key store, and key on the canonical Caddy client address, token, user, or
client as stated.

## Benchmark protocol

A threshold without a reproducible measurement is not a gate. A synthetic run
over tiny documents can pass while representative documents fail. Measure each
applicable target under this protocol and retain the raw evidence.

| Parameter          | Requirement                                                                                                                                                                                                                                                                              |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Fixture corpus     | Three sizes measured separately: `minimal.json`, `full.json` (typical), and a **worst-case document at the 512 KB bound** with max sections/entries and 16 KB rich text                                                                                                                  |
| Hardware / limits  | The media intake gate pins the local host identity, toolchain, CPU quota, and 512 MiB controlled cgroup for its provisional gate. Staging rehearsal repeats on the selected production ARM64 Graviton class and task cgroup; an unpinned laptop run is never accepted as launch evidence |
| Concurrency        | Stated per target; SSE measured with connection **churn**, not just steady state                                                                                                                                                                                                         |
| Warm-up            | Discarded warm-up period before sampling; cold-start measured separately and reported as its own number                                                                                                                                                                                  |
| Duration & samples | Minimum sustained duration and sample count declared per target; single-shot numbers are not accepted                                                                                                                                                                                    |
| Percentile method  | Explicit (e.g. HdrHistogram); **queue time is included** in latency, never excluded                                                                                                                                                                                                      |
| Repeatability      | Repeat runs; a target passes only if it holds across runs, not on a best-of                                                                                                                                                                                                              |
| Evidence           | Raw results retained locally and cited from the phase exit checklist                                                                                                                                                                                                                     |

**Hard safety limits vs measured gates and SLOs.** The 512 MiB cgroup, render
timeout, queue depth, and pgx pool ceiling are hard runtime limits.
Normalization's five-second ceiling is a measured provisional gate and staging
launch gate because its in-process decoder cannot be killed safely. The latency
numbers are service-level objectives (SLOs), measured against the corpus above
and not enforced per request.

**Baseline before freeze.** The riskiest limits (512 MiB whole-task memory with
Chromium, 2000 SSE connections) are **baselined with a real measurement by the
print worker and SSE transport respectively**; if the measurement contradicts
the number, the number changes and the change cites the evidence.
