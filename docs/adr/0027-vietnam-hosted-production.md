# 0027: Vietnam-hosted production

Status: Accepted (2026-09-24) for the provider choice, by the human owner's
direction; the design choices in the
[Vietnam production design](../design/vietnam-production.md) were approved on
2026-10-10. Until cutover, ADR 0025 describes what runs.

## Context

Production runs on one AWS host in Singapore (ADR 0025), with RDS, S3, SES, and
a Google Workspace mailbox. Every user's account, resumes, photos, and sign-in
data are stored abroad.

Vietnam's Personal Data Protection Law 91/2025/QH15, Article 20, governs
transfer of personal data abroad. Decree 356/2025/ND-CP, Article 17(1)(a),
treats storing personal data on a foreign provider's cloud as such a transfer.
Hosting in Vietnam removes that transfer for the core service. A data protection
impact assessment (DPIA) stays required either way.

## Decision

Production moves to providers that store and process data in Vietnam:

- GreenNode (VNG Cloud) for compute (vServer) and object storage (vStorage). Its
  vWAF is the escalation if a flood exceeds the host.
- Caddy on the host carries TLS, the Coraza web application firewall with the
  OWASP Core Rule Set, and the CrowdSec bouncer, replacing CloudFront and AWS
  WAF. Nothing stands in front of the host.
- Bizfly Email Transaction for authentication mail.
- PostgreSQL 18 runs self-hosted on the vServer with pgBackRest to vStorage,
  because the managed database offers at most PostgreSQL 17 and no point-in-time
  recovery.
- The release fence, secrets, jobs, logs, and alarms move to the host: journald
  keeps the logs and a host timer mails alarms. The Route 53 health check stays
  as the one check from outside.

The AWS stack stays as a test environment that holds fictional data only, with
no domain of its own. Once the cutover is verified, every copy of real data in
AWS is deleted. The [Vietnam production design](../design/vietnam-production.md)
states the rules and the migration order.

These stay outside the move:

- Have I Been Pwned receives only a five-character hash prefix, which does not
  identify a person. The DPIA records it.
- Google and LinkedIn sign-in stay on. The person signs in at the provider;
  aboutme sends only the one-time authorization code and its own client
  credentials, and receives the name, email, and account ID. No stored personal
  data leaves Vietnam.
- MCP clients that a user connects may run abroad. The user starts that
  transfer, and the privacy notice says so.
- Public DNS stays at Route 53 with DNSSEC and plain address records to the
  host. An authoritative name server answers resolvers and sees no person. P.A
  Vietnam stays the registrar.
- The Google Workspace support mailbox stays. A person who writes to support
  sends their own mail; the application never sends account data to it.
- GitHub holds code and public images with no personal data.

These stand through the move: the single-host shape, production without hosted
UAT until about 500 users, laptop-run deploys by digest, public infrastructure
code without secrets, GitHub without cloud credentials, one serving replica and
deploy-step migrations (ADRs 0005 and 0026), the second-factor fence rules with
a new store (ADR 0017), and the live-state gate (ADR 0010), with no edge cache.
Public HTTP, SSE, and MCP contracts, privacy deadlines, private media
authorization, and every data invariant are unchanged.

## Consequences

- Personal data at rest, in backups, in logs, and at the edge stays in Vietnam.
- The team runs PostgreSQL: upgrades, backups, point-in-time recovery, and
  quarterly restore drills. A host or volume loss means a restore from vStorage.
- There is no instance role or parameter store. Static keys and secret files on
  the host replace them, so host compromise exposes every runtime secret, as it
  would on AWS through the task roles.
- GreenNode's OpenTofu provider covers compute and network only. The `vngcloud`
  CLI covers IAM, storage, and budgets from scripts; the vWAF escalation is a
  root-portal runbook step.
- Caddy terminates TLS and takes every request. A flood larger than the host is
  downtime until vWAF is switched on by a DNS change.
- Release images must also build for `linux/amd64`.
- The cutover replaces CloudFront and AWS WAF with Caddy's own TLS, Coraza, and
  CrowdSec. DNS records change at Route 53; no name server moves.
- Cutover needs a maintenance window. Moving data back after Vietnam accepts
  writes would be a new transfer abroad, so recovery after that is forward only.
- AWS test costs continue beside the new production costs.

## History

Former ADR 0051 (2026-09-24), unchanged in substance. Later records it did not
foresee: former ADR 0054 put CloudFront in front of AWS production as an interim
edge, and former ADR 0056 moved DNS to Route 53, making the move to P.A Vietnam
DNS a second name-server move. 2026-10-09: the owner chose vWAF from launch,
Cloudflare DNS-only, and the Google Workspace mailbox over Bizfly Business
Email. 2026-10-10: the owner dropped vCDN, then vWAF, for host-level controls
(Coraza, CrowdSec) with vWAF as the escalation, so DNS stays at Route 53.
