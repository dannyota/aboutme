# Delivery plans

Plans hold open work only. A plan is deleted when its work ships; Git keeps it. Code, [architecture](../architecture.md), and the traceability rows record what shipped. Design wins over a plan; numeric limits live in [budgets](../design/budgets.md).

## Files

|Path|Holds|
|-|-|
|[v0.5-roadmap.md](v0.5-roadmap.md)|Active release order|
|[backlog.md](backlog.md)|Open follow-ups and launch gates|
|[restore-drill.md](restore-drill.md)|Isolated backup restore drill and its remaining safety proofs|
|[traceability/](traceability/README.md)|Acceptance-criterion ownership and evidence|

A multi-release goal gets one plan file; its task briefs go in a directory of the same name while work is open. Delete both when the work ships, after moving any still-open item to `backlog.md`.

## Shipped

Production runs on one GreenNode host in Vietnam ([ADR 0027](../adr/0027-vietnam-hosted-production.md)). Release verification follows the [production runbook](../runbooks/production.md).

|Tag|Shipped|
|-|-|
|v0.4.0|Vietnamese resume list, creation, editor, publish, and PDF controls|
|v0.4.1|Vietnamese settings and agent consent|
|v0.4.2|Optional passkey second factor with recovery codes and the release fence|
|v0.4.3|Public PDF download as a button|
|v0.4.4|MCP owner workflow runner on the official Go SDK|
|v0.4.5|MCP accepts the public host behind the reverse proxy|
|v0.4.6|Editor phone fixes: contact field width, one sheet per preview page|
|v0.4.7|Optional authenticator-app (TOTP) second factor; release fence 4007|
|v0.5.0|Owner homepage sample|
|v0.5.1|Homepage sample fits one A4 sheet|
|v0.5.2|Editor PDF and Web preview modes; gallery sample PDF page images and their CI check|
|v0.5.3|Preview-to-PDF gap check; zoomed preview re-pagination fix; homepage sample fit|
|v0.5.4|List spacing kept across a split entry; preview-gap ceilings tightened; CI proofs in parallel shards|
|v0.6.25|MCP connection guide in Vietnamese and English; guarded public-root checks before deployment|

## Remaining

- The open rows of [v0.5-roadmap.md](v0.5-roadmap.md) remain open. Mobile display fixes stay in 0.6.x.
- [backlog.md](backlog.md): open follow-ups, Vietnam production work, acceptance evidence, and launch gates.
- Flutter app: deferred beyond web v1 (AC-API-002).

## Gates

[ADR 0024](../adr/0024-delivery-gates.md) governs: failing test first, independent review under [workflow](../../instructions/workflow.md), green GitHub CI on the exact commit before tag and deploy. Authentication, sessions, CSRF, concurrency, idempotency, migrations, sanitizing, publish revocation, secrets, and production deploys are high risk; the code reviewer confirms those invariants by name.
