# CrowdSec cutover and restore

This contract composes the [CrowdSec privacy gate](crowdsec-http-privacy.md)
with the existing host operation fence. It owns bootstrap admission, completion,
and restore provisioning. The privacy contract owns data use, retention,
credentials, loss, backups, and release compatibility. The
[notice restore contract](crowdsec-privacy-notification.md#send-authority-and-restore-fence)
owns notice authority and its private controller protocol. ADR 0027 remains
unchanged. This technical completion ships with the PostgreSQL cutover feature;
it adds no API, UI, document schema, or older-client fallback.

## Start authority

Keep the same persistent cutover operation, existing fence file lock, and
`reset-db` mutation marker. Retained ownership or a marker blocks unattended
starts, including after a crash or reboot. No new lock, marker, takeover rule,
or general bypass is added. The owner checkpoints the exact release before each
effect and preserves the monotonic notice and PostgreSQL release floors.

An owner-controlled command creates a root-only permit under `/run` for one
exact operation, release, and unit, then starts that unit. Its `ExecStartPre`
atomically consumes the permit before any vendor pre-start action. Allow only:

- `aboutme-server.service` and `aboutme-web.service`;
- `crowdsec.service` and `crowdsec-firewall-bouncer.service`;
- the selected final `aboutme-caddy.service` or `aboutme-maintenance.service`;
- `aboutme-crowdsec-retention-startup.service`,
  `aboutme-crowdsec-retention.service`, and `aboutme-crowdsec-vacuum.service`.

Never permit both Caddy modes together, application jobs, timers, migrations,
database setup, or arbitrary units. A permit authorizes one start, not a passed
database gate. Every schema-capable action still needs its own fresh preflight.

Consumption may retain the spent record's binding to the exact live service
invocation until it exits. Verify invocation identity and caller membership
against trusted service-manager state, not a supplied invocation ID. The record
is never available for another start. Retire it on success, failure, timeout, or
cancellation; stale evidence cannot survive retry as authority. Arguments,
environment flags, root UID, a marker alone, or another invocation's record
cannot establish admission. Root owns all records and their safe parent paths.

## Quiescent bootstrap

1. Complete notice activation. Stop both Caddy modes, engine, firewall bouncer,
   and application workloads. Disable and join retention/vacuum scheduling and
   pending safety-unit jobs before issuing permits. Prove quiescence, including
   pending jobs, processes, and container state. Package hooks cannot restart
   consumers. Activate only the reviewed PostgreSQL/feed bundle under the fence.
2. Provision authentication and PostgreSQL configuration before any CrowdSec
   binary runs. Only the fenced provisioner accepts verified empty schema. Apply
   policy and pass the complete gate before registration. Generate fresh
   credentials into protected files, synchronize the Caddy secret, and drain the
   old feed. Keep the reviewed package registration suppression and exact
   pure-call classification; package success grants no lease or start authority.
3. Stop and join `aboutme-watch.timer` and `aboutme-watch.service` before a new
   lease is created. The pause covers only this bounded quiescent bootstrap. It
   prevents the ordinary watchdog from deleting a lease while timers are
   deliberately disabled. No consumer may start during the pause.
4. Run the exact permitted vacuum unit, then startup retention, then runtime
   retention. Require each to execute and succeed in the same database epoch,
   within its existing deadline. Do not trust saved oneshot success. Vacuum has
   no lease prerequisite; putting its five-minute budget first preserves the new
   lease's 360-second lifetime. Startup clears/rebuilds owned cache and firewall
   state while consumers remain stopped.

Only the first runtime service invocation whose start guard consumed its exact
permit may omit the enabled/active predicates for
`aboutme-crowdsec-retention.timer` and `aboutme-crowdsec-vacuum.timer`. Its
inner admission proves the live invocation, exact owner, `reset-db` marker,
release, both timers disabled/inactive with no pending starts, and all-consumer
quiescence. Preserve every normal database, role, schema, policy, epoch, clock,
lease-file/age, lock, deadline, cleanup, and verification check. The exception
applies only inside that invocation, including its own later lease rechecks. It
never restarts consumers. Ordinary callers and watchdog ticks cannot use it.

An expired lease fails closed. Repeat the required owner-controlled passes;
never extend lease age or renew it from vacuum, a permit, or a mode record. The
existing pass, stop, and lock-wait deadlines remain in force. The temporary
watchdog pause grants no additional ingestion time or retention allowance.

## Cutover mode

After the three actual successful outcomes, retire the spent runtime record and
establish a root-only `/run` cutover mode. Bind it to those outcomes, the
current database epoch, fresh normal retention lease, exact operation owner,
marker, and release. Never create it early or infer success from an unused or
spent permit. An expired lease requires fresh passes, not timestamp repair.

This mode lets lease checks tolerate only intentionally disabled retention and
vacuum timers. All other checks remain mandatory. Missing outcomes, wrong owner,
changed epoch/marker/release, malformed state, stale/future lease, or reboot
refuse admission. Mode authorizes no unit start and renews no lease.

Restore watchdog scheduling and require a successful cutover-mode tick before
owner-permitted engine, bouncer, app, and selected Caddy starts. Verify
authentication, cache rebuild, and enforcement. Repeat permitted runtime passes
when proofs change candidate state. Bootstrap admission is unavailable after
consumers start; only the complete mode supports this later timer-disabled
interval. Every start still consumes its own exact permit.

## Completion and failure

Completion validates the mode, full database gate, fresh lease, current release,
expected final consumers/app state, inactive maintenance mode, and all required
proofs. Stop and join the watchdog before the serialized transition, then
revalidate those prerequisites. Retire all outstanding and spent permits.

Under the existing fence file lock, remove the marker, enable and verify both
safety timers, remove cutover mode, restore watchdog scheduling, and prove one
ordinary watchdog tick before durably releasing the exact operation owner.
Catch-up start guards must remain blocked until this transition and owner
release succeed. Never wait for a safety service whose guard needs that lock.
Timer activation does not prove its triggered service completed.

Validation during this critical section must reuse its held lock. The fixed
watchdog check and database preflight need an internal composition that
validates the inherited canonical lock and runs under it, without reopening the
fence lock or calling a lock-taking fence helper. This is no public bypass
argument, environment flag, alternate gate, or caller-selected command.

Before entering its ordinary lease checker or fail-closed body, every scheduled
watchdog invocation acquires the existing canonical fence lock shared, with the
existing 30-second wait bound inside its whole-pass deadline. Retain that lock
through the ordinary lease/database checks; never release and reacquire it at
checker entry. A timeout refuses the invocation before the fail-closed body
runs. Once admitted, every ordinary lease check and failure action still
applies. No helper under this shared guard may acquire the fence lock
exclusively. This also guards persistent timer catch-up while completion holds
the exclusive lock; correctness must not depend on completion finishing within
two seconds.

Stay within existing wait/deadline bounds; failed completion fails closed. After
release, observe the scheduled watchdog, retention, and vacuum passes. Only this
completion path may release cutover ownership after permitted starts; ordinary
release cannot use the temporarily absent marker to bypass it.

Any failure or uncertainty invalidates temporary admission, stops consumers,
disables safety scheduling, and retains or durably restores the marker under the
same owner before returning failure. A crash before owner release, including
after marker removal, leaves the persistent operation blocking starts. Reboot
removes `/run` permits and modes. Recovery proves quiescence and current gates;
saved oneshot status and spent permits prove no success. Keep the existing
quiescent `mutation-clear` recovery before any owner-permitted start. There is
no SQLite recovery or lock takeover. PostgreSQL stays available to the app. If
the final durable owner release succeeds but its response is lost, report
unknown and inspect state read-only. Never recreate a released owner or infer
successful completion merely from its absence.

## Restore composition

Every cluster or point-in-time restore discards CrowdSec state under the privacy
contract. Keep `notice_restore` ownership from the outer controller's begin
through its exact private completion and finish. Never release it to acquire
`cutover`, change its kind, synthesize `reset-db`, or introduce a second owner.

The fixed workload-locked restore child invokes `restore-reset` after PostgreSQL
recovery settles, before final database checks, notice postcheck, and private
completion. This purpose-specific provisioner branch accepts only that current
`notice_restore` operation. The existing `fresh` path and cutover
`restore-reset` path still require their exact `cutover` owner and `reset-db`
marker.

Pass restore context privately from the validated child, using the existing
source-only/inherited-pipe pattern. No public operation-ID, path, command,
release, epoch, or environment override is added. Before any CrowdSec database
effect, prove exact owner/kind/admission tag, unchanged canonical mount
identity, inherited workload ownership, adopted notice authority, this pass's
fresh notice epoch, durable reconciliation marker, absent send/activation
receipts, and quiescent consumers, safety jobs, and watchdog. Preserve complete
app absence on the drill; a full-host controller must prove its existing
full-host quiescence.

The provisioner acquires the existing fence file lock once while workload
ownership remains held. Checkpoint before acquiring it, then directly revalidate
all bound authority under it before each destructive or schema-capable effect.
Reuse the held lock for internal preflight/policy functions; never call a
lock-taking notice/fence helper while holding it. Preserve workload descriptor
inheritance through all foreground descendants and close private completion
descriptors to them. Neither provisioner success nor exit releases either owner.

Drop/recreate only the CrowdSec database, apply policy, verify empty attack and
registration state, and invalidate old credentials/caches. Keep app database
checks, recovery-time measurement, role isolation, and the fixed smoke check.
The role inventory must recognize and validate the expected least-privilege
CrowdSec role rather than accept arbitrary extra roles. Provisioning requires
the pinned engine binary and reviewed PostgreSQL configuration even on an
app-absent drill; missing dependencies fail before restore effects. Stage only
the pinned `crowdsec`/`cscli` executables, reviewed PostgreSQL config, schema
manifest, and fixed provisioner/DB dependencies there. Verify executable
provenance against the same authenticated pinned package artifacts; do not
configure their packages or run hooks to obtain those files on the drill. Record
the exact file inventory and prove it sufficient in hosted integration. Add no
consumer unit, timer, application release, or mail credential. The full-host
package path retains its existing package-state and suppression checks. Do not
skip reset for pre-CrowdSec backups or absent schema.

The child completes only after fresh provisioning, all database checks,
foreground joins, and notice postcheck succeed. Preserve the existing private
context/completion framing and exact parent/helper join protocol unchanged. Only
its ordinary finish releases `notice_restore`; reconciliation and receipt
absence remain. Any failed reset or uncertain completion retains denial and
ownership under the notice failure rules. An app-absent drill proves fresh
CrowdSec storage, not production credentials, notice reconciliation, or consumer
restart. A later full-host start must generate fresh managed credentials,
synchronize consumer secrets, and satisfy both notice activation and the full
retention/start gate. Restore never grants cutover permits or mode.

## Trade-offs and security

The quiescent watchdog pause and invocation binding resolve bootstrap without
weakening ordinary checks. Enabling persistent timers early can race a manual
permit; creating full mode early would claim missing outcomes. Both alternatives
need extra authority or exclusion mechanisms. Retaining `notice_restore` avoids
an unguarded interval and preserves the existing restore protocol. Internal lock
reuse adds a narrow composition interface that requires hosted proof.

No additional service, database, key, client contract, or data lifetime is
added. Keep existing host memory/connection budgets and sequential safety
passes. Credentials stay in the privacy contract's protected files; this
authority is root-owned host state, never database content or a transferable
receipt.

| Attacker                                                                 | Boundary                                                                                                                           |
| ------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------- |
| Anonymous visitor, signed-in cross-user attacker, hostile resume content | No HTTP, renderer, or application input grants host-operation or invocation authority.                                             |
| Stolen session/MCP token or visitor-to-edge network position             | No token/header-based cutover or restore entry point exists.                                                                       |
| Compromised non-root container                                           | Root-only host records and the existing socket/UID boundary prevent permit creation and peer access.                               |
| Database writer without application keys                                 | Cannot forge host authority; schema/policy/epoch corruption closes the gate.                                                       |
| Leaked backup                                                            | Cannot restore `/run` authority or notice authority; fresh provisioning removes restored registrations and samples before startup. |

Host root remains trusted. Failure retires temporary authority; restart and
recovery require current evidence. Emit only fixed outcomes and aggregate
counts.

## Required proof

Use existing hosted fence, lease, watchdog, retention, restore, and integration
suites with real systemd for job/invocation behavior:

- A real startup-produced lease admits exactly the permitted first runtime
  invocation with both timers disabled and all consumers stopped. Ordinary
  consumer, watchdog, and unpermitted runtime checks still refuse.
- Wrong/replayed invocation, owner, unit, release, marker, malformed/spent
  records, missing outcomes, stale/future lease, changed epoch, and failed
  database/policy gates refuse before cleanup or starts.
- Joined bootstrap prevents watchdog lease deletion; bootstrap evidence never
  admits a watchdog tick. Full-mode tick succeeds before any consumer starts.
- Pending jobs and persistent catch-up cannot consume a manual permit, bypass
  retained ownership, or deadlock completion. Ordinary tick uses the held lock
  safely; scheduled watchdog catch-up waits under its shared-lock guard before
  checker entry, and guard timeout never enters its fail-closed body. Timer
  enablement never substitutes for a completed safety pass.
- Interrupt every boundary through owner release, including marker removal;
  reboot; fail timer enablement, ordinary tick, and durable state writes. Prove
  closed starts, permit retirement, marker recovery, and forward-only recovery.
- Restore latest and point-in-time, including pre-CrowdSec backups, within the
  exact `notice_restore` child. Wrong kind/owner/tag/epoch/mount, missing
  private context, lost workload ownership, reset failure, child death, and
  parent death cannot release ownership, emit success, or regain notice/start
  authority.
- Prove no recursive fence lock, no kind switch or release gap, application DB
  preservation, reset before completion, and unchanged private protocol/denial.

Independent design review precedes implementation. Independent adversarial
implementation review and exact-commit CI precede release. Baseline CI cannot
prove these new transitions; production proof follows the privacy contract.

## Sources

- [systemd timer activation and persistent catch-up](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html)
- [systemd service pre-start and execution ordering](https://www.freedesktop.org/software/systemd/man/latest/systemd.service.html)
- [systemd unit dependencies](https://www.freedesktop.org/software/systemd/man/latest/systemd.unit.html)

The timer race is an inference from those semantics and the shared target units;
hosted tests must prove the deployed behavior. The timer/service manuals were
read locally; no secondary-source claim is used.
