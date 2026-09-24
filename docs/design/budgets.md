# Numeric budgets

Each owning component enforces its rows. Production measurements repeat latency
and resource checks on the real host. A hard-budget breach fails the gate;
changing a number requires a reviewed change with evidence.

## Operating budget

Production costs about $45–55 a month before tax
([ADR 0037](../adr/0037-single-host-production-without-hosted-uat.md)); the
[single-host design](single-host-production.md#monitoring-and-cost) itemizes it.
Spend alerts live outside this repository and are delayed, not a cap. A second
replica must keep every per-account and per-IP limit, revocation deadline, and
admission bound; the [scaling contract](scaling/README.md) and
[policy catalog](scaling/policy-catalog.md) fix its shared pools and policies.

## Runtime bounds

| Budget                                          | Target                                              | Where enforced                                           |
| ----------------------------------------------- | --------------------------------------------------- | -------------------------------------------------------- |
| API p95 latency (read, warm)                    | ≤ 150 ms                                            | Production synthetic benchmark                           |
| API p95 latency (granular PATCH)                | ≤ 250 ms                                            | Production synthetic benchmark                           |
| Public SSR page p95 (origin, uncached)          | ≤ 400 ms                                            | Production synthetic benchmark                           |
| PDF render deadline and latency                 | 20 s cancellation deadline; p95 ≤ 8 s               | Print queue and Chromium                                 |
| Concurrent renders                              | 1 (v1)                                              | Print queue and Chromium                                 |
| Render queue depth                              | ≤ 8, then 503 + readiness unhealthy                 | Print queue and Chromium                                 |
| Whole server task memory (Go + Chromium)        | ≤ 512 MiB cgroup                                    | Print queue and Chromium; production benchmark           |
| Print capability lifetime                       | ≤ 60 s; job deadline remains 20 s                   | Render queue                                             |
| Private print JSON / HTML                       | ≤ 3,407,872 / 6,291,456 bytes                       | Snapshot encoder and Nuxt print boundary                 |
| PDF / share-image bytes                         | ≤ 16,777,216 / 4,194,304 bytes                      | Streamed PDF and PNG validation                          |
| Owner PDF requests per account and IP           | ≤ 10/min                                            | Owner PDF route                                          |
| Public artifact requests / render misses per IP | ≤ 300/min / 20/min                                  | Public artifact routes                                   |
| Public artifact cache                           | ≤ 128 entries, 32 MiB bodies, 60 s TTL              | Shared public cache                                      |
| pgx pool size                                   | ≤ 20 per app task; 12 once a second replica serves  | pgx pool configuration                                   |
| SSE concurrent connections per task             | ≤ 2000                                              | SSE transport; local churn measurement                   |
| SSE file descriptors headroom                   | ≥ 25% below ulimit                                  | SSE transport; local churn measurement                   |
| SSE concurrent connections per client IP        | ≤ 100                                               | SSE admission                                            |
| SSE concurrent connections per account          | ≤ 20                                                | SSE admission                                            |
| SSE queued notifications per connection         | ≤ 8, then disconnect                                | Local hub                                                |
| SSE write deadline                              | ≤ 2 s per flush                                     | SSE writer                                               |
| SSE heartbeat interval                          | 25 s (< Cloudflare 100 s idle timeout)              | SSE transport; production edge check                     |
| Request body                                    | ≤ 256 KB                                            | API middleware                                           |
| Global API requests per client IP               | ≤ 300/min                                           | API middleware                                           |
| In-memory rate-limiter keys per instance        | ≤ 10,000                                            | Every rate-limiter instance                              |
| Anonymous login starts per client IP            | ≤ 30/min                                            | Auth service                                             |
| Privileged OAuth starts/account and IP          | ≤ 30/min                                            | Auth service                                             |
| Public revocation drain                         | ≤ 5 s hard, then fail before mutation               | Publish transitions                                      |
| Public render `canonicalOrigin`                 | ≤ 512 ASCII bytes                                   | Public render boundary (Go and Nuxt)                     |
| Internal public-render JSON                     | ≤ 532,480 bytes                                     | Public render boundary (Go and Nuxt)                     |
| Internal public-render HTML                     | ≤ 2,097,152 bytes; provisional gate                 | Public render corpus test                                |
| Direct public-render wall time                  | ≤ 5 s hard; cancel and join                         | Direct-render client                                     |
| Slug claim/rename attempts                      | ≤ 30/account/hour                                   | Slug route limiter                                       |
| OAuth cleanup batch                             | ≤ 200 rows/start                                    | Auth service                                             |
| Resume photo multipart body                     | ≤ 2,162,688 bytes                                   | Photo route                                              |
| Resume photo file / normalized object           | ≤ 2,097,152 bytes                                   | Media package                                            |
| Resume photo source edge / pixels               | ≤ 8,192 px / 16,777,216 pixels                      | Media package                                            |
| Resume photo normalized edge                    | ≤ 2,048 px opaque / 1,024 px alpha                  | Media package                                            |
| Concurrent photo intake                         | 1 per server task; wait ≤ 1 s                       | Media package                                            |
| Photo body read / object write                  | ≤ 60 s / 5 s hard deadline                          | Media package                                            |
| Photo candidate create-to-commit lifetime       | ≤ 5 min hard deadline                               | Media package                                            |
| Photo normalization                             | ≤ 5 s; synchronous                                  | Media package; controlled-host and production benchmarks |
| Photo normalization peak RSS delta              | ≤ 192 MiB                                           | Media package; controlled-host and production benchmarks |
| Resume document total                           | ≤ 512 KB                                            | Resume store                                             |
| Resume title length                             | ≤ 160 characters                                    | Database constraint and resume store                     |
| `lng` tag length                                | ≤ 35 characters                                     | Database constraint                                      |
| Idempotency record TTL                          | 24 h                                                | Resume store                                             |
| Idempotency cleanup batch                       | ≤ 200 rows/mutation                                 | Idempotency store                                        |
| Retained idempotency records/account            | ≤ 50,000 after new-key insert                       | Idempotency store                                        |
| Retained idempotency stored bytes/account       | ≤ 1 GiB after new-key insert                        | Idempotency store                                        |
| Global idempotency expiry sweep                 | hourly; 1,000/page; 10,000/run                      | Privacy sweep                                            |
| Account JSON export                             | ≤ 12,582,912 bytes; 5/min per account and IP        | Account export route                                     |
| Account export / photo read deadline            | 20 s / 5 s; cancel and join                         | Account export route                                     |
| Account deletion attempts                       | 5/min per account and IP; 3 plan attempts/request   | Account deletion route                                   |
| Provider unlink attempts                        | 5/min per account and IP                            | Provider unlink route                                    |
| Privacy job run                                 | ≤ 30 min; cancel and join                           | One-shot job commands                                    |
| Daily retention per category                    | 1,000/page; 10,000/run                              | Session/audit/completed-job sweeps                       |
| Media job lease / object I/O                    | 30 s / 5 s                                          | Media cleanup                                            |
| Resume reads per account and IP                 | ≤ 600/min                                           | Resume route limiters                                    |
| Resume writes per account and IP                | ≤ 240/min                                           | Resume route limiters                                    |
| Photo uploads per account and IP                | ≤ 20/h                                              | Resume route limiters                                    |
| Structure commands per request                  | ≤ 100                                               | Resume handlers                                          |
| Customization deltas per request                | ≤ 100                                               | Resume handlers                                          |
| Media orphan minimum age                        | ≥ 48 h                                              | Media cleanup                                            |
| Media orphan sweep page / run                   | 1,000 / 10,000 objects                              | Media cleanup                                            |
| Media orphan delete concurrency                 | ≤ 4                                                 | Media cleanup                                            |
| Media deletion physical-removal target          | ≤ 24 h from reference revocation                    | Media cleanup                                            |
| Media deletion queue page / run                 | 200 / 2,000 jobs                                    | Media cleanup                                            |
| Media deletion retry / concurrency              | 1/run, ≤ 6 h backoff / ≤ 4                          | Media cleanup                                            |
| Password route body                             | ≤ 4,096 bytes                                       | Password routes                                          |
| Canonical account email                         | 5–254 ASCII bytes, stored lowercase                 | Account email parser                                     |
| Registration name                               | 1–100 code points after NFC; ≤ 400 raw UTF-8 bytes  | Account email parser                                     |
| Password input                                  | ≤ 1,024 UTF-8 bytes; 15–128 code points after NFC   | Password policy                                          |
| Argon2id cost                                   | 64 MiB, 3 iterations, parallelism 1                 | Password hasher                                          |
| Argon2id encoding                               | 16 B salt, 32 B result, PHC ≤ 192 ASCII bytes       | Password hasher                                          |
| Hash admission                                  | 2 running, 16 queued                                | Password hasher                                          |
| Bundled blocklist                               | 99,840 lines; pinned commit and SHA-256             | Password blocklist                                       |
| HIBP range lookup                               | 5 s deadline, 128 KiB response cap                  | HIBP lookup                                              |
| HIBP prefix cache                               | 256 prefixes, 16 MiB entries, 24 h                  | HIBP lookup                                              |
| Bearer token                                    | 32 random bytes → 43-char base64url; 32-byte digest | Password tokens                                          |
| Registration token expiry                       | 24 h                                                | Password store                                           |
| Reset token expiry                              | 30 min                                              | Password store                                           |
| Encoded password hash                           | 1–192 bytes                                         | Password store                                           |
| Outbox plaintext / ciphertext                   | ≤ 4,096 / ≤ 4,112 bytes                             | Auth mail outbox                                         |
| Outbox key ring                                 | 1 active + ≤ 1 previous 32-byte key                 | Auth mail outbox                                         |
| Mail claim batch / concurrency                  | ≤ 10 / ≤ 2 sends                                    | Auth mail worker                                         |
| Mail lease / send deadline                      | 30 s / 10 s                                         | Auth mail worker                                         |
| Mail retry backoff                              | min(30 s × 2^(n−1), 1 h); terminal at attempt 8     | Auth mail worker                                         |
| Expired auth cleanup                            | ≤ 200 rows/tick                                     | Auth mail worker                                         |
| Login admission                                 | 30/min/IP                                           | Password rate policies                                   |
| Login failures                                  | 10/15 min per email HMAC                            | Password rate policies                                   |
| Register/forgot                                 | 5/h per email, 20/h per IP                          | Password rate policies                                   |
| Verify/reset token consumption                  | 10/h per IP                                         | Password rate policies                                   |
| Add/change/reauth                               | 10/h per (account, IP)                              | Password rate policies                                   |
| Pending-auth token and CSRF secret              | 32 random bytes each; token stored as SHA-256       | Second-factor authentication                             |
| Pending-auth lifetime / live rows / failures    | 5 min / 5 per account / 5 per row                   | Second-factor authentication                             |
| Second-factor attempts                          | 10/15 min per (account, IP); 30/min per IP          | Second-factor rate policies                              |
| Factor management mutations                     | ≤ 10/hour per (account, IP), passkey and TOTP       | Second-factor rate policies                              |
| Active passkeys per account                     | ≤ 5                                                 | Second-factor store                                      |
| WebAuthn challenge / ceremony lifetime          | 32 random bytes / 5 min                             | WebAuthn service                                         |
| WebAuthn ceremony token                         | 32 random bytes; stored as SHA-256                  | WebAuthn service and store                               |
| WebAuthn credential ID / public key             | 16–1,023 / ≤ 2,048 bytes                            | WebAuthn service and store                               |
| WebAuthn request body                           | ≤ 32,768 bytes                                      | Second-factor routes                                     |
| WebAuthn client data / attestation object       | ≤ 4,096 / 16,384 bytes                              | WebAuthn decoder                                         |
| WebAuthn authenticator data / signature         | ≤ 4,096 / 1,024 bytes                               | WebAuthn decoder                                         |
| WebAuthn user handle                            | 32 bytes generated; ≤ 64 bytes received             | WebAuthn service                                         |
| WebAuthn transport hints                        | ≤ 8 hints, ≤ 32 bytes each                          | WebAuthn decoder                                         |
| WebAuthn and pending cleanup                    | ≤ 200 expired rows per run                          | Second-factor store                                      |
| Security mail job expiry                        | 24 h after event, rounded to whole seconds          | Second-factor security mail                              |
| Authentication security-event retention         | 180 days; 1,000/page; 10,000/run                    | Privacy sweep                                            |
| Session metadata retention                      | 88 days after sign-in; 1,000/page; 10,000/run       | Privacy sweep                                            |
| Slug tombstone retention                        | 180 days; 1,000/page; 10,000/run                    | Privacy sweep                                            |
| Recovery-code set / entropy                     | 10 codes / 128 random bits per code                 | Second-factor recovery                                   |
| Recovery verification request body              | ≤ 4,096 bytes                                       | Second-factor recovery route                             |
| Recovery code input length                      | ≤ 128 characters before canonicalization            | Second-factor recovery                                   |
| TOTP profile                                    | HMAC-SHA-1; 20-byte secret; 6 digits; 30 s; ±1 step | TOTP service                                             |
| TOTP code input                                 | Exactly 6 ASCII digits                              | TOTP routes                                              |
| TOTP secret display                             | 32 Base32 chars; 39-char grouped form               | TOTP provisioning                                        |
| TOTP credentials / enrollment rows              | 1 / 1 per account                                   | TOTP store                                               |
| TOTP enrollment lifetime / token                | 10 min / 32 random bytes; stored as SHA-256         | TOTP service and store                                   |
| TOTP request bodies                             | ≤ 4,096 bytes                                       | TOTP routes                                              |
| TOTP provisioning URI / issuer                  | ≤ 2,048 / 1–253 bytes                               | TOTP provisioning                                        |
| TOTP cool-down                                  | Group k of 5: 15 min × 2^min(k−1, 7), ≤ 24 h        | TOTP credential row                                      |
| TOTP cool-down `Retry-After`                    | ≤ 86,400 s                                          | TOTP verification route                                  |
| TOTP consecutive-failure counter                | 0–1,000, saturating                                 | TOTP credential row                                      |
| Attempt-exhaustion mail                         | ≤ 1/h per account                                   | Factor policy row                                        |
| First-policy handle candidates                  | ≤ 3 per first TOTP completion                       | TOTP service                                             |
| TOTP key ring / derived key ID                  | 1 active + ≤ 1 previous 32-byte key / 26 bytes      | TOTP key ring                                            |
| TOTP key encoding                               | 43-char unpadded base64url                          | TOTP key ring                                            |
| TOTP nonce / ciphertext                         | 12 / 36 bytes                                       | TOTP key ring and store                                  |
| TOTP stored key-ID check                        | ≤ 3 distinct IDs; at startup and every 5 min        | TOTP key health                                          |
| TOTP re-encryption                              | ≤ 200 rows/transaction; ≤ 10,000 rows, 30 min/run   | Re-encryption command                                    |
| TOTP rotation final count                       | ≥ 10 min after the key-switch deploy                | Rotation procedure                                       |
| TOTP enrollment cleanup                         | ≤ 200 rows per admitted start                       | TOTP store                                               |
| `totp_unavailable` log line                     | ≤ 1/min per reason per process                      | TOTP key health                                          |
| Local mail capture                              | ≤ 50 messages, ≤ 256 KiB total, ≤ 16 KiB/message    | Local mail capture                                       |
| Capture ports                                   | 127.0.0.1:20091 native; 127.0.0.1:20444 HTTPS       | Local mail capture                                       |
| `/oauth/register` per IP                        | ≤ 5/hour                                            | OAuth and MCP rate policies                              |
| `/oauth/token` per IP                           | ≤ 30/min                                            | OAuth and MCP rate policies                              |
| Failed grants per client                        | ≤ 10 per 15 min                                     | OAuth and MCP rate policies                              |
| `/mcp` tool calls per token                     | ≤ 120/min                                           | OAuth and MCP rate policies                              |
| `/mcp` tool calls per user                      | ≤ 240/min                                           | OAuth and MCP rate policies                              |
| Concurrent `/mcp` requests/user                 | ≤ 4                                                 | OAuth and MCP rate policies                              |
| Live grants per user                            | ≤ 10 (11th consent refused)                         | Consent handler                                          |
| `/mcp` request body                             | ≤ 4,194,304 bytes                                   | OAuth and MCP routes                                     |
| OAuth request bodies                            | ≤ 4,096 bytes                                       | OAuth and MCP routes                                     |
| Idle-client GC                                  | 24 h idle; ≤ 200 rows/sweep                         | OAuth server                                             |

Notes on rows whose reason is not obvious:

- **Documents.** The `lng` row is the database's coarse bound; the HTTP boundary
  canonicalizes BCP 47 tags first ([data design](data.md#relational-model)).
  Request-path idempotency cleanup is opportunistic; the hourly sweep is the
  retention guarantee ([ADR 0016](../adr/0016-transactional-idempotency.md)).
- **Photos.** The edge and pixel caps admit a 4,032×3,024 photo while bounding
  an eight-byte-per-pixel working image at 128 MiB. The decoders are
  synchronous, so the five-second normalization ceiling is a measured gate on a
  frozen hostile corpus, repeated on the production host, not a timer. A failing
  fixture blocks release; it never creates detached work.
- **Rate limiters.** Each instance keeps at most 10,000 keys with the shared
  overflow of [ADR 0018](../adr/0018-bounded-rate-limiter.md). Resume read and
  write limits leave room for several editor tabs above the one-second autosave
  cadence. Operation counts per request are separate from the 256 KiB body cap.
- **Public render.** `canonicalOrigin` is one normalized ASCII origin with no
  userinfo, path, query, or fragment; production requires `https`. The 532,480
  byte request is the 524,288-byte document ceiling plus an 8,192-byte envelope.
  The HTML bound stays provisional until minimal, full, and 512 KiB fixtures
  pass beneath it. The five-second revocation bound is one deadline across every
  drain and fails before the mutation transaction.
- **Slugs and media.** The slug limit applies only when a request changes the
  slug. Orphan deletion retries each object up to three times, waiting one and
  then two seconds; queue retries double from one minute to six hours.
- **Passwords.** Argon2id parameters meet the OWASP minimum; unknown and
  provider-only logins run the same admitted dummy verification. The blocklist
  is the pinned NCSC 100k list as sorted SHA-256 digests. HIBP gets a padded
  five-character SHA-1 prefix and fails closed when needed but unavailable.
- **Second factor.** WebAuthn sizes are decoded-byte ceilings checked before
  CBOR, COSE, or signature work; the 32 KiB body covers encoding overhead. The
  factor-management limit keeps bounded cleanup ahead of creation. The TOTP
  budget lives on the credential row, so it holds across IPs and restarts: at
  most 35 guesses the first day and 5 a day after, and it never blocks a passkey
  or recovery code.
- **Agent access.** Five registrations an hour per IP admit a real first
  connection while making bulk clients useless. Per-token and per-user tool
  limits keep one agent from starving another. The eleventh consent is refused
  rather than evicting a grant. `/mcp` allows 4 MiB so `upload_photo` fits a
  base64 2 MiB photo plus the JSON-RPC envelope; decoded media limits still
  apply.
- **Realtime.** Queue overflow disconnects a slow consumer so its next
  unconditional read repairs it. The two-second write deadline fits inside the
  five-second revocation drain.

## Benchmark protocol

A threshold without a reproducible measurement is not a gate. A synthetic run
over tiny documents can pass while representative documents fail. Measure each
applicable target under this protocol and retain the raw evidence.

| Parameter          | Requirement                                                                                                                                                                                                                                                        |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Fixture corpus     | Three sizes measured separately: `minimal.json`, `full.json` (typical), and a **worst-case document at the 512 KB bound** with max sections/entries and 16 KB rich text                                                                                            |
| Hardware / limits  | The media intake gate pins the local host identity, toolchain, CPU quota, and 512 MiB controlled cgroup for its provisional gate. A production run repeats on the ARM64 Graviton host and task cgroup; an unpinned laptop run is never accepted as launch evidence |
| Concurrency        | Stated per target; SSE measured with connection **churn**, not just steady state                                                                                                                                                                                   |
| Warm-up            | Discarded warm-up period before sampling; cold-start measured separately and reported as its own number                                                                                                                                                            |
| Duration & samples | Minimum sustained duration and sample count declared per target; single-shot numbers are not accepted                                                                                                                                                              |
| Percentile method  | Explicit (e.g. HdrHistogram); **queue time is included** in latency, never excluded                                                                                                                                                                                |
| Repeatability      | Repeat runs; a target passes only if it holds across runs, not on a best-of                                                                                                                                                                                        |
| Evidence           | Raw results retained locally and cited by the change that claims the gate                                                                                                                                                                                          |

The 512 MiB cgroup, render timeout, queue depth, and pgx pool are hard runtime
limits. The render timer runs 20 seconds from admission; on timeout the queue
cancels the job and returns only after browser teardown joins. A timed-out job
never counts as a latency sample, and a success above 20 seconds fails the
benchmark. The latency rows are service-level objectives (SLOs) measured against
the corpus above, not enforced per request. When a measurement contradicts a
number, the number changes and the change cites the evidence.
