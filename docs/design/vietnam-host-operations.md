# Vietnam host operations

Status: Proposed (2026-10-12), revised 2026-10-14. The owner approved building
these operations on 2026-10-13; independent design review precedes
implementation. [ADR 0026](../adr/0026-vietnam-hosted-production.md) owns the
host boundary, [ADR 0028](../adr/0028-community-showcase.md) requires operator
deletion, and
[ADR 0003](../adr/0003-public-namespace-and-no-operator-surface.md) forbids an
operator HTTP surface. No accepted decision changes.

## Release split

Ship three patch releases, one feature each, in this order:

1. **Account deletion:** the durable runner, target confirmation, deletion and
   cleanup status, audit retention, and public-read verification. This closes
   the live abuse gap without waiting for rotation or browser credentials.
2. **TOTP rotation:** staging, recovery copies, maintenance/reboot guards,
   re-encryption, retirement, and the TOTP proof channel.
3. **View-pass rotation:** reuse those guards and add the old-pass witness.

Each release includes its commands, tests, and shipping docs listed in
[delivery](vietnam-host-operations-delivery.md). Publish the reviewed image,
install its reviewed host bundle, deploy that image normally, then enable use of
its operations. Installation must preserve live secrets, flags, and unresolved
requests. Ordinary installation refuses runner replacement while a request may
run or a key marker exists. A reviewed bundle update may repair a suspended
request after proving unit/container/backend quiescence and safe serving with no
fence owner. It must preserve schema-version-1 state and both keys, support that
request's saved protocol, and pass installed-hash checks before any resume. An
incompatible state change requires its own reviewed migration; never erase an
unresolved request to make installation succeed. A rotation that cannot serve
uses the separate
[quiescent repair](vietnam-host-operations-delivery.md#quiescent-repair)
contract, shipped with TOTP rotation; account deletion does not depend on it.
Ordinary commands must exist in the live release; repair binds a checked forward
image before resume. Installing host scripts alone never authorizes new actions.
There is no schema migration, HTTP change, or document schema change. Older
browser tabs keep their existing contracts.

This costs two short maintenance windows per TOTP rotation (switch and retire),
one per view-pass rotation, and durable host state. Direct SQL bypasses target
checks and deletion side effects. Generic secret import cannot coordinate
running containers. A single release coupling all three delays abuse response.

## Operator interface

New `deploy/vn/scripts/operations.sh` uses `DEPLOY_HOST`, `SSH_PORT`,
`aboutme-admin`, and fixed root scripts under `/usr/local/lib/aboutme/`:

| Invocation                                    | Contract                                                                            |
| --------------------------------------------- | ----------------------------------------------------------------------------------- |
| `account-delete show <slug>`                  | Read owner and slugs; save a 15-minute deletion plan.                               |
| `plan totp-rotate` or `plan view-pass-rotate` | Save a 15-minute plan with live release, slots, nonsecret key IDs, and effects.     |
| `apply <id> --confirm <text>`                 | Accept that exact plan once; duplicate accepted requests return its state.          |
| `status <id>`                                 | Read saved state; never start work.                                                 |
| `resume <id>`                                 | Reconcile interrupted effects, then continue only that request.                     |
| `verify <id>`                                 | Run the action's read/proof checks; never repeat deletion or replace a key.         |
| `abort <id>`                                  | Stop at the action's safe boundary below; not an undo.                              |
| `retire <id>`                                 | TOTP only; remove the previous mapping after retirement checks.                     |
| `rebind <id> --confirm <text>`                | Suspended TOTP only; bind a compatible repaired live release without changing keys. |

IDs are host-generated 32-byte random canonical base64url, 43 characters.
Confirmation text is exactly `delete-account:<id>:<uuid>:<reported-slug>`,
`rotate-totp:<id>:<tag>:<active-slot>:<next-slot>`, or
`end-all-view-passes:<id>:<tag>`. Display the complete text and consequences
before apply. Reject extra arguments, invalid slug/UUID, expired or changed
plans, arbitrary commands, paths, images, and environment overrides. A deletion
plan must include the reported slug in its returned list. The operator reviews
the reported content, UUID, and every slug before confirming.

The wrapper exits 0 only for a successful read or durably accepted transition;
this does not mean completion. It prints closed JSON with `requestId`, `kind`,
`phase`, `outcome`, and action-specific nonsecret fields in the
[wire schemas](vietnam-host-operations-delivery.md#wire-schemas). Exit 2 means
invalid input; 1 means refusal or transport failure, requiring `status`.
Deletion show preserves exit 4 for not found and creates no applicable plan.
Pending state is explicit, never a success claim about deletion or rotation.

Reuse deploy eligibility, tag-bound provenance, SSH quoting and port handling.
Require clean, current reviewed `main` before opening SSH, then verify the live
release's eligibility and exact amd64 digests. Pin the known SSH host key,
refuse unknown/changed keys, and disable agent forwarding. Hash-check every
installed runner, helper, unit, and template it uses, not just the deployer's
three files. Root parses inputs again. Send target/confirmation data in a closed
JSON request on stdin to fixed `sudo .../operation-run.sh request`, never in
sudo arguments or systemd unit names. The envelope is at most 4 KiB, with `verb`
and the exact fields for that verb; reject extras. Unit argv contains only
`run <id>`. This keeps UUIDs/slugs out of sudo's journal. Reloads accept only
the request's saved live record.

## Server command inventory and contracts

These contracts describe the baseline at `f63edede`. Operation containers run as
`aboutme_app` over the PostgreSQL Unix socket, with no network. The separately
scheduled media sweep retains its existing media credentials and network.
Configuration contains the DB password and, only for TOTP jobs, the selected
ring. No media, SMTP, OAuth, migration, or database-admin credential enters
these containers. New read commands use one read-only repeatable-read
transaction, a 30-second context/statement deadline, no row locks, and bounded
output. They are repeatable snapshots, not evidence that another process
stopped.

| Command                                       | Exists  | Arguments, output, exits, retries                                                                                                                                                                                                             |
| --------------------------------------------- | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `server account-delete show <slug>`           | Yes     | Up to two minutes; `account user=<uuid> slugs=<sorted,csv>` then `account-delete: end`; 0 success, 2 usage, 4 absent, 1 failure. Read-only; retry safe.                                                                                       |
| `server account-delete confirm <slug> <uuid>` | Yes     | Same limit; `deleted user=<uuid> slugs=<sorted,csv>` and end marker. 0 committed, 2 usage, 3 unknown, 4 slug absent, 5 owner mismatch, 1 failure. Never automatically retry a write.                                                          |
| `server totp-key-reencrypt`                   | Yes     | No arguments; 0 success, 1 invalid arguments/configuration, job or output failure. JSON `job`, `success`, `result` uses existing `TOTPReencryptResult` fields. Batches commit separately; rerun moves only rows off active.                   |
| `server account-delete status <uuid>`         | **New** | Canonical lowercase UUID; 0 emits `{"success":true,"state":"present"}` or `absent`; 2 usage, 1 failure. Select existence by user ID, not slug; no user fields returned.                                                                       |
| `server media-deletion-status`                | **New** | No arguments; 0 emits `{"success":true,"pending":N}`; 2 usage, 1 failure. Count all `completed_at IS NULL`, including leased/delayed rows, through existing `GetMediaDeletionQueueState`.                                                     |
| `server media-deletion-sweep`                 | Yes     | No arguments; 30-minute bound; 0 success, 1 usage/config/job/output failure. JSON `job`, `dryRun:false`, `success`, `result` uses existing deletion counts. Queue leases and completed rows make retry safe; success is not global emptiness. |
| `server totp-key-status`                      | **New** | No arguments; 0 emits the ring/count snapshot below; 2 usage, 1 failure. Load reduced TOTP configuration; never decrypt or change rows.                                                                                                       |

New command success output is one JSON line, at most 1 KiB, with exactly its
listed fields. On failure stdout is empty; stderr is a fixed diagnostic, never
an error object or row. A write/flush failure exits 1. Unknown fields, negative
counts, malformed IDs, extra lines, truncation, or nonzero exit cannot prove
success. Preserve the existing re-encryption JSON field names and nullable
`DecryptFailures`; accept it only with exit 0 and `success:true`. The host caps
that output at 2 MiB, discards raw diagnostics, and retains only allowed fields.

TOTP status fields are `success:true`, `activeKeyId`, `previousKeyId` (null when
absent), `credentialsOffActive`, and `enrollmentsOffActive`. IDs use the
[derived-ID algorithm](totp-key-management.md#key-ring). Counts include every
row, including expired enrollments and unknown key IDs, not the health check's
three-ID sample. Any query/configuration failure produces no counts. Compare
counts from one snapshot; do not use successful re-encryption or `/readyz` as a
substitute. A prospective key must differ from both configured IDs; initial zero
off-active counts prove no row needs an unconfigured slot.

Existing `server media-deletion-sweep` stays behind
`aboutme-job@media-deletion-sweep` and `job-run.sh`. Do not add account/TOTP
commands to that generic job allowlist: it injects media credentials and uses
journald. No `server view-pass-rotate`, credential-reset, or key-generation
subcommand is needed.

## Durable execution and fence

New `host/operation-run.sh` dispatches only validated requests. New
`aboutme-operation@.service` is root oneshot, `Restart=no`,
`KillMode=control-group`, `TimeoutStartSec=45min`, `TimeoutStopSec=60s`, with
the host failure alert and `LimitCORE=0`. Each request has a flock, acquired
before the shared fence. A timeout does not prove a container or database
backend exited. Before start, fsync intent, container name, and attempt number;
after effects, fsync results. All state replacements rename atomically and sync
the parent directory.

Use `/srv/data/host-operations/`, root 0700, files 0600. Require the mounted
data volume and verified root-owned parent directories. Reject symlinks, unknown
state versions/fields, malformed state, and size overflow. A version 1 request
contains its ID/kind, phase/outcome, creation/expiry UTC, saved release record,
flags, secret metadata IDs, confirmation, attempts, fence binding, results,
proof challenge, and action-specific target or rotation state. Store only the
fields needed by the contracts here; never raw Podman inspect or environment.

Plan/show execute bounded read containers under the request unit and locks; the
wrapper waits at most 60 seconds. A detached read can finish, but cannot become
an applicable plan after a host-enforced 60-second planning deadline. Persist
that deadline before starting and require a fresh plan. Capture the returned
UUID/slugs only after a complete result. Release locks only after proving
container and backend exit. This avoids untracked reads holding the workload
lock after SSH loss. Saved `status` needs no mutation lock. Unknown read
outcomes never authorize apply.

Lock order is request flock, persistent fence, rotation job drain, then
`workload-lock.sh` (60-second acquisition limit). New
`fence.sh lock-for <kind> <tag> <id>` stores that ID as `operation_id` plus its
bound tag atomically. Kinds add `account_delete` and `view_pass_rotate`; TOTP
uses `totp_reencrypt`. An existing identical tuple returns the same ID; another
owner or tag refuses. Reacquisition after a safe suspension revalidates all
saved state. Lost SSH replies are resolved by reading the binding, not by
creating another request. Checkpoint before every effect and image start.

Match release-json, unit state, container digest/tag/number, and named secret
metadata IDs. Do not emit full inspect output or inspect secret payloads.
Require live tag and fence >=4007 for TOTP, >=6026 for view-pass, plus every
enabled flag's floor. Operations never raise/lower floors or change enrollment
flags. An operator command is available only in a release that includes its
contract; record the first supporting tag in the shipping runbook and wrapper
allowlist.

Only one attempt may run. Each DB connection uses
`application_name=hostop:<id>:<attempt>` within PostgreSQL's length limit. On
resume/abort, prove the old unit inactive, named container stopped/removed, and
matching DB backends absent before another attempt. Use host `postgres` peer
access to inspect backend metadata only, never query text or rows. A read
failure is not absence. PID reuse, timeout, OOM, and missing result retain the
fence until quiescence is proved. Neither EXIT traps nor `fence.sh clear` may
release while a request unit, container, backend, or key-change marker remains.

## Secret custody and maintenance

Keys are generated on-host as 32 random bytes encoded in canonical 43-character
base64url. Root owns `/etc/aboutme/secrets/` (0700), active files (0400),
`staging/<id>/` (0700), and `archive/<derived-id>` (0400). Only root may read
staging or archives. They are never directory-mounted into containers, included
by generic Podman sync, or passed to the application before activation. Reject
symlinks and unsafe modes before reads/writes. A request adopts its existing
staged key after a crash; it never generates a second key for the same intent.

Inject selected named Podman `type=env` secrets from files/stdin. Secret values
must never appear in command arguments, unit Environment lines, exported shell
variables, traces, diagnostics, environment dumps, journal, or core dumps. They
necessarily exist in the receiving process environment and memory; that process
and host root can read them. Disable core dumps for the runner and its
containers. Containers use no host PID namespace, privilege, Podman socket,
archive mount, or broad host mount; drop capabilities, set no-new-privileges,
read-only rootfs, 256 MiB memory and equal memory-swap limit, and no restart.
Only the socket and bounded tmpfs are mounted. Use `--log-driver=none` and
capture stdout privately before validation. Do not reuse job-run.sh wholesale.

Before activation, encrypt a stable snapshot of rotation keys and their staging
and archives in the closed
[recovery archive](vietnam-host-operations-delivery.md#recovery-archive) format
on-host to the configured age recipient. The identity stays off-host. New
`secrets-host.sh operation-copy <id>` takes no caller-selected recipient/path;
it emits only ciphertext. Root-owned 0600 `/etc/aboutme/operations.json` pins
the recipient as the sole `ageRecipient` field, copied from
`host/env/operations.json.example` only when missing. No identity is stored
there. The wrapper uses the owner's configured `ABOUTME_INFRA_DIR` outside the
checkout; no remote output path is accepted. The wrapper saves it to the owner's
configured `aboutme-infra` directory, mode 0600, fsyncs file and directory, and
sends `recovery-copy <id> <sha256>` to the runner. Hash the exported ciphertext,
not plaintext. The receipt must match the saved export hash and staged key ID.
Lost replies reuse that same export; export failure or missing receipt blocks
activation. Only an authenticated operator can attest off-host durability.

Before changing an active file, Podman secret, or flags, write
`/srv/data/host-operations/key-change.json` with request ID, saved release, and
stage. Fence checks reject every serving, migration, and job start while it
exists, including after reboot or if malformed. Missing data mounts fail these
checks closed. Maintenance may start. Only the owning runner may render saved
units under the marker; it may not start them. Generic secret import/replacement
and sync for these keys refuse outside this protocol, except missing-key
bootstrap before any serving release exists.

Stop timers, drain jobs, acquire the workload lock, start maintenance, stop
serving Caddy and Go, and prove units, containers, and their DB backends
stopped. Require an external marked 503 before active writes. Staging and
recovery export precede maintenance. New `deploy-host.sh <id> <tag> reload-live`
reconciles the saved digests, flags and secret metadata without migration,
backup, observer change, or nested deploy.sh. It renders under the owner
checkpoint, fsyncs `starting`, removes the marker only after all
files/secrets/units agree, then starts web/Go/Caddy and checks local readiness.
Finish by stopping maintenance, restoring timers and proving serving externally.
Failed starts retain the fence and maintenance; ordinary resume uses the same
keys and release, never old secrets. An incompatible image or defective runner
uses quiescent repair, without requiring successful startup or safe suspension.

Release the workload lock while awaiting a proof or ten-minute wait, retaining
the fence. Reacquire and revalidate before effects. The key marker blocks starts
only while configuration is inconsistent; a retained rotation lock alone must
allow the reconciled release to serve for sign-in proofs and owner recovery.
Protect key file changes and exports with that same protocol so a concurrent
import cannot invalidate a receipt. Backups retain their existing timers.

## TOTP rotation and recovery

[TOTP key management](totp-key-management.md) owns cryptography and batching. A
plan requires no previous slot and zero off-active rows, including expired
enrollments. Apply rechecks this, chooses the other slot, archives its retired
contents before reuse, stages the new key, and obtains the recovery receipt. The
`prepared` proof enrolls a temporary factor under the old active key and proves
sign-in before switching. Missing proof inputs block before maintenance.

Under maintenance, install the staged key in the other slot, sync that named
Podman secret, atomically set new active/old previous in flags, and reload.
Persist switch boot ID and monotonic time after the new configuration is active;
uncertain timing restarts the ten-minute wait. Users see maintenance 503 during
the switch; afterwards existing codes work through either ring entry. Factor
secrets, recovery codes, sessions, and authentication epochs are not changed by
re-encryption. Normal concurrent enrollments and lazy rotation use active.

Run existing `totp-key-reencrypt`: at most 200 rows per batch, 10,000 rows and
30 minutes per run. Limit reached, skipped rows, or decrypt failure can leave
work. Retain both keys. A failed run's counts prove nothing; `resume` explicitly
runs the next bounded pass. Unknown effects first require quiescence. Two
successful status snapshots must report zero off-active rows, with the second at
least ten monotonic minutes after switch; a reboot restarts that wait. Require
the `serving` sign-in proof before retirement.

`retire` takes locks, rechecks zero, archives the previous key, obtains a fresh
recovery-copy receipt including the archive, then uses maintenance to remove
only the previous-slot mapping and reload. Remove that slot's Podman secret only
after old containers stop. Require the `retired` proof. Do not infer zero from a
failed query, health check, skip count, or elapsed enrollment lifetime.

| Interrupted point       | `resume` / `abort` behavior                                                                                                                                                             |
| ----------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Before active writes    | Resume same staging/proof; abort discards staging and restores saved serving/timers if stopped. Retained ciphertext remains encrypted.                                                  |
| During switch or reload | Reconcile forward to staged active plus old previous. Abort is refused until that ring serves safely. Never return to the old-only ring.                                                |
| During re-encryption    | Prove stopped, retain both keys, continue bounded batches on resume. Abort may close as `suspended` after ring/readiness checks; it releases the fence but keeps request and both keys. |
| Suspended               | Owner can repair a failing factor through passkey/recovery. Resume reacquires fence at the same release/ring; drift refuses. A new rotation is refused until this request retires.      |
| During retirement       | Reconcile new active and archived previous, repeat zero/readiness/proof checks. Never discard either file on an uncertain result.                                                       |
| Completed               | Resume/retire return saved outcome without writes; verify may repeat reads.                                                                                                             |

A suspended request may permit an image fix after quiescence and safe serving. A
normal deploy must preserve both ring mappings and derived IDs and meet all
floors. Automatic resume refuses release drift. `rebind` requires exact
`resume-totp:<id>:<new-tag>:<active-key-id>:<previous-key-id>` confirmation,
fresh eligibility/provenance and installed-hash checks, and a healthy live
release in the wrapper's tested rotation-contract allowlist. Under locks, prove
old attempts stopped and the live ring matches the saved ring. Atomically save
the new release record, invalidate proof nonces and zero snapshots, and restart
the ten-minute wait. No key, flag, floor or row changes. Duplicate identical
rebind succeeds; other drift refuses. Resume then runs the same bounded
re-encryption and fresh proofs. An image with an incompatible command contract
needs a forward fix that preserves this contract before rebind. A decrypt
failure leaves the row unavailable with `503 authentication_unavailable`;
passkeys, recovery, and `/readyz` continue. Only the account owner can
replace/remove the factor. Never reset credentials as an operator
([ADR 0017](../adr/0017-second-factor-authentication.md)).

Keep derived-ID archives and encrypted copies for every dependent backup/WAL
restore target, at least 30 days. These releases do not implement key pruning;
retain archives until a reviewed inventory-based disposal procedure exists.
Restore follows the recovery archive's ring selection and validation rules,
including staged keys after partial activation, with at most two in runtime.
Unknown IDs or unavailable keys block TOTP recovery, never authorize rewriting
factors. A DB restore never restores the release floor or authorizes an older
image.

## Account deletion and recovery

Run the existing transactional `confirm` exactly once after persisting intent.
It rechecks reported-slug ownership under transaction locks and uses self-delete
side effects: resumes, sessions, grants, showcase rows, media queue, and 180-day
slug tombstones. No maintenance, notification email, or backup precedes it. The
confirmation authorizes the account and all its current resumes, including ones
added since show. Retain separately the shown slugs and committed slugs.

Exit 4 or 5 with a complete end marker means no deletion by this attempt. Exit
3, lost output, missing marker, unexpected output, or uncertain termination
means `unknown`. After proving unit/container/backend quiescence, `resume` uses
new `account-delete status <uuid>`: absence proves the account is gone, not who
deleted it; record `absent_after_unknown`. Presence requires a new show and
confirmation; never retry the saved destructive command. A vanished/renamed slug
does not prove account absence. An abort cannot cancel a committed delete.
Before dispatch it closes without mutation; afterwards it resolves outcome by
the same quiescence/status rules and leaves any cleanup pending.

`verify` checks the union of known slugs: HTML, JSON, photo, PDF, markdown,
preview alias and live endpoint reject new reads; discovery and showcase omit
them; show returns 4. Use bounded full pagination for the showcase, not only
page one; a page cap or interrupted traversal is limited evidence. Show returns
no card version, so record versioned-card coverage as limited; an invented
version proves nothing. Hosted deletion tests use known versioned-card fixtures.
Preserve [ADR 0010](../adr/0010-public-artifact-revocation.md): admitted
responses may finish and live streams close on notification. If a commit result
was lost, the full committed slug list may be unavailable; record that
verification limit, never reconstruct deleted content from a backup.

Release the operation fence once the write outcome and quiescence are known (or
after the evidence-expired abort below); public proof or physical cleanup may
remain pending without blocking deploys. Trigger/retry the existing media sweep
through its systemd job after release. Sweep failure alerts and never undoes
deletion. Only a successful global zero from `media-deletion-status` after
account absence permits physical cleanup `complete`; nonzero/error stays
`pending`. Unrelated accounts can delay zero. `verify` at a later capable live
release may recheck cleanup. Existing backup retention applies; rollback never
restores the account.

## View-pass rotation and recovery

Stage a key, recovery receipt and `prepared` old-pass witness as for TOTP. Under
maintenance persist intent, replace `view-pass-key` atomically, replace its
named Podman secret, and recreate the saved release. Go loads the key at
startup; replacing a Podman secret alone does not update a running process.
Resume reconciles to that staged key. Before replacement abort may discard it;
after replacement abort can only finish safe serving with the new key and record
`unverified`. Never restore the old key, including during image rollback or DB
restore. Preserve resume epochs, publish state, flags, and release floor.

Every viewer must sign in again. Account sessions are unaffected. Verify the
same unexpired old pass rejects every protected route while a fresh pass admits
those routes for the same resume ID and epoch, as specified in
[proofs](vietnam-host-operations-delivery.md#external-proofs). An expired/lost
witness or changed epoch is inconclusive: restore readiness, close `unverified`,
then make a new plan/witness and rotate again. Do not claim revocation proof.
Recovery manifests mark every exported view key retired for restore; restore
always generates a fresh view key instead of loading any historical copy.

## Audit and threat boundaries

Root-only account audit keeps action, outcome, UUID, reported/shown/returned
slugs and UTC time for 180 days. Tie the report to that record by reported slug
and time; store no email/report body. Other operation metadata and TOTP failure
row IDs expire after 30 days. Measure each record from its event UTC time;
status reads never extend retention. Daily retention removes expired personal
fields even from unresolved requests; unresolved state keeps only nonpersonal
recovery metadata. If target evidence expires, require a fresh show; never
reconstruct or replay an old deletion. If an unresolved deletion reaches expiry,
retain its attempt IDs and fence binding but erase its UUID/slugs. After proving
all old attempts stopped, `abort` closes it `unverified` and releases the fence
without claiming account absence or public verification. With no target
evidence, resume/verify refuse; a new report/show/confirmation is needed for
another action. Pruning never releases a fence itself. Root can alter this audit
and host loss can lose it.

Reserve 2 MiB/request within 64 MiB before planning or accepting work. Count
each reserved request once at 2 MiB, including its records and temporary writes;
count unreserved retained records at their actual bytes. Serialize admission,
release and accounting so concurrent requests cannot overbook. Release unused
capacity only after durable closure and proven attempt quiescence: expired or
refused plans, aborted requests, completed rotations, and resolved deletion
writes qualify even while public verification or media cleanup remains pending.
Keep actual records until their event retention expires. Suspended or unresolved
requests retain the reservation. Expiry never frees a running attempt's space. A
later resume/verify must reacquire the reservation before writing; a saved read
needs none. Crash recovery recomputes this accounting from durable state,
charging an uncertain closure the full reservation. Compact closed state, not
unexpired audit events; 32 small retained records must not prevent request 33.
Refuse true capacity or audit-write failure; never evict unexpired records. Keep
key archives/export ciphertext separate from this quota. Journal and alert
output contain only request ID, kind, phase and closed outcome, never target or
raw diagnostics. No personal audit export to CI. The 180-day root audit replaces
the proposed use of 30-day server journald for that purpose.

- Anonymous visitors, signed-in users, hostile resume content and stolen
  application/MCP sessions have no command route. Inputs never become shell
  code. Proof browsers cannot call SSH or read host state.
- A network attacker meets verified TLS and pinned SSH. A proof nonce binds
  request, digest and phase; it cannot authorize a different action.
- A compromised web/renderer/edge container lacks operation state, staging,
  archives and the Podman socket. A compromised Go container already has live
  keys and DB authority: it can decrypt its ring and mint passes. Re-encrypting
  cannot revoke copied factor seeds. Rebuild the host if isolation is uncertain,
  rotate credentials, and use account-owner factor replacement, never reset.
- A DB writer without keys cannot decrypt bound TOTP ciphertext or mint a pass,
  but can corrupt/delete rows or change gate state. Host controls do not repair
  that authority; quiesce and investigate. DB rows never clear host fences.
- A leaked DB backup lacks application keys; an age copy needs the separate
  identity. A plaintext host snapshot or stolen age identity defeats that claim.
- A stolen usable operator SSH/sudo session has root authority and can bypass
  confirmation, forge proof receipts, delete accounts, or export keys. These are
  accident guards, not a second authorization factor. Revoke SSH keys and
  terminate existing SSH/control-master sessions from the provider console;
  recover from a clean device and rotate exposed credentials. Root remains a
  named privileged bypass; the age identity must not share that session.
