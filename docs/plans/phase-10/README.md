# Phase 10 — Production deployment

Status: **In progress** (2026-09-16). Nothing is deployed.

**Goal:** deploy web v1 to `https://aboutme.vn` on one AWS Singapore host and
test the product there, under
[ADR 0037](../../adr/0037-single-host-production-without-hosted-uat.md). There
is no hosted UAT environment.

**Authority:** the
[single-host production design](../../design/single-host-production.md), the
[deployment design](../../design/deployment.md), and
[ADR 0036](../../adr/0036-single-replica-launch-and-pipeline-migrations.md) for
the one-replica runtime.

## Work

| Plan                                                            | Scope                                                         |
| --------------------------------------------------------------- | ------------------------------------------------------------- |
| [Replica runtime](replica/README.md)                            | Migrations 13–23 and their transports; closing checks pending |
| [Single-replica direction](replica/single-replica-direction.md) | Final verification and review of the replica work             |
| Single-host production plan                                     | To be written: code changes, infrastructure, deploy, launch   |

The integration owner owns Git, shared configuration, deploys, and phase
closure. Follow ADR 0024: one author per task, one fresh phase review. Run local
heavy checks one at a time.

## Candidate and verification

Run local `make ci`, connected `make scan`, and the fresh review at one
candidate commit before the first deploy. Commit
`apps/server/migrations/.uat-baseline` before the first production migration;
never rewrite migration history afterward. Complete the
[exit checklist](exit-criteria.md) before closing the phase.
