# CrowdSec privacy-change notification

Status: accepted. Owner approval is recorded below. The accepted
[CrowdSec feed design](crowdsec-http-privacy.md) owns the collection change. The
[initialization contract](crowdsec-privacy-notice-initialization.md) adds
first-run reconciliation and bootstrap ordering. Follow the
[operator procedure](../runbooks/crowdsec-privacy-notice.md) for first rollout.

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
| 5     | The owner issues [send authority](crowdsec-privacy-notification-commands.md#send-authority-and-restore-fence) and enqueues bounded batches for the worker. Accounts created after sealing see the updated notice in registration and stay outside the cohort. Even previously loaded pages' registration and privacy links must resolve to the deployed notice.               |
| 6     | Once all members are accepted or deleted, check activation; host-bundle installation switches CrowdSec storage before the separate image deployment, under the [feed design](crowdsec-http-privacy.md#migration-loss-and-release-order). Time and queue emptiness grant no permission; there is no additional waiting period.                                                 |

Zero accounts require the same membership proof. Addresses go only through
existing SMTP to Bizfly, never to laptops, artifacts, or command arguments.

## Durable state

Add two goose tables in the app database with explicit `aboutme_app` grants.

`privacy_notice_campaigns` allows only ID `crowdsec-postgres-paths-v1`:

| Field                                                                                                                                                                                                                 | Contract                                                                                                                                                                                                                                                                                                               |
| --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `id`                                                                                                                                                                                                                  | Text primary key, fixed campaign literal.                                                                                                                                                                                                                                                                              |
| `content_sha256`                                                                                                                                                                                                      | `bytea`: 32 raw SHA-256 bytes of the manifest below.                                                                                                                                                                                                                                                                   |
| `cutoff_at`, `expected_count`, `capture_cursor`                                                                                                                                                                       | `timestamptz`, nonnegative `bigint`, nullable UUID respectively.                                                                                                                                                                                                                                                       |
| `state`                                                                                                                                                                                                               | Text: `capturing`, `sealed`, `activated`, `aborted`, or `retired`.                                                                                                                                                                                                                                                     |
| `sealed_at`, `first_activated_at`, `activated_at`, `aborted_at`, `retired_at`, `purged_at`                                                                                                                            | Nullable `timestamptz`. Sealed/activated require seal time; current activation is non-null exactly in activated state; purge requires `retired_at`.                                                                                                                                                                    |
| `activation_count`                                                                                                                                                                                                    | Nonnegative `bigint NOT NULL DEFAULT 0`; first activation is null exactly when count is zero; activated requires count > 0. First time/count history survive every cycle, abort, and purge; current time follows [activation transitions](crowdsec-privacy-notification-commands.md#send-authority-and-restore-fence). |
| `accepted_count`, `deleted_count`, `last_accepted_at`                                                                                                                                                                 | Nullable nonnegative bigints and nullable `timestamptz`, written at activation and retained through retirement purge. Accepted plus deleted equals expected; time is null exactly when accepted is zero.                                                                                                               |
| `recovery_count`, `recovery_expired_count`, `recovery_attempts_exhausted_count`, `recovery_provider_rejected_count`, `recovery_payload_invalid_count`, `recovery_job_missing_count`, `recovery_delivery_failed_count` | Nonnegative `bigint NOT NULL DEFAULT 0`. Total equals the six reason counters' sum without overflow. Count committed generations, not failed attempts; totals may exceed cohort size. Preserve through deletion, activation, abort, retirement, and purge.                                                             |

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
expiry/decryption failure; intentional abort uses
[cancellation](crowdsec-privacy-notification-commands.md#permanent-abort). Clear
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

[Owner commands and recovery](crowdsec-privacy-notification-commands.md) owns
send authority and the restore fence, activation transitions, recovery,
permanent abort, and the receipt and aggregate wire contract.

### Send authority and restore fence

See
[send authority and restore fence](crowdsec-privacy-notification-commands.md#send-authority-and-restore-fence).

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

| Area              | Required hosted proof                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| ----------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Manifest          | Frozen manifest bytes/hex for both languages and a fixed date, independently reproduced in Go and shell; deployed text/date match; changed Unicode, LF/CR, field order, content, or date changes the hash; receipt order/escapes do not.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Wire fixtures     | Cross-implementation receipt/aggregate fixtures, zero/max/overflow counts, duplicate/unknown keys, bad UTF-8/types/version/times/digests, epoch/release mismatch, receipt-free authorization, and interrupted activation recovery. All four commands' exact stdout/stderr/exits, sealed prepare retries without send authority, partial capture, enqueue progress/worker wait, no-mutation queue_full, blocked/invalid precedence, and missing enqueue authority.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| Recovery counters | Atomic recovery counters for all reasons, retries/rollback/overflow, repeated generations, concurrent deletion, and survival through abort/retirement/purge. Late-failure sent-job abort preserves retention; recovery reuses sent job ID with a new generation or reconstructs an absent job atomically.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
| Activation cycles | First activation, late failure, later acceptance/reactivation, and repeated receipt reconstruction preserve first/count/current rules; old send authority cannot claim recovered generations before renewal, including after crashes.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       |
| Abort             | Every abort recipient/job pair, missing/orphan jobs, every interruption point, concurrent deletion, legal terminal clearing, and resumable completion.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Cutoff            | Exact cutoff membership, zero users, pending registration, concurrent account creation, resumed capture, maintenance interruption, and unchanged cutoff.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Enqueue           | Concurrent/repeated enqueue, batch/backlog limits, statement/command timeouts, missing encryption keys, and rollback between outbox and ledger writes.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Payload           | Strict payload validation, reparented jobs, generation replay, fixed bilingual text/HTML copy, canonical link, and no raw values in forced-error output.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Delivery          | Authentication-mail priority, temporary failure, expiry, attempt exhaustion, terminal/decryption failure, stale leases, and acceptance/commit crash.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| Deletion          | Account deletion before enqueue, during a send, after acceptance, and after job cleanup, with unchanged cohort totals, terminal deleted outcomes, correct accepted-plus-deleted totals, and no send after deletion commits.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| Durability        | Accepted/terminal atomicity, seven-day cleanup, later activation retries/crashes and delivery failures, retirement-only purge, and no repeat activated/retired notice.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Send gate         | Missing/malformed/stale-epoch send receipts block both claim and send while ordinary mail continues; a receipt revoked after claim blocks SMTP.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| Restore           | Cluster/PITR restore, a restored sent job, host replacement, crash/reboot, and old-receipt replay retain denial until current owner authority; prove pre-revocation worker quiescence and no DB marker clearing. Use actual files, subprocesses, FDs, flock, and controlled fake DB/system commands, not static ordering alone. Cover exact action/root/pipe parsing and override refusal; operation-before-workload contention through replacement, recovery, every DB check and postcheck; privilege-drop descriptor inheritance and parent/child kills with surviving descendants; all-holder death/reboot and systemd recovery settling; distinct initial/latest/PITR/retry epochs despite absent receipts; every check/persistence failure, malformed/truncated/extra pipe record, early EOF, lost helper, signal, SSH loss, and mount/owner/epoch change. Require completion plus helper join, not unlocked SSH checks. Strict release rejects empty/wrong owners; inject failure before store, after rename, after sync, and after release before caller success. Both durable outcomes retain notice denial without false PASS, takeover, or auto-retry. Preserve floor, DB-check meanings/recovery timing, app absence, --resume refusal, and fixed output without private records. Fixtures prove protocol, not physical power loss; drill evidence never replaces full-host reconciliation/send/consumer proofs. |
| Host mounts       | Both install modes use the same primitive before the PostgreSQL-only exit, without images/app commands or transferred authority. Cover missing/non-ext4 data mounts, absent binds despite root shadow files, missing/duplicate/ambiguous labels, wrong data/bind source, nested/stacked mounts, device/inode mismatch, and changed identity after locking. Refusal precedes authority access; prove guest provenance only, never a provider-ID oracle.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| Host adoption     | Pinned legacy eligibility rejects unknown/post-notice hosts; intent precedes helper replacement under legacy cutover guards. Cover ownership/modes, unsafe paths, each persistence failure/crash, exact-operation resume, lost/corrupt adopted state, lost evidence, preserved fields on every fence write/reinstall/full-host continuation, and startup denial during initialization. Prove notice_restore start blocking with no db-setup exception, recovery/maintenance availability, and unchanged other-operation guards.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
| Activation gate   | All activation paths reject missing/mismatched receipts, incomplete counts, failure, old notice releases, database restore, and unsupported old workers.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                    |
| Recovery boundary | Reviewed recovery cannot retry accepted/deleted rows, expose recipients, bypass restore reconciliation, or extend a job lifetime without a new generation; irrecoverable active-account failure can only abort the campaign.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                |
| Publication       | The existing privacy promise and new publication date appear in both deployed languages before SMTP begins, and changed ingestion starts only after the gate.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                               |

Production evidence contains release digests, publication date, fixed campaign
and content digest, cutoff, aggregate counts, acceptance/activation times, and
fence result only. Keep recipient lists, job IDs, message bodies, addresses, and
provider screenshots out of artifacts, public CI, and the repository.

## Owner decisions and sources

The owner approved the copy and delivery/deletion/abort rules above and in
[owner commands](crowdsec-privacy-notification-commands.md) on 2026-10-19.
Database integrity and absent delivery callbacks remain limits. Independent
design review approved the wire and host-fence clarifications on 2026-10-19.

First-party privacy: [English](../../apps/web/app/i18n/legal-en.ts) and
[Vietnamese](../../apps/web/app/i18n/legal-vi.ts);
[`job wrapper`](../../deploy/vn/host/job-run.sh),
[mail worker/outbox/SMTP](https://github.com/dannyota/aboutme/tree/main/apps/server/internal/authmail),
[email runbook](https://github.com/dannyota/aboutme/blob/main/docs/runbooks/email.md).
