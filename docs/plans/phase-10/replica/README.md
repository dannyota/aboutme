# Task 10.18 implementation

The accepted [scaling contract](../../../design/scaling/README.md) and
[ADR 0035](../../../adr/0035-replica-coordination-and-uat-lifecycle.md) own the
design. Implementation and proof remain pending.

| Plan                                            | Scope                                             |
| ----------------------------------------------- | ------------------------------------------------- |
| [Role bootstrap](role-bootstrap.md)             | First bounded database foundation slice           |
| [Write foundation](write-foundation.md)         | SQL primitives, private runner and migrator order |
| [Runtime tasks](runtime-tasks.md)               | Dependency order, owned paths and narrow checks   |
| [Infrastructure tasks](infrastructure-tasks.md) | Public/private ownership, local and hosted proof  |

The integration owner assigns exclusive paths before dispatch. Designers do not
implement their own slices. The same fresh phase reviewer confirms fixes and
reviews the integrated phase under ADR 0024.

Local proof precedes dependent infrastructure wiring under
[Task 10.18](../task-18-replica-safety-and-scaling-contract.md).
