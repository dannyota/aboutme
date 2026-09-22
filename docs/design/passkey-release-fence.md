# Passkey release-fence contract

Status: Approved for v0.4.2 under
[ADR 0048](../adr/0048-passkey-second-factor-authentication.md).

This contract prevents the supported production path from starting an image
below the minimum release after second-factor enrollment can exist. It does not
constrain OpenTofu, the AWS-login principal, an account administrator, or an old
script run with those privileged credentials.

## DynamoDB table and item

OpenTofu creates `aboutme-prod-release-fence` in `ap-southeast-1` in the
configured production account. It uses on-demand capacity, AWS-owned encryption,
point-in-time recovery, deletion protection, and `prevent_destroy`. Its string
partition key is `id`; the only item key is `application`.

The item has numeric `minimum_release`, string `minimum_tag`, and string
`updated_at`. A strict `vMAJOR.MINOR.PATCH` tag with components from 0 through
999 maps to `MAJOR*1000000 + MINOR*1000 + PATCH`; v0.4.2 maps to `4002`. Every
read uses `ConsistentRead=true`. Missing item means zero only while the running
app task definition has passkey enrollment false. Missing or malformed state
otherwise blocks all deployment mutation.

## Serialized production operation

Every normal deploy, rollback, failed-deploy restoration, and fence activation
uses one nonexpiring lock on the application item. A 32-byte random base64url
`operation_id`, closed `operation_kind` of `deploy`, `rollback`, or `activate`
(extended below for key re-encryption), and UTC `operation_started_at` name the
owner. There is no time-based takeover.

After assuming the deploy role, the current deployer acquires the lock with one
conditional `UpdateItem`. The update initializes a missing minimum to numeric
zero and tag `v0.0.0`. Its condition is:

```text
attribute_not_exists(operation_id) AND
(attribute_not_exists(minimum_release) OR minimum_release <= :candidate)
```

A lower target or existing operation fails before any other AWS mutation. An
unknown acquisition result is resolved by a strong read for the exact operation
ID; no retry may replace another owner. Before each task registration, service
update, one-shot start, schedule change, alarm suppression, or EventBridge rule
change, the deployer conditionally updates `operation_checked_at` only when the
operation ID still matches and `minimum_release <= :candidate`.

The lock remains held through normal completion and every automatic restoration
path. Release removes only the four operation attributes with a condition on the
exact operation ID. A crash leaves the lock closed. A privileged manual clear
first proves no deploy process or AWS mutation is active, inspects task,
service, schedule, alarm, and rule state, and records its reason. It never uses
an expiry.

The idempotent fence raise runs while the activation operation owns the lock. It
sets `minimum_release`, `minimum_tag`, and `updated_at` with this condition:

```text
operation_id = :operation AND
(attribute_not_exists(minimum_release) OR minimum_release <= :candidate)
```

A lower value fails. An equal value succeeds. The operation lock serializes a
lower deployment against activation, so neither can act on an earlier read while
the other raises the minimum.

## IAM boundary

The ignored production variables add `operator_principal_arn`, the canonical
same-account IAM ARN behind the owner's `aws login` session. OpenTofu validates
the account and makes that exact ARN the only trust principal of
`aboutme-prod-operator`. The deployer verifies that caller or its assumed-role
session, then assumes the operator role.

The operator role may describe and strongly read the fence and assume only
`aboutme-prod-deploy`. An explicit deny covers direct ECS, Scheduler, release
snapshot, DynamoDB fence-write, CloudWatch alarm-action, and EventBridge
rule-state mutations. The deploy role trusts only the operator role. The
AWS-login principal retains OpenTofu and administrator access as a named bypass.

The deploy role has `dynamodb:DescribeTable`, `dynamodb:GetItem`, and
`dynamodb:UpdateItem` on the fence table only. Application, job, task-execution,
host, migration, database, and scheduler roles have no fence access.

Deployment permissions are limited to describing ECS state; registering task
definitions; updating the three production services; running the migrate,
database setup, and jobs task families, plus the re-encryption family below;
listing and updating schedules in `aboutme-prod-jobs`; creating, tagging, and
describing release snapshots; describing referenced SSM parameters and Secrets
Manager secrets; and passing only the production task and execution roles to
ECS.

The deploy role also has `cloudwatch:DescribeAlarms`,
`cloudwatch:DisableAlarmActions`, and `cloudwatch:EnableAlarmActions` on
`aboutme-prod-*` alarm ARNs. It has `events:DescribeRule`, `events:DisableRule`,
and `events:EnableRule` on the exact production task-stopped notification rule.
These permissions preserve the reviewed maintenance suppression and cleanup
path. They grant no alarm, rule, target, or notification creation or deletion.

## Deployer order and task evidence

The current `deploy.sh` runs from the current main checkout. It verifies the
base caller, assumes the operator role, strongly reads the fence, assumes the
deploy role through a refreshable AWS CLI profile, and acquires the operation
lock before its first mutation. The script parses and compares release numbers.
IAM does not inspect a tag, image, task definition, or release number.

Every registered task definition records its exact release tag and numeric
release in environment metadata. The script validates those fields and the lock
before registering or starting maintenance, web, app, any one-shot task, or job
schedules. Failed-deploy restoration rejects a previous task definition with a
missing or lower release. A read or conditional-check failure stops. If
maintenance already serves, it stays serving rather than start an unproved
image. Every exit path restores alarm actions and the task-stopped notification
rule before it releases the operation lock.

## Activation and rollback

After a healthy flag-off v0.4.2 deploy and production proof, an activation
operation raises the fence to 4002 while holding the lock. Only after the raise
succeeds may reviewed OpenTofu set `PASSKEY_ENROLLMENT_ENABLED=true`; the same
capable tag is then redeployed. A failed raise leaves enrollment off. A
successful raise followed by apply or deploy failure leaves the higher fence in
place. The running flag stays off, or restoration uses the capable flag-off
revision.

Once the raise commits, a lower target cannot acquire the operation lock. Normal
deploy, explicit rollback, and automatic restoration use this contract. They
never run the target release's copy of the script. Rollback below the fence is a
forward fix or privileged administration, not a supported deploy.

## Authenticator-app key re-encryption

The TOTP release adds one supported one-shot operation. It extends the closed
`operation_kind` set with `totp_reencrypt` and adds the
`aboutme-prod-totp-reencrypt` task family to the families the deploy role may
run. That family uses the existing production app execution and task roles, so
the deploy role's pass-role scope does not grow. The scheduler and jobs roles
gain nothing.

`deploy.sh --totp-key-reencrypt <tag>` acquires the same operation lock with
kind `totp_reencrypt`. It requires the tag and the stored minimum to be at least
v0.4.3 (numeric 4003), and the tag to equal the running app release. It runs the
conditional `operation_checked_at` update before registering the one-shot task
definition and before `RunTask`. It waits for the task to stop, then releases
the exact owned lock. It updates no service and changes no schedule, alarm, or
rule. An older deployer that finds this lock held fails like any other held
lock.

## Privileged bypass

OpenTofu, direct use of the AWS-login credentials, and account administration
remain privileged bypasses. DynamoDB and IAM do not make an old script safe when
it runs with those credentials. The production runbook requires proof from
account factor state that a bypass target is compatible and records the reason.
