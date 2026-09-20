# Minimum-release fence infrastructure brief

Role: devops. Model: `gpt-5.6-terra`.

## Objective and authority

Provision the accepted durable minimum-release fence, dedicated deploy role, and
disabled-by-default passkey enrollment environment. Read `AGENTS.md`,
`docs/design/second-factor-authentication.md`,
`docs/design/passkey-second-factor-contract.md`,
`docs/design/passkey-release-fence.md`, `docs/design/deployment.md`, ADRs 0033
through 0039, ADR 0048, and the production runbook before editing.

## Owned paths

- Modify `deploy/aws/modules/data/main.tf`.
- Modify `deploy/aws/modules/data/outputs.tf`.
- Modify `deploy/aws/modules/data/variables.tf`.
- Modify `deploy/aws/modules/identity/main.tf`.
- Modify `deploy/aws/modules/identity/outputs.tf`.
- Modify `deploy/aws/modules/identity/variables.tf`.
- Modify `deploy/aws/modules/tasks/main.tf`.
- Modify `deploy/aws/modules/tasks/variables.tf`.
- Modify `deploy/aws/prod/main.tf`.
- Modify `deploy/aws/prod/variables.tf`.
- Modify `deploy/aws/prod/prod.tfvars.example`.

Do not edit deploy scripts, runbooks, application code, workflows, secrets, or
design sources. Do not run AWS or apply OpenTofu.

## Required behavior

- Create the exact accepted DynamoDB table, singleton fence item contract, and
  nonexpiring operation-lock contract with encryption, deletion protection,
  stable identity, and least privilege.
- Add the ignored `operator_principal_arn` production variable, validate its
  same-account canonical IAM ARN, and trust only that ARN in
  `aboutme-prod-operator`.
- The exact operator role may describe and strongly read the fence and assume
  only the deploy role. An explicit deny blocks its direct ECS, Scheduler,
  snapshot, fence-write, alarm-action, and EventBridge rule-state mutations.
- Only the dedicated deploy role may perform the accepted conditional lock and
  fence updates and the bounded ECS, Scheduler, snapshot, alarm-action, and
  EventBridge rule-state mutations.
- App, web, jobs, maintenance, migration, execution, host, and scheduler roles
  receive no fence access.
- The AWS-login principal may assume only the exact operator entrypoint for the
  supported deploy path. The deploy role trusts only that operator role. The
  AWS-login principal retains OpenTofu and administrator access as the named
  privileged bypass.
- The deploy role has only the accepted fence read, lock acquire and check,
  monotonic raise, exact-owner release, and bounded deployment operations.
- Wire `PASSKEY_ENROLLMENT_ENABLED` into only the required task environment and
  default the production variable to false.
- Keep every secret out of variables, state output, plans, logs, and source.
- Preserve existing RDS, SSM, task, host, and explicit-deny boundaries.

## Test-first cycle and checks

Start with `git status --short`. Add configuration validation or static contract
tests before resources where the repository pattern supports them. The top
manager must grant the infrastructure lane before these commands run:

```bash
tofu fmt -check -recursive deploy/aws
tofu -chdir=deploy/aws/bootstrap init -backend=false -input=false
tofu -chdir=deploy/aws/bootstrap validate
tofu -chdir=deploy/aws/probe init -backend=false -input=false
tofu -chdir=deploy/aws/probe validate
tofu -chdir=deploy/aws/prod init -backend=false -input=false
tofu -chdir=deploy/aws/prod validate
```

Definition of done: a reviewed plan can create the durable fence and role with
enrollment false and no task access to the table. Report every changed line in
shared Terraform files, commands and results, skipped commands with reason,
expected plan resources, and open items. Do not perform Git operations, AWS
writes, plan, or apply. Use short plain text with no em dash.
