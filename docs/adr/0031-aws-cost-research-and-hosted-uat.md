# 0031 — Research AWS cost before hosted UAT

Status: Accepted (2026-09-05), by the human owner's direction.

## Context

The earlier roadmap required an isolated local HTTPS deployment on port 443
before cloud activation. The owner now wants UAT in AWS Singapore at
`uat.aboutme.vn`, with Cloudflare DNS, and a cost study before deployment. The
owner also wants numeric phase names that are easy to remember.

## Decision

- Phase 9 researches AWS cost in Singapore (`ap-southeast-1`). It produces a
  workload model, compares deployment options, and records the recommended
  configuration, UAT lifetime, and spending limits before provisioning.
- OpenTofu is the infrastructure tool. Prefer managed AWS services over
  self-managed equivalents when they meet the workload and cost requirements.
  Phase 9 includes operating effort, service limits, and compatibility in its
  comparison; it does not choose an incompatible service solely for low price.
- Deployment OpenTofu code and environment workflows belong in a separate
  private repository, planned as `aboutme-infra`. The public app keeps source,
  local development tools, and a deployment contract. Its contributors do not
  need the private repo or AWS access to build and test. Repository creation and
  infrastructure implementation wait until the remaining product phases.
- The owner selected native GitHub Actions ARM64 runners on 2026-09-05.
  Deployment images use `ubuntu-24.04-arm` and target `linux/arm64`; development
  and affected checks stay on the laptop. Existing AMD64 browser baselines keep
  their pinned architecture. Phase 10 adds native ARM64 image smoke tests before
  publication. CodeBuild and self-hosted build runners are not required. The
  private infrastructure repository's Actions minutes and storage belong in the
  Phase 9 cost model; public-repository free runner usage does not cover it.
- Phase 10 contains infrastructure preparation, AWS deployment, complete product
  UAT, and the operational rehearsal previously assigned to staging.
  Infrastructure is a workstream within this phase, not a lettered phase.
- Phase 11 is production promotion. Phase 12 is deferred Flutter work. Active
  tasks use numeric identifiers such as 7.1 and 10.14. Completed phase
  identifiers remain in historical evidence; they are not reassigned.
- The owner authorizes AWS UAT resources in `ap-southeast-1` and Cloudflare DNS
  for `uat.aboutme.vn`, including supporting origin and certificate records for
  that environment. Record the resolved resource and DNS inventory before
  applying it. This does not authorize production cutover, unrelated DNS edits,
  or long-term purchase commitments.
- Phase 9 settles cost assumptions and resource sizing before activation. No
  budget amount is implied by the hostname authorization. Present the priced
  recommendation for any unresolved budget decision; do not ask again for the
  already-authorized region and UAT scope.
- Features and affected checks remain local. Full local `make ci`, connected
  `make scan`, infrastructure simulation, and one fresh review precede
  activation at the candidate commit. Separate local port-443 UAT is no longer
  required.
- The owner is configuring AWS SES and will provide documentation. Phase 10
  consumes that handoff, inventories existing mail resources, and implements
  only missing integration. It does not recreate the owner's mail setup. Local
  mail capture remains the feature-development path.
- Production still requires successful Phase 10 evidence and separate owner
  approval. Cost reductions cannot silently weaken authentication, private
  media, revocation, backup, or recovery requirements.

The [email runbook](../runbooks/email.md) now records the SES sandbox setup, the
existing `aboutme-email` CloudFormation stack, and missing runtime IAM. Phase 10
adopts existing resources under one management authority before OpenTofu applies
overlapping changes. It preserves Google Workspace DNS and does not treat the
unconsumed feedback queue as application processing.

Singapore is the application and data region. Required global-service
dependencies, such as CloudFront's ACM certificate in `us-east-1`, must be named
in the resource inventory and cost model. They do not relocate application data.

## Supersession

This decision replaces the local-UAT-before-cloud timing in ADRs 0011, 0024,
and 0025. ADR 0024's author, review, and verification rules remain in force. It
clarifies ADR 0020: the migration baseline marker must land before the first
hosted UAT database migration. Later corrections use forward migrations even if
the UAT environment is subsequently destroyed.

The deployment design remains Phase 9's comparison baseline. Research may
recommend a different topology, but changing accepted architecture or security
boundaries requires a follow-up ADR and matching design and plan updates before
infrastructure implementation.

## Consequences

UAT exercises real DNS, TLS, SES, edge routing, and runtime limits. It incurs
cloud cost, so its lifetime, cleanup, backup retention, and idle charges are
part of Phase 9's recommendation. Local development keeps one capped database
and serial heavy checks; there is no new local UAT stack to keep running.

UAT uses synthetic fixtures and owner-approved test mailboxes. Evidence remains
private and ignored locally; only redacted summaries and stable acceptance
references belong in this public repository.
