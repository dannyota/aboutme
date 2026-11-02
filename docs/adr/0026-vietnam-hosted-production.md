# 0026: Vietnam-hosted production

Status: Accepted (2026-09-24) for the provider choice, by the human owner's
direction; the design choices in the
[Vietnam production design](../design/vietnam-production.md) were approved on
2026-10-10 and amended on 2026-10-11. The CrowdSec storage and backup amendment
was accepted on 2026-10-18. Production runs on the Vietnam host.

## Decision

Production runs on one host and uses providers that store and process data in
Vietnam:

- GreenNode (VNG Cloud) for compute (vServer) and object storage (vStorage). Its
  vWAF is the escalation if a flood exceeds the host.
- Caddy on the host carries TLS, the Coraza web application firewall with the
  OWASP Core Rule Set, and the CrowdSec bouncer, replacing CloudFront and AWS
  WAF. Nothing stands in front of the host.
- Bizfly Email Transaction for authentication mail.
- PostgreSQL 18 runs self-hosted on the vServer with pgBackRest to vStorage,
  because the managed database offers at most PostgreSQL 17 and no point-in-time
  recovery.
- CrowdSec uses a separate database and least-privilege role in that PostgreSQL
  cluster. Physical backups include its attack records. The owner accepts the
  shared resource and recovery boundary and the longer encrypted-backup
  lifetime, with live retention and fresh CrowdSec state after restore in the
  [CrowdSec privacy contract](../design/crowdsec-http-privacy.md).
- The release fence, secrets, jobs, logs, and alarms move to the host: journald
  keeps the logs and a host timer mails alarms. A GreenNode vMonitor synthetic
  check probes `/readyz` from outside the host. Its `aboutme-readyz` alarm fires
  when one of two locations fails three consecutive one-minute tests. It sends
  In-alarm, Up, and Undetermined notifications to the Telegram channel
  `aboutme-alerts`; the GitHub uptime workflow remains a slower backstop.

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

Production has no hosted UAT until about 500 users. The owner runs deployments
from the laptop by digest. Release images build from the exact reviewed commit,
are public, and carry signed build provenance; public build archives and logs
contain only public inputs and synthetic evidence. Infrastructure code is public
and contains no secrets, account identifiers, or private hostnames. GitHub holds
no cloud credentials other than the vMonitor quota-renewal service account
accepted on 2026-10-14 (see History). Cost reductions must not silently weaken
authentication, private media, revocation, backup, or recovery requirements.

One serving replica and deploy-step migrations follow ADRs 0025 and 0005. The
second-factor fence rules (ADR 0017) use a host store, and the live-state gate
(ADR 0010) applies without an edge cache. Public HTTP, SSE, and MCP contracts
and private media authorization remain in force. The CrowdSec amendment
separates its live-retention deadline from the existing physical-backup
retention policy; other privacy deadlines remain in force.

## Context

At the provider decision, production ran on one AWS host in Singapore
([historical AWS production decision](history/aws-production.md)), with RDS, S3,
SES, and a Google Workspace mailbox. Account, resume, photo, and sign-in data
were stored abroad.

Vietnam's Personal Data Protection Law 91/2025/QH15, Article 20, governs
transfer of personal data abroad. Decree 356/2025/ND-CP, Article 17(1)(a),
treats storing personal data on a foreign provider's cloud as such a transfer.
Hosting in Vietnam removes that transfer for the core service. The owner files
no data protection impact assessment (DPIA) or cross-border dossier
(2026-10-10): the processors are GreenNode and Bizfly, both in Vietnam.

## Consequences

- Personal data at rest, in backups, in logs, and at the edge stays in Vietnam.
- The team runs PostgreSQL: upgrades, backups, point-in-time recovery, and
  quarterly restore drills. A host or volume loss means a restore from vStorage.
- CrowdSec shares the database cluster's failure and backup boundary without
  sharing application tables or grants. Fresh state replaces data migration;
  deployment and restore must satisfy its privacy gate before consumers start.
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

## Amendment: Host alerts through Telegram (2026-11-02)

Status: Proposed. Requires owner acceptance; host SMTP remains in force until
cutover. This does not amend application authentication mail.

Replace the host timer and unit-failure mail sender with one Bot API HTTPS
`sendMessage` request per alert, using existing curl and jq. Preserve metadata
limits and per-key daily suppression. Choose Telegram only, with no automatic
SMTP fallback: one transport is simpler, but blocking or provider failure can
lose host notifications. vMonitor checks remain external; their Telegram
notifications share that delivery risk. The slower GitHub uptime backstop stays.

This proposes an explicit exception to the Vietnam-only consequence above for
the owner's Telegram chat ID (personal data), host source IP, and operational
alert metadata sent to Telegram. It permits no resume, account, or raw log data.
Telegram location/retention and the owner's required transfer obligations remain
unverified; this amendment makes no legal compliance claim. The owner must
accept that boundary before activation.

The
[host alert contract](../design/vietnam-production.md#proposed-host-telegram-alerts)
owns transport, custody, authorization, limits, failures, and verification.
Permitted token storage is the ignored local `.env` key `TELEGRAM_BOT`, the host
secret, and any age-encrypted backup copy. The chat ID may reside in its host
secret and encrypted backup. Imports use pipes only; no plaintext staging.
Telegram receives the token in the HTTPS request URL for authentication,
separately from the bounded alert content. Neither token nor chat ID belongs in
alert text. The owner can revoke the token. The benefit is one alert destination
without a new service or dependency. SMTP fallback offers delivery diversity at
the cost of a second maintained transport; keeping SMTP alone would avoid this
new provider but not meet the requested move. No app contract or stored-data
migration is required. Cutover needs an actual receipt proof; rollback restores
the reviewed mail sender and verifies receipt without reverting unrelated host
safety changes.

### Proposed amendment history

2026-11-02: Proposed replacing host alert email and accepting the bounded
Telegram transfer because the owner requested the same destination as vMonitor.
Token custody names the approved local source, host secrets, and encrypted
backups; authentication transfer is distinct from alert content. Acceptance and
production cutover are not recorded.

## Amendment: Anonymous-approval pull deployment (2026-11-01)

Status: Accepted with conditions (2026-11-01), by the owner after the
[live approval probe](../research/approval-probe.md). Implementation remains
pending in the
[design's release order](../design/production-pull-deploy/README.md#release-order-and-owner-decisions).

Production supports host-initiated deployment through `ansible-pull` at an exact
signed coordinator commit, using a request explicitly approved by either
maintainer. Anonymous GitHub reads supply approval evidence; the host holds no
GitHub credential. Solo approval by the owner is an accepted supplied
requirement, so environment self-review remains allowed. The host independently
verifies approval, exact-main CI, publication evidence, image identity, release
ordering, and compatibility.

Acceptance requires a wait of at least 60 seconds after observing a terminal run
state before trusting anonymous reads, because the probe observed
`cache-control: public, max-age=60`. Refuse a run whose review history spans
more than one page or contains more than one relevant review. The
[host evidence contract](../design/production-pull-deploy/evidence.md#anonymous-approval-and-freshness)
owns revalidation and the combined cache/freshness bound. Multi-page history and
approval by a second maintainer remain unverified; neither is required to start
implementation under these conditions.

GitHub holds no deployment credentials and has no administrative network route
to production. Laptop deployment remains available in parallel and shares the
durable host protocol. External deployment proofs remain attended initially.
Ownership survives interruption; uncertain operations require attended recovery.
Local configuration changes invalidate pending requests before deployment
resumes.

One serving replica, deployment downtime, privacy gates, backup custody, and
explicit data-loss rules remain unchanged. This acceptance introduces no
monitoring host and does not change the separate Synthetic quota-renewal
amendment below or its credential exception.

## Amendment: Synthetic quota renewal in GitHub Actions (2026-11-01)

Status: Accepted (2026-11-01), under the owner's standing approval for reviewed
solutions and request to renew the free Synthetic quota through GitHub Actions.
The dispatch workflow is live: `check` passed on 2026-11-01 and hold issue
[#146](https://github.com/dannyota/aboutme/issues/146) is closed. First due
`renew` verification remains pending after 2026-11-28.

The renew-only GreenNode service account `aboutme-vmonitor-renew` is the one
approved cloud-credential exception in CI. Its client ID, client secret, and
project ID live only in environment `vmonitor-renew`, as three secrets:
`VNGCLOUD_CLIENT_ID`, `VNGCLOUD_CLIENT_SECRET`, and `VNGCLOUD_PROJECT_ID`.
Secret mapping masks the project identifier before the runner prints step
environment values. The environment admits the selected branch `main` only, with
no tag rule. No repository or organization secret duplicates are allowed. GitHub
receives no host, database, storage, deployment, or general cloud administration
credentials.

The account may read the quota and quote and renew only the free Basic Synthetic
Test quota. It has no console login, notification-quota permissions, Buy,
Recover, or other writes. The
[renewal design](../design/vmonitor-quota-renewal.md) owns the exact IAM
actions, zero-price checks, pinned CLI identity, environment-only
authentication, persistent issue hold, custody, revocation, and redacted logging
contract. Credentials never reach application containers, build jobs, artifacts,
or logs. The owner accepts trusted-main and runner custody and the issue hold's
stated limits; the CLI price check cannot contain a stolen cloud key.

Only `.github/workflows/vmonitor-renew.yml`, running trusted-main code on
GitHub-hosted runners, may use the environment. Start with `workflow_dispatch`;
enable a schedule only after reviewed implementation, green CI, a read-only
credentialed check, and a successful due dispatch confirmed by read-back. Keep
uncertain writes behind the durable hold and require human reconciliation. Do
not grant `actions: write` or make this workflow a dependency of `ci.yml`.

This amendment narrows the 2026-10-14 exception to Synthetic because Telegram
needs no notification quota. When accepted, it superseded the manual-only
renewal sentence in the then-proposed deployment amendment without accepting
that proposal at that time. The deployment amendment was accepted separately
with conditions. This renewal amendment also makes its named workflow the sole
exception to the blanket no-stored-secrets wording in
[ADR 0024](0024-delivery-gates.md#decision). Build, test, release, and
code-scanning workflows remain without stored secrets; GitHub's per-run token
remains allowed under their existing permissions.

The benefit is unattended renewal without exposing the key to host compromise or
depending on laptop uptime. The cost is a narrowly scoped static cloud key in
GitHub and trusted issue writers. A missed schedule can still let the quota
lapse. Rollback disables automation and returns to coordinated manual renewal
after settling any retained hold. No app contract or stored-data migration is
required. Provider renewal rules and first-party evidence are cited in the
renewal design; live term arithmetic remains unverified.

### Amendment history

2026-11-01: Named and narrowed the accepted cloud-credential exception to
Synthetic renewal, with a main-only environment and a dispatch-first rollout,
because Basic cannot auto-renew and Telegram removed the notification quota. The
project ID also uses an environment secret to mask runner logging.

## History

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

2026-10-18: the owner chose a separate CrowdSec database in the existing
PostgreSQL cluster with no SQLite data migration. This removes SQLite checkpoint
dependency from the serving gate and accepts attack records in encrypted cluster
backups. The [CrowdSec privacy contract](../design/crowdsec-http-privacy.md)
owns live retention, backup expiry, the bounded incident-copy exception, and
recovery before consumer startup. The owner also approved conditional samples of
rejected target paths, bounded by that contract. Fresh CrowdSec provisioning
after every restore prevents old statistics and revoked credentials from
returning to service. This is an accepted design, not evidence of cutover.

2026-11-01: Consolidated the surviving single-host and public release rules from
ADR 0025 here. This records existing decisions without changing their acceptance
dates. The `aboutme-readyz` alarm moved to the Telegram channel `aboutme-alerts`
for In-alarm, Up, and Undetermined notifications. It fires when one of two
locations fails three consecutive one-minute tests. The email notification quota
was deleted.

2026-11-01: Accepted anonymous approval reads and Ansible pull with a cache wait
and single-page, single-review limits after the live probe. Retained solo owner
approval and parallel laptop deployment; implementation follows the design's
release order.
