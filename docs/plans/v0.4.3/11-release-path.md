# TOTP release path brief

Role: devops. Model: `gpt-5.6-terra`.

## Objective and authority

Add the hosted TOTP proof job and document the accepted flag-off deploy, floor
4003 activation, key rotation, production proof, and failure order. Read
`AGENTS.md`, the accepted TOTP contract, accepted ADR 0049, the passkey
release-fence contract, deployment design, production runbook, release plan,
verified runtime-secret report, and verified QA report.

## Owned paths

- Modify `.github/workflows/ci.yml` only for `totp-browser-proof`.
- Modify `scripts/test/workflow-safety-test.sh` only for that job.
- Modify `deploy/aws/scripts/deploy_test.sh` and
  `deploy/aws/scripts/testdata/respond` only for v0.4.3 floor and same-tag
  activation and one-shot rotation cases.
- Modify `deploy/aws/scripts/deploy.sh` only to add the supported
  `--totp-key-reencrypt <tag>` operation.
- Modify `docs/runbooks/production.md`.

Do not alter existing deploy or rollback behavior beyond the accepted one-shot
operation. Do not edit Terraform, application code, QA harness, another
workflow, secrets, or design. Do not run AWS, OpenTofu, production commands,
local tests, builds, lint, browsers, database writes, or stacks.

## Required behavior

- Add a fixed 45-minute `totp-browser-proof` job that uses repository lifecycle
  targets, builds the pinned browser image, executes `make dev-https-totp-check`
  on the exact candidate, and uploads only bounded secret-free
  `.dev/native-https/evidence/totp-*`.
- Use unconditional cleanup for the HTTPS stack and runner-local database on
  success, failure, and cancellation. Give the job no production secret or AWS
  access.
- Add workflow-safety assertions for the exact target, timeout, artifact path,
  lack of production credentials, and cleanup. Listing or compilation is not a
  substitute for execution.
- Prove the generic serialized activation accepts 4003 after 4002, never lowers
  the floor, rejects lower deploy and restoration targets, and preserves the
  higher floor after later failure.
- Add `deploy/aws/scripts/deploy.sh --totp-key-reencrypt <tag>` as the supported
  one-shot operator path. It acquires the durable operation lock, requires a
  v0.4.3-or-later image and floor, runs the dedicated task with app roles, emits
  only bounded counts and internal row IDs, and never grants scheduled jobs TOTP
  parameter access.
- Document valid-key flag-off deployment, readiness and compatibility proof,
  floor raise, flag enablement, same-tag redeploy, bounded key re-encryption,
  previous-key removal, fictional-account proof, cleanup, and forward-fix rule.
- Include the exact two bounded production browser exceptions from the plan.
  State the 8 GiB memory floor, nonblocking shared lock, 2 GiB hard memory cap,
  no swap, 200 percent CPU cap, 60-minute timeout, isolated profile, no local
  stack, fictional account, owner-only ignored credential file, and cleanup on
  every exit.

## Hosted checks and report

Write failing workflow and shell-stub cases first. Report these as unrun pending
exact-candidate GitHub CI:

```bash
bash -n deploy/aws/scripts/deploy.sh deploy/aws/scripts/deploy_test.sh
bash deploy/aws/scripts/deploy_test.sh
node_modules/.bin/prettier --check docs/runbooks/production.md .github/workflows/ci.yml
npx markdownlint-cli2 docs/runbooks/production.md
```

The exact run must also show docs, workflow safety, and `totp-browser-proof`
green. Definition of done: hosted proof executes with unconditional cleanup and
the runbook gives one safe release, supported one-shot rotation, rollback, and
production-proof order. Report exact files and hunks, checks and results or
awaiting CI, artifact path or none, skipped commands and reason, and open items.
Do not perform Git or external writes. Use short plain text with no em dash.
