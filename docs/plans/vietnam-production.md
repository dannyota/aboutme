# Vietnam production migration

Status: open (owner decision 2026-09-24). Moves production to GreenNode and Bizfly in Vietnam; AWS stays as a fictional-data test environment. Design: [vietnam-production.md](../design/vietnam-production.md). Decision: [ADR 0051](../adr/0051-vietnam-hosted-production.md). Design wins over this plan.

Code, comments, tests, and living docs cite the design or ADR 0051, never this plan or its phase numbers. Each app release is one feature per [How we work](../../AGENTS.md#how-we-work).

## Owner actions

1. Approve or change each **Owner approval** item in the design (list below).
2. Create a GreenNode account (https://greennode.ai), verify identity (eKYC; business or personal as the owner chooses), add a payment method, top up enough for one month of vServer, volumes, vStorage, vCDN, and vMonitor.
3. Create a Bizfly Cloud account (https://bizflycloud.vn), verify identity, top up, and subscribe to Email Transaction and Business Email for `aboutme.vn`.
4. In GreenNode IAM, create a service account `aboutme-tofu` with only the vServer, volume, security group, and floating IP permissions OpenTofu needs; create its client ID and secret.
5. In vStorage, create a service account `aboutme-state` with an S3 key limited to the state bucket (devops creates the bucket with that key's parent account in step 3 of the build, or the owner creates `aboutme-tfstate` in the console first).
6. Put those values in `.dev/credentials/greennode.env`, mode 0600: `GREENNODE_CLIENT_ID`, `GREENNODE_CLIENT_SECRET`, `VSTORAGE_STATE_ACCESS_KEY_ID`, `VSTORAGE_STATE_SECRET_ACCESS_KEY`, `TOFU_STATE_PASSPHRASE` (at least 32 random bytes, base64). Agents load it with `set -a; . .dev/credentials/greennode.env; set +a` inside the command that needs it and never print, echo, or log a value.
7. Put Bizfly values in `.dev/credentials/bizfly.env`, mode 0600: `BIZFLY_SMTP_HOST`, `BIZFLY_SMTP_PORT`, `BIZFLY_SMTP_USERNAME`, `BIZFLY_SMTP_PASSWORD`, and `BIZFLY_API_KEY` if Bizfly offers one. `secrets.sh` pipes the SMTP values to the host without printing them.
8. Generate an age key pair for `aboutme-infra`; keep the private key off the host; give devops the public recipient.
9. Send the provider questions (design table Q1 to Q22) by support ticket or email; forward each written answer to the manager.
10. Choose and register the test domain for the AWS environment, if approved.
11. Move the support mailbox: create the Bizfly Business Email mailbox and import Google Workspace mail (after Q21).
12. At cutover: run the SSM-to-host secret pipe, approve the DNS switch at Cloudflare, and later the NS change at the `.vn` registrar.
13. After the rollback window: approve the AWS real-data deletion and cancel Google Workspace.

## Owner approvals (recommendation first)

- Region HCM03 for every GreenNode resource. Recommend yes.
- vServer 2 vCPU, 4 GiB, Ubuntu 24.04 LTS, separate encrypted SSD data volume. Recommend yes.
- Self-hosted PostgreSQL 18 with pgBackRest, 30 days, PITR, quarterly drill. Recommend yes (managed vDB is PG17, no PITR).
- Second pgBackRest repository in Hanoi. Recommend yes.
- vCDN-managed viewer certificate if offered, else Let's Encrypt uploaded by devops. Recommend yes.
- Launch without vWAF. Recommend yes; revisit on abuse.
- SMTP sender, not the Bizfly HTTP API. Recommend SMTP.
- Release fence as a root-owned host file with a start-time check. Recommend yes over a PostgreSQL table.
- Password reset may create the first password for a Google-only account. Recommend yes; otherwise those accounts lose sign-in.
- Hardware-backed SSH key (`ed25519-sk`). Recommend yes.
- 7-day rollback window, forward-only after Vietnam accepts writes. Recommend yes.
- Separate test domain on Cloudflare for the AWS environment. Recommend yes.
- Keep Have I Been Pwned and record it in the DPIA. Recommend yes.

## Phases

|Phase|Owner role|Done when|
|-|-|-|
|1 Confirm with providers|owner, manager|Accounts verified and topped up; credentials files exist; written answers to Q1 to Q22; no cutover blocker open (Q1, Q3, Q5, Q6, Q11). Q10 blocks only the NS move.|
|2a amd64 images|devops|Release workflow publishes `linux/amd64` and `linux/arm64` manifests; smoke on both; deploy/aws unchanged in behavior.|
|2b SMTP sender|backend|`AUTH_EMAIL_MODE=smtp` with config validation, TLS verification, outcome classification, stub-server tests; SES mode unchanged.|
|2c Caddy edge selection|devops|`EDGE` selects cloudflare or vcdn, host from environment, edge-secret check with two values, vCDN client-IP trust; Caddy tests cover forged headers and missing secret.|
|2d Google-only accounts|backend, frontend|Notice sent; reset rule per owner decision, with tests; count of Google-only accounts recorded without identifiers.|
|2e Legal text|frontend|Privacy notice and terms name GreenNode and Bizfly in Vietnam; reviewed; first deployed at cutover.|
|3 Build|devops|`deploy/vn/` per design layout; tofu applied after adversarial review; buckets, host, PostgreSQL, pgBackRest, agents, alarms; probe passes.|
|4 Rehearsal|devops, qa|All rehearsal checks in the design pass under the temporary hostname with fictional data; restore drill recorded; cutover dry run timed.|
|5 Cutover|manager, devops, owner|Design cutover steps 1 to 6 done; verification hashes equal; smoke green; AWS in maintenance.|
|6 NS move|devops, owner|After 48 h, NS at vDNS with identical records.|
|7 AWS deletion|devops, owner|After the rollback window, every real-data item in the design deleted and recorded in `aboutme-infra`; keys rotated; AWS rebuilt empty as test.|

Each phase with production infrastructure or deploy code needs a reviewer's adversarial pass before merge. Runbooks (`docs/runbooks/production.md`, `email.md`, `privacy.md`), `docs/architecture.md`, `docs/design/deployment.md`, `docs/design/single-host-production.md`, and `docs/design/decisions.md` update in the change that makes them true.
