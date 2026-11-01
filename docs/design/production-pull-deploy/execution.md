# Production pull deployment execution

See the [design overview](README.md), [protocol](protocol.md), and
[recovery](recovery.md).

## Discovery, admission, and execution

Use `aboutme-pull.timer` with `OnBootSec=2min`, `OnUnitInactiveSec=5min`, and
`RandomizedDelaySec=30s`. Discovery has `Restart=no`, a 120-second timeout, and
a nonblocking singleton lock. Cap poller REST calls at 40 per rolling hour and
observer calls at 12; reserve the remainder. Count pagination and conditional
requests conservatively, respect reset headers, and defer without production
mutation when evidence or budget is incomplete.

Every deployment needs attended external proofs. GitHub requests additionally
need valid GitHub approval; attendance never substitutes for it. Under bundle
lock, then fence lock, require matching epoch/generation/expected state,
permitted pause state, no configuration transaction, no existing operation, and
no pending executor or cleanup. Fsync request and initial journal before
atomically consuming generation and binding operation/hash in the fence. A
staged journal grants no execution authority.

`aboutme-deploy.service`, with `Restart=no` and an eight-hour timeout, owns
execution independently of SSH. All admission paths, laptop helpers, and host
installation check persistent ownership and systemd lifecycle state. Invoke
pinned `ansible-pull` against the verified local repository, exact checkout SHA,
`localhost,` inventory, local connection, explicit admitted `ANSIBLE_CONFIG`,
one fork, and `deploy/vn/ansible/site.yml`. Never follow a moving branch or use
`--only-if-changed`.

| Transition               | Required behavior                                                                                                        |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------ |
| `admitted → prepared`    | Stage exact images; verify configuration baseline, secret names, disk space, managed manifest, policy, and compatibility |
| `prepared → backed_up`   | Deploy: successful annotated pgBackRest incremental backup; rollback skips backup                                        |
| `→ quiescent`            | Persist intent; stop job timers/jobs; start maintenance; stop serving Caddy, Go, and web; prove stops                    |
| `→ maintenance_proved`   | Accept fresh external maintenance proof                                                                                  |
| `→ host_applied`         | Persist host-write intent before convergence; verify resulting managed manifest                                          |
| `→ rendered`             | Persist render intent before unit writes; render exact digests and recheck compatibility                                 |
| `→ migrated`             | Deploy: persist migration intent before dispatch; prove success; rollback skips migration                                |
| `→ started`              | Persist startup intent; start web and Go; prove local readiness; start Caddy; stop and prove maintenance stopped         |
| `→ smoke`                | Start expected timers; obtain external smoke proofs; remove the test ban                                                 |
| `→ ready_to_commit`      | Verify running digests, floors, configuration, timers, proof set, and no pending step                                    |
| `→ committed → complete` | Apply completion and cleanup protocol                                                                                    |

Fsync `pending_step` and each relevant sticky intent before a side effect; clear
the step only after explicit verification. Missing results mean uncertainty.
Never automatically repeat a migration or resume an interrupted deployment. The
workload lock serializes heavy steps after persistent ownership; helpers must
not acquire it recursively.

Bound backup at six hours, convergence at 30 minutes, migration at one hour, web
readiness at 120 seconds, Go readiness at 180 seconds, and each external proof
phase at 120 seconds. Preserve mount, privacy, CrowdSec, key-change, restore,
CIMD, and floor guards. First deployment, database setup, history
reconciliation, infrastructure changes, and reboot remain attended procedures
outside automatic admission.

## Completion and cleanup

Under the fence lock, atomically install the measured snapshot, update the
security floor where applicable, increment revision, and write completion for
the exact operation/hash/generation. Keep ownership set, then mark the journal
committed. Successful deployment records the configuration revision actually
applied.

`ExecStopPost` always invokes immutable cleanup with the exact binding and
service invocation. Exit status alone never determines success:

| Durable state                         | Cleanup action                                                                                                                                                   |
| ------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Exact completion exists               | Revoke permits and remove temporary resources; preserve settled serving containers and timers; never establish maintenance merely because the coordinator exited |
| No completion; activation only        | Revoke permits, retain any floor already written, leave serving unchanged, retain ownership                                                                      |
| No completion; deploy or rollback     | Revoke permits, stop application/jobs, establish maintenance, record failure or uncertainty, retain ownership                                                    |
| Missing, corrupt, or mismatched state | Refuse release; require attended recovery; never act on another operation                                                                                        |

Cleanup marks `done` only after verification. Failure retains ownership and
records `unknown` when possible. A committed deployment with failed cleanup
remains committed with unresolved cleanup.

A separate reconciliation pass releases ownership only after the executor is
inactive or failed, its systemd job is empty, its cgroup and invocation are
gone, `ExecStopPost` has finished, and cleanup is done. It then marks the
journal complete. Every admission path repeats these lifecycle checks under the
locks.

An exact fence completion repairs a journal interrupted during commit without
rerunning deployment. After reboot, incomplete operations cannot start serving
units without current-boot, live-invocation permits naming operation, unit, and
digest. Committed operations may start only settled digests under existing
guards; ownership still blocks admission until reconciliation finishes.

## Ansible scope and compatibility

Install `ansible-core` in a root-owned Python 3.12 virtual environment at
`/opt/aboutme/ansible/<lock-hash>/`. Select a supported stable version and pin
every dependency version/hash during the toolchain change; pins remain
unverified offline. Pin `ansible-lint` separately. Use only `ansible.builtin`,
with no external collections, fact cache, runtime pip installation, or Galaxy
downloads.

Initially converge unchanged enrolled configuration. Move one ordinary
responsibility at a time from `host/install.sh`, removing its former writer.
Keep the installer as an attended entry point into shared roles; never run it
inside an owned deployment. The managed manifest records path, owner, mode,
content hash, and handler. Preserve local flags, secrets, and operation records;
delete only explicitly retired managed paths. Unchanged convergence causes no
changes or restarts.

Preserve package fingerprints, pins and holds, PostgreSQL reload behavior,
pgBackRest, CrowdSec admission, Quadlet templates, firewall, sysctl, tmpfiles,
journals, alerts, and timers. Handlers cannot bypass startup gates. Control
upgrades and one-time privacy/storage transitions remain attended.

Use `deploy/vn/ansible/` for roles, inventory, locks, and manifest;
`deploy/vn/pull/` for schemas, policy, and controls; and `deploy/vn/tests/` for
fixtures/failure tests. Cap the controller process tree at 256 MiB with one fork
and prove combined host memory fit before activation. Older application clients
follow existing release/schema compatibility rules; older administrative clients
receive no bypass.
