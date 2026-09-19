# Phase 10: Production deployment

Status: **In progress** (2026-09-20). Production serves v0.3.30. Product and
operational acceptance remain open.

**Goal:** complete production acceptance for web v1 at `https://aboutme.vn` on
one AWS Singapore host, under
[ADR 0037](../../adr/0037-single-host-production-without-hosted-uat.md). There
is no hosted UAT environment.

**Authority:** the
[single-host production design](../../design/single-host-production.md), the
[deployment design](../../design/deployment.md), and
[ADR 0036](../../adr/0036-single-replica-launch-and-pipeline-migrations.md) for
the one-replica runtime.

## Work

| Plan                                           | Scope                                                   |
| ---------------------------------------------- | ------------------------------------------------------- |
| [Single-host production](production/README.md) | Code changes, infrastructure, deploy, and launch checks |

The integration owner owns Git, shared configuration, deploys, and phase
closure. Follow ADR 0024: one author per task, one fresh phase review. Run local
heavy checks one at a time.

## Candidate and verification

The migration baseline is committed. Complete the
[exit checklist](exit-criteria.md), including the remaining candidate evidence,
before closing the phase.
