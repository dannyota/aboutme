# CrowdSec privacy-change notification

Status: accepted. Owner approval is recorded below. The accepted
[CrowdSec feed design](crowdsec-http-privacy.md) owns the collection change.

Publish the dated notice, then email every cutoff account before collection
changes, as promised. Disable changes until all sends are accepted or accounts
deleted. Forbid public/admin endpoints, periodic broadcasts, recipient
exports/overrides, and arbitrary messages for this fixed transactional notice.

## Authority and limits

Use the encrypted `auth_email_jobs` outbox and Bizfly sender. An application
ledger preserves membership/outcomes beyond seven-day outbox cleanup. A root-run
one-shot command creates/enqueues notices without a listener; the worker sends.
[ADR 0003](../adr/0003-public-namespace-and-no-operator-surface.md) permits this
out-of-band operator boundary.

Keep one logical job per account and at-least-once retries: a crash after SMTP
acceptance before commit can duplicate delivery. A committed successful reply
after `DATA` means accepted send, not inbox delivery. There is no Bizfly
webhook; see the [email runbook](../runbooks/email.md#feedback).

A page-only notice breaks the promise. Address export adds a transfer and
delivery authority; a new queue duplicates encryption/retries. Outbox-only
evidence disappears on expiry, deletion, or cleanup.

## Release and cutoff

| Order | Required action                                                                                                                                                                                                                                                                                                                                                               |
| ----- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1     | Publish the feed design's copy with the actual date in both languages; deploy schema, fixed template, worker, command, and host send/activation fences. Keep expanded feed and PostgreSQL attack ingestion disabled. Verify both `/privacy` languages before sending. Keep the terms date and promise intact.                                                                 |
| 2     | Under the workload lock, enter maintenance, stop all account writers including the server, and confirm quiescence through the deployer. Before cutoff, persist a root-owned capture marker blocking restart until sealing, including after interruption. Previously disclosed protection may continue during notification.                                                    |
| 3     | `server crowdsec-privacy-notice prepare` fixes database UTC cutoff and committed `users` count with writers stopped. Capture only account IDs, UUID-ordered, at most 100 per batch, committing the cursor with each batch. Pending password registrations are excluded. Retries reuse cutoff/cursor; never recapture or classify later accounts by `created_at` or UUID time. |
| 4     | Count equality and bidirectional anti-joins against quiescent `users` prove exact membership in the sealing transaction. Clear the marker and resume with updated notice. Pre-seal crashes resume under maintenance; changed membership or missing quiescence requires reviewed recovery.                                                                                     |
| 5     | The owner issues send authority below and enqueues bounded batches for the worker. Accounts created after sealing see the updated notice in registration and stay outside the cohort. Even previously loaded pages' registration and privacy links must resolve to the deployed notice.                                                                                       |
| 6     | Once all members are accepted or deleted, check activation and perform the feed design's fenced cutover. Time and queue emptiness grant no permission; there is no additional waiting period.                                                                                                                                                                                 |

Marker, campaign, and count expose incomplete capture across reboot. Zero
accounts require the same membership proof. Addresses go only through existing
SMTP to Bizfly, never to laptops, artifacts, or command arguments.

## Durable state

Add two goose tables in the app database with explicit `aboutme_app` grants.

`privacy_notice_campaigns` allows only ID `crowdsec-postgres-paths-v1`:

| Field                                                                                                                                                                                                                 | Contract                                                                                                                                                                                                                                                   |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`                                                                                                                                                                                                                  | Text primary key, fixed campaign literal.                                                                                                                                                                                                                  |
| `content_sha256`                                                                                                                                                                                                      | `bytea`: 32 raw SHA-256 bytes of the manifest below.                                                                                                                                                                                                       |
| `cutoff_at`, `expected_count`, `capture_cursor`                                                                                                                                                                       | `timestamptz`, nonnegative `bigint`, nullable UUID respectively.                                                                                                                                                                                           |
| `state`                                                                                                                                                                                                               | Text: `capturing`, `sealed`, `activated`, `aborted`, or `retired`.                                                                                                                                                                                         |
| `sealed_at`, `first_activated_at`, `activated_at`, `aborted_at`, `retired_at`, `purged_at`                                                                                                                            | Nullable `timestamptz`. Sealed/activated require seal time; current activation is non-null exactly in activated state; purge requires `retired_at`.                                                                                                        |
| `activation_count`                                                                                                                                                                                                    | Nonnegative `bigint NOT NULL DEFAULT 0`; first activation is null exactly when count is zero; activated requires count > 0. First time/count history survive every cycle, abort, and purge; current time follows transitions below.                        |
| `accepted_count`, `deleted_count`, `last_accepted_at`                                                                                                                                                                 | Nullable nonnegative bigints and nullable `timestamptz`, written at activation and retained through retirement purge. Accepted plus deleted equals expected; time is null exactly when accepted is zero.                                                   |
| `recovery_count`, `recovery_expired_count`, `recovery_attempts_exhausted_count`, `recovery_provider_rejected_count`, `recovery_payload_invalid_count`, `recovery_job_missing_count`, `recovery_delivery_failed_count` | Nonnegative `bigint NOT NULL DEFAULT 0`. Total equals the six reason counters' sum without overflow. Count committed generations, not failed attempts; totals may exceed cohort size. Preserve through deletion, activation, abort, retirement, and purge. |

`privacy_notice_recipients` has these fixed fields:

| Field                                                                 | Contract                                                                                                                                                         |
| --------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`                                                                  | Random UUID primary key.                                                                                                                                         |
| `campaign_id`                                                         | Campaign foreign key with restricted deletion.                                                                                                                   |
| `user_id`                                                             | Nullable UUID referencing users with ON DELETE SET NULL; unique `(campaign_id, user_id)` while non-null.                                                         |
| `job_id`                                                              | Immutable UUID, unique and non-null, allocated at capture; no outbox foreign key.                                                                                |
| `state`                                                               | `unqueued`, `queued`, `accepted`, `deleted`, `failed`, or `cancelled`.                                                                                           |
| `generation`                                                          | Integer >= 0, initially zero; only reviewed recovery increments it.                                                                                              |
| `queued_at`, `accepted_at`, `deleted_at`, `failed_at`, `cancelled_at` | Nullable timestamps; require the state's outcome time. Cancellation preserves earlier times; queued/accepted times may survive failure/deletion.                 |
| `failure_code`                                                        | Nullable `expired`, `attempts_exhausted`, `provider_rejected`, `payload_invalid`, `job_missing`, or `delivery_failed`; forbidden for accepted/deleted/cancelled. |

Deleted requires null user. Index `(campaign_id, state, id)` and `user_id`.
Neither table stores email, name, token, body, SMTP reply, or provider ID;
campaign has no individual identity.

Every batch/worker transition verifies compiled campaign and digest. Unknown
campaign, digest mismatch, invalid state, unexpected jobs, or missing ledger
fail closed. Enqueue/workers lock account, recipient, then job; capture/status
never reverse this order. One campaign advisory lock serializes
capture/enqueue/recovery. Status reads one consistent snapshot without writes.

Keep recipients after activation for delayed recovery and late failures; deleted
accounts leave null-user rows without addresses. Forbid seven-day/scheduled
purge. Reviewed retirement alone marks retired and purges at most 200 rows per
batch, after permanent collection disablement and receipt revocation. Keep
campaign and retirement aggregates forever against replay. Routine commands
refuse aborted/retired campaigns. Unresolved members require completed abort
before retirement; abort keeps the ledger. Application backup policy applies.

### Content manifest bytes

Manifest version 1 is UTF-8 without a byte-order mark. Its first line is exactly
`aboutme-crowdsec-privacy-content-v1`. Append these `key=value` lines in order:
`campaign_id`, `publication_date`, `canonical_link`, `public_en`, `public_vi`,
`email_subject`, `email_button`, `email_heading_vi`, `email_heading_en`,
`email_preheader`, `email_body_vi`, `email_body_en`. Every line, including the
last, ends with one LF byte (0x0a). There are no blank lines or other bytes.

Use the fixed campaign and `https://aboutme.vn/privacy`. Before preparation, fix
both notices' actual publication date: Gregorian `YYYY-MM-DD`, years 0001..9999.
Public values are the quoted
[notice paragraphs](crowdsec-http-privacy.md#public-notice-and-release); email
values are the [fixed strings and bodies](#fixed-bilingual-message). Remove
Markdown quote prefixes and join each paragraph's wrapped lines with one ASCII
space. Remove Markdown delimiters/labels, preserving all other code points,
including combining/precomposed Vietnamese. Never normalize, trim, translate,
HTML-escape, or JSON-encode. Forbid CR, LF, NUL, and edge whitespace in values.
Render these values; do not hash layout, HTML markup, or the existing footer.

Hash all bytes with SHA-256. CLI/receipt `content_sha256` is 64 lowercase ASCII
hex characters without prefix/separators, decoding to the database's 32 bytes.
Go writes literal UTF-8; shell uses `LC_ALL=C`, fixed `printf '%s\n'` per line,
never `echo`, locale transforms, or JSON. Compiled server and reviewed host
bundle carry the same manifest and expected digest.

## Enqueue and delivery

With valid send authority and no restore marker,
`server crowdsec-privacy-notice enqueue` handles 100 unqueued members at most,
in ledger-ID order. Cap concurrent pending/leased notice jobs at 100; return
fixed `queue_full` without mutation when full. Read live email only in
production; `Outbox.EnqueueTx` and ledger `queued` commit together or neither
does. Duplicate calls reuse the job ID. Statement timeout is ten seconds, lock
timeout two seconds, and command deadline five minutes.

Add the closed kind `privacy_notice` with user-only scope and no token digest.
Its strict payload version 3 contains only `version`, `to`, `userId`,
`noticeId`, and `generation`. Require the canonical account email, exact
campaign literal, matching UUID and generation, and no unknown or duplicate
keys. Keep the 4,096-byte plaintext bound. Derive the fixed copy and
`https://aboutme.vn/privacy` from compiled constants, never from the payload,
environment, database text, request host, or command arguments.

Keep AES-256-GCM, fresh nonces, job/kind/key-ID authenticated data,
current/previous key custody, and secret injection. Before sending, decrypt and
match account, address, notice, and generation against locked account/ledger.
Reparented jobs, replayed generations, deleted users, or bad ciphertext cannot
send.

Expiry is 24 hours after enqueue, not cutoff. Keep the 30-second lease,
ten-second send deadline, eight attempts, bounded jitter, ten-job claim batch,
and two concurrent sends. Claim due authentication/security mail first to
prevent sign-in starvation. Preserve every other mail kind's semantics.

Before notice claim and again under send locks before SMTP, require the host
send gate and sealed campaign. Missing/mismatched receipts, restore markers, or
unreadable state forbid claim/send, including already leased notices until
authorized recovery. Stale-lease requeue and old ciphertext cannot bypass this
gate; normal mail continues.

After SMTP acceptance, commit ledger `accepted` with outbox `sent`. Commit
ledger `failed` and a fixed reason with delivery-terminal transitions, including
expiry/decryption failure; intentional abort uses cancellation below. Clear
sent/terminal payloads; cleanup preserves ledger outcomes. Temporary
failure/stale-lease recovery keeps the job and `queued` ledger.
Acceptance/commit crashes may resend, never record uncommitted acceptance.

Deletion locks user, recipient, then job; erases/cascades ordinary account/mail
data; nulls `user_id`; and atomically records `deleted`, `deleted_at`, and no
failure code unless already accepted. Accepted remains accepted with null user.
Never send after deletion commits or reduce `expected_count`. Deletion waits if
sending holds the user lock; otherwise sending finds no authority. Deleted is
terminal/resolved even after failure. Null users without accepted/deleted
outcomes and unexplained missing queued jobs block activation. Missing accepted
jobs after cleanup are normal. Recovery never recreates deleted accounts or
mail.

## Owner commands, activation, and recovery

Allow only `prepare`, `enqueue`, `status`, and `activate`, without recipient,
subject, body, URL, campaign, or cutoff arguments. The current release wrapper
pins the image and holds release/workload locks. Commands use the Unix socket as
`aboutme_app`, `--network=none`, 256 MiB memory, and no SMTP/media credentials.
Only enqueue needs mail encryption keys. Only the normal worker uses SMTP
credentials/network access. Forbid command timers and HTTP/MCP dispatch.

### Send authority and restore fence

Root-owned `/var/lib/aboutme/privacy-notice/` stays outside restored
data/backups: directory 0755, files 0444, no container-writable parent or
alternate path. Files: random UUID `epoch`, `send.json`, `activation.json`, and
`restore-reconciliation` when needed. Atomically replace receipts and durably
sync file/directory. Mount the directory read-only into server/commands; forbid
individual mounts and cached checks so replacement/removal stays visible. Server
cannot write authority files.

Only owner-run host `authorize-send` issues initial `send.json`, after the wire
contract's checks below. Startup, deploy, queue retry, and database state cannot
authorize sending.

Before every cluster or point-in-time restore, including drills/new hosts, stop
the server, join sends, persist `restore-reconciliation`, rotate epoch, and
durably remove both receipts on the target before database replacement/startup.
Never recover authority from an old database or host snapshot. New hosts start
fenced; interruptions/reboots preserve the marker. Ordinary mail may resume with
notices disabled.

Reviewed reconciliation handles ambiguous sends and restored ledger/job
generations in production; restored queues prove no delivery. The marker forbids
all send/activation authorization. After reconciliation commits, a reviewed host
operation durably removes it; `authorize-send` can issue current epoch/cohort
authority. Crashes leave notices disabled pending explicit owner resumption.
Revocation stops/joins workers before receipt removal, fencing in-flight sends.
Activated, aborted, or retired campaigns receive no send receipt.

Status/results follow the wire contract below; forbid raw database/provider
errors. Activation forbids unqueued, queued, failed, cancelled, or unexplained
members. Empty outbox proves nothing; retained ledger governs recovery and late
failures.

`activate` rechecks the ledger under host locks; restore markers forbid it.

| Transition                              | Required behavior                                                                                                                                                                                                                                                                                                                              |
| --------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| sealed to activated                     | In one transaction set state activated and current `activated_at` from the database clock at transition, at least seal/first activation/latest acceptance; replace final counts/acceptance time; increment `activation_count`; set `first_activated_at = activated_at` only if first is null. Clock failure or overflow rolls back everything. |
| activated receipt reconstruction        | Recheck retained ledger; change no timestamps, counts, or state.                                                                                                                                                                                                                                                                               |
| activated to sealed after known failure | First revoke activation authority and stop changed ingestion; clear current `activated_at`, preserving first time and activation count.                                                                                                                                                                                                        |

After activation/reconstruction, revoke send authority and write
`activation.json`. Every expanded-feed install/start/rollback/restore requires
it, matching notice release, and the CrowdSec retention gate. Reconstruction
works months later; missing receipts/old epochs cannot reopen ingestion. Forbid
overrides/hand-supplied success; trust root.

Before activation, the owner checks Bizfly failures and records only production
aggregates. Reviewed recovery marks accepted existing accounts failed with
`delivery_failed` on known failure; deleted accounts resolve as `deleted`
without contact. Absent feedback confirms nothing. The transition table governs
late failures at any age.

Terminal failure, expiry, job/key loss, or inconsistency blocks activation.
Forbid automatic re-enqueue, skips, cohort changes, and overrides. Independently
reviewed owner-run recovery selects fixed reasons in sealed campaigns. Under
host locks, stop/join workers and durably revoke send authority before
failed-to-queued mutation. Keep notices fenced through commit until fresh owner
authority; ordinary mail may resume with notices disabled. For failed existing
accounts, atomically increment generation, reuse job ID, reseal current address
with a fresh nonce, reset attempts/24-hour lifetime, requeue, and clear
`failure_code`. Under account/recipient/job locks, the same transaction adds one
to `recovery_count` and the counter for the prior failure reason. All six
reasons qualify; missing/unknown reasons refuse recovery. Queued
retries/rollback add nothing. Counter/generation overflow rolls back everything;
never clamp, wrap, or reset history. Never retry accepted/deleted rows;
documented late failure first marks accepted as failed. Renew authority after
recovery without a restore marker. Irrecoverable active accounts require
permanent abort and disabled changed ingestion. Recipient evidence stays in
production.

### Permanent abort

The owner executes independently reviewed fixed SQL for this campaign as
`aboutme_app` through the production wrapper, under host/campaign locks and
command isolation, without mail keys. Abort adds no routine command or arbitrary
input. Stop/join workers, stop changed ingestion, and durably revoke both
receipts first. Require sealed membership; finish capturing under maintenance
first. Atomically latch `aborted` and clear current `activated_at`; retries
retain the latch and `aborted_at`. Routine notice claim/retry/expiry stop;
ordinary mail and terminal/sent cleanup may continue. No authority can be
reissued.

Each invocation takes at most 100 unqueued/queued IDs in order under
account/recipient/job locks and enqueue's time bounds. Changed rows leave this
selection, so retries advance without a cursor. Valid pairs and atomic outcomes:

| Recipient | Matching job          | Abort outcome                       |
| --------- | --------------------- | ----------------------------------- |
| unqueued  | absent                | cancelled                           |
| queued    | pending/leased/absent | cancelled; terminalize any job      |
| accepted  | sent/absent           | unchanged                           |
| deleted   | absent                | unchanged                           |
| failed    | terminal/absent       | unchanged, including failure reason |
| cancelled | terminal/absent       | unchanged on retry                  |

Cancellation sets `cancelled_at` to database UTC and clears `failure_code`; it
is not a delivery failure. Terminalization sets `terminal_at` to that same time
and nulls `key_id`, `nonce`, `ciphertext`, `next_attempt_at`, `lease_owner`, and
`lease_expires_at`; `sent_at` stays null. Preserve attempts and creation/expiry
times. Other pairs, foreign jobs, or invalid state stop the batch with the abort
latch intact. Never delete jobs for abort or leave processed rows queued.
Validate all other pairs, count equality, and absence of unqueued/queued rows
and pending/leased/orphan notice jobs before setting `aborted_at` once to
database UTC. It stays null until completion; completed retries change nothing.
Interrupted operations resume; retirement requires completion. Preserve cohort
count, outcomes, recovery history, and lifetime times; normal atomic account
deletion still applies.

Restore fences also cover jobs accepted after the restore point. Older v0
workers may lack this kind: stop workers for schema reconciliation, deploy
compatible code before enqueue, and recover forward if rollback loses
ledger/notice. [ADR 0005](../adr/0005-database-migrations.md) permits pre-v1
edits with production reconciliation. Client API and resume schema stay intact.

### Receipt and aggregate wire contract

Receipts are closed JSON objects, at most 4,096 UTF-8 bytes, no byte-order mark.
Require every field; reject null, unknown/duplicate decoded keys, bad Unicode,
trailing nonwhite data, or wrong types/versions. Order, whitespace, and
equivalent escapes do not matter; never hash receipts. Numbers are exact decimal
integers without signs, fractions, exponents, or leading zeros. Never use
floats.

Common fields: `schema_version` (number `1`), `campaign_id` (fixed string),
`content_sha256` (hex string above), `epoch` (lowercase canonical UUID v4
string, variant bits 10), `cutoff_at` (timestamp string), `expected_count`
(count number). `send.json` adds only `issued_at` (timestamp). `activation.json`
adds only `accepted_count`, `deleted_count` (counts), and `activated_at`
(timestamp). Counts are `0..9223372036854775807`; sum without overflow.
Activation requires `accepted_count + deleted_count = expected_count`.

Timestamps are exactly `YYYY-MM-DDTHH:mm:ss.ffffffZ`: Gregorian UTC, years
0001..9999, seconds 00..59, six fractional digits. Reject offsets, leap seconds,
infinity, or other precision; compare stored microseconds without rounding. The
`epoch` file is the current UUID in ASCII plus one LF. Receipt epoch must match
it; cutoff/count/digest/campaign match the row; activation totals/time match its
activation record and rechecked ledger. Send issue time uses host UTC, at least
`sealed_at`, never future-dated; activation time is stored `activated_at`, at
least `sealed_at` and latest acceptance, never future-dated. Require
`cutoff_at <= sealed_at`. Clock failure/mismatch refuses authority; no time
expiry.

`status` and `activate` emit one closed JSON object plus LF on stdout under the
receipt parsing rules and 4,096-byte bound, except nullable times below. Fields:
`schema_version`, `result`, `campaign_id`, `content_sha256`, `cutoff_at`,
`expected_count`, `state`, `actual_count`, `counts`, `failure_counts`,
`recovery_count`, `recovery_counts`, `activation_count`, `sealed_at`,
`first_activated_at`, `activated_at`, `aborted_at`, and `last_accepted_at`.
State is a campaign-state string. Counts are numbers; `counts` has exactly
`unqueued`, `queued`, `accepted`, `deleted`, `failed`, `cancelled`;
`failure_counts` has exactly the six failure-code keys from Durable state. Each
nested value follows the count bound. Outcome counts sum to actual count;
failure counts sum to current failed members. `recovery_counts` has the same six
reason keys, mapped to campaign counters; its sum equals `recovery_count`, not
current failed count. History remains visible on blocked status. Times use the
format above; seal/first/current/abort times and activation count mirror the row
under the lifecycle/null rules above. Latest acceptance is the maximum
`accepted_at` among current accepted rows, null when none. Emit no recipient/job
data. The closed `result` string is `ready` (status exit 0, sealed/activated,
all resolved, valid host), `activated` (activate exit 0), `pending` (exit 2,
capturing/sealed with work left), or `blocked` (exit 1, failed members or
aborted/retired state). Pending/blocked activation changes nothing.
Invalid/unreadable state or host gates emit no JSON, only `invalid_state` plus
LF on stderr and exit 1. JSON results emit no stderr.

For send authorization, `status` exit 0 or 2 requires `sealed`, actual equal to
expected, valid counts, and no failures. Missing receipts permit this read and
activated recovery, never sending/ingestion. Invalid receipts or restore markers
return 1. Under host locks, validate the object, read epoch, verify deployed
pinned server/web release and host bundle match manifest digest/date, then
construct send. For activation it uses successful `activate` output, requiring
exit 0, state `activated`, and current accepted plus deleted equal to expected.
Receipt reconstruction calls `activate` only for an activated campaign,
returning its unchanged current time/count; first revoke remaining send
authority with the stop/join sequence. Recheck epoch, marker, and deployed
release before durable receipt replacement; failure writes nothing. This is a
local aggregate exchange through the existing four commands, with no new
arguments, endpoint, credential, or authority source.

## Fixed bilingual message

Reuse the existing mail layout, Vietnamese first then English, with both plain
text and HTML, existing Bizfly/Google Workspace disclosure, and no external
assets, tracking, personal greeting, token, or unsubscribe action. The single
button and plain-text fallback use only `https://aboutme.vn/privacy`.

- Subject: `Cập nhật quyền riêng tư aboutme.vn / aboutme.vn privacy update`
- Button: `Đọc chính sách quyền riêng tư · Read the privacy policy`
- Vietnamese heading: `Thông báo trước về thay đổi chống tấn công`
- English heading: `Advance notice of an attack-protection change`
- Preheader:
  `Chính sách quyền riêng tư đã được cập nhật. The privacy policy has been updated.`

Vietnamese body:

> Chúng tôi đã cập nhật chính sách quyền riêng tư của aboutme.vn. Trước khi áp
> dụng thay đổi, chúng tôi gửi email này để thông báo: mẫu cảnh báo tấn công có
> thể chứa đường dẫn đích của tuyến không được nhận diện trả về mã 400, 404 hoặc
> 405, tối đa 512 byte và không có phần truy vấn. Đường dẫn của tuyến đã biết,
> gồm tài khoản, API và CV công khai hợp lệ, không được ghi. Dữ liệu CrowdSec sẽ
> được lưu trong PostgreSQL tại Việt Nam; bản ghi đang hoạt động được giữ tối đa
> 24 giờ. Bản sao lưu được mã hóa tại Việt Nam có thể giữ bản ghi cũ trong
> khoảng năm tuần sau khi xóa. Vui lòng đọc chính sách đã cập nhật để biết đầy
> đủ phạm vi thu thập và cách lưu giữ. Bạn không cần thực hiện thao tác nào. Bạn
> có thể trả lời email này nếu có câu hỏi.

English body:

> We have updated the aboutme.vn privacy policy. Before this change takes
> effect, we are emailing you to explain it: attack alert samples may include
> target paths for unrecognized routes returning 400, 404, or 405, up to 512
> bytes and without the query. Paths for known routes, including account, API,
> and valid public resume routes, are not recorded. CrowdSec data will be stored
> in PostgreSQL in Vietnam; live records are kept for at most 24 hours.
> Encrypted backups in Vietnam may retain earlier records for about five weeks
> after deletion. Please read the updated policy for the full collection and
> retention rules. You do not need to take any action. You can reply to this
> email with questions.

## Security and required proof

CLI keys use host secret injection; logs contain only fixed codes and
aggregates.

| Attacker                                                     | Boundary and limit                                                                                                                                                                          |
| ------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Visitors, users, hostile content, stolen sessions/MCP tokens | Cannot invoke commands or choose recipients/copy. Authorized deletion erases only the owner's account and obligation, never another account's row.                                          |
| Network position                                             | Verified SMTP TLS; the fixed HTTPS link grants no capability.                                                                                                                               |
| Web/render container                                         | No mail keys or database credentials.                                                                                                                                                       |
| Server container                                             | Holds both authorities and can bypass send checks; the independent root fence limits feed enablement. Send gates protect normal workers against replay/restores, not stolen SMTP authority. |
| Database writer without keys                                 | Cannot forge ciphertext but can falsify accounts/outcomes. Completion trusts database integrity, not authenticated bookkeeping.                                                             |
| Leaked backup                                                | Account data keeps existing protection; payloads need separate keys. Persistent markers/epochs fence restored sending/activation.                                                           |
| Host root                                                    | Trusted; host compromise lies outside this boundary.                                                                                                                                        |

Hosted proofs must cover:

| Area              | Required hosted proof                                                                                                                                                                                                                                                                                         |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Manifest          | Frozen manifest bytes/hex for both languages and a fixed date, independently reproduced in Go and shell; deployed text/date match; changed Unicode, LF/CR, field order, content, or date changes the hash; receipt order/escapes do not.                                                                      |
| Wire fixtures     | Cross-implementation receipt/aggregate fixtures, zero/max/overflow counts, duplicate/unknown keys, bad UTF-8/types/version/times/digests, epoch/release mismatch, receipt-free authorization, and interrupted activation recovery.                                                                            |
| Recovery counters | Atomic recovery counters for all reasons, retries/rollback/overflow, repeated generations, concurrent deletion, and survival through abort/retirement/purge.                                                                                                                                                  |
| Activation cycles | First activation, late failure, later acceptance/reactivation, and repeated receipt reconstruction preserve first/count/current rules; old send authority cannot claim recovered generations before renewal, including after crashes.                                                                         |
| Abort             | Every abort recipient/job pair, missing/orphan jobs, every interruption point, concurrent deletion, legal terminal clearing, and resumable completion.                                                                                                                                                        |
| Cutoff            | Exact cutoff membership, zero users, pending registration, concurrent account creation, resumed capture, maintenance interruption, and unchanged cutoff.                                                                                                                                                      |
| Enqueue           | Concurrent/repeated enqueue, batch/backlog limits, statement/command timeouts, missing encryption keys, and rollback between outbox and ledger writes.                                                                                                                                                        |
| Payload           | Strict payload validation, reparented jobs, generation replay, fixed bilingual text/HTML copy, canonical link, and no raw values in forced-error output.                                                                                                                                                      |
| Delivery          | Authentication-mail priority, temporary failure, expiry, attempt exhaustion, terminal/decryption failure, stale leases, and acceptance/commit crash.                                                                                                                                                          |
| Deletion          | Account deletion before enqueue, during a send, after acceptance, and after job cleanup, with unchanged cohort totals, terminal deleted outcomes, correct accepted-plus-deleted totals, and no send after deletion commits.                                                                                   |
| Durability        | Accepted/terminal atomicity, seven-day cleanup, later activation retries/crashes and delivery failures, retirement-only purge, and no repeat activated/retired notice.                                                                                                                                        |
| Send gate         | Missing/malformed/stale-epoch send receipts block both claim and send while ordinary mail continues; a receipt revoked after claim blocks SMTP.                                                                                                                                                               |
| Restore           | Cluster/PITR restore, a restored previously sent job, host replacement, crash/reboot during fence creation or reconciliation, and old-receipt replay keep notices blocked until the owner issues current send authority; prove worker quiescence before revocation and that no restored DB can clear markers. |
| Activation gate   | All activation paths reject missing/mismatched receipts, incomplete counts, failure, old notice releases, database restore, and unsupported old workers.                                                                                                                                                      |
| Recovery boundary | Reviewed recovery cannot retry accepted/deleted rows, expose recipients, bypass restore reconciliation, or extend a job lifetime without a new generation; irrecoverable active-account failure can only abort the campaign.                                                                                  |
| Publication       | The existing privacy promise and new publication date appear in both deployed languages before SMTP begins, and changed ingestion starts only after the gate.                                                                                                                                                 |

Production evidence contains release digests, publication date, fixed campaign
and content digest, cutoff, aggregate counts, acceptance/activation times, and
fence result only. Keep recipient lists, job IDs, message bodies, addresses, and
provider screenshots out of artifacts, public CI, and the repository.

## Owner decisions and sources

The owner approved the copy and delivery/deletion/abort rules above on
2026-10-19. Database integrity and absent delivery callbacks remain limits.
Independent design review approved the wire addendum on 2026-10-19.

First-party privacy: [English](../../apps/web/app/i18n/legal-en.ts) and
[Vietnamese](../../apps/web/app/i18n/legal-vi.ts);
[`job wrapper`](../../deploy/vn/host/job-run.sh),
[mail worker/outbox/SMTP](https://github.com/dannyota/aboutme/tree/main/apps/server/internal/authmail),
[email runbook](https://github.com/dannyota/aboutme/blob/main/docs/runbooks/email.md).
