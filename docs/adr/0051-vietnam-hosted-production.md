# 0051: Vietnam-hosted production

Status: Accepted (2026-09-24) for the provider choice, by the human owner's
direction. The design choices marked for owner approval in the
[Vietnam production design](../design/vietnam-production.md) stay open until the
owner approves them.

## Context

Production runs on one AWS host in Singapore behind Cloudflare, with RDS, S3,
SES, and a Google Workspace mailbox
([ADR 0037](0037-single-host-production-without-hosted-uat.md)). Every user's
account, resumes, photos, and sign-in data are stored abroad.

Vietnam's Personal Data Protection Law 91/2025/QH15, Article 20, governs
transfer of personal data abroad. Decree 356/2025/ND-CP, Article 17(1)(a),
treats storing personal data on a foreign provider's cloud as such a transfer.
Hosting in Vietnam removes that transfer for the core service. A data protection
impact assessment (DPIA) stays required either way.

## Decision

Production moves to providers that store and process data in Vietnam:

- GreenNode (VNG Cloud) for compute (vServer), object storage (vStorage), CDN
  (vCDN, all PoPs in Vietnam), DNS (vDNS), and monitoring (vMonitor).
- Bizfly Email Transaction for authentication mail, and Bizfly Business Email
  for the support mailbox.
- Google sign-in is off in production (`PROVIDER_LOGIN_ENABLED=false`), because
  it sends identity data to Google.
- PostgreSQL 18 runs self-hosted on the vServer with pgBackRest to vStorage,
  because the managed database offers at most PostgreSQL 17 and no point-in-time
  recovery.
- The release fence, secrets, jobs, logs, and alarms move to the host and
  vMonitor.

The current AWS stack stays as a test environment that holds fictional data
only, with no domain of its own. Once the cutover is verified, every copy of
real data in AWS, Cloudflare, and Google Workspace is deleted. The
[Vietnam production design](../design/vietnam-production.md) states the rules
and the migration order.

These stay outside the move:

- Have I Been Pwned receives only a five-character hash prefix, which does not
  identify a person. The DPIA records it.
- MCP clients that a user connects may run abroad. The user starts that
  transfer, and the privacy notice says so.
- GitHub holds code and public images with no personal data.

## Consequences

- Personal data at rest, in backups, in logs, and at the edge stays in Vietnam.
- The team now runs PostgreSQL: upgrades, backups, point-in-time recovery, and
  quarterly restore drills. A host or volume loss means a restore from vStorage.
- There is no instance role or parameter store. Static keys and secret files on
  the host replace them, so host compromise exposes every runtime secret, as it
  would on AWS through the task roles.
- GreenNode lacks OpenTofu coverage for buckets, vCDN, vDNS, and vMonitor. Those
  settings live in scripts and runbook steps.
- vCDN has no authenticated origin pulls. An IP allowlist and a secret header
  replace them.
- Release images must also build for `linux/amd64`.
- Accounts that sign in only with Google lose sign-in unless they add a password
  before cutover or password reset can create one.
- Cutover needs a maintenance window. Moving data back after Vietnam accepts
  writes would be a new transfer abroad, so recovery after that is forward only.
- AWS test costs continue beside the new production costs.

## Compatibility and review

This record supersedes the host parts of ADR 0037: the AWS Singapore EC2
instance, RDS, S3, Cloudflare in front of production, ARM64-only images, and the
`deploy/aws/` production root. ADR 0037's single-host shape, production without
hosted UAT until about 500 users, laptop-run deploys by digest, public
infrastructure code without secrets, and GitHub without cloud credentials stand.

[ADR 0036](0036-single-replica-launch-and-pipeline-migrations.md) and
[ADR 0038](0038-single-baseline-and-plain-migrator.md) stand. The fence rules of
[ADR 0048](0048-passkey-second-factor-authentication.md) and
[ADR 0049](0049-totp-second-factor-authentication.md) stand with a new store.
[ADR 0022](0022-public-artifact-revocation.md) stands: vCDN caches only hashed
assets. Public HTTP, SSE, and MCP contracts, privacy deadlines, private media
authorization, and every data invariant are unchanged.
