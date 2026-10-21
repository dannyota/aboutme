# CrowdSec PostgreSQL

The source provides a fenced transition to a separate CrowdSec database in
PostgreSQL 18. Source inspection and configured limits do not establish
successful deployment, retention, enforcement, or absence of outbound traffic.

The [privacy contract](../design/crowdsec-http-privacy.md) owns data,
connection, logging, and retention limits. The
[cutover contract](../design/crowdsec-cutover.md) owns admission and completion.
Use the [production runbook](production.md) for release eligibility, pinned SSH,
normal deploys, and backups.

## Before the first transition

Require all of the following before changing production storage:

- Independent adversarial review, successful exact-main CI, and authenticated
  release images for the exact candidate. Run from clean, current reviewed
  `main`; a branch or manual CI run grants no release authority.
- A successful real isolated
  [pre-cutover rehearsal](../design/crowdsec-rehearsal.md), including setup,
  every workload/repeat, capacity evidence, and confirmed resource cleanup. The
  [host memory gate](../design/vietnam-host-memory.md#rehearsal-gate) applies
  unchanged. Missing measurements or failed cleanup block cutover.
- Valid existing notice activation through the
  [notice procedure](crowdsec-privacy-notice.md). The production campaign is
  complete. Do not resend it, prepare another cohort, or recapture membership.
- Verified private evidence for the single approved incident copy: restricted
  access on encrypted local storage, exclusion from backup and synchronization,
  original capture time plus a 30-day deadline, persistent private expiry
  tracking, and key destruction at expiry. Do not claim encryption or expiry
  without that evidence. Public evidence must contain no snapshot path, name,
  hash, size, or content. Do not create another copy or use it for recovery.
- A usable current cluster backup and the approved production proof procedure.
  Record backup, resource, enforcement, privacy, retention, and recovery
  evidence as fixed outcomes and aggregates, never attack rows or credentials.

Do not run the ordinary full host installer or `deploy.sh` before this first
storage transition. The cutover command stages its reviewed controls without
activating the new storage/feed bundle until consumers are quiescent. Do not
copy internal helper commands into an alternate installation procedure.

## Transition and completion

From the eligible operator checkout:

```sh
DEPLOY_HOST=<host> bash deploy/vn/scripts/crowdsec-cutover.sh <tag>
```

The coordinator verifies exact AMD64 image provenance and transfers a private
bundle. Staging verifies its archive and extracted bytes and retains the bundle
lock through persistent cutover-owner acquisition. Later host actions recheck
the same bundle and exact owner/tag. The coordinator records `reset-db`. Its
host transition stops and joins consumers and jobs before rendering candidate
units or activating storage/feed changes. It owns pinned PostgreSQL
configuration, fresh CrowdSec provisioning, credential replacement and
synchronization, and feed draining. It imports no SQLite history, bans,
detection windows, or registrations. Keep non-database assets; do not replace
this path with manual file deletion or package installation.

The transition runs the exact permitted vacuum, startup retention, and runtime
retention passes in that order. Each must actually succeed in the same database
epoch. Saved oneshot success, timer activation, and unused permits prove no
pass. Only those outcomes and a fresh lease permit cutover mode. Watchdog
coverage resumes and a successful cutover-mode tick precedes permitted consumer
starts. The narrow bootstrap admission does not authorize ordinary callers or
application jobs, migrations, or database setup under cutover ownership.

A successful transition reports `PASS cutover-serving-fenced`. Serving remains
under the exact owner and mutation marker while required external proofs run;
that result is not completion. Keep the owner private. Revalidate resource
observations, current lease/gate, fresh credentials and old-credential
rejection, HTTP/firewall enforcement, backup, feed/log privacy, and physical
retention. Use only the reviewed proof procedure. Repeat owner-controlled
retention passes when proof changes require them; never edit a lease or
manufacture success records to extend admission.

Only after those proofs succeed, complete with the same tag and exact owner:

```sh
DEPLOY_HOST=<host> bash deploy/vn/scripts/crowdsec-cutover.sh \
  --complete <tag> <owner>
```

The owner must verify external proofs before completion. The command revalidates
the exact owner, operation kind, tag and release floor before watchdog effects,
then current database, lease, release, and consumer state. Under the canonical
fence lock it removes the mutation marker, enables and verifies both safety
timers, retires temporary admission, restores watchdog scheduling, and proves an
ordinary tick before durably releasing ownership. Timer catch-up cannot
substitute for the actual safety passes or bypass the retained owner.

After completion, observe normal scheduled watchdog, retention, and vacuum
passes, ordinary serving and jobs, and backup work under normal guards. Record
those outcomes and resource limits before accepting the deployed result. These
observations cannot replace the pre-cutover rehearsal.

## Failure and unknown responses

Treat a failed or uncertain transition as incomplete. Failure recovery stops
consumers, invalidates temporary admission and the lease, and retains the exact
owner and mutation marker. A lost reply does not prove those effects completed.
Reboot removes temporary permits and mode; persistent ownership still blocks
starts. Do not delete fence files, lower the release floor, retry a destructive
step after a lost reply, or restore SQLite. Use read-only release, fence,
service/job, container, and database metadata to establish what completed. Keep
operation values and private state out of public reports.

Recovery is forward under the same owner. Prove full quiescence and current
gates before recovery; spent permits and saved service status grant no
authority. The existing quiescent `mutation-clear` recovery is available only
before any owner-permitted start. After permitted starts, only cutover
completion may release ownership. Follow the
[completion and failure contract](../design/crowdsec-cutover.md#completion-and-failure)
for the supported boundary; stop for reviewed recovery if current state cannot
be proved.

If durable owner release succeeded but its reply was lost, the outcome is
unknown. Inspect read-only state. Owner absence alone does not prove completion;
never recreate a released owner. Preserve fail-closed handling until the actual
outcome and required proofs are known.

After successful completion, ordinary image rollback retains the PostgreSQL host
bundle, compatible notice, and monotonic floor. It cannot restore SQLite or
bypass the [normal rollback conditions](production.md#rollback).

## Retention and maintenance

`crowdsec-db.sh preflight-ready` implements the read-only gate used before every
schema-capable binary action and SQL cleanup. It validates the pinned catalog,
identity, ownership, grants, epoch, logging, and statistics policy. Runtime uses
the root-to-`crowdsec` peer map on `/run/postgresql`, a four-connection engine
pool, and role connection limit eight. No app identity, TCP, or password
fallback is supported. Only the fenced provisioner may accept verified empty
schema.

Startup retention requires synchronized time, the mounted data volume, and the
current PostgreSQL service. Runtime retention runs every five minutes and
reconciles exact deletion candidates with active consumers before physical SQL
cleanup. Missing acknowledgement requires stopped consumers and cleared caches
and owned nftables state before deletion. Failure never counts as deletion.

Only complete cleanup and verification renew the root-owned lease. Ordinary
lease checks require both retention and vacuum timers enabled and active. The
watchdog stops both Caddy modes, engine, and firewall bouncer when the lease,
clock, database, policy, or cleanup gate fails. PostgreSQL stays available to
the app. The
[live-retention contract](../design/crowdsec-http-privacy.md#live-retention-and-failure)
owns deadlines, age limits, and reconciliation details.

`aboutme-crowdsec-vacuum.timer` runs hourly serialized `VACUUM (ANALYZE FALSE)`
with a five-minute deadline. Never run `ANALYZE` or `VACUUM ANALYZE` on
CrowdSec. Unexpected planner statistics fail the gate and require fenced
database replacement, not catalog editing. Preserve protected role logging
settings; inspect fixed outcomes and aggregate counts rather than rows, raw
diagnostics, sensitive logs, or credential files.

## Restore

Ordinary pgBackRest backups include the whole cluster: CrowdSec attack records,
registrations, credentials, and WAL. The 30-day recovery window and weekly fulls
normally retain copies for about five weeks after live deletion. Expiration
failure alarms apply; this is not a strict physical-erasure deadline. Backups
serve recovery only, with the
[accepted access and lifetime limits](../design/crowdsec-http-privacy.md#backups-and-restore).

Every cluster or point-in-time restore, including a pre-CrowdSec backup, keeps
consumers fenced and fresh-provisions only CrowdSec storage. It discards attack
rows, statistics, history, bans, and registrations while preserving application
data. Never rewind the application cluster to repair CrowdSec alone.

The fixed workload-locked restore child invokes `restore-reset` inside the
existing `notice_restore` owner before final database checks, notice postcheck,
and private completion. Do not release that owner to acquire cutover ownership,
change its kind, or synthesize `reset-db`. An app-absent drill stages only the
verified pinned binaries and fixed dependencies, without package hooks, consumer
units, mail credentials, or an application release.

A successful app-absent drill proves fresh CrowdSec storage, not production
credential replacement or consumer restart. A later full-host start still needs
fresh managed credentials, synchronized consumer secrets, notice reconciliation
and activation, and the complete retention/start gate. Failed or uncertain reset
retains denial and ownership. The
[restore composition](../design/crowdsec-cutover.md#restore-composition) owns
the private protocol; the
[host restore guide](../../deploy/vn/README.md#restore-drill) owns the outer
drill command.
