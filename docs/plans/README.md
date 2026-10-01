# Delivery plans

Plans hold open work only. A plan is deleted when its work ships; Git keeps it. Code, [architecture](../architecture.md), and the traceability rows record what shipped. Design wins over a plan; numeric limits live in [budgets](../design/budgets.md).

## Files

|Path|Holds|
|-|-|
|[v0.5-roadmap.md](v0.5-roadmap.md)|Active release order|
|[viewer-analytics.md](viewer-analytics.md)|Sign in to view and the join popup (v0.6.26); view counts shipped in v0.6.4|
|[vietnam-production.md](vietnam-production.md)|Move production to GreenNode and Bizfly in Vietnam ([ADR 0027](../adr/0027-vietnam-hosted-production.md))|
|[showcase.md](showcase.md)|Community showcase ([ADR 0029](../adr/0029-community-showcase.md), proposed), waiting for owner approvals|
|[backlog.md](backlog.md)|Open follow-ups and launch gates|
|[traceability/](traceability/README.md)|Acceptance-criterion ownership and evidence|

A multi-release goal gets one plan file; its task briefs go in a directory of the same name while work is open. Delete both when the work ships, after moving any still-open item to `backlog.md`.

## Shipped

Production runs the tag in the `aboutme-prod-app` task definition's `DEPLOY_RELEASE_TAG` on one AWS Singapore host ([ADR 0025](../adr/0025-single-host-production.md)), live since v0.1.1.

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

- Release order, next: [community showcase](showcase.md), which takes its version number when it ships. Sign in to view shipped in v0.6.26 and waits only for its live check; the light/dark theme ships with this change. The open rows of [v0.5-roadmap.md](v0.5-roadmap.md) stay open beside them.
- [Vietnam production migration](vietnam-production.md): provider confirmation, app preparation releases, build, rehearsal, cutover, AWS real-data deletion.
- [backlog.md](backlog.md): the app-page CSP gap, traceability remaps, production acceptance, and launch gates.
- Flutter app: deferred beyond web v1 (AC-API-002).

## Gates

[ADR 0024](../adr/0024-delivery-gates.md) governs: failing test first, one fresh review per plan or release, green GitHub CI on the exact commit before tag and deploy. Authentication, sessions, CSRF, concurrency, idempotency, migrations, sanitizing, publish revocation, secrets, and production deploys are high risk; the reviewer confirms those invariants by name.
