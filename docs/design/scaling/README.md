# Replica scaling and UAT lifecycle

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). These
pages define the target. Runtime implementation and local two-replica checks
precede infrastructure wiring; hosted proof remains a Phase 10 gate.

| Contract                                                  | Scope                                                                    |
| --------------------------------------------------------- | ------------------------------------------------------------------------ |
| [Runtime schema](runtime-schema.md)                       | Durable state, immutable outcomes and database privileges                |
| [Replica coordination](runtime-coordination.md)           | Joining, transitions, recovery, draining and connection limits           |
| [Replica membership](replica-membership.md)               | Exact node identity, task trios, leave and EC2 proof                     |
| [Membership evidence](membership-evidence.md)             | Historical leave and proof records, atomic claim reclamation             |
| [Evidence transport](membership-evidence-transport.md)    | Fixed SQL and Go leave/proof interfaces                                  |
| [Membership cases](membership-cases.md)                   | Required identity, lifecycle, proof and concurrency cases                |
| [Lifecycle operations](lifecycle-operations.md)           | Serving capacity, replacement and private maintenance wake               |
| [Ordinary lifecycle contract](lifecycle-controller.md)    | Fixed controller functions, partition changes and historical results     |
| [Lifecycle replay](lifecycle-replay.md)                   | Immutable operation results and permitted action order                   |
| [Lifecycle digest vectors](lifecycle-vectors.md)          | Exact typed field order and literal SQL/Go replay hashes                 |
| [Lifecycle write entry](lifecycle-write-entry.md)         | Fixed exclusive wake methods, markers and completion                     |
| [Wake operations](wake-operations.md)                     | Fresh wake state, historical results and scalar transport                |
| [Wake migrations](wake-migrations.md)                     | Protected migration bound to the recorded closing-gate wake              |
| [Public transitions](public-transitions.md)               | Target digest, replica acknowledgements and durable recovery             |
| [Transition recovery](transition-recovery.md)             | Atomic outcome evidence and parent-locked rollback                       |
| [Transition reconciliation](transition-reconciliation.md) | One snapshot, local apply order and durable retirement                   |
| [Transition storage](transition-storage.md)               | Four-table constraints, complete results and immutable evidence          |
| [Transition commit](transition-commit.md)                 | Business execution fence and fenced-initiator rollback                   |
| [Admission](admission.md)                                 | Fleet rate policies, heavy work, render affinity and realtime            |
| [Rate storage](rate-storage.md)                           | Exact state, clock arithmetic, pending debt and bounded cleanup          |
| [Rate operations](rate-operations.md)                     | Fixed results, historical replay, roles, query transport and clock tests |
| [Rate identities](rate-identities.md)                     | Canonical key frames, pinned versions and rotation preconditions         |
| [Claim operations](claim-operations.md)                   | Exact results, role checks, errors and query transport                   |
| [Policy catalog](policy-catalog.md)                       | Exact limiter identities, scopes and existing caller behavior            |
| [UAT lifecycle](uat-lifecycle.md)                         | Maintenance deadlines, write barriers and final stop receipts            |
| [Transaction entry](transaction-entry.md)                 | Shared barrier before any row lock or write                              |
| [Migrator](migrator.md)                                   | Dedicated backend, protected history and read-only status                |
| [Migration provisioning](migration-provisioning.md)       | Fixed grants and data-preserving ownership adoption                      |
| [Topology](topology.md)                                   | Complete replicas, paired routes and edge trust                          |
| [Controller](controller.md)                               | Off-environment ownership, recovery, tasks and infrastructure changes    |

Production remains one to two EC2 application replicas with fixed RDS compute.
PostgreSQL owns shared application coordination. Public HTTP and SSE schemas
stay unchanged. UAT shutdown preserves privacy deadlines and the USD 30 ceiling.
Production activation remains a separate Phase 11 approval.
