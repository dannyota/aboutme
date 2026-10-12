# Vietnam host operations

Proposed under [ADR 0027](../adr/0027-vietnam-hosted-production.md). Install new
`deploy/vn/host/operation-run.sh` in `/usr/local/lib/aboutme/`. Call new
`deploy/vn/scripts/operations.sh` with `DEPLOY_HOST=<host>`:

- `plan totp-rotate|view-pass-rotate`: ID, live tag/digests, slots, effects.
- `account-delete show <slug>`: existing listing and request ID; reported slug
  must occur in the returned list.
- `apply <id> --confirm <text>`: exact `rotate-totp`, `end-all-view-passes`, or
  `delete-account:<uuid>:<reported-slug>`.
- `status|resume|verify|abort <id>`; TOTP also `retire <id>`.

Reuse `deploy.sh` SSH, clean-main, eligibility, provenance, and installed-hash
checks. Refuse arbitrary commands, images, and paths. Plan/show are synchronous
60-second reads, with no unit, checkpoint, or proof acknowledgement. Save a
root-only plan for 15 minutes; interrupted reads yield none. Apply rechecks
release, flags, secret IDs, target. Changes need a new plan; resume reuses keys.

Keys, including staging, are root-owned 0400 files in `/etc/aboutme/secrets/`.
Generate 32 random bytes on-host, canonical 43-character base64url. Inject named
Podman `type=env` secrets from files/stdin. Never trace/print keys, env, secret
payloads. TOTP jobs receive only `db-app-password` and selected TOTP slots.
Account and queue-status jobs receive only `db-app-password`. All use
`aboutme_app`, the PostgreSQL socket, no network, the live digest, 256 MiB, no
swap, and no restart.

## Execution, recovery, and audit

New `host/etc/systemd/system/aboutme-operation@.service` runs mutations: root
oneshot, `Restart=no`, 45-minute timeout, failure alert, request flock. Persist
intent before start; rotations await `prepared`. SSH loss cannot resubmit. Lock
order: fence (`totp_reencrypt`, new `account_delete`/`view_pass_rotate`),
key-change job drain, `workload-lock.sh` (60-second wait). Plan/show skip drain,
release after capture. Status reads atomic state without mutation locks. Match
`release-json` to runtime digest/tag/number/secret IDs using metadata. Require
TOTP tag/floor >=4007, view-pass >=6026, enabled flag floors; never change
floors. New `fence.sh lock-for <kind> <tag> <id>` atomically binds request/fence
IDs; same tuple returns its ID, other owners fail. Lost replies reread the
binding. Checkpoint mutations and their starts.

Fsync intent before effects and results after; atomically rename and sync dirs.
`/srv/data/host-operations/` is root 0700, files 0600; require the data mount,
reject symlinks, unknown fields, and malformed state. Record IDs, phase,
release, slots, times, and closed outcomes. Before active secret/flag writes,
create `key-change.json` there. Extend fence checks to block serving and job
starts while marked, across reboot; maintenance is allowed. Only the runner
removes it after files, Podman secrets, flags, and units agree. Allow owner-only
checkpointed render while marked. Persist `starting` before removal; resume
starts the saved release.

For key changes, stop timers, drain jobs, start maintenance, stop Caddy and Go,
prove stopped, then require the external marked 503 before active secret, Podman
secret, or flag writes. Staging and encrypted export precede maintenance. Add
`deploy-host.sh reload-live`: recreate the exact live release under held locks
without migrations or nested `deploy.sh`. Check local readiness, stop
maintenance, restore timers, and verify outside. Release the workload lock while
awaiting proofs or ten-minute checks; retain the fence until completion or safe
abort. Missing results, timeout, OOM, or uncertain stop state retain the fence.
`resume` proves old unit/container/DB backend stopped. Tag connections with
request ID in `application_name`; inspect metadata only. `fence.sh clear` must
refuse an operation unit/container/backend or key marker. Abort needs quiescence
and safe serving; no exit trap unlocks. Failed reload retains maintenance.

Root/admin JSON retains account action/outcome/UUID/slugs/UTC time 180 days
([contract](showcase-contract.md#derived-values-and-reports)); other IDs,
phases, outcomes, digests, times, counts, and TOTP failure row IDs 30 days. Use
`--log-driver=none`; accept only closed output, discard diagnostics, mark
unknown outcomes. Journald: request ID/kind/phase/outcome only. Never retain
email, name, headline, IP, viewer identity, cookies, keys, ciphertext, or report
text. Daily `aboutme-operation-retention.service`/`.timer` calls private
`prune`; retain only non-personal unresolved state. Reserve 2 MiB/request within
64 MiB; block mutation on capacity/audit-write failure. No unexpired eviction or
auto-export. Host loss loses audit; root can alter it.

## External proof channel

`scripts/host-operations-proof.sh <id>` reads the host action and runs the
pinned browser: 8 GiB memory floor, shared local-check lock, no active stack, 2
GiB/no-swap/two-CPU cap, 60-minute timeout. Stage allowlisted sources; disable
traces, screenshots, video, downloads, and raw output. Modes are
`hostops-prod-totp`, `hostops-prod-view-pass`, and
`hostops-prod-account-delete`. Origin is `https://aboutme.vn`. Read-only 0600
inputs: `request.json` contains `requestId`, `action`, `digest`, `slugs`, and
view-pass `witnessSlug`; rotations add `account.env`. TOTP requires
`ABOUTME_TEST_EMAIL` and `ABOUTME_TEST_PASSWORD` for the fictional account.
View-pass uses `PROVIDER` (`google|linkedin`), `PROVIDER_USERNAME`,
`PROVIDER_PASSWORD` for the named fictional provider account. Missing
inputs/provider challenges block; create no account or resume.

In laptop tmpfs, mount 0700 `control/` read-only and `reply/` writable into the
browser. Wrapper writes `control/phase.json`, browser `reply/result.json`, by
atomic rename. Each is <=1 KiB closed JSON: `requestId`, `digest`, `phase`,
32-byte random base64url `nonce`; reply adds only `result` (`pass` or `fail`).
Reject unknown fields, symlinks, oversized files, stale tuples, and duplicate
advances. Poll once a second; one challenge outstanding. The wrapper gets
challenges from host `status` and forwards valid replies by SSH as
`proof <id> <phase> <nonce> <pass|fail>`. Keep `run.sh` output buffered; use
only this channel for progress. Mount no SSH/agent, Podman socket, host paths,
or archive.

At `prepared`, enroll a temporary TOTP factor, refusing existing TOTP; retain
setup/recovery codes only in browser memory and prove sign-in. View-pass uses
the witness below. Then `maintenance` permits active key writes, `serving`
checks reload, and TOTP-only `retired` checks retirement. Deletion uses only
`serving` to verify deleted slugs. Each challenge expires in five minutes.
Failure, expiry, or browser exit never advances host state; resume replaces the
nonce and repeats the proof. The host unit ends at a saved phase when waiting
expires. Restarting a lost view-pass browser follows the unverified rule below.
Finally remove only the proof factor, log out, and delete channel/profile files.
Lost TOTP seed/codes can permanently lose that fictional account (ADR 0017).
Resume keeps its phase and prior evidence; enroll another named factorless
account under the current ring and prove sign-in. Before activation repeat
`prepared`; afterwards repeat `serving` or `retired`, never rewind writes.
Retirement still needs serving proof and both zero snapshots; never reset
factors.

## TOTP rotation

1. New `server totp-key-status`: read-only snapshot of derived ring IDs and all
   off-active credential/enrollment counts, including expired enrollments.
   Query/config errors fail closed. Require no previous slot, zero off-active
   rows, and a prospective slot unused by rows or runtime.
2. After `prepared`, generate/adopt the staged key under locks; archive any
   retired slot before overwriting. Use `age-copy` to export staging, current
   ring, and archives in one stable snapshot. Encrypt on-host to the configured
   owner recipient; identity stays off-host. `operations.sh` saves ciphertext in
   `aboutme-infra` outside this host, fsyncs file and directory, then sends
   `recovery-copy <id> <key-id> <sha256>` over SSH. Host matches the staged key
   and export digest and saves the receipt before proceeding. Copy failure or
   lost receipt blocks activation; resume recopies the same staged key. Retain
   the copy by the archive rule below. Under maintenance, sync/validate its
   Podman secret, set new active/old previous atomically in `flags.env`, reload,
   and record switch time.
3. Run existing `server totp-key-reencrypt` with both keys under both locks and
   its [limits](totp-key-management.md#rotation). Failure retains both keys;
   earlier batches may commit. Failed counts prove nothing; remaining rows need
   explicit `resume`.
4. Require zero off-active counts in a successful status snapshot, then another
   at least ten monotonic minutes after switch. Reboot or uncertain switch time
   restarts the wait; skipped rows cannot count as zero. `verify` proves outside
   readiness and TOTP sign-in with the prepared factor.
5. `retire` needs both snapshots and proof; recheck zero under locks, archive
   old key, remove its mapping, reload same tag. Remove its Podman secret after
   old containers stop; repeat outside proof.

Keep derived-ID archives and encrypted copies for all dependent backups, at
least 30 days. `prune-key <key-id> --confirm no-restorable-backup-needs-key`
checks backup inventory and records offline-copy attestation. Restore needs
matching keys, at most two; otherwise TOTP recovery blocks. Decrypt failure
needs owner passkey/recovery, never credential rewrites. Known failure may
release after quiescence/ring checks; retain both keys/state. Resume rechecks
live release/ring under a new lock. Unknown writes retain it. Pre-activation
abort discards staging; new rotation waits for retirement.

## Account deletion

Review content, show, confirm UUID/slug. Run
`server account-delete confirm <slug> <uuid>`: transactional owner check,
self-delete path, deletion of resumes/sessions/grants/showcase rows; queue media
deletion and keep 180-day slug tombstones. No maintenance or email. Preserve
exit 4 (not found) and 5 (owner mismatch), neither deletes. Exit 3, missing end
marker, or lost result is unknown. Prove job/backend stopped, then rerun show
and new read-only `server account-delete status <uuid>` (`present` or `absent`).
Absence proves deletion; missing slug alone does not, since owners can rename.
Presence needs fresh show/confirmation, no retry; distinguish shown/returned
slugs.

`verify` checks every known slug: HTML/representations 404; showcase, sitemap,
and `llms.txt` omit it; show returns 4. Preserve
[ADR 0010's exception](../adr/0010-public-artifact-revocation.md): admitted
responses may finish; streams end on notification. Sweep failure never undoes
deletion: alert and resume the existing sweep after releasing the operation
fence. New `server media-deletion-status` takes no arguments, loads DB-only
config, and runs `GetMediaDeletionQueueState` in a read-only snapshot within 30
seconds. Success emits only `{"success":true,"pending":N}`; query/config failure
exits nonzero with no count. Count every `completed_at IS NULL` row, including
leased and delayed retries. Only a successful zero snapshot after confirmed
deletion permits physical cleanup `complete`; otherwise `pending`. Unrelated
work can delay the global zero; sweep success alone proves nothing. Reuse
SQL/schema; no object keys or media credentials. Backups keep existing
retention; never restore the account as rollback.

## View-pass rotation

Under maintenance, persist intent, atomically replace `view-pass-key` with the
validated staged key, replace its Podman secret, and recreate the live release.
Reconcile with that staged key. Existing containers retain old secrets
([Podman][podman]); Go loads its key at startup. Plan states every pass ends;
never restore the old key. At `prepared`, obtain a pass for the fictional gated
resume with photo/PDF. Prove all pass-protected routes admit it and reject
requests without it. Keep cookie, decoded resume ID, epoch, and expiry in
browser memory only. After reload obtain a fresh provider pass in a separate
context: require identical resume ID and epoch, unchanged live/gated state and
slug, and old expiry later than server HTTP `Date` by at least 60 seconds
throughout. Replay the exact old cookie: HTML must gate, every pass-protected
API must 404; fresh pass must admit the same requests. Check discovery/preview
rules ([routes](viewer-analytics/sign-in-to-view.md#gated-routes)), `/readyz`,
and public reads. Missing/expired witness, changed ID or epoch, or ambiguous
state cannot prove rotation: recover readiness, safely abort as `unverified`,
and rotate again with a new witness. Pre-replacement abort only discards
staging. Preserve epochs, flags, floor; refresh encrypted secret copies.

## Threats and delivery

HTTP/MCP callers, hostile content, and stolen sessions gain no host entry. TLS
and pinned SSH protect transport. Containers get no host state/archive/Podman
socket. API compromise exposes keys/DB: rebuild, rotate, and replace copied TOTP
factors through owner recovery. DB writers can change gate state, not decrypt
bound ciphertext or mint passes; fence/audit stay outside DB. Backups lack keys;
encrypted copies need the separate age identity. Stolen usable SSH/sudo or age
access defeats these boundaries: revoke SSH keys/connections from console and
provider credentials, recover on a clean device. Root bypass remains.

Build named scripts and units under `deploy/vn/`; update `host/install.sh`,
`scripts/fence.sh`, `scripts/deploy-host.sh`, `scripts/secrets-host.sh`
(replacement guard, export receipt, archive/prune), `scripts/secrets.sh`
(durable copy). In `apps/server/cmd/server/`, update `main.go`, `totp_keys.go`,
`account_delete.go`, and their tests; add `media_deletion_status.go` and its
`_test.go`. Extend `internal/secondfactor/totp_rotation_test.go` and
`internal/mediacleanup/integration_test.go` for status/races and queue
snapshots. Add VN tests `tests/host-operations-test.sh` and
`tests/host-operations-failure-test.sh`: isolation, stale plans, floors,
duplicates, locks, interrupted phases, reboot, lost commit/copy receipt, audit
capacity, retirement, key resurrection. Add the proof wrapper and
`deploy/dev-https-browser/host-operations-production.spec.ts`; update harness
`run.sh`, `spec-sources.txt`, `static-test.sh`, `production.config.ts`. Map
three hostops modes to the new spec; keep TOTP modes on
`totp-production.spec.ts`. Test bounds, timeout/replay/browser loss,
epoch/expiry, failed/nonzero counts. Update runbooks `totp-keys.md`,
`sign-in-to-view.md`, `account-delete.md`, `production.md`; design links in
`README.md`, `totp-key-management.md`, `passkey-release-fence.md`; marker rules
in `vietnam-production.md`, audit wording in `showcase-contract.md`, and
`deploy/vn/README.md`. Ship commands, then host bundle/runbooks after hosted
Go/DB/VN CI and adversarial review. Older images refuse missing commands;
clients/schemas stay compatible. SQL bypass lacks checks; durable recovery costs
outages. No owner decisions.

[podman]: https://docs.podman.io/en/latest/markdown/podman-secret-create.1.html
