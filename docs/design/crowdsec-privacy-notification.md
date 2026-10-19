# CrowdSec privacy-change notification

Status: accepted. The owner approved the fixed copy and delivery limits on
2026-10-19. The accepted [CrowdSec feed design](crowdsec-http-privacy.md) owns
the collection change.

The public privacy policy promises email before a change to collection takes
effect. Publish the revised notice and its date first, then notify every account
in a durable cutoff cohort. Keep the changed collection disabled until every
cohort member has an accepted send outcome or has deleted their account. This is
one fixed transactional notice, with no public or admin endpoint, periodic
broadcast, recipient export, recipient override, or arbitrary message input.

## Authority and limits

Use the existing encrypted `auth_email_jobs` outbox and Bizfly sender. A small
application-database ledger preserves cohort membership and outcomes beyond the
outbox's seven-day cleanup. A root-invoked one-shot command creates and enqueues
the notice; the existing worker sends it. The command never opens a listener.
[ADR 0003](../adr/0003-public-namespace-and-no-operator-surface.md) permits this
out-of-band operator boundary.

One notice means one logical job per cutoff account. SMTP retries cannot
guarantee exactly one delivered copy: a crash after provider acceptance but
before the database commit can cause another delivery. Preserve the existing
at-least-once retry policy, including this limit. A committed successful SMTP
reply after `DATA` is an accepted send outcome, not proof of inbox delivery.
Bizfly has no delivery webhook in the current integration; see the
[email runbook](../runbooks/email.md#feedback). A known terminal delivery
failure for an existing account blocks activation even if the provider
previously accepted the message. Account deletion resolves its notice obligation
without sending; it never decreases the original cohort count.

A page-only announcement would break the existing promise. Exporting addresses
to a mailing service adds a data transfer and another delivery authority. A new
mail queue duplicates encryption and retry work. Reusing only the outbox loses
completion evidence when jobs expire, are deleted, or are cleaned up.

## Release and cutoff

1. Ship the notice copy specified by the feed design, update both language dates
   to the actual publication date, and deploy the schema, fixed template, worker
   support, command, and host send/activation fences. The expanded feed and
   PostgreSQL attack ingestion remain disabled. Verify `/privacy` in both
   languages before any notification is sent. Do not change the terms date or
   promise: this is a privacy notice, not a terms amendment.
2. Under the host workload lock, enter maintenance, stop the server and all
   other account writers, and confirm quiescence through the current deployer.
   Persist a root-owned cohort-capture marker before creating the cutoff. It
   blocks app restart after interruption until the cohort is sealed. Existing
   protection may keep its previously disclosed behavior during notification.
3. `server crowdsec-privacy-notice prepare` fixes the database UTC cutoff time
   and count of all committed `users` rows while writers are stopped. Copy only
   account IDs into the ledger in UUID-ordered batches of at most 100. Store the
   cursor with each batch commit. Pending password registrations are not
   accounts. On retry, use the existing cutoff and cursor; never take a new
   cohort or rely on `created_at` or UUID time to classify later accounts.
4. Seal only after count equality and an anti-join prove exact membership in
   both directions against the quiescent `users` table. Mark the campaign sealed
   in the same transaction. Clear the capture marker and resume the app with the
   updated notice. A crash before sealing resumes capture under maintenance;
   missing quiescence or a changed account set requires reviewed recovery.
5. The owner issues the send-authorization receipt described below, enqueues
   bounded batches, and lets the normal worker drain them. Accounts created
   after sealing see the updated notice through the existing registration flow
   and are not added to the cohort. A previously loaded page does not authorize
   serving the old notice: registration and privacy links must resolve to the
   deployed notice.
6. After all members are accepted or deleted, perform the activation check and
   the feed design's fenced cutover. Never infer permission from elapsed time or
   queue emptiness. There is no extra waiting period after the last member
   resolves.

The cutoff is the quiescent account snapshot, not a timestamp filter. The
capture marker, campaign identity, and stored count make incomplete capture
visible across reboot. A zero-account cohort is valid only after the same
membership proof. No production address enters a laptop, artifact, or command
argument; the existing SMTP transport still sends each address to Bizfly.

## Durable state

Add two application tables through goose with explicit `aboutme_app` grants.
These belong to the application database, never the CrowdSec database.

`privacy_notice_campaigns` has exactly one allowed campaign ID,
`crowdsec-postgres-paths-v1`, with these fields:

- `id text PRIMARY KEY`, constrained to that literal;
- `content_sha256 bytea`, exactly 32 bytes, matching a compiled manifest of the
  bilingual page copy, email copy, canonical link, and publication date;
- `cutoff_at timestamptz`, `expected_count bigint >= 0`, and nullable
  `capture_cursor uuid`;
- `state text`: `capturing`, `sealed`, `activated`, `aborted`, or `retired`;
- nullable `sealed_at`, `activated_at`, `retired_at`, and `purged_at`
  timestamps; sealed/activated require `sealed_at`, activated also requires
  `activated_at`, and purging requires `retired_at`. Historical timestamps
  survive recovery;
- nullable nonnegative `accepted_count bigint`, `deleted_count bigint`, and
  `last_accepted_at timestamptz`, written at activation; accepted plus deleted
  must equal expected count. The time is null exactly when accepted count is
  zero. These aggregate values survive retirement purge.

`privacy_notice_recipients` has a random `id uuid PRIMARY KEY`, campaign foreign
key with deletion restricted, nullable
`user_id uuid REFERENCES users(id) ON DELETE SET NULL`, and a unique
`(campaign_id, user_id)` while non-null. Its `job_id uuid UNIQUE NOT NULL` is
allocated during capture and never changes. It is not a foreign key to the
short-lived outbox. Additional fields are:

- `state`: `unqueued`, `queued`, `accepted`, `deleted`, or `failed`;
- `generation integer >= 0`, initially zero, incremented only by reviewed
  recovery; nullable `queued_at`, `accepted_at`, `deleted_at`, and `failed_at`;
- nullable `failure_code` from `expired`, `attempts_exhausted`,
  `provider_rejected`, `payload_invalid`, `job_missing`, or `delivery_failed`.

State constraints require the matching outcome timestamp. Accepted and deleted
states forbid a failure code; deleted requires null `user_id`. Historical
queued/accepted timestamps may survive a later failure or deletion. No email,
name, token, message body, SMTP reply, or provider identifier belongs in either
table. Index `(campaign_id, state, id)` and `user_id`. The campaign row stores
no individual identity.

Every batch and worker transition verifies the compiled campaign and content
digest. Unknown campaigns, mismatched digests, invalid state, unexpected jobs,
or a missing ledger fail closed. Enqueue and worker transactions lock account,
recipient, then outbox job. Capture and status never lock those rows in reverse
order. Serialize capture/enqueue/recovery commands with one campaign advisory
lock; status is read-only and uses one consistent database snapshot.

Keep recipient rows for the lifetime of this collection change, including after
activation, so delayed receipt recovery and known late failures can resolve
individual outcomes. Deleted accounts leave null-user rows without an address.
No seven-day recipient cleanup or scheduled purge is permitted. Only reviewed
retirement may mark the campaign retired and purge recipient rows in batches of
at most 200, after permanently disabling this collection change and revoking
both host receipts. Keep the campaign and aggregate retirement record forever to
prevent replay. Commands refuse retired or aborted campaigns. Abort revokes both
receipts, clears queued payloads, and permanently disables the change; its
ledger remains until retirement. Backups follow the application policy.

## Enqueue and delivery

With a valid send receipt and no restore marker,
`server crowdsec-privacy-notice enqueue` processes at most 100 unqueued members
per invocation, ordered by ledger ID. Permit at most 100 pending/leased notice
jobs at once; when full, return a fixed `queue_full` result without mutation.
Read the live account email inside production and use `Outbox.EnqueueTx` in the
same transaction that changes the ledger to `queued`. Duplicate calls find the
same job ID and do not create another job. A failed commit changes neither. Each
enqueue transaction has a ten-second statement timeout and two-second lock
timeout; the one-shot command has a five-minute whole-run deadline.

Add the closed kind `privacy_notice` with user-only scope and no token digest.
Its strict payload version 3 contains only `version`, `to`, `userId`,
`noticeId`, and `generation`. Require the canonical account email, exact
campaign literal, matching UUID and generation, and no unknown or duplicate
keys. Keep the 4,096-byte plaintext bound. Derive the fixed copy and
`https://aboutme.vn/privacy` from compiled constants, never from the payload,
environment, database text, request host, or command arguments.

Keep AES-256-GCM sealing, fresh nonces, existing job/kind/key-ID authenticated
data, current/previous key custody, and secret injection. Before sending,
decrypt and compare the payload's account, recipient address, notice, and
generation against the locked account and ledger. Reparented jobs, replayed
generations, deleted users, and mismatched ciphertext must not send.

The job expires 24 hours after its own enqueue, not 24 hours after the cohort
cutoff. Keep the worker's 30-second lease, ten-second send deadline,
eight-attempt limit, bounded jitter backoff, ten-job claim batch, and two
concurrent sends. Claim due authentication/security mail before notice mail so a
notice backlog cannot starve sign-in. Preserve ordinary mail semantics for every
other kind.

Before claiming any `privacy_notice` job and again under the send locks before
SMTP, the worker checks the host send gate below and requires a sealed campaign.
No matching receipt, a restore marker, or unreadable state means no notice claim
or send. Already leased notices remain unsent until authorized recovery; normal
mail continues. Neither stale-lease requeue nor an old ciphertext bypasses this
check. Authentication mail keeps its existing behavior.

Within the send-finalization transaction, set the ledger `accepted` only with
the corresponding outbox `sent` transition after SMTP acceptance. Set ledger
`failed` with a fixed reason whenever its job becomes terminal, including expiry
and decryption failure, and commit both transitions. Clear encrypted payloads on
sent/terminal as usual. Job cleanup must not erase the ledger outcome. Temporary
failure or stale-lease recovery keeps the same logical job and ledger `queued`;
an acceptance/commit crash may resend, but never records acceptance without a
committed accepted outcome.

Account deletion locks user, recipient, then job, erases/cascades ordinary
account and mail data, nulls `user_id`, and commits ledger `deleted` with
`deleted_at` and no failure code in the same transaction if not already
accepted. A prior accepted outcome remains accepted with a null user. Never send
after deletion commits or lower `expected_count`. If sending holds the user lock
first, deletion waits for its accepted/failure transaction; if deletion wins,
the sender finds no authority. Deleted is terminal and counts as resolved,
including deletion after a failed attempt. A null user without an accepted or
deleted outcome is inconsistent and blocks activation, not an implied deletion.
Unexplained missing queued jobs also block; missing accepted jobs are normal
after outbox cleanup. Recovery never recreates a deleted account or its mail.

## Owner commands, activation, and recovery

Allow only `prepare`, `enqueue`, `status`, and `activate` subcommands. No
recipient, subject, body, URL, campaign, or cutoff arguments are accepted. The
host's current release wrapper pins the server image and applies the existing
release and workload locks. These commands use the Unix socket as `aboutme_app`,
with `--network=none`, a 256 MiB container memory cap, and no SMTP or media
credentials. Only enqueue needs the existing mail encryption keys. The normal
server worker alone uses SMTP credentials and network access. Add no timer for
these commands and never dispatch them from an HTTP or MCP request.

### Send authority and restore fence

Keep host authority under root-owned `/var/lib/aboutme/privacy-notice/`, outside
the restored PostgreSQL data and backups. Use directory mode 0755 and file mode
0444, with no container-writable parent or alternate authority path. The
directory contains a current random UUID `epoch`, `send.json`,
`activation.json`, and a `restore-reconciliation` marker when reconciliation is
required. Receipt writes use atomic replacement and durable file/directory
synchronization. Mount this directory read-only into the server and enqueue
command so replacement and removal remain visible; do not mount individual
receipt files or cache checks. The server cannot create, replace, or remove any
host authority file.

The owner-run host `authorize-send` action validates a sealed complete cohort,
matching deployed notice/content digest, and absence of the restore marker. It
writes `send.json` containing only the fixed campaign, content digest, cutoff,
expected count, current epoch, and issue time. Every field must match the
compiled manifest, campaign row, and host epoch. Missing, malformed,
stale-epoch, or mismatched state fails closed. Only this action may issue the
initial receipt; startup, deploy, queue retry, and database state alone cannot
create one.

Before every cluster or point-in-time restore, stop the server and join
in-flight sends, persist `restore-reconciliation`, rotate the host epoch, and
remove both receipts durably before replacing the database or permitting server
startup. Every restore entry point, including a drill and replacement host,
establishes this fence on its target host first. Never restore this directory
from an old database or host snapshot as authority. A new host starts fenced. An
interrupted restore or reboot preserves the marker; the server may resume
ordinary mail with notices disabled.

Reviewed reconciliation accounts for ambiguous external sends and restored
ledger/job generations inside production. It cannot infer delivery from restored
queue state. While the marker exists, no action may authorize sending or
activation. After reconciliation commits, a reviewed host operation durably
removes the marker; `authorize-send` may then issue a receipt for the current
epoch and reconciled cohort. A crash in either step leaves notices disabled
until an explicit owner operation resumes it. Revocation stops/joins the worker
before removing the receipt, so an in-flight send cannot cross the boundary.
Activated, aborted, and retired campaigns receive no send receipt.

`status` reports only the fixed campaign, content digest, cutoff, state,
expected and actual totals, counts by outcome/fixed reason, and the latest
acceptance time. Exit 0 means sealed/activated with every member accepted or
deleted and valid host state; exit 2 means work remains; exit 1 means invalid
state, a failure, a restore marker, or an aborted/retired campaign. It never
emits recipient or job IDs, addresses, payloads, or raw database/provider
errors. Before activation, an outbox count of zero is insufficient; ledger count
must equal expected count and accepted plus deleted count, with no unqueued,
queued, failed, or unexplained members. An activated campaign's retained ledger
remains the authority for receipt recovery and late-failure handling, not its
old totals.

`activate` repeats that read and records campaign `activated` atomically, under
the host operation lock, including the final aggregate counts and acceptance
time. The host wrapper revokes the send receipt, then writes `activation.json`
with the current epoch, compiled digest, cutoff, expected/accepted/deleted
counts, and activation time. A restore marker forbids this action. All
expanded-feed install, start, rollback, and restore paths require this receipt
and the matching notice release. The receipt permits the collection change only
after the normal CrowdSec retention gate passes. A crash between database
activation and receipt creation resumes by checking the activated campaign and
ledger again, even months later; it cannot reopen ingestion from a missing
receipt or use a previous epoch. The wrapper does not expose an override or
accept a hand-supplied success file. Root remains the operator trust boundary.

Before activation, the owner checks known Bizfly delivery failures in production
and records only an aggregate result. If a known delivery failure follows SMTP
acceptance for a still-existing account, a reviewed recovery operation changes
that member to failed with `delivery_failed`, invalidating activation
eligibility. An account deleted after acceptance resolves as `deleted` if a late
failure arrives; it must not be contacted. No delivery confirmation is inferred
from an absence of feedback. A known failure after activation revokes the
activation receipt and stops changed ingestion before setting the campaign back
to sealed for recovery. The retained ledger supports this regardless of age.

Terminal failure for an existing account, expiry, job loss, key loss, or
inconsistent state blocks activation. There is no automatic re-enqueue, skip,
count adjustment, or success override. Recovery requires an independently
reviewed, owner-run production operation selected by fixed state and reason
inside production. For a recoverable failed row whose account still exists, it
increments generation, reuses the logical job ID, reseals to the current account
address with a fresh nonce, and resets job attempts and the 24-hour enqueue
lifetime atomically. Never retry accepted or deleted rows. A documented late
failure first changes accepted to failed as above. Renew send authority only
after reviewing the recovery, with no restore marker. If an active account
cannot obtain acceptance, the only finite outcome is permanent campaign abort
with changed ingestion disabled; never skip it, waive it, or reduce the cutoff
count. Preserve failure history as aggregate recovery counts and fixed reasons,
without retaining recipient evidence outside production.

The restore fence above applies to both sending and ingestion, including a
restored queued generation previously accepted outside the restore point. An
older v0 binary may not consume the new kind. Stop the worker for schema
reconciliation, deploy compatible code before enqueue, and recover forward if
rollback would discard the ledger or notice. Pre-v1 migration history remains
editable under [ADR 0005](../adr/0005-database-migrations.md); reconcile
already-applied production state explicitly. No client API or resume schema
changes are needed.

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

Anonymous visitors, signed-in users, hostile resume content, stolen sessions,
and MCP tokens cannot invoke these commands or choose a recipient or copy. The
fixed HTTPS link grants no capability. Network attackers face the existing
verified TLS SMTP transport. The CLI receives encryption keys only through host
secret injection; logs retain fixed codes and aggregate counts only.

A compromised web/render container receives neither mail keys nor database
credentials. A compromised server container already holds the mail and database
authority and can bypass worker send checks; the independent root-owned
activation fence limits its ability to enable the feed. The send gate protects
the normal worker against replay and restored state, not stolen SMTP authority.
A database writer without application keys cannot forge encrypted payloads but
can falsify ledger outcomes or change account rows. Completion relies on
application-database integrity; encryption does not authenticate delivery
bookkeeping. Host compromise is outside that boundary. Leaked backups retain
account data under existing protection; encrypted payloads still require
separately held keys. Persistent host markers and epoch checks keep restored
jobs from reviving sending or activation. An account owner may end their own
notice obligation through normal authorized deletion, which erases their account
and cannot resolve another account's row.

Hosted proofs must cover:

- exact cutoff membership, zero users, pending registration, concurrent account
  creation, resumed capture, maintenance interruption, and unchanged cutoff;
- concurrent/repeated enqueue, batch/backlog limits, statement/command timeouts,
  missing encryption keys, and rollback between outbox and ledger writes;
- strict payload validation, reparented jobs, generation replay, fixed bilingual
  text/HTML copy, canonical link, and no raw values in forced-error output;
- authentication-mail priority, temporary failure, expiry, attempt exhaustion,
  terminal/decryption failure, stale leases, and acceptance/commit crash;
- account deletion before enqueue, during a send, after acceptance, and after
  job cleanup, with unchanged cohort totals, terminal deleted outcomes, correct
  accepted-plus-deleted totals, and no send after deletion commits;
- accepted/terminal outcome atomicity, seven-day outbox cleanup, activation
  retries/crashes delayed beyond seven days, late delivery failure after that
  interval, retirement-only recipient purge, and refusal to repeat an activated
  or retired notice;
- missing/malformed/stale-epoch send receipts block both claim and send while
  ordinary mail continues; a receipt revoked after claim blocks SMTP;
- cluster/PITR restore, a restored previously sent job, host replacement,
  crash/reboot during fence creation or reconciliation, and old-receipt replay
  keep notices blocked until the owner issues current send authority; prove
  worker quiescence before revocation and that no restored DB can clear markers;
- all activation paths reject missing/mismatched receipts, incomplete counts,
  failure, old notice releases, database restore, and unsupported old workers;
- reviewed recovery cannot retry accepted/deleted rows, expose recipients,
  bypass restore reconciliation, or extend a job lifetime without a new
  generation; irrecoverable active-account failure can only abort the campaign;
- the existing privacy promise and new publication date appear in both deployed
  languages before SMTP begins, and changed ingestion starts only after the
  gate.

Production evidence contains release digests, publication date, fixed campaign
and content digest, cutoff, aggregate counts, acceptance/activation times, and
fence result only. Keep recipient lists, job IDs, message bodies, addresses, and
provider screenshots out of artifacts, public CI, and the repository.

## Owner decisions and sources

The owner approved the message copy, SMTP acceptance threshold with possible
duplicate delivery on retries, no additional waiting interval, deleted accounts
counting as resolved without email, and permanent campaign abort for
irrecoverable active accounts on 2026-10-19. Database-integrity dependence and
the existing provider's lack of confirmed-delivery callbacks are explicit
limits. Independent design review passed before implementation.

Repository sources: the privacy promise in
[`legal-en.ts`](../../apps/web/app/i18n/legal-en.ts) and
[`legal-vi.ts`](../../apps/web/app/i18n/legal-vi.ts), the
[`authmail` worker](../../apps/server/internal/authmail/worker.go),
[`outbox`](../../apps/server/internal/authmail/outbox.go),
[`SMTP sender`](../../apps/server/internal/authmail/smtp.go), and
[`job wrapper`](../../deploy/vn/host/job-run.sh). These are first-party
repository contracts; no external provider behavior beyond the email runbook is
assumed. The corresponding upstream repository sources are
[authmail](https://github.com/dannyota/aboutme/tree/main/apps/server/internal/authmail)
and the
[email runbook](https://github.com/dannyota/aboutme/blob/main/docs/runbooks/email.md).
