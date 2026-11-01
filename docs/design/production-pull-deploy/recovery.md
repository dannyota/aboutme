# Production pull deployment recovery

See the [design overview](README.md), [protocol](protocol.md), and
[execution](execution.md).

## Configuration revisions and repair

`config_revision` identifies enrolled local deployment inputs;
`applied_config_revision` identifies inputs used by the settled running
deployment. Enrollment starts them equal. Inputs include flags, preserved
example-derived settings, secret-file/Podman-secret versions, and other local
values consumed by rendering or startup. They exclude generated deployment
outputs and mutable application data.

Ordinary changes require pause and `config_begin` with null `recovery_binding`.
Under bundle then fence lock, require no operation, executor, cleanup, or
existing configuration transaction. Persist `ConfigChange` with null recovery
binding, previous revision, and next revision exactly one greater; increment
expectation revision before allowing writes. Every supported writer and
installer honors this barrier, which blocks delivery admission and polling
resumption.

Stage and validate changes through reviewed administrative helpers. Keep secret
values within existing custody, never commands or reports. Coordinated key
rotations retain their safety protocol and reconcile this barrier before
completing. Ordinary changes must leave running service safe until
reapplication; other changes use attended maintenance.

`config_finish` requires the exact transaction, epoch, current expectation
revision, and recovery binding. Verify schemas, permissions, secret references,
feature floors, release compatibility, and safe runtime state. Write and fsync
an immutable private baseline indexed by the reserved next revision, then
atomically select it by advancing `config_revision`, increment expectation
revision, and clear the transaction. Leave `applied_config_revision` unchanged
and polling paused. Reverting all edits still consumes the reserved next
revision.

Private baselines cover content, ownership, modes, and secret versions; secret
fingerprints use a host-held keyed hash and never enter public status. Until the
atomic revision update, a staged baseline is not enrolled. Admission and
execution compare inputs with the selected baseline before consuming them.
Unrecorded drift refuses admission; drift after admission fails the operation
and retains ownership.

The sole exception to the owned-deployment write prohibition is an explicit
configuration transaction inside a live `settle_repaired` recovery invocation.
`config_begin` supplies the exact non-null retained binding and current
revision. Require pause, matching owner/journal/executor, no completion for that
binding, no configuration transaction, and the recovery termination proofs
below. The recovery invocation must establish maintenance, stop affected
consumers, and persist the applicable sticky intents before permitting input
writes.

Persist that binding in `ConfigChange`, reserve the next configuration revision,
and increment expectation revision before repair helpers write. Keep operation
ID, request hash, epoch, consumed generation, and ownership unchanged. Only
reviewed helpers acting for this recovery invocation and transaction may write;
ordinary installers and concurrent recovery remain refused.

Recovery `config_finish` uses the same baseline and revision transition as
ordinary completion. It enrolls repaired inputs without claiming they were
applied. The recovery invocation then explicitly converges and renders from that
baseline, reconciles migrations and documents without automatic migration
replay, starts guarded units, and obtains fresh proofs. It independently
measures the final snapshot and commits
`applied_config_revision=config_revision` only through terminal recovery
completion.

A crash or validation failure retains the configuration barrier and operation
ownership. A fresh `settle_repaired` invocation may resume only the exact
binding and existing change ID after proving the prior invocation and cleanup
terminated; it reuses the reserved revision. No starts or settlement may bypass
an unfinished transaction. If configuration finished before failure, recovery
uses its enrolled baseline and outstanding applied/current mismatch without
silently reserving another revision.

After ordinary configuration completion, prepare a fresh SSH deployment,
normally of the installed tag; its expected-state hash binds the new revision.
Successful completion makes applied/current revisions equal. Activation requires
equal revisions. Neither configuration path has a timeout, generic clear, or
stale-approval bypass.

## Activation, rollback, and recovery

Activation requires healthy settled state and exact requested
release/application digests already running. Its path is
`admitted → ready_to_commit → committed → complete`. It raises the floor without
backup, convergence, rendering, migration, restart, or flag changes. The floor
never decreases.

Attended rollback uses current controls and maintenance image, skips backup and
migration, and requires verified evidence that the failed release applied no
migration and the target reads every stored document version. It never rewrites
document versions or restores the database automatically. A database restore
explicitly names its recovery target and loss of later writes. Restoring
delivery state or a host snapshot requires fresh paused enrollment with a new
epoch.

Recovery requires pause, exact binding/revision, terminated prior executor and
cleanup, and proof that matching migrators, containers, queued starts, and
database sessions cannot execute. It uses a fresh service invocation under
retained ownership, increments `recovery_attempt`, and persists recovery intent
before changes. An existing recovery configuration transaction admits only its
matching `settle_repaired` continuation.

| Recovery action    | Preconditions and terminal outcome                                                                                                                                                               |
| ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `abort_unchanged`  | No quiesce, host-write, render, migration, startup, or activation intent; live state equals previous; settle `aborted`                                                                           |
| `restore_previous` | No host-write, render, migration, or target-startup intent; floors and compatibility permit previous snapshot; attended restoration and fresh proofs settle `restored`                           |
| `settle_repaired`  | Reviewed repair reconciles configuration, migrations, documents, and runtime; non-null `proposed_installed` independently measured and verified before fresh proofs and settlement as `repaired` |

For `settle_repaired`, the supplied snapshot states the requested result,
including any reserved configuration revision; it never proves that result.
Refuse terminal completion unless independent measurements match it, the
baseline remains unchanged, both configuration revisions agree, and no
configuration transaction remains. Other recovery actions require null
`proposed_installed`.

The evidence hash binds a private recovery record; a hash alone is insufficient.
Preserve original intents. `failed` means known failure, `unknown` means
uncertain effects or termination, and `manual_required` means an unsupported
transition. None releases ownership or triggers retry. Terminal recovery uses
the same completion/cleanup protocol. Generic `fence.sh clear` refuses delivery
and configuration transactions.
