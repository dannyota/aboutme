# Replica runtime implementation

The accepted [scaling contract](../../../design/scaling/README.md) and
[ADR 0035](../../../adr/0035-replica-coordination-and-uat-lifecycle.md) own the
design.
[ADR 0036](../../../adr/0036-single-replica-launch-and-pipeline-migrations.md)
narrows the first release to one replica. The
[scaling index](../../../design/scaling/README.md) records what is built.

| Plan                                    | Scope                                             |
| --------------------------------------- | ------------------------------------------------- |
| [Role bootstrap](role-bootstrap.md)     | First bounded database foundation slice           |
| [Write foundation](write-foundation.md) | SQL primitives, private runner and migrator order |
| [Runtime tasks](runtime-tasks.md)       | Dependency order, owned paths and narrow checks   |

The integration owner assigns exclusive paths before dispatch. Designers do not
implement their own slices. The same fresh phase reviewer confirms fixes and
reviews the integrated phase under ADR 0024.

The [single-replica direction](single-replica-direction.md) plan closes this
work for the first release.
