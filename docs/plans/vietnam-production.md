# Vietnam production migration

Status: open (owner decisions 2026-09-24 and 2026-10-10). Moves production to GreenNode and Bizfly in Vietnam; AWS stays as a fictional-data test environment. Design: [vietnam-production.md](../design/vietnam-production.md). Decision: [ADR 0027](../adr/0027-vietnam-hosted-production.md). Design wins over this plan.

Code, comments, tests, and living docs cite the design or ADR 0027, never this plan or its phase numbers. Each app release is one feature per [How we work](../../AGENTS.md#how-we-work).

## Owner actions

1. Top up the existing GreenNode account for one month of vServer, volumes, and vStorage. Devops quotes each paid create first (`--max-price`).
2. Done: the GreenNode IAM user in the vngcloud session's `.env` is the aboutme setup account (owner, 2026-10-10); agents run `vngcloud --read-only` unless a step needs a write.
3. Service accounts come from the CLI, not the console: one per bucket (media, backups, state), each with a bucket policy naming its principal and a key made by `storage create-s3-key --service-account-id`, plus one for the OpenTofu provider. Every secret goes by `--secret-file` to a 0600 file under `.dev/credentials/` and is never printed. **Unconfirmed:** the provider's authentication fields (service-account client ID and secret) until phase 1 tests them.
4. Put `TOFU_STATE_PASSPHRASE` (at least 32 random bytes, base64) in `.dev/credentials/greennode.env`, mode 0600. Agents load credentials with `set -a; . <file>; set +a` inside the command that needs them and never print, echo, or log a value.
5. Create a Bizfly Cloud account (https://bizflycloud.vn), verify identity, top up, and subscribe to Email Transaction for `aboutme.vn`.
6. Put Bizfly values in `.dev/credentials/bizfly.env`, mode 0600: `BIZFLY_SMTP_HOST`, `BIZFLY_SMTP_PORT`, `BIZFLY_SMTP_USERNAME`, `BIZFLY_SMTP_PASSWORD`, and `BIZFLY_API_KEY` if Bizfly offers one. `secrets.sh` pipes the SMTP values to the host without printing them.
7. Generate an age key pair for `aboutme-infra`; keep the private key off the host; give devops the public recipient.
8. The admin key is the public half of the owner's `ssh-ed25519` commit-signing key.
9. At cutover: run the SSM-to-host secret pipe and approve the Route 53 switch of the apex and `www`.
10. Once the cutover is verified: approve the AWS real-data deletion and the CloudFront teardown.
11. After the cutover and before real users: prepare and file the data protection impact assessment and the cross-border transfer dossier with the Ministry of Public Security. The public announcement waits for the cutover.

## Decided

- SSH on TCP 22922 open to all sources (owner, 2026-10-10).
- 2026-09-24: self-hosted PostgreSQL 18 with pgBackRest (30 days, point-in-time recovery, quarterly drill); no rollback window after Vietnam accepts writes; no domain for the AWS test environment; Have I Been Pwned stays and the DPIA records it.
- 2026-10-10: nothing in front of the host; Caddy with Coraza and CrowdSec; vWAF only as a DNS-change escalation.
- 2026-10-10: DNS stays at Route 53; Google Workspace support mailbox stays; no Bizfly Business Email.
- 2026-10-10: compute HCM03 (zone HCM03-1C, the zone enabled for the account; HCM03-1A needs a provider request), vStorage HCM04, one backup repository.
- 2026-10-10: one s2-general-2x4 vServer, Ubuntu 24.04; root 30 GB and data 20 GB, both GreenNode-encrypted at server create (one CES line); 4 GiB RAM with hard service caps, serialized jobs, 512 MiB zram, and a 64 MiB Caddy-log tmpfs; grow disks online at 70%.
- 2026-10-10: compute zone HCM03-1C; no DPA requests to GreenNode or Bizfly, the published terms stand (owner).
- 2026-10-10: admin SSH key is the owner's `ssh-ed25519` commit-signing key; host-file release fence; SMTP sender instead of the Bizfly HTTP API; no vMonitor; journald on the encrypted data volume for 30 days, capped at 2 GB; CrowdSec community sharing off.
- 2026-10-10: CrowdSec receives a five-field RAM-only HTTP feed; encrypted attack-IP records stay in Vietnam for at most 24 hours; the public notice changes at cutover.

## Current status

- The amd64 branch head is `52372cbd`; branch CI run `38022739301` and release-images run `38022741220` are green.
- The SMTP branch head is `76cc9279`; branch CI run `38022537867` is green.
- The Caddy branch head is `060111d2`; branch CI run `38029254709` passed encoder unit tests but failed the new IPv6 HTTPS probe because its TLS hostname was wrong. The probe fix awaits hosted verification.
- These phase 2 releases remain open because none is merged, tagged, or deployed.
- Vietnam host code at `984bd2c6` passed full branch CI `38024727139` and VN infrastructure CI `38024726866`. The new privacy controls and memory budget passed independent review and await hosted checks. No provider apply has started because provider account prerequisites remain open.
- The approved CrowdSec HTTP feed, parser, scenarios, retention gate, and cutover notice are implemented. Root and data defaults are 30 GB and 20 GB. Service memory caps and workload serialization target the 4 GiB host; actual peak memory remains a rehearsal gate.

## Phases

|Phase|Owner role|Done when|
|-|-|-|
|1 Verify on the account|owner, devops|Owner actions 1 to 8 done; devops has tested design Q1 to Q12 on the account and recorded results in the runbook; Q3 (cutover blocker) and Q4 (pgBackRest and state backend) answered; Q5, Q8, and Q12 (data location, subprocessors, DPA) answered before cutover, since they carry the ADR's legal reason.|
|2a amd64 images|devops|Release workflow publishes `linux/amd64` and `linux/arm64` manifests; smoke on both; deploy/aws unchanged in behavior.|
|2b SMTP sender|backend|`AUTH_EMAIL_MODE=smtp` with config validation, TLS verification, outcome classification, stub-server tests; SES mode unchanged.|
|2c Caddy direct edge|devops|`EDGES` gains `direct`: socket address only, every forwarding header stripped, one `X-Real-IP` to Go; site host from environment. Caddy image built with `xcaddy` adds `coraza-caddy` (OWASP CRS v4) and the CrowdSec Caddy bouncer. Tests: forged `X-Forwarded-For`, `X-Real-IP`, and `CloudFront-Viewer-Address` on `direct` never reach Go; each Caddy process writes only safe rule metadata to its own log and passes a rule-matching request in detection-only mode; the CloudFront listener is unchanged.|
|2d Legal text|frontend|Privacy notice and terms name GreenNode and Bizfly in Vietnam; reviewed; first deployed at cutover.|
|3a Build code|devops, reviewer|`deploy/vn/` per the design layout: OpenTofu root for server, volumes, security groups, floating IP; `vngcloud` CLI scripts for service accounts, buckets, keys, bucket policies, budgets; host, edge (Coraza detection-only with the resume write-path exclusions, CrowdSec sharing off), and probe config. CI runs `tofu fmt -check` and `tofu validate`. Reviewer's adversarial pass done before any apply.|
|3b Apply|devops|On the manager's go, after `tofu plan` and every CLI quote are shown: buckets, service accounts, keys, and policies created; tofu applied; host, secrets, PostgreSQL, pgBackRest, CrowdSec, the watch timer, and alert mail installed; probe passes; first backup and `pgbackrest verify` green.|
|4 Rehearsal|devops, qa|All rehearsal checks in the design pass under the temporary hostname with fictional data; restore drill recorded; cutover dry run timed; reboot with retained cutover lock and each marker kind refuses serving images and missed jobs before Podman while maintenance stays startable; matching `reset-db` alone permits `db-setup`, and malformed, symlinked, missing, wrong-kind, or mismatched marker state refuses it.|
|5 Cutover|manager, devops, owner|Design cutover steps 1 to 6 done; each cutover subcommand held its cutover fence lock through mutation or verification and the final quiescence check; verification hashes equal; smoke green, including the CrowdSec 403; AWS in maintenance.|
|6 AWS teardown|devops, owner|After 48 h with no AWS traffic: design cutover step 7 done (CloudFront, its certificates and validation records deleted; Route 53 health check retained) and record TTL 300. Once the cutover is verified: every real-data item in the design deleted and recorded in `aboutme-infra`; keys rotated; AWS rebuilt empty as test.|
|7 Coraza blocking|devops|After two weeks of detection-only in production, WAF rule diagnostics reviewed, exclusions updated for any false positive, Coraza switched to blocking; smoke green.|

Each phase with production infrastructure or deploy code needs a reviewer's adversarial pass before merge. Runbooks (`docs/runbooks/production.md`, `email.md`, `privacy.md`, and the vWAF escalation), `docs/architecture.md`, `docs/design/deployment.md`, `docs/design/single-host-production.md`, and `docs/design/decisions.md` update in the change that makes them true.
