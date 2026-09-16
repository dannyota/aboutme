# Replica scaling and UAT lifecycle

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md), narrowed
by [ADR 0036](../../adr/0036-single-replica-launch-and-pipeline-migrations.md).
These pages define what a second replica needs. The first release runs one
replica, so only part of this contract is built. The State column is the single
record of what exists:

- **Built:** migrations 13–23 and their store transports implement it.
- **Deferred:** ADR 0036 postpones it until a second replica is wanted.

These pages name code areas by their implementation slice:

| Label | Code area                                                          |
| ----- | ------------------------------------------------------------------ |
| B1–B3 | Write barrier, live runner proof, and the protected migrator       |
| R1    | Runtime coordination schema and store transports                   |
| R1a   | Central write transaction entry                                    |
| R2    | Distributed `publicstate` coordinator                              |
| R3    | Mutation and deletion callers                                      |
| R4    | Fleet render claims and origin affinity                            |
| R5    | Distributed rate and claim packages                                |
| R6    | Realtime fleet admission and drain                                 |
| R7    | Policy callers: R7b for OAuth, R7d for maintenance commands        |
| R8    | Server composition, readiness, shutdown, and the resource envelope |

| Contract                                                  | Scope                                                                    | State                                     |
| --------------------------------------------------------- | ------------------------------------------------------------------------ | ----------------------------------------- |
| [Runtime schema](runtime-schema.md)                       | Durable state, immutable outcomes and database privileges                | Built                                     |
| [Replica coordination](runtime-coordination.md)           | Joining, transitions, recovery, draining and connection limits           | Deferred                                  |
| [Replica membership](replica-membership.md)               | Exact node identity, task trios, leave and EC2 proof                     | Built                                     |
| [Membership evidence](membership-evidence.md)             | Historical leave and proof records, atomic claim reclamation             | Built                                     |
| [Evidence transport](membership-evidence-transport.md)    | Fixed SQL and Go leave/proof interfaces                                  | Built                                     |
| [Membership cases](membership-cases.md)                   | Required identity, lifecycle, proof and concurrency cases                | Built                                     |
| [Lifecycle operations](lifecycle-operations.md)           | Serving capacity, replacement and private maintenance wake               | Built; wake deferred                      |
| [Ordinary lifecycle contract](lifecycle-controller.md)    | Fixed controller functions, partition changes and historical results     | Built                                     |
| [Lifecycle replay](lifecycle-replay.md)                   | Immutable operation results and permitted action order                   | Built                                     |
| [Lifecycle digest vectors](lifecycle-vectors.md)          | Exact typed field order and literal SQL/Go replay hashes                 | Built                                     |
| [Lifecycle write entry](lifecycle-write-entry.md)         | Fixed exclusive wake methods, markers and completion                     | Deferred                                  |
| [Wake operations](wake-operations.md)                     | Fresh wake state, historical results and scalar transport                | Deferred                                  |
| [Wake migrations](wake-migrations.md)                     | Protected migration bound to the recorded closing-gate wake              | Deferred                                  |
| [Public transitions](public-transitions.md)               | Target digest, replica acknowledgements and durable recovery             | Schema built; operations deferred         |
| [Transition recovery](transition-recovery.md)             | Atomic outcome evidence and parent-locked rollback                       | Deferred                                  |
| [Transition reconciliation](transition-reconciliation.md) | One snapshot, local apply order and durable retirement                   | Deferred                                  |
| [Transition storage](transition-storage.md)               | Four-table constraints, complete results and immutable evidence          | Built                                     |
| [Transition commit](transition-commit.md)                 | Business execution fence and fenced-initiator rollback                   | Deferred                                  |
| [Transition functions](transition-functions.md)           | Exact SQL declarations, result presence and database time                | Deferred                                  |
| [Transition operations](transition-operations.md)         | Entry, roles, lock order and terminal replay                             | Deferred                                  |
| [Transition transport](transition-transport.md)           | Typed Go values, commit callback and physical connection cleanup         | Deferred                                  |
| [Admission](admission.md)                                 | Fleet rate policies, heavy work, render affinity and realtime            | Database built; callers deferred          |
| [Rate storage](rate-storage.md)                           | Exact state, clock arithmetic, pending debt and bounded cleanup          | Built                                     |
| [Rate operations](rate-operations.md)                     | Fixed results, historical replay, roles, query transport and clock tests | Built                                     |
| [Rate identities](rate-identities.md)                     | Canonical key frames, pinned versions and rotation preconditions         | Database built; adapters deferred         |
| [Claim operations](claim-operations.md)                   | Exact results, role checks, errors and query transport                   | Built                                     |
| [Shared claims](shared-claims.md)                         | Fleet concurrency claims and their callers                               | Database built; callers deferred          |
| [Claim identities](claim-identities.md)                   | Scope digests and ambiguous outcomes                                     | Database built; encoding deferred         |
| [Admission attempts](admission-attempts.md)               | OAuth failed-grant reservations                                          | Database built; callers deferred          |
| [Maintenance entry](maintenance-entry.md)                 | One backend per maintenance command                                      | Deferred                                  |
| [Migrator retirement](migrator-retirement.md)             | Physical closure of migration connections                                | Built                                     |
| [Policy catalog](policy-catalog.md)                       | Exact limiter identities, scopes and existing caller behavior            | Describes current limiters and the target |
| [UAT lifecycle](uat-lifecycle.md)                         | Maintenance deadlines, write barriers and final stop receipts            | Deferred                                  |
| [Transaction entry](transaction-entry.md)                 | Shared barrier before any row lock or write                              | Built                                     |
| [Migrator](migrator.md)                                   | Dedicated backend, protected history and read-only status                | Built                                     |
| [Migration provisioning](migration-provisioning.md)       | Fixed grants and data-preserving ownership adoption                      | Built; hosted provisioning pending        |
| [Topology](topology.md)                                   | Complete replicas, paired routes and edge trust                          | Deferred                                  |
| [Controller](controller.md)                               | Off-environment ownership, recovery, tasks and infrastructure changes    | Deferred                                  |

PostgreSQL owns shared application coordination. Public HTTP and SSE schemas
stay unchanged. Raising the service above one replica requires the deferred rows
first.
