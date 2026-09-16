# Managed lifecycle controller

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
[scaling index](README.md) records whether it is built.

## 6. Managed controller execution and trust boundary

Select EventBridge Scheduler plus one Step Functions Standard state machine as
the off-environment control plane. Both are managed regional services and
survive ASG zero, RDS stopped, node loss, and removal of the UAT ALB. Standard
workflows persist waits and history, have exactly-once execution semantics, and
call AWS APIs through service integrations:
<https://docs.aws.amazon.com/step-functions/latest/dg/choosing-workflow-type.html>
<https://docs.aws.amazon.com/step-functions/latest/dg/supported-services-awssdk.html>

Do not use Lambda Durable Functions here. The work is mainly AWS service calls,
and the state-machine definition is the reviewable private-repository contract.
Do not add a NAT gateway, VPC endpoint, always-on instance, RDS Proxy, DynamoDB
lock, or other control-state store.

One disabled-by-default Scheduler schedule starts the exact state-machine alias
for each reviewed UAT transition. Its target role has `states:StartExecution` on
that alias and the exact DLQ permission below. Delivery retry has a fixed
maximum age and attempt count smaller than the earliest safety deadline;
exhaustion goes to the approved operations SQS dead-letter queue and alarm. Set
maximum age to 15 minutes and maximum retries to 3; the state machine still
deduplicates a late success. Scheduler supports one-time schedules, retry, and a
dead-letter queue, but delivery is still treated as duplicable:
<https://docs.aws.amazon.com/step-functions/latest/dg/using-eventbridge-scheduler.html>

The Scheduler execution role also has `sqs:SendMessage` on exactly its dead-
letter queue because Scheduler, not the state-machine role, writes exhausted
deliveries. Set `SqsManagedSseEnabled=true`; no customer KMS permission is
needed. Same-account access is granted only through that exact execution-role
policy; do not add a public or cross-account queue grant.

The existing private versioned OpenTofu state S3 bucket also holds one small
control object per environment under a separate protected prefix. This is the
off-RDS authority, not a new service or runtime store. Bootstrap alone creates
it with PutObject `If-None-Match:*`. Runtime roles have no DeleteObject, and the
current version has no expiration rule; a missing object fails closed because a
delete marker would otherwise let If-None-Match create again. Every controller
write uses `If-Match:<ETag>`. Bucket policy rejects unconditional writes. The
object contains a strictly increasing generation, owner execution ARN, requested
state, exact resource IDs, and phase. A generation change ensures a new
body/ETag even when desired state repeats. S3 conditional writes reject
concurrent winners:
<https://docs.aws.amazon.com/AmazonS3/latest/userguide/conditional-writes.html>

On PutObject 412, another writer won: reread and wait. On 409, reread; retry
only if the same owner/generation still holds. On timeout or any ambiguous PUT,
reread and accept success only when owner, generation, phase, and expected body
digest all match. No child or AWS mutation starts from an assumed PUT result.

No lease expiry permits takeover. Scheduled, manual, production autoscale, and
redrive executions wait on the same object. Before every ECS RunTask, the owner
writes a `launching` ledger item with command kind and a unique `startedBy`
token. After RunTask it CAS-adds task ARN, task-definition digest, launch type,
and status. An ambiguous RunTask is reconciled with ListTasks by that exact
`startedBy` before any retry. No unledgered retry is allowed.

Recovery calls StopExecution on the prior Standard execution and observes it
terminal. Cancellation of a `.sync` task is best-effort and does not prove the
child stopped:
<https://docs.aws.amazon.com/step-functions/latest/dg/service-integration-iam-templates.html>
The recovery execution lists both ledger ARNs and every task with the prior
execution's `startedBy` tokens, calls StopTask on nonterminal children, and
polls DescribeTasks until each is STOPPED. Missing or ambiguous task observation
blocks transfer without a TTL. It then describes/reconciles every AWS resource
and DB receipt before an If-Match transfer. Proof and maintenance commands are
transaction/receipt idempotent. Interrupted OpenTofu is reconciled from remote
state before a new plan. Only after all child processes are joined may ownership
transfer.

An in-flight stale exact-instance termination can only terminate its old
instance ID; the successor restores desired capacity after reconciliation.
Convergent start, stop, registration, and desired-state calls are described
before retry. The workflow rechecks owner ARN and ETag before every non-read AWS
call. Runtime expected-generation CAS is a second boundary when RDS is up.

The private booking plan also rejects overlapping windows and assigns
`<environment>-<generation>-<transition>` execution names. Only the protected
deploy role may create/update schedules or start manual recovery. Scheduler and
autoscale event roles can start only the exact state-machine alias with bounded
schema input; none can edit the control object directly.

Step Functions holds only resource identifiers, generation, attempt counters,
deadlines, and receipt IDs. It holds no secret, personal data, body, or database
credential. Every mutating SDK task is followed by describe until the intended
exact state appears. A timeout or ambiguous SDK result returns to describe; it
never blindly repeats a mutation. Retry lists name throttling and transient
errors. Use 2-second initial delay, factor 2, full jitter, 30-second maximum
delay, and 6 attempts. After an ambiguous mutation, describe every 15 seconds
until its operation deadline. Failure finishes closed plus alert. Catch-all
retry is forbidden. Redrive rereads every resource and the runtime generation
before mutation.

The state-machine role owns only the exact tagged ASG, ECS cluster/services, RDS
instance, S3 controller object, and approved task definitions. It has the
actions below plus `ecs:RunTask`/`ecs:StopTask` on that cluster/task families,
`iam:PassRole` on the exact task/execution roles with
`iam:PassedToService=ecs-tasks.amazonaws.com`. The `.sync` integration also
needs `ecs:DescribeTasks` and `events:PutRule`, `events:PutTargets`, and
`events:DescribeRule` on Step Functions' managed ECS events rule. It cannot read
secrets, connect to the database, write proof, mutate schedules, pass any other
role, or run another task family.

Lifecycle-command and fence-proof run as independent arm64 ECS Fargate tasks in
a public application subnet with AssignPublicIp=ENABLED and no ingress rule. Its
private ENI address reaches RDS; its public address permits ECR/control-plane
bootstrap through the internet gateway. No ALB, NAT, endpoint, or EC2 node is
required. Fargate requires 0.25 vCPU and 512 MiB, so each has a distinct task
definition and priced invocation. Step Functions uses `ecs:runTask.sync` and
rejects HTTP success with Failures:
<https://docs.aws.amazon.com/step-functions/latest/dg/connect-ecs.html>

Maintenance remains the existing 256 MiB one-shot EC2 ops task placed only on
the selected complete replica. Mail/media claims and failure bind to that
incarnation; ambiguous leave needs exact node termination proof. Lifecycle and
proof Fargate tasks own no app/external-work claim. `lifecycle-command` may
EXECUTE only the runtime prepare/activate/begin/finish/final-stop/wake
functions. `fence-proof` may EXECUTE only record_ec2_termination. Maintenance
preserves the current generated SQL with exact table/column
SELECT/INSERT/UPDATE/DELETE grants listed in
[the UAT lifecycle contract](uat-lifecycle.md); it gets EXECUTE only for new
incarnation, claim, graceful-leave, run-result, and quiescence functions. It has
no broad app or lifecycle grant.

Each has a distinct task definition, execution role, credential, and database
login. Cluster logins are provisioned once outside goose; migrations grant
per-database functions/objects, so rolling back aboutme cannot drop roles used
by aboutme_dev. Credentials are existing SSM Parameter Store standard
SecureString parameters under the retained KMS key. ECS `secrets.valueFrom`
resolves them at task start. Each execution role has `ssm:GetParameters` only
for its parameter and `kms:Decrypt` only for the retained key, constrained by
ViaService for SSM in ap-southeast-1 and the exact parameter encryption context.
No Secrets Manager resource is introduced. Secret values never enter workflow
history or arguments.

Task roles have log delivery only, except fence-proof's read-only
DescribeInstances and infra-apply's narrow provider actions defined below.

Step Functions observes termination to decide when to run the proof task, but
passes identifiers only. The fence-proof task itself calls
`ec2:DescribeInstances` immediately before its database transaction and requires
one result for the exact instance ID with state `terminated`. Its task role has
only that read plus logs; `ec2:DescribeInstances` requires Resource `*`, bounded
by `aws:RequestedRegion=ap-southeast-1`. It has no application or AWS control
mutation. The database function separately matches the durable intent. Stale or
forged task input cannot pass both checks. A timeout or lost ECS result is
ambiguous, so the workflow reruns the same digest and evidence ID; only a
byte-identical existing proof succeeds idempotently. Termination observation
polls every 30 seconds for 30 minutes, then every 15 minutes while leaving the
Standard execution alive and alarmed. Elapsed time never becomes proof. Before
workflow history expires, an operator-reviewed successor uses the same evidence
ID and describes first.

Fargate proof has no replica incarnation and creates no recursive node-fencing
dependency. Every EC2 removal, including normal hourly shutdown, waits for exact
terminated observation and writes the audit proof before the lifecycle
generation completes. A graceful `left` receipt permits release before proof;
missing/ambiguous `left` permits release only after proof. Exceeding the RDS or
Fargate start bound keeps public state closed and alerts; an ordinary RDS outage
does not trigger an application replacement loop.

A private-subnet Lambda writer was rejected for this selected small RDS shape.
It avoids the temporary node, but either needs NAT/endpoints to resolve a
password secret or requires RDS IAM authentication. AWS says IAM database auth
needs 300–1,000 MiB extra database memory, which is not in the selected RDS
budget and could force a larger paid class:
<https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/UsingWithRDS.IAMDBAuth.html>
<https://docs.aws.amazon.com/lambda/latest/dg/configuration-vpc-internet.html>

The selected gross planning envelope includes up to 500 lifecycle/proof Fargate
tasks at 5 minutes and 0.25 vCPU/0.5 GiB, 10 infra applies at 10 minutes and 0.5
vCPU/1 GiB, 50,000 Step Functions transitions, 10,000 S3 PUT-equivalent
requests, 10,000 retained-KMS calls, and four node/four RDS recovery hours.
Durations are forecast inputs, not completion guarantees:
<https://aws.amazon.com/step-functions/pricing/>

The 2026-09-06 AWS public Price List file for AmazonStates in ap-southeast-1
quotes USD 0.000025 per Standard state transition. Reserve and alarm at 50,000:
USD 1.25 gross. The controller total is USD 2.101179 gross. The selected initial
2-day/16-hour UAT envelope is USD 27.636091 gross with no allowances, against a
USD 30 forecast and USD 25 actual alert. Source:
[UAT lifecycle model](../../research/aws-cost/uat-lifecycle.md) and
[generated result](../../research/aws-cost/uat-lifecycle-results.csv). These are
planning bounds pending hosted measurements and do not approve extra task count
or duration.

ADR 0035 accepts this controller within the existing USD 30 ceiling. Local
checks and measured hosting assumptions still precede activation. The estimate
grants no budget increase.

## 7. Controller desired states and AWS calls

The durable controller record is one generation with desired state, exact
resource IDs, deadlines, attempts, last AWS observations, runtime receipt IDs,
and outcome. One compare-and-swap owner executes a generation. Retries reuse
idempotency identifiers and reread actual state before each mutation.

Production SERVING_1 or SERVING_2:

1. `rds:DescribeDBInstances`; require available. Run lifecycle-command
   `prepare_scale_out(expected_generation, desired)` and require its receipt.
2. `autoscaling:UpdateAutoScalingGroup` to fixed min=1/max=2 and desired=1 or 2;
   never update desired above 2.
3. `ecs:DescribeContainerInstances`, `ecs:ListTasks`, `ecs:DescribeTasks`, and
   `elasticloadbalancing:DescribeTargetHealth` until each candidate passes the
   runtime-owned complete-replica readiness receipt.
4. Run `activate_replica_capacity(expected_generation, exact trio)`; it
   atomically activates the replica and its rate partition. Then call
   `autoscaling:SetInstanceProtection(false)` for that exact ready node.

Replacement or scale-in:

1. Follow topology-lifecycle section 5: prepare/drain to `left`, or record the
   abnormal begin-termination receipt for a missing/ambiguous leave.
2. `elasticloadbalancing:DeregisterTargets` for exact instance and port;
   `DescribeTargetHealth` until unused or the fixed deadline.
3. `autoscaling:SetInstanceProtection(false)` and `SetInstanceHealth` unhealthy
   for a failed node. Normal scale-in calls
   `TerminateInstanceInAutoScalingGroup(exact ID,true)` only; it never lowers
   desired capacity generically.
4. Lifecycle EventBridge delivery permits `RecordLifecycleActionHeartbeat`, then
   `CompleteLifecycleAction(CONTINUE)`. Duplicate delivery is harmless.
5. `ec2:DescribeInstances` until exact terminated state. Then run the proof
   task, which repeats that observation immediately before writing. There is no
   `ec2:TerminateInstances` grant.

IAM policy tests classify every action. Mutations use exact supported ARNs and
documented service condition keys: ASG ARN; RDS DB ARN; target-group ARN; S3
control/plan object ARN; exact ECS cluster/task-definition plus cluster
condition; exact PassRole ARNs plus PassedToService. Resource `*` is permitted
only for this closed controller/proof read allowlist where the action has no
resource-level ARN: `ec2:DescribeInstances`,
`autoscaling:DescribeAutoScalingGroups`, and
`autoscaling:DescribeScalingActivities`. ECS ListTasks is scoped to the exact
cluster ARN; DescribeTasks to task ARNs plus cluster; DescribeContainerInstances
to container-instance ARNs plus cluster; RDS DescribeDBInstances to the DB ARN;
ELB DescribeTargetHealth to the target-group ARN; Step Functions
DescribeExecution/StopExecution to the prior execution ARN; S3 Get/Head/Put to
the exact environment control and artifact prefixes. Every wildcard read has
`aws:RequestedRegion=ap-southeast-1`. Tests use the AWS service-authorization
tables to fail an unlisted wildcard, wildcard mutation, unsupported condition,
or missing region bound. The infra provider role has its own generated policy
and is not covered by this controller read allowlist.

OpenTofu remains the sole writer for the ephemeral ALB, listener, target group,
CloudFront origin settings, and Cloudflare origin DNS. A saved plan is never
reused across state serials. The controller runs the reviewed planning code at
each on/off transition, validates the fresh plan against a closed policy, then
immediately applies that exact plan under both the controller object and
OpenTofu S3 lock. Routine cycles need no new per-run human approval. Changing
the allowed resource/action schema requires reviewed controller/config changes
and, because this replaces ADR 0034's private-workflow execution ownership, a
superseding ADR before dispatch.

The infra runner uses the existing fourth public `ops` image, extended in Task
10.8 with the pinned OpenTofu binary and a generic fail-closed plan wrapper. The
public image contains no private HCL, backend, account ID, hostname, state,
provider credential, or Cloudflare value. At runtime a distinct Fargate task
definition fetches the digest-pinned private config bundle and provider lock
from the encrypted private artifact prefix. The execution role resolves its
dedicated Cloudflare credential; the task role owns only exact backend objects
and the provider actions admitted by policy. It has no database login.

The wrapper runs `tofu init -lockfile=readonly`, creates a fresh saved plan,
checks backend lineage/serial, converts it to JSON, and rejects anything outside
the exact environment and transition. ON may create only one ALB, listener,
target group, rules, target attachments, and origin DNS/config values; OFF may
only delete those same ephemeral objects and restore the fixed disabled origin
state. Both reject IAM, role/policy, KMS, RDS, ASG, ECS service/task definition,
state backend, provider, provisioner, local-exec, external data source, unknown
address, replacement outside the allowlist, and secret output. The wrapper
applies only that in-process plan file and emits its plan hash, prior/new
serial, and bounded resource-address receipt. Any validation, drift, lock, or
apply ambiguity leaves public admission closed; recovery first joins the task
and reconciles remote state. No scheduled private Actions job exists.

Plan JSON unknowns use a closed leaf allowlist. For each resource change,
`after_unknown` may be true only at these provider-computed-only leaves:

- `aws_lb`: `id`, `arn`, `arn_suffix`, `dns_name`, `zone_id`.
- `aws_lb_listener`: `id`, `arn`.
- `aws_lb_target_group`: `id`, `arn`, `arn_suffix`, `load_balancer_arns`.
- `aws_lb_listener_rule`: `id`, `arn`.
- `aws_lb_target_group_attachment`: `id`.
- `cloudflare_dns_record`: `id`, `created_on`, `modified_on`.
- existing `aws_cloudfront_distribution` update only: `etag`, `id`,
  `in_progress_validation_batches`, `last_modified_time`, `status`, and
  `hosted_zone_id`.

The pinned provider schema must mark each allowed leaf Computed=true and
Optional=false; otherwise reject it despite this list. Reject unknown resource
address, provider, action, count/for_each identity, any whole object/list/map,
or any other leaf. In particular, reject unknown security groups, subnets,
scheme/internal flag, IP type, ports, protocols, TLS policy/certificate, target
type, health check, listener action/condition/priority, origin domain,
custom-header name/value, cache/origin policy ID, DNS name/type/proxy/TTL,
deletion protection, tags, and all sensitive markers.

Known values must equal the reviewed environment contract, and
`before_sensitive`/`after_sensitive` shape cannot change. Creation may expose
only the computed leaves above; all security and trust inputs must be known
before apply. Unit fixtures flip each allowed leaf to unknown successfully and
each forbidden leaf/address/action to unknown unsuccessfully. A captured pinned
provider-schema fixture and create/update/delete plan fixtures are mandatory;
provider upgrade blocks until this matrix is regenerated and reviewed.

Permissions are split: the protected deploy role changes persistent OpenTofu and
controller code; infra-apply changes only policy-approved ephemeral resources;
runtime app role owns app data; lifecycle-command, maintenance, and fence-proof
have the three DB contracts above; state-machine role owns control calls and
exact task launch; Scheduler only starts and dead-letters. IAM binds
environment, cluster, ASG, target group, and resource tags. PassRole is limited
to the exact four command/infra task and execution-role pairs. Secret reads name
exact ARNs and roles.
