# Vietnam production migration

Status: open (owner decisions 2026-09-24 and 2026-10-10). Moves production to GreenNode and Bizfly in Vietnam; AWS stays as a fictional-data test environment. Design: [vietnam-production.md](../design/vietnam-production.md). Decision: [ADR 0027](../adr/0027-vietnam-hosted-production.md). Design wins over this plan.

Code, comments, tests, and living docs cite the design or ADR 0027, never this plan or its phase numbers. Each app release is one feature per [How we work](../../AGENTS.md#how-we-work).

## Owner actions

1. Top up the existing GreenNode account for one month of vServer, volumes, and vStorage. Devops quotes each paid create first (`--max-price`).
2. Create an IAM user for the aboutme tooling, separate from the user the other session uses, with policies per the vngcloud IAM wiki page (`~/src/vngcloud/docs/wiki/CLI-IAM.md`, `Configuration.md`). Store it as `vngcloud` profile `aboutme` (`configure set`, password and TOTP secret from stdin, credentials file mode 0600), and a second profile `aboutme-ro` with `read_only 1` for agent reads. Per that wiki page, agent profiles hold no IAM write rights: the owner runs the IAM writes (service accounts, policy attaches) that devops prepares.
3. Service accounts come from the CLI, not the console: one per bucket (media, backups, state), each with a bucket policy naming its principal and a key made by `storage create-s3-key --service-account-id`, plus one for the OpenTofu provider. Every secret goes by `--secret-file` to a 0600 file under `.dev/credentials/` and is never printed. **Unconfirmed:** the provider's authentication fields (service-account client ID and secret) until phase 1 tests them.
4. Put `TOFU_STATE_PASSPHRASE` (at least 32 random bytes, base64) in `.dev/credentials/greennode.env`, mode 0600. Agents load credentials with `set -a; . <file>; set +a` inside the command that needs them and never print, echo, or log a value.
5. Create a Bizfly Cloud account (https://bizflycloud.vn), verify identity, top up, and subscribe to Email Transaction for `aboutme.vn`.
6. Put Bizfly values in `.dev/credentials/bizfly.env`, mode 0600: `BIZFLY_SMTP_HOST`, `BIZFLY_SMTP_PORT`, `BIZFLY_SMTP_USERNAME`, `BIZFLY_SMTP_PASSWORD`, and `BIZFLY_API_KEY` if Bizfly offers one. `secrets.sh` pipes the SMTP values to the host without printing them.
7. Generate an age key pair for `aboutme-infra`; keep the private key off the host; give devops the public recipient.
8. Generate an `ed25519-sk` key on the hardware key; give devops the public key and the SSH source allowlist.
9. At cutover: run the SSM-to-host secret pipe and approve the Route 53 switch of the apex and `www`.
10. Once the cutover is verified: approve the AWS real-data deletion and the CloudFront teardown.
11. After the cutover and before real users: prepare and file the data protection impact assessment and the cross-border transfer dossier with the Ministry of Public Security. The public announcement waits for the cutover.

## Decided

- 2026-09-24: self-hosted PostgreSQL 18 with pgBackRest (30 days, point-in-time recovery, quarterly drill); no rollback window after Vietnam accepts writes; no domain for the AWS test environment; Have I Been Pwned stays and the DPIA records it.
- 2026-10-10: nothing in front of the host; Caddy with Coraza and CrowdSec; vWAF only as a DNS-change escalation.
- 2026-10-10: DNS stays at Route 53; Google Workspace support mailbox stays; no Bizfly Business Email.
- 2026-10-10: compute HCM03 (zone HCM03-1A), vStorage HCM04, one backup repository.
- 2026-10-10: one s2-general-2x4 vServer, Ubuntu 24.04; root 20 GB unencrypted, data 20 GB encrypted; grow online at 70%.
- 2026-10-10: `ed25519-sk` SSH key; host-file release fence; SMTP sender over the Bizfly HTTP API; no vMonitor, journald 30 days on the host; CrowdSec community sharing off.

## Phases

|Phase|Owner role|Done when|
|-|-|-|
|1 Verify on the account|owner, devops|Owner actions 1 to 8 done; `vngcloud --profile aboutme-ro portal get-user-info` succeeds; devops has tested design Q1 to Q13 on the account and recorded results in the runbook; Q3 (cutover blocker) and Q4 (pgBackRest and state backend) answered; Q5, Q9, and Q13 (data location, subprocessors, DPA) answered before cutover, since they carry the ADR's legal reason.|
|2a amd64 images|devops|Release workflow publishes `linux/amd64` and `linux/arm64` manifests; smoke on both; deploy/aws unchanged in behavior.|
|2b SMTP sender|backend|`AUTH_EMAIL_MODE=smtp` with config validation, TLS verification, outcome classification, stub-server tests; SES mode unchanged.|
|2c Caddy direct edge|devops|`EDGES` gains `direct`: socket address only, every forwarding header stripped, one `X-Real-IP` to Go; site host from environment. Caddy image built with `xcaddy` adds `coraza-caddy` (OWASP CRS v4) and the CrowdSec Caddy bouncer. Tests: forged `X-Forwarded-For`, `X-Real-IP`, and `CloudFront-Viewer-Address` on `direct` never reach Go; Coraza in detection-only mode logs and passes a rule-matching request; the CloudFront listener is unchanged.|
|2d Legal text|frontend|Privacy notice and terms name GreenNode and Bizfly in Vietnam; reviewed; first deployed at cutover.|
|3a Build code|devops, reviewer|`deploy/vn/` per the design layout: OpenTofu root for server, volumes, security groups, floating IP; `vngcloud` CLI scripts for service accounts, buckets, keys, bucket policies, budgets; host, edge (Coraza detection-only with the resume write-path exclusions, CrowdSec sharing off), and probe config. CI runs `tofu fmt -check` and `tofu validate`. Reviewer's adversarial pass done before any apply.|
|3b Apply|devops|On the manager's go, after `tofu plan` and every CLI quote are shown: buckets, service accounts, keys, and policies created; tofu applied; host, secrets, PostgreSQL, pgBackRest, CrowdSec, the watch timer, and alert mail installed; probe passes; first backup and `pgbackrest verify` green.|
|4 Rehearsal|devops, qa|All rehearsal checks in the design pass under the temporary hostname with fictional data; restore drill recorded; cutover dry run timed.|
|5 Cutover|manager, devops, owner|Design cutover steps 1 to 6 done; verification hashes equal; smoke green, including the CrowdSec 403; AWS in maintenance.|
|6 AWS teardown|devops, owner|After 48 h with no AWS traffic: design cutover step 7 done (CloudFront, its certificates and validation records, the Route 53 health check deleted) and record TTL 300. Once the cutover is verified: every real-data item in the design deleted and recorded in `aboutme-infra`; keys rotated; AWS rebuilt empty as test.|
|7 Coraza blocking|devops|After two weeks of detection-only in production, audit log reviewed, exclusions updated for any false positive, Coraza switched to blocking; smoke green.|

Each phase with production infrastructure or deploy code needs a reviewer's adversarial pass before merge. Runbooks (`docs/runbooks/production.md`, `email.md`, `privacy.md`, and the vWAF escalation), `docs/architecture.md`, `docs/design/deployment.md`, `docs/design/single-host-production.md`, and `docs/design/decisions.md` update in the change that makes them true.
