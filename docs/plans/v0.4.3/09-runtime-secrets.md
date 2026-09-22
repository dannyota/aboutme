# TOTP runtime secrets brief

Role: devops. Model: `gpt-5.6-terra`.

## Objective and authority

Provision TOTP key references and disabled-by-default enrollment without
exposing key values. Read `AGENTS.md`, the accepted TOTP contract, accepted ADR
0049, the deployment design, ADRs 0033 through 0039 and 0048, the release-fence
contract, production runbook, and verified v0.4.2 infrastructure report.

## Owned paths

- Modify `deploy/aws/scripts/secrets.sh`.
- Modify `deploy/aws/modules/identity/main.tf` only for exact TOTP parameter
  read permission.
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

- Add runtime references for active key ID and value plus optional previous key
  ID and value. Secure key values use the existing protected parameter path. IDs
  may use nonsecret string parameters.
- Grant only the app ECS execution role reads for the exact configured TOTP
  parameter ARNs. Scheduled jobs, migration, web, host, scheduler, and deploy
  roles gain none.
- Add a dedicated one-shot task definition for
  `/usr/local/bin/server totp-key-reencrypt`. It uses the app execution and task
  roles, database access, and the same exact TOTP parameter injection. It is not
  scheduled.
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
perform Git or AWS operations. Use short plain text with no em dash.
