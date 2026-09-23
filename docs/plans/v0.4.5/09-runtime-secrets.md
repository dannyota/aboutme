# TOTP runtime secrets brief

Role: devops. Model: Sonnet (Codex: `gpt-5.6-terra`).

## Objective and authority

Provision TOTP key references and disabled-by-default enrollment without
exposing key values. Read `AGENTS.md`, the accepted TOTP contract and
key-management design, accepted ADR 0049, the deployment design, ADRs 0033
through 0039 and 0048, the release-fence contract, production runbook, and the
v0.4.2 passkey wiring on main (`PASSKEY_ENROLLMENT_ENABLED` in
`deploy/aws/modules/tasks` and `deploy/aws/prod`, and the fence resources in
`deploy/aws/modules/data` and `deploy/aws/modules/identity`).

## Owned paths

- Modify `deploy/aws/scripts/secrets.sh`.
- Modify `deploy/aws/modules/identity/main.tf` only for exact TOTP slot read
  permission and deploy-role `RunTask` on the `aboutme-prod-totp-reencrypt`
  family.
- Modify `deploy/aws/modules/ops/main.tf` only for the `totp_unavailable` metric
  filter and alarm.
- Modify `deploy/aws/modules/tasks/main.tf` and
  `deploy/aws/modules/tasks/variables.tf` and
  `deploy/aws/modules/tasks/outputs.tf`.
- Modify `deploy/aws/prod/main.tf`, `deploy/aws/prod/variables.tf`, and
  `deploy/aws/prod/prod.tfvars.example`.
- Modify `.env.example` only to add empty TOTP variable names.

Do not edit application code, deploy and rollback scripts, workflows, runbooks,
other infrastructure modules, secret values, or design. Do not run AWS,
OpenTofu, installs, tests, builds, browsers, database writes, or stacks.

## Required behavior

- Add protected parameters `totp/key-a` and `totp/key-b` and nonsecret variables
  `totp_active_key_slot` (`a` or `b`, default `a`) and `totp_previous_key_slot`
  (empty, `a`, or `b`, default empty) with validation that they differ. Inject
  `TOTP_ACTIVE_KEY` from the active slot and `TOTP_PREVIOUS_KEY` only when the
  previous slot is set. Add no key-ID parameter or variable.
- Add `secrets.sh totp-key <a|b>`. It writes a fresh random value without
  printing it and refuses a slot the running app task definition names.
- Grant only the app ECS execution role reads for the two exact slot ARNs.
  Scheduled jobs, migration, web, host, and scheduler roles gain none.
- Add task family `aboutme-prod-totp-reencrypt` for
  `/usr/local/bin/server totp-key-reencrypt`. It uses the app execution and task
  roles, database access, and the same injection. It is not scheduled. Let the
  deploy role run only that new family in addition to the existing ones.
- Add a log metric filter on the app log group for
  `{ $.msg = "totp_unavailable" }` and alarm `aboutme-prod-totp-unavailable` to
  the existing alert topic: sum at least one in one 300-second period, missing
  data not breaching. Do not change the Route 53 or `/readyz` checks.
- Wire `TOTP_ENROLLMENT_ENABLED` to the app task and default the production
  variable false.
- Keep key values out of OpenTofu variables, state, output, plans, command
  arguments, source, tests, and logs. The secret script creates values without
  printing them and remains idempotent.
- Preserve every existing RDS, SSM, task, host, fence, and explicit-deny
  boundary. Add no new data store or release-fence item.

## Hosted checks and report

Add static contract assertions where the repository pattern supports them.
Report these as unrun pending exact-candidate GitHub CI:

```bash
bash -n deploy/aws/scripts/secrets.sh
tofu fmt -check -recursive deploy/aws
tofu -chdir=deploy/aws/bootstrap init -backend=false -input=false
tofu -chdir=deploy/aws/bootstrap validate
tofu -chdir=deploy/aws/probe init -backend=false -input=false
tofu -chdir=deploy/aws/probe validate
tofu -chdir=deploy/aws/prod init -backend=false -input=false
tofu -chdir=deploy/aws/prod validate
```

Definition of done: a reviewed plan wires valid keys and enrollment false to the
app and one-shot task only, with no secret value in plan or state. Report every
changed line in shared files, hosted commands and results or awaiting CI,
skipped commands and reason, expected plan resources, and open items. Do not
perform Git or AWS operations. Use short plain text with no em dash. Code,
tests, comments, and living docs never cite plans, tasks, phases, or review
findings; cite the design, ADR, or `AC-*` ID instead.
