# 0037 — Single-host production without hosted UAT

Status: Accepted (2026-09-16), by the human owner's direction.

## Context

[ADR 0031](0031-aws-cost-research-and-hosted-uat.md) placed a hosted UAT
environment at `uat.aboutme.vn` before production.
[ADR 0033](0033-public-image-builds-private-deployment.md) split deployment into
a private `aboutme-infra` repository.
[ADR 0034](0034-scheduled-uat-and-production-autoscaling.md) chose ECS on EC2
behind CloudFront and an Application Load Balancer, with scheduled UAT.
[ADR 0036](0036-single-replica-launch-and-pipeline-migrations.md) then reduced
the first release to one serving replica.

None of that infrastructure exists yet. The product is a personal project with
no users. A second environment, a load balancer, a CDN distribution, a second
repository and a cross-repository image handoff add work and monthly cost before
anyone can use the site, and they protect users who do not exist yet.

## Decision

The first release deploys straight to production at `https://aboutme.vn` and is
tested there. There is no hosted UAT environment for this release. A separate
UAT environment returns when the product has real users, at roughly 500.

Production runs on one `t4g.small` EC2 instance in `ap-southeast-1` with the
Bottlerocket ECS image, one `db.t4g.micro` RDS PostgreSQL instance, and the
private S3 media bucket. Cloudflare proxies the site. There is no ALB and no
CloudFront distribution.

Images build on public GitHub Actions `ubuntu-24.04-arm` runners and publish to
public GitHub Container Registry packages. The host pulls them by digest.

OpenTofu modules live in this public repository under `deploy/aws/`. State,
account identifiers and environment values stay out of Git. The owner applies
infrastructure and runs deploys from the laptop. GitHub holds no cloud
credentials.

The [single-host production design](../design/single-host-production.md) states
the resulting rules.

## Consequences

- Monthly cost falls to about $45–55 from the $140–170 production estimate.
- Deploys and monthly OS updates interrupt service for a few minutes.
- A host failure takes the site down until EC2 auto-recovery or a manual
  replacement completes.
- Cloudflare terminates TLS and can read all traffic, including passwords and
  resume content. The privacy documentation must say so.
- Test accounts and real accounts share one database.
- Infrastructure code is public. It must contain no account identifier, secret
  or private hostname, and CI never receives cloud credentials.
- Small code changes become launch prerequisites. The single-host design lists
  them.

- Accepting this record updates the deployment design, the roadmap and the Phase
  10 plans in the same change, so no document still describes hosted UAT or the
  fleet topology as the release path.

## Compatibility and review

This record supersedes, for the first release only:

- ADR 0031: hosted UAT before production and the separate Phase 11 approval.
  This record is the production approval.
- ADR 0033: the private `aboutme-infra` repository and private image
  publication. Public ARM64 image builds stand.
- ADR 0034: CloudFront, the ALB, scheduled UAT and the fleet topology. The
  Singapore region, RDS, private S3 and EC2 Graviton choices stand.

ADR 0035's coordination contracts and ADR 0036's single-replica rules stand.
Public HTTP and SSE contracts, privacy deadlines, private media authorization
and every proved database invariant are unchanged.
