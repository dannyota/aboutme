# CrowdSec notice initial reconciliation

Status: proposed extension, pending independent review.

This extends the [notice contract](crowdsec-privacy-notification.md) only for
the first, never-run campaign. The
[operator procedure](../runbooks/crowdsec-privacy-notice.md) owns the command
sequence. Existing copy, receipt formats, campaign states, delivery limits, and
restored-campaign recovery stay unchanged.

## Bootstrap and provenance

Before replacing any installed guard during first adoption, the incoming
reviewed helper must establish its canonical installed copy at
`/usr/local/lib/aboutme/crowdsec-privacy-notice-host.sh`. Do this after durable
initializing evidence under the existing bundle, cutover, and workload locks,
and before installing `fence.sh`, `deploy-host.sh`, or `deploy-safety.sh`.
Validate the incoming helper and destination parents, reject unsafe objects, and
install through a same-directory temporary file, root:root 0755, file sync,
atomic rename, and directory sync. Verify exact bytes against the reviewed
incoming helper before replacing guards. Resume only the recorded adoption;
partial replacement does not make a host eligible as legacy again.

Bootstrap also creates `/var/lib/aboutme/privacy-notice/initial-epoch`, with the
same 37 bytes as the first `epoch`: canonical lowercase UUID v4 plus LF. Its
ownership, mode, safe-path checks, and persistence protocol are the existing
authority-file rules. After the reconciliation marker and epoch are durable,
write this anchor before persisting adopted evidence. An interrupted
initializing operation may finish the missing anchor from its validated epoch
under the same recorded owner. Any present mismatch refuses.

Both `initialize` on an app-absent host and first adoption establish this
anchor. Adopted hosts never regenerate it, including when it is missing.
Ordinary installation preserves it. Restore preserves the anchor and rotates
`epoch` under the existing restore protocol; equality then fails permanently for
this initial-only action. Even an empty restored database cannot qualify. Do not
copy the anchor from backups, receipts, or another host. Losing it requires
separately reviewed recovery, not another initialization.

## Fixed initial action

Root invokes exactly:

```sh
/usr/local/lib/aboutme/crowdsec-privacy-notice.sh reconcile-initial
```

The wrapper invokes only `server crowdsec-privacy-notice reconcile-initial` from
the deployed pinned server image. This is a fifth, separate initial action; the
four campaign commands retain their existing report and marker refusals. No
arguments, alternative paths, campaign selectors, environment overrides, SQL
input, timer, HTTP route, or MCP dispatch are added. The host-only primitive
still cannot query the database or clear reconciliation.

Require the compatible deployed server/web release and reviewed host bundle,
matching manifest/date, adopted evidence, safe authority, equal initial/current
epochs, no capture marker, and absent send/activation receipts. An existing
receipt refuses even if malformed. The reconciliation marker must be an empty
safe regular file, or absent only for a repeat of the empty-state proof below.
Never treat an absent marker alone as completion.

Take the existing bundle lock, then fresh persistent `cutover` ownership at the
installed release, then the workload lock. Refuse an existing operation; never
take it over or accept its ID as input. Revalidate mount identities and release
after locking. Enter maintenance, stop account writers, drain jobs, join mail
sends, and prove cutover quiescence. Require stopped migrations, DB setup,
restore processes/recovery jobs, and changed-ingestion consumers. Persist the
existing `quiesce` mutation marker before the database call. The operation and
mutation marker block unattended app starts across reboot. No new operation
kind, lock, recovery dispatcher, or start exception is needed.

Capture initial/current epoch, mount identities, installed release record and
image digests, manifest digest, and newly acquired operation ID in private
process memory. These bind this invocation through commit and removal. Check
them again before effects and after the database child exits. Keep workload
ownership through child completion and host persistence; foreground children
inherit its descriptor. On signal or lost child, suppress success and retain the
persistent cutover guard. Never release from an EXIT trap.

### Database transaction and output

Use the existing one-shot isolation: Unix socket as `aboutme_app`,
`--network=none`, 256 MiB, read-only container, read-only whole authority
directory mount, no mail encryption, SMTP, media, or backup credentials. No new
credential or grant is needed. The deadline is five minutes, statement timeout
ten seconds, and lock timeout two seconds.

In one transaction, take the existing campaign advisory lock, then `SHARE` table
locks in order on `privacy_notice_campaigns`, `privacy_notice_recipients`, and
`auth_email_jobs`. Require zero rows in both notice tables and zero
`auth_email_jobs` of kind `privacy_notice` in every state, including
sent/terminal. Count all campaigns, not just the fixed ID. Missing tables,
insufficient grants, incompatible schema, any nonzero count, or query/lock
failure refuse. Ordinary mail rows do not disqualify the host. Do not read
addresses, payloads, account IDs, or account count.

Require safe matching epoch/initial-epoch and absent receipts/capture marker
before and immediately before commit. Validate the reconciliation marker if
present. Commit the validation transaction without changing business rows. This
is an empty-state reconciliation, not a new campaign or cutoff, and does not
call sealed-campaign `Recover`. A commit error or unknown commit outcome
produces no success. Host quiescence and ownership continue after transaction
locks are released, so no supported writer can invalidate proof between commit
and marker removal.

Only after successful commit emit one closed JSON object plus LF, empty stderr,
exit 0. Its exact fields are `schema_version` integer 1, `result` string
`initial_reconciled`, `campaign_id` the compiled literal, `content_sha256` the
compiled manifest digest, and `campaign_count`, `recipient_count`,
`notice_job_count`, each integer 0. Apply the existing strict JSON rules and
4,096-byte bound including LF. There is no cohort, cutoff, epoch, operation ID,
receipt, or user data in this result. There is no exit 2.

The wrapper captures this output privately, requires the exact shape, values,
EOF, and zero child exit, and joins the child. It rechecks all invocation
bindings, quiescence, and receipt/capture absence before removing only
`restore-reconciliation`. Check unlink and authority-directory sync, then
recheck absence, authority, and bindings. An already absent marker still
requires the full transaction and directory sync. Never create a receipt, rotate
epoch, change the release floor, or remove the initial anchor.

Clear the exact operation's `quiesce` marker through its existing guard, release
workload ownership, and release that operation through the existing fence. Only
after all steps succeed may the wrapper emit the validated initial aggregate and
exit 0. It leaves maintenance active and writers stopped. Every refusal,
timeout, persistence failure, or uncertain completion gives empty stdout,
exactly `invalid_state` plus LF on stderr, and exit 1. Raw child, database,
filesystem, and systemd errors stay out of output.

### Interruption and retry

A crash before removal retains reconciliation. A crash during unlink/sync may
leave it present or absent; the retained cutover guard prevents normal work in
either case. A crash after cutover release can lose the reply even though the
action completed. No saved aggregate certifies such an invocation.

For retained ownership, settle the old wrapper, database child/backends, all
descendants, sends, and systemd jobs first. Under the existing workload lock,
prove quiescence and inspect the exact cutover owner/mutation. Use only the
existing reviewed `recover-cutover` and release procedure, without touching
authority files. If the mutation was already cleared, inspect the exact owner
before its existing release. Recovery must not resume the app. Then explicitly
invoke `reconcile-initial` again with fresh ownership and the complete DB proof.
If ownership is already absent, an explicit new invocation performs that same
proof directly. No automatic retry, owner adoption, or saved-output replay.

Any campaign/recipient/notice job, changed epoch, missing anchor, altered
release, restore evidence, or unprovable quiescence stops this procedure. Keep
notices and changed ingestion disabled for separately reviewed recovery. Do not
delete rows, reset a campaign, edit receipts, manually remove the marker, or use
initial reconciliation for an existing campaign, even one with zero recipients.
Existing sealed-campaign recovery is a separate boundary.

## Security, trade-offs, and proof

Visitors, signed-in users, hostile resume content, and stolen session/MCP tokens
have no route to the action. Network attackers gain no new listener or
secret-bearing exchange. Containers see the authority directory read-only; only
trusted host root can remove the marker. The new transaction has no mail keys
and cannot send. A database writer can falsify emptiness, as it can falsify the
existing ledger; database integrity remains trusted. A leaked backup cannot
provide the host anchor or restore the current epoch. Root compromise and
unguarded manual database replacement remain outside this trust boundary.

One 37-byte root file distinguishes initial admission from every supported
restore without adding an application table or recovery receipt. The cost is
fail-closed refusal when that anchor is lost. A validation-only transaction
needs no durable DB token because every retry repeats the proof under fresh host
ownership. Generic restore reconciliation would require ambiguous-send and
generation rules beyond this initial-only scope. Manual marker removal would
have no database/host binding and is forbidden.

Ship helper ordering, anchor creation, both action implementations, and their
proofs together before adoption. Older v0 clients need no API fallback; older
host bundles and server images cannot perform this action. No existing campaign
data migrates or is discarded. Public date/manifest updates must use the actual
publication date before prepare; they are not inputs to this action.

Hosted regressions must exercise:

- A truly legacy host missing the canonical helper; helper persistence failure,
  crash at each replacement boundary, exact-operation resume, and new guards
  reaching only the verified helper.
- New-host and adoption anchor creation; preservation on reinstall; missing,
  unsafe, malformed, or mismatched anchors; initial/second restore rotations,
  including empty restored tables and PostgreSQL-only hosts without an app.
- Zero accounts and ordinary mail; every campaign state, orphan recipients, and
  notice jobs in every state; missing schema, grants, lock contention, timeout,
  and commit failure without mutation or host clearance.
- Child failure, malformed/partial/duplicate output, wrong digest, unknown
  fields, extra records, surviving descendants, reboot, changed mount/release/
  epoch/owner, and each unlink/sync/release crash boundary. Retry always reruns
  the DB proof; first success alone permits prepare, never send or ingestion.
- Existing prepare/enqueue/status/activate refusals while marked; no secret or
  recipient output; the full first-adoption through activation sequence with
  explicit app restart after sealing and after activation.
