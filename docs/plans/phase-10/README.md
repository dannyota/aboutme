# Phase 10 — Infrastructure and AWS UAT

Status: **Planned** (2026-09-05). The owner authorized UAT in AWS Singapore and
Cloudflare DNS for `uat.aboutme.vn`. Nothing has been deployed by this phase.

**Goal:** deploy the completed web v1 to `https://uat.aboutme.vn`, prove its
user workflows, and rehearse the operational requirements before production.

Infrastructure uses OpenTofu and the managed AWS services selected in Phase 9.
Deployment images build natively on GitHub Actions `ubuntu-24.04-arm` in the
planned private `aboutme-infra` repository. Development stays on the laptop; the
existing AMD64 browser baseline gate keeps its pinned architecture. The
[build contract](infrastructure/contracts.md#build-and-runner-contract) defines
the ARM64 smoke tests and immutable image handoff.

**Authority:** [ADR 0031](../../adr/0031-aws-cost-research-and-hosted-uat.md),
[deployment](../../design/deployment.md), [budgets](../../design/budgets.md),
[roadmap](../implementation-plan.md), and Phase 9's completed cost
recommendation.

## Sequence and ownership

| Tasks                                             | Work                                                                       | Gate                                                                          |
| ------------------------------------------------- | -------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| [10.1–10.13](infrastructure/README.md)            | Refresh contracts, implement IaC, images, policies, workflows and runbooks | Phase 9 and final Phase 6–8 runtime; local checks before activation           |
| [10.14](harness.md)                               | Hosted browser harness and fixtures                                        | Author/test before 10.15; live preflight after deployment                     |
| [10.15](infrastructure/task-15-uat-activation.md) | Provision and deploy UAT                                                   | Infrastructure local checkpoint, cost limits, and recorded resource inventory |
| [10.16](execution.md)                             | Complete product workflows                                                 | Healthy deployed candidate and SES handoff                                    |
| [10.17](evidence.md)                              | Operational drills, evidence, and closure                                  | Same candidate, workflow results, and provisioned runbooks                    |

The integration owner owns activation, Git, shared configuration, evidence, and
phase closure. Assign implementation paths before dispatch and follow ADR 0024:
one author per task, one fresh phase review. Run local heavy checks one at a
time to stay within laptop RAM limits.

## Deployment scope

- Application and data region: `ap-southeast-1` (Singapore).
- UAT origin: `https://uat.aboutme.vn`; DNS is managed in Cloudflare. The
  existing design uses DNS-only records and CloudFront. Phase 9 must record any
  approved topology change before these tasks are dispatched.
- Record supporting origin/certificate records and any global-service region
  exceptions in the deployment inventory. Keep UAT state, database, media,
  fixtures, and deployment roles separate from production.
- Existing internal `staging` environment names may remain implementation
  details. They refer to this UAT environment, not a second paid deployment.
- Production DNS cutover and launch belong to Phase 11 and need separate
  approval.

## Required contract refresh

Before implementation, reconcile every infrastructure task against the Phase 9
decision and current code. Resolve `PUBLIC_RENDER_ORIGIN`, password/MCP flags,
disabled-provider startup validation, and the current mail runtime settings.
Read the [email runbook](../../runbooks/email.md) and inventory the existing
`aboutme-email` CloudFormation stack before OpenTofu adopts overlapping
resources. It records the Singapore sandbox, `danny@aboutme.vn`, `aboutme-auth`,
missing runtime IAM, and an unconsumed feedback queue. Preserve Google Workspace
DNS. Use simulator smoke separately from real verification/reset tests, which
need approved verified recipients while SES remains in sandbox. Broader
production mail waits for production access. Do not infer feedback processing
from SQS delivery.

Specify the complete edge policy for MCP discovery, `/oauth/*`, `/authorize`,
and `/mcp`, including methods, cookies, Bearer authorization, and no-store. The
old blanket Basic-auth staging gate conflicts with MCP Bearer authorization;
settle a route-aware UAT access policy and tests before implementing it. Noindex
remains required and is not a substitute for access control.

The existing infrastructure tasks retain detailed baseline contracts for review.
They are not dispatchable until these refresh decisions are reflected in the
affected task, design, and traceability rows.

## Candidate and verification

The candidate contains completed Phase 6–8 behavior and locally verified
infrastructure. Commit `apps/server/migrations/.uat-baseline` before the first
hosted database migration; never rewrite that history afterward.

Run local `make ci`, connected `make scan`, and the fresh review before
activation. Then deploy immutable candidate image digests and execute the hosted
checks at that unchanged candidate. A later fix creates a new candidate and
requires affected checks and stale UAT evidence to be rerun. Complete the
[exit checklist](exit-criteria.md) before phase closure and push.

The native HTTPS harness remains the local feature-check tool. No isolated
localhost port-443 stack or host sysctl change is needed for Phase 10.
