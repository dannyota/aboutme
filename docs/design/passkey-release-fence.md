# Production release fence

The release fence stops the supported production path from starting an image
below the minimum release once second-factor enrollment can exist
([ADR 0048](../adr/0048-passkey-second-factor-authentication.md),
[ADR 0049](../adr/0049-totp-second-factor-authentication.md)). Two floors apply:

| Enrollment flag              | Floor  | Numeric release |
| ---------------------------- | ------ | --------------- |
| `PASSKEY_ENROLLMENT_ENABLED` | v0.4.2 | 4002            |
| `TOTP_ENROLLMENT_ENABLED`    | v0.4.7 | 4007            |

The fence does not bind OpenTofu, the AWS-login principal, an account
administrator, or an old script run with those credentials. The
[production runbook](../runbooks/production.md) owns the commands.

## DynamoDB table and item

OpenTofu creates `aboutme-prod-release-fence` in `ap-southeast-1` with on-demand
capacity, AWS-owned encryption, point-in-time recovery, deletion protection, and
`prevent_destroy`. Its string partition key is `id`; the only item is
`application`.

The item holds numeric `minimum_release`, string `minimum_tag`, and string
`updated_at`. A strict `vMAJOR.MINOR.PATCH` tag with components from 0 through
999 and no leading zero maps to `MAJOR*1000000 + MINOR*1000 + PATCH`. Every read
is `ConsistentRead=true`.

A missing item reads as zero only on a verified first deploy, or while the
running app task definition proves both enrollment flags false. Otherwise a
missing or malformed item blocks every mutation. A running app with an
enrollment flag on while the item is below that flag's floor also blocks every
mode.

## Serialized production operation

Every deploy, rollback, failed-deploy restoration, activation, and TOTP key
re-encryption holds one nonexpiring lock on the item. A 32-byte random base64url
`operation_id`, a closed `operation_kind` (`deploy`, `rollback`, `activate`, or
`totp_reencrypt`), and UTC `operation_started_at` name the owner. There is no
time-based takeover.

After assuming the deploy role, the deployer acquires the lock with one
conditional `UpdateItem` that also initializes a missing minimum to zero and tag
`v0.0.0`:

```text
attribute_not_exists(operation_id) AND
(attribute_not_exists(minimum_release) OR minimum_release <= :candidate)
```

A lower target or a held lock fails before any other AWS mutation. An unknown
acquisition result is settled by a strong read for the exact operation ID; no
retry may replace another owner.

Before each task registration, service update, one-shot start, schedule change,
alarm suppression, or EventBridge rule change that can start an image, the
deployer updates `operation_checked_at` on the condition
`operation_id = :operation AND minimum_release <= :candidate`. Two mutations
need no checkpoint: scaling or stopping a service, which starts nothing, and
starting the maintenance service, whose revision was registered under a
checkpoint and whose one Caddy container serves a static 503 page with no
authentication or database logic.

The lock stays held through normal completion and every automatic restoration.
Release removes the four operation attributes on the condition of the exact
operation ID. A crash leaves the lock closed. A privileged manual clear first
proves no deploy process or AWS mutation is active, inspects task, service,
schedule, alarm, and rule state, and records its reason.

Activation raises the floor while it owns the lock:

```text
operation_id = :operation AND
(attribute_not_exists(minimum_release) OR minimum_release <= :candidate)
```

It sets `minimum_release`, `minimum_tag`, and `updated_at`. An equal value
succeeds and a lower one fails. Activation requires the running app to be the
exact candidate and changes no image, service, snapshot, or notification.

## IAM boundary

The ignored production variables name `operator_principal_arn`, the same-account
IAM ARN behind the owner's `aws login` session. OpenTofu makes it the only trust
principal of `aboutme-prod-operator`. The deployer verifies that caller, then
assumes the operator role.

The operator role may describe and strongly read the fence and assume only
`aboutme-prod-deploy`. An explicit deny covers direct ECS, Scheduler, release
snapshot, fence-write, alarm-action, and EventBridge rule-state mutations. The
deploy role trusts only the operator role. The AWS-login principal keeps
OpenTofu and administrator access as a named bypass.

The deploy role has `dynamodb:DescribeTable`, `GetItem`, and `UpdateItem` on the
fence table only. Application, job, execution, host, migration, database, and
scheduler roles have no fence access. Its other permissions are limited to:
describing ECS state; registering task definitions; updating the three
production services (`app`, `web`, `maintenance`); running the `migrate`,
`db-setup`, `jobs`, and `totp-reencrypt` families; listing and updating
schedules in `aboutme-prod-jobs`; creating, tagging, and describing release
snapshots; describing referenced SSM parameters and Secrets Manager secrets;
passing only the production task and execution roles to ECS; describing and
disabling or enabling actions on `aboutme-prod-*` alarms; and describing,
disabling, and enabling the production task-stopped notification rule. It cannot
create or delete an alarm, rule, target, or notification.

## Deployer order and task evidence

`deploy.sh` runs from the current `main` checkout. It verifies the base caller,
assumes the operator role, strongly reads the fence, assumes the deploy role
through a refreshable AWS CLI profile, and takes the lock before its first
mutation. The script parses and compares release numbers; IAM inspects no tag,
image, or task definition.

Every registered task definition records `DEPLOY_RELEASE_TAG` and
`DEPLOY_RELEASE_NUMBER`. The script checks those fields and the lock before it
registers a revision, starts `web`, `app`, or a one-shot task, or changes job
schedules. Before taking the lock, it refuses to register an app revision that
turns an enrollment flag on while the item is missing or below that flag's
floor. Failed-deploy restoration rejects a previous task definition with a
missing or lower release. A read or conditional-check failure stops the run; if
maintenance already serves, it stays up rather than start an unproved image.
Every exit restores alarm actions and the task-stopped rule before releasing the
lock.

Enabling a flag follows one order: deploy the capable release with the flag off,
prove it in production, raise the fence with `--activate`, then apply the flag
through reviewed OpenTofu and redeploy the same tag. A failed raise leaves
enrollment off. A failed apply or deploy after a raise leaves the higher fence
in place.

Once the raise commits, a lower target cannot take the lock. Deploy, rollback,
and restoration never run the target release's copy of the script. Rollback
below the fence is a forward fix or privileged administration.

## Authenticator-app key re-encryption

`deploy.sh --totp-key-reencrypt <tag>` takes the lock with kind
`totp_reencrypt`. It requires the tag and the stored minimum to be at least
v0.4.7 and the tag to equal the running app release. It checkpoints before
registering the one-shot task definition and before `RunTask`, waits for the
task to stop, then releases the lock. It changes no service, schedule, alarm, or
rule.

The `aboutme-prod-totp-reencrypt` family uses the app execution and task roles,
so the deploy role's pass-role scope stays the same, and scheduler and job roles
gain nothing. The [key-management design](totp-key-management.md#rotation)
defines what the task does.

## Privileged bypass

OpenTofu, direct AWS-login credentials, and account administration remain
privileged bypasses; DynamoDB and IAM do not make an old script safe under them.
The production runbook requires proof from account factor state that a bypass
target is compatible, and a recorded reason.
