# Replica scaling and UAT lifecycle

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). These
pages define the target. Runtime implementation and local two-replica checks
precede infrastructure wiring; hosted proof remains a Phase 10 gate.

| Contract                                            | Scope                                                                 |
| --------------------------------------------------- | --------------------------------------------------------------------- |
| [Runtime schema](runtime-schema.md)                 | Durable state, immutable outcomes and database privileges             |
| [Replica coordination](runtime-coordination.md)     | Joining, transitions, recovery, draining and connection limits        |
| [Admission](admission.md)                           | Fleet rate policies, heavy work, render affinity and realtime         |
| [Policy catalog](policy-catalog.md)                 | Exact limiter identities, scopes and existing caller behavior         |
| [UAT lifecycle](uat-lifecycle.md)                   | Maintenance deadlines, write barriers and final stop receipts         |
| [Transaction entry](transaction-entry.md)           | Shared barrier before any row lock or write                           |
| [Migrator](migrator.md)                             | Dedicated backend, protected history and read-only status             |
| [Migration provisioning](migration-provisioning.md) | Fixed grants and data-preserving ownership adoption                   |
| [Topology](topology.md)                             | Complete replicas, paired routes and edge trust                       |
| [Controller](controller.md)                         | Off-environment ownership, recovery, tasks and infrastructure changes |

Production remains one to two EC2 application replicas with fixed RDS compute.
PostgreSQL owns shared application coordination. Public HTTP and SSE schemas
stay unchanged. UAT shutdown preserves privacy deadlines and the USD 30 ceiling.
Production activation remains a separate Phase 11 approval.
