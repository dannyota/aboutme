# Replica topology and network trust

Status: Accepted under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md). The
[scaling index](README.md) records whether it is built.

> **Superseded for the first release.**
> [ADR 0037](../../adr/0037-single-host-production-without-hosted-uat.md)
> replaces the CloudFront, ALB, fleet and scheduled-UAT parts of this page with
> the [single-host design](../single-host-production.md). The page stays as the
> reference for a later fleet.

## 1. Desired topology

The unit of placement, admission, drain, replacement, and external fencing is
one EC2 instance. A node is eligible only after all three tasks below have the
same approved release tuple and runtime coordination reports the complete
replica ready. A partial or mixed replica never enters an ALB target group.

Production uses one ECS cluster backed by one Graviton launch-template ASG with
minimum 1, desired 1 or 2, maximum 2. No deployment may request a third node.
Three independent ECS EC2 DAEMON services converge one task of each service on
each admitted container instance. Each service uses a `memberOf` placement
constraint on an explicit `aboutme.app-node=true` ECS instance attribute. ECS
daemon tasks do not participate in ECS cluster-auto-scaling calculations, so ASG
policies own desired capacity:
<https://docs.aws.amazon.com/AmazonECS/latest/developerguide/ecs_service-options.html>

Launch template requirements:

- arm64 ECS-optimized AMI selected by a pinned SSM public-parameter reference;
  IMDSv2 required, hop limit 1, no SSH ingress, encrypted gp3 root volume, and
  DeleteOnTermination=true.
- No Elastic IP. The approved cost shape uses an auto-assigned public IPv4 and
  an internet gateway for image/control-plane egress. Replace the old fixed-EIP
  and fixed-origin-host rows identified in section 9.
- Node role is limited to ECS registration/control plane, read-only ECR pulls,
  log delivery, metrics, and its separately named runtime secret references.
- User data configures ECS, applies the instance attribute only after bootstrap
  succeeds, and contains no secret value. Failed bootstrap leaves the node
  ineligible and requests ASG replacement.
- Instance scale-in protection stays enabled until the three-task replica is
  ready. The lifecycle controller removes protection only for an admitted node.

## 2. Task boundaries

All images are immutable arm64 manifest digests. Tags are evidence labels only.
Tasks have `readonlyRootFilesystem=true`, `privileged=false`, no host devices,
no-new-privileges, all capabilities dropped, and explicit bounded writable tmpfs
or volumes. Each task has its own execution role, task role, secret set, UID,
GID, cgroup memory limit, and CPU reservation/limit. A role cannot fetch another
task's secrets.

- Caddy: UID/GID 10001; 128 MiB; 128 CPU units; host network; host TCP 443 and
  the node-local bridge-gateway API listener. Add only CAP_NET_BIND_SERVICE. It
  cannot invoke application or lifecycle AWS APIs.
- Go plus Chromium: UID/GID 10002; 512 MiB; 512 CPU units; host network; public
  API and private print redemption listen on loopback only. Chromium stays in
  this task's cgroup and receives no host namespace or device access.
- Nuxt: UID/GID 10003; 256 MiB; 256 CPU units; bridge network with a fixed
  node-local host port. Its task security group surface is empty because bridge
  mode is controlled by the node security group and host firewall/route rules.
- Ops/bootstrap jobs: UID/GID 10004; 256 MiB; 256 CPU units; a separate task
  definition, role, secrets, and writable paths. It is not a fourth daemon.

Caddy and Go communicate only over loopback. Caddy and Go reach the paired Nuxt
task only through the fixed node-local bridge port. Nuxt reaches only Caddy's
API-only bridge-gateway listener. Private print redemption is Caddy-to-Go over
loopback and has no ALB, CloudFront, Nuxt, or cross-node route. Cloud Map and
multi-node service discovery are forbidden because they can break pairing.

Readiness verifies the exact release tuple, all paired local listeners, runtime
coordination, database readiness, and the runtime owner's safety predicates. The
ALB health endpoint opens only after that aggregate succeeds. It closes before
drain. Exact endpoint names, payloads, and deadlines come from the runtime
contract; infrastructure must not infer them from process health.

## 3. Public TLS and origin authentication

Viewer traffic is Cloudflare DNS to CloudFront HTTPS, then CloudFront HTTPS to
an ALB, then ALB HTTPS to Caddy. The CloudFront origin is a dedicated public DNS
name such as origin-uat.aboutme.vn that aliases the ALB and appears on the ACM
certificate. CloudFront validates its origin certificate and hostname:
<https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/cnames-and-https-requirements.html>

The ALB does not validate a target certificate. HTTPS to Caddy encrypts the hop
but supplies no target identity claim:
<https://docs.aws.amazon.com/elasticloadbalancing/latest/application/load-balancer-target-groups.html>

The ALB security group accepts 443 only from the AWS-managed CloudFront
origin-facing prefix list. The node security group accepts 443 only from the ALB
security group. The listener default is a fixed deny response. Forwarding
requires the current or next origin-secret value during bounded rotation. Caddy
repeats an exact-one-value secret check before routing. Health checks use a
separate exact path and source and do not open application routes.

Use native pinned Caddy 2.11.4 CEL. Its request-header placeholder joins all
physical values with commas. Exact membership of the joined origin-secret
placeholder in the current/next set therefore rejects duplicate and list forms;
fixture secrets and generated live secrets must exclude commas. The permissive
header matcher is forbidden because it accepts any matching member. Evidence:
[Caddy header experiment](../../research/caddy-header-boundary/README.md) and
pinned source:
<https://github.com/caddyserver/caddy/blob/v2.11.4/modules/caddyhttp/replacer.go#L66-L73>
<https://caddyserver.com/docs/caddyfile/matchers#header>

## 4. Canonical client address

Choose the edge-stamped custom-header chain over exact-two X-Forwarded-For. It
has fewer trust-dependent list positions and makes ALB XFF irrelevant to
identity. The already-priced viewer-request CloudFront Function assigns the
entire lowercase event entry on every request:

request.headers['x-aboutme-client-ip'] = {value: event.viewer.ip};

CloudFront documents `event.viewer.ip`, mutable request headers, lowercase
header keys, and a one-field header object. X-Aboutme-Client-IP is neither a
disallowed nor viewer-request read-only header:
<https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/functions-event-structure.html>
<https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/function-code-choose-purpose.html>
<https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/edge-function-restrictions-all.html>

Assignment replaces a viewer-supplied entry before cache lookup. The cache
policy must exclude this per-viewer value from its cache key. The origin-request
policy explicitly forwards X-Aboutme-Client-IP and does not grant authority to
X-Forwarded-For. A CloudFront Function unit fixture and hosted request capture
must prove overwrite and one forwarded value before activation.

Caddy configures server-level `trusted_proxies` to the exact private ALB subnet
CIDRs and `client_ip_headers X-Aboutme-Client-IP`. The node security group is
the independent proof that the socket peer is an ALB. Caddy's native parser uses
the custom header only for a trusted immediate peer and produces `{client_ip}`:
<https://caddyserver.com/docs/caddyfile/options#client-ip-headers>

Before proxying, Caddy deletes inbound X-Real-IP and all other app-recognized
client-address headers, then sets the Go service's sole canonical X-Real-IP to
`{client_ip}`. Go trusts that header only from its loopback Caddy peer and never
parses X-Forwarded-For. Caddy must reject absent, malformed, list-valued, or
duplicate X-Aboutme-Client-IP. Native CEL requires the joined placeholder to be
nonempty, contain no comma, equal `{client_ip}`, and arrive from the trusted
immediate peer. The isolated raw-HTTP experiment passed 21/21 cases; the
permissive matcher failed 17/21. This proves feasibility, not the full chain.
The versioned harness must add IPv6 variants, mixed-case fields, untrusted peer,
actual upstream output, and hosted CloudFront/ALB behavior. Current evidence
does not justify a custom module.

Tests cover viewer spoofing of the custom header, XFF, X-Real-IP, duplicates,
comma lists, whitespace, invalid IPv4/IPv6, direct-ALB attempts, wrong/duplicate
origin secrets, current/next rotation, IPv4, IPv6, and cache hit/miss behavior.

## 5. Production scale-out and scale-in

Scale-out changes ASG desired capacity from 1 to 2. The new node remains
protected and outside the target group until the complete replica is ready. A
bounded readiness failure marks the instance unhealthy through ASG and waits for
replacement. Existing capacity remains serving. With max 2, deployment is
one-at-a-time within current capacity: drain and terminate one old node, wait
for its replacement to become ready, then repeat. Availability is reduced while
replacing a one-node fleet; this follows the fixed no-third-node decision.

The controller selects the exact active replica and calls the runtime-owned
`prepare_scale_in(expected_generation, replica_id, instance_id)`. It closes new
admission for that replica without marking it terminating, then drains to an
immutable `left` receipt after all owned work joins. If drain is missing or
ambiguous, call `begin_replica_termination` for the same tuple; this is the
abnormal path that keeps claims charged pending external proof. Only then call
`TerminateInstanceInAutoScalingGroup` for that exact instance with
`ShouldDecrementDesiredCapacity=true`. Do not lower desired first: ordinary
scale-in may select another instance. The explicit ASG operation fires the
termination hook and respects ASG protections:
<https://docs.aws.amazon.com/autoscaling/ec2/APIReference/API_TerminateInstanceInAutoScalingGroup.html>
<https://docs.aws.amazon.com/autoscaling/ec2/userguide/lifecycle-hooks.html>

Before the ASG call, the controller closes readiness, deregisters the exact
target, and waits the target-group delay. The hook therefore identifies and
holds an already-drained or abnormal exact instance; it does not begin a normal
runtime drain after state became terminating. Termination always proceeds. The
controller records exact EC2 termination proof for every physical removal as
audit. Proof may release claims only for missing/ambiguous `left`; it never
changes a graceful `left` incarnation to fenced. Then
`finish_scale_in(expected_generation, receipt)` atomically sets runtime desired
count and disables the same capacity/rate partition. A stale generation cannot
activate, terminate, or reassign a partition.

The lifecycle hook has a bounded heartbeat extension and `DefaultResult` of
CONTINUE. Both ABANDON and CONTINUE terminate an instance; ABANDON merely skips
remaining actions. The controller always calls CompleteLifecycleAction with
CONTINUE when done and allows timeout to CONTINUE. It never asks to reverse
Terminating:Wait or restore admission on that instance:
<https://docs.aws.amazon.com/autoscaling/ec2/userguide/adding-lifecycle-hooks.html>

After termination begins, an independent reconciler polls EC2 for the same
instance ID. Only `DescribeInstances` returning InstanceState=terminated can
authorize the lifecycle-fencing role to write the immutable `ec2_terminated_v1`
proof bound to instance ID, replica incarnation, release digest,
request/evidence ID, and observed time. ALB health, deregistration, ECS STOPPED,
advisory-lock/session loss, heartbeats, readiness, and elapsed TTL are
diagnostics only. Missing proof keeps the incarnation unresolved and shared
claims charged. Proof-write failure retries idempotently; it never changes the
result of a timed-out mutation. See runtime-coordination.md.

## 6-7. Managed controller and desired-state operations

The executable location, durable serialization, Fargate command roles, exact AWS
actions, OpenTofu ownership, retries, and price boundary are specified in
controller.md. That file is part of this contract.

## 8. UAT scheduled lifecycle

UAT starts disabled. ADR 0035 selects the controller services within the
approved USD 30 ceiling. The private repository approval feature remains
unresolved. A booking is allowed only after local state-machine proof and hosted
timing measurements support the existing USD 30/month ceiling.

During a booked 10- or 14-calendar-day campaign, RDS remains available through
the campaign and at least 24 hours after the last accepted writer. The ALB and
CloudFront origin route exist only for booked test windows. Between tests, drain
and externally fence nodes, set ASG min/desired/max to 0, and remove the ALB,
target group, listener, and origin DNS route through a reviewed OpenTofu destroy
plan generated and closed-policy validated at that transition. Retained
CloudFront must return the disabled response and have no live origin route.

Hourly operations during the campaign launch one brief complete-replica node
with no ALB, run only after RDS and runtime coordination are ready, obtain the
runtime-owned operation receipt, then drain, terminate, and externally fence it.
The practical sensitivity assumes 10 minutes per hourly node window. That is a
measurement target, not a startup or completion guarantee.

After campaign end plus the 24-hour accepted-writer tail, shutdown is exactly:

1. The no-ALB maintenance replica takes the exclusive DB barrier, proves all
   categories empty/not due, sets its irreversible local admission latch, and
   atomically commits a quiescence snapshot plus graceful `left` receipt.
2. Controller terminates that exact EC2 node through ASG, fence-proof Fargate
   repeats exact terminated observation and records audit proof, and lifecycle-
   command Fargate finishes UAT capacity/partition shutdown. These writes
   necessarily invalidate the preparation snapshot generation.
3. Lifecycle-command Fargate calls `finalize_stop_receipt`. Under the exclusive
   barrier it recomputes every category, requires exact snapshot/left/proof,
   zero replicas/transitions/claims/leases/backlog, closed admission/allocation,
   and the accepted-writer tail. As final DB mutation it closes the durable
   write gate, advances generation once, and inserts the immutable final
   receipt.
4. No DB mutation follows. Controller may update S3 without advancing its
   controller generation, rereads receipt/gate/generation, then StopDBInstance.

The receipt records every category watermark/next due and authorizes stop only
while its final generation and closed gate match. Missing, stale, ambiguous,
overdue, or changed proof keeps RDS available and alerts. The trigger/barrier
coverage and category contract are
[the UAT lifecycle contract](uat-lifecycle.md).

The hourly controller heartbeat remains. It may suppress RDS/node work only with
the immutable final receipt. Wake lead is measured start bound plus the
30-minute command bound plus 5-minute margin. Missing timing or complete-list
proof disables suppression. Weekly orphan due always wakes RDS and one complete
maintenance replica. No deadline is rounded past or waived.

Stopped RDS is explicitly resumed before seven days, because RDS automatically
starts a stopped instance after seven days. Storage and backups still bill:
<https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_StopInstance.html>

Future maintenance uses StartDBInstance, then lifecycle-command `begin_wake`
under the exclusive barrier invalidates the receipt before opening the gate.
After reconciliation it launches one no-ALB complete replica, runs due work, and
repeats the four-stage shutdown above. AWS bills RDS starts with a 10-minute
minimum and EC2 with a 60-second minimum, but these minima do not prove
duration:
<https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/User_DBInstanceBilling.html>
<https://aws.amazon.com/ec2/pricing/on-demand/>

The selected initial 2-day/16-hour gross envelope is USD 27.636091 with no
allowances. Controller share is USD 2.101179 and includes the task/request
counts in controller.md. It omits measured distributions for startup, readiness,
jobs, drain/fence, and recovery. It cannot guarantee USD 30. Reduce optional
booked test hours first; never reduce maintenance or privacy cadence.

## 9. Failure and restoration rules

- Start/describe throttling or timeout: exponential jitter within a persisted
  attempt/deadline budget. Do not start a second generation.
- RDS does not become available: keep public disabled and terminate nodes. Do
  not loop node replacement because RDS is unavailable. Proof writes and
  database cleanup stay pending until managed control starts RDS; retain
  evidence, alert, and do not claim the booked window.
- One daemon task fails or releases differ: close admission, deregister, request
  whole-node replacement, and await exact EC2 termination proof.
- Drain or deregistration misses its deadline: continue termination. Keep the
  incarnation unresolved until proof. Never restore admission on that node.
- Lifecycle notification/controller failure: hook timeout CONTINUE terminates;
  the independent reconciler discovers the exact instance and records proof.
- EC2 describe is unavailable or ambiguous: no proof and no reclamation. Retry
  and alert. Never substitute TTL, ECS, ALB, or database-session observations.
- Proof insert conflict: accept only a byte-for-byte idempotent existing proof;
  conflicting evidence is terminal/manual and keeps safety fences closed.
- UAT stop proof invalidates before StopDBInstance: leave RDS available. If stop
  has begun, resume at the earliest deadline, run recovery, and alert.
- Restoration orders database available, coordination/migrations, complete
  replicas, aggregate readiness, target registration, origin secret/rule,
  CloudFront route, then public probe. Any failure unwinds public admission.

All time bounds must be named configuration with monotonic local deadlines and
persisted wall-clock audit times. Implementation must set them from measured
runtime-owner contracts. This draft intentionally does not invent values.

## 10. Ownership and rows requiring rewrite

Public aboutme owns Dockerfiles/build scripts, digest/architecture assertions,
Caddy source/config/module and hostile-header tests, task runtime contract,
local lifecycle fakes, OpenAPI only if the runtime owner exposes a public API,
and portable runbooks. Public CI receives no account ID, private state, secret,
origin hostname, Cloudflare credential, or personal data.

Planned private aboutme-infra owns OpenTofu root/environment composition,
backend state, AWS/Cloudflare providers, launch template, ASG/hooks/policies,
ECS services/task definitions, ALB/CloudFront/ACM/DNS, RDS scheduled lifecycle,
IAM bindings, secret ARNs, deploy/controller automation, account-specific tests,
cost alarms, and hosted evidence. No private repository or feature purchase has
been approved; integration must record that decision before creating it.

Rewrite these accepted-plan concepts during integration:

- deployment.md rows that assign a stable EIP/fixed-host origin to each replica:
  replace with ALB-to-ASG target registration and ephemeral node public IPv4.
- infrastructure decisions/contracts/task 05 rows that describe a single
  multi-container ECS task or distinctInstance spread: replace with three DAEMON
  services plus exact complete-replica readiness.
- edge/task 07 client-IP rows that grant XFF position authority: replace with
  CloudFront viewer-request overwrite of X-Aboutme-Client-IP and Caddy boundary.
- ADR 0034/task 10.18 lifecycle wording implying ABANDON cancels termination or
  ALB/ECS/session state proves death: replace with CONTINUE and exact terminated
  instance proof.
- UAT cost/lifecycle rows that stop RDS after each test window without the
  campaign/tail/deadline proof: replace with section 8's conditional lifecycle.

ADR text wins where accepted. If a required rewrite contradicts an ADR rather
than correcting stale explanatory text, integration must author a superseding
ADR before implementation.
