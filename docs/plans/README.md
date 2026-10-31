# Delivery plans

Plans hold open work only. A plan is deleted when its work ships; Git keeps it. Code, [architecture](../architecture.md), and the traceability rows record what shipped. Design wins over a plan; numeric limits live in [budgets](../design/budgets.md).

## Files

|Path|Holds|
|-|-|
|[v0.8-roadmap.md](v0.8-roadmap.md)|Active release order|
|[backlog.md](backlog.md)|Open follow-ups and launch gates|
|[crowdsec-postgresql.md](crowdsec-postgresql.md)|CrowdSec PostgreSQL storage, bounded probe paths, retention, and ordinary deployment|
|[restore-drill.md](restore-drill.md)|Isolated backup restore drill and its remaining safety proofs|
|[traceability/](traceability/README.md)|Acceptance-criterion ownership and evidence|

A multi-release goal gets one plan file; its task briefs go in a directory of the same name while work is open. Delete both when the work ships, after moving any still-open item to `backlog.md`.

Shipped history lives in GitHub Releases and git history.

## Remaining

- [v0.8-roadmap.md](v0.8-roadmap.md): agent access and editor/PDF parity.
- [backlog.md](backlog.md): open follow-ups, Vietnam production work, acceptance evidence, and launch gates.
- Flutter app: deferred beyond web v1 (AC-API-002).

## Gates

[ADR 0024](../adr/0024-delivery-gates.md) governs: failing test first, independent review under [workflow](../../instructions/workflow.md), green GitHub CI on the exact commit before tag and deploy. Authentication, sessions, CSRF, concurrency, idempotency, migrations, sanitizing, publish revocation, secrets, and production deploys are high risk; the code reviewer confirms those invariants by name.
