# 0027: Vietnam-hosted production

Status: Accepted (2026-09-24) for the provider choice, by the human owner's
direction; the design choices in the
[Vietnam production design](../design/vietnam-production.md) were approved on
2026-10-10 and amended on 2026-10-11. Production runs on the Vietnam host.

## Context

At the provider decision, production ran on one AWS host in Singapore (ADR
0025), with RDS, S3, SES, and a Google Workspace mailbox. Account, resume,
photo, and sign-in data were stored abroad.

Vietnam's Personal Data Protection Law 91/2025/QH15, Article 20, governs
transfer of personal data abroad. Decree 356/2025/ND-CP, Article 17(1)(a),
treats storing personal data on a foreign provider's cloud as such a transfer.
Hosting in Vietnam removes that transfer for the core service. The owner files
no data protection impact assessment (DPIA) or cross-border dossier
(2026-10-10): the processors are GreenNode and Bizfly, both in Vietnam.

## Decision

Production uses providers that store and process data in Vietnam:

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
  keeps the logs and a host timer mails alarms. A GreenNode vMonitor synthetic
  check probes `/readyz` from outside the host and mails the owner; the GitHub
  uptime workflow remains a slower backstop.

The AWS production stack and its state bootstrap are removed. Nothing from
`deploy/aws/` is deployed. The code stays for a possible return to AWS ECS or
move to Kubernetes. The
[Vietnam production design](../design/vietnam-production.md) states the running
topology and recovery rules.

These stay outside the move:

- Have I Been Pwned receives only a five-character hash prefix, which does not
  identify a person.
- Google and LinkedIn sign-in stay on. The person signs in at the provider;
  aboutme sends only the one-time authorization code and its own client
  credentials, and receives the name, email, and account ID. No stored personal
  data leaves Vietnam.
- MCP clients that a user connects may run abroad. The user starts that
  transfer, and the privacy notice says so.
- Public DNS uses Cloudflare on the Free plan with DNSSEC and DNS-only address
  records to the host. The owner adds Cloudflare's DS record at the `.vn`
  registrar. P.A Vietnam stays the registrar.
- The Google Workspace support mailbox stays. A person who writes to support
  sends their own mail; the application never sends account data to it.
- GitHub holds code and public images with no personal data.

These stand through the move: the single-host shape, production without hosted
UAT until about 500 users, laptop-run deploys by digest, public infrastructure
code without secrets, GitHub without cloud credentials other than the vMonitor
quota-renewal service account (History, 2026-10-14), one serving replica and
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
- Caddy provides TLS, Coraza, and CrowdSec. Cloudflare provides authoritative
  DNS with proxying off. The OpenTofu root `deploy/vn/dns/` owns the records;
  DNSSEC signing and the registrar DS record stay manual.
- Cutover needs a maintenance window. Moving data back after Vietnam accepts
  writes would be a new transfer abroad, so recovery after that is forward only.
- No AWS test environment remains deployed.

## Amendment: 2026-10-11

Cloudflare became authoritative for `aboutme.vn` on 2026-10-10 at 20:56 UTC, on
the Free plan. Every record is DNS-only. Apex and `www` A records point to the
host's floating IP with TTL 300. Cloudflare signs with DNSSEC algorithm 13; the
owner adds its DS record at the `.vn` registrar. This replaces the Route 53 DNS
choice. The zone, its key-signing key, and its KMS key were deleted on
2026-10-11. The GitHub uptime workflow replaces the Route 53 health check.

Production cut over on 2026-10-11 with release v0.7.3. The AWS production stack
and state bootstrap were destroyed that day. RDS was deleted without a final
snapshot. Production data lives only on the Vietnam host and its pgBackRest
backups in GreenNode vStorage. The `aboutme-email` CloudFormation stack was also
deleted, including its SES identity, configuration set, feedback queue, and
alarms. Keeping AWS as a fictional-data test environment is superseded by
removal of every deployed AWS resource.

## History

Former ADR 0051 (2026-09-24), unchanged in substance. Later records it did not
foresee: former ADR 0054 put CloudFront in front of AWS production as an interim
edge, and former ADR 0056 moved DNS to Route 53, making the move to P.A Vietnam
DNS a second name-server move. 2026-10-09: the owner chose vWAF from launch,
Cloudflare DNS-only, and the Google Workspace mailbox over Bizfly Business
Email. 2026-10-10: the owner dropped vCDN, then vWAF, for host-level controls
(Coraza, CrowdSec) with vWAF as the escalation, so DNS stays at Route 53.

2026-10-11: Cloudflare DNS-only replaces Route 53; the AWS production stack,
state bootstrap, and email stack are removed after the Vietnam cutover. AWS code
remains undeployed for a possible AWS ECS or Kubernetes path.

2026-10-13: the owner moved the Cloudflare DNS records from hand edits to a
separate OpenTofu root, `deploy/vn/dns/`, so host plans need no Cloudflare
token. DNSSEC signing and the registrar DS record stay manual.

2026-10-14: GitHub's scheduled uptime runs arrived 3 to 10 hours apart, so the
owner chose a GreenNode vMonitor synthetic check with free email notification as
the site-down alarm. The free check and email quotas need monthly renewal.

2026-10-14: the owner amended the GitHub-without-cloud-credentials rule for one
credential: a GreenNode service account limited to reading and renewing the free
vMonitor Synthetic Test and Email Notification quotas, stored in a GitHub
environment restricted to `main` and used by a scheduled workflow, because the
free quotas cannot auto-renew and a lapse stops the site-down alarm. The
[quota renewal design](../design/vmonitor-quota-renewal.md) states the controls.
