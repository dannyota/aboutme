# Infrastructure implementation tasks

Status: Planned, blocked on local runtime proof under
[ADR 0035](../../../adr/0035-replica-coordination-and-uat-lifecycle.md). Root
assigns exclusive paths before each task. Infrastructure wiring waits for the
local runtime proof.

These are bounded author slices after the runtime interface and private-repo
decision are frozen. They are not assignments or approval to implement.

Existing task-ID ownership remains: 10.1 owns state-bucket prefixes and backend;
10.2 owns public-subnet Fargate networking; 10.3 owns RDS/SG settings; 10.4 owns
the command/infra role pairs, three DB credentials, and grants; 10.5 owns ASG,
daemon placement, exact termination, and command task definitions; 10.6 owns ALB
and CloudFront OpenTofu; 10.7 owns Caddy trust; 10.10 owns maintenance commands;
10.8 owns the public ops image and generic pinned OpenTofu wrapper; 10.12 owns
private config, plan policy/apply, and lifecycle orchestration; 10.14 owns
hosted scaling/failure proof; 10.15 owns booking, activation, cost, and cleanup;
10.18 owns runtime receipts, state machine contract, and local safety proof.
Changes crossing these files stay root-owned until serialized.

## 1. Integrate accepted design

Owned tracked paths: docs/design/deployment.md, docs/design/budgets.md,
docs/adr/0034-scheduled-uat-and-production-autoscaling.md or one superseding
ADR, docs/plans/phase-10/task-18-replica-safety-and-scaling-contract.md, and
relevant traceability rows. Resolve every section 9 rewrite and name the runtime
contract version. Keep the private approval feature explicitly unresolved.

Root-owned shared files: phase plan, traceability index, ADR index, and any
generated documentation index. Integration owner edits and stages these.

Required local verification after implementation:

```sh
npx prettier --check --ignore-path /dev/null <owned-markdown-paths>
npx markdownlint-cli2 <owned-markdown-paths>
```

## 2. Public image and Caddy boundary

Owned paths: deploy/caddy/**; relevant Dockerfiles under deploy/ or apps/**;
native CEL boundary fixtures and unit tests; deploy/README.md; task-definition
contract fixtures in a new deploy/aws-contract/** directory if the accepted
ownership decision keeps portable fixtures public.

Implement arm64 digest assertions, numeric UIDs, read-only roots, tmpfs/volume
ownership, caps, host/bridge listeners, pinned native CEL exact-one
origin-secret and client-IP validation, custom-header client parsing, canonical
X-Real-IP emission, and paired-route isolation. Preserve route-table generation
source.

Failing-first tests must cover duplicate/list headers, current/next secret,
viewer spoof attempts, IPv4/IPv6 canonicalization, untrusted socket peer,
loopback-only Go/print routes, bridge-only Nuxt/Caddy route, mixed releases, and
missing peers. Preserve the 21-case native proof and extend it to the full
versioned route harness; do not add a Caddy module without new failing evidence.

Required local verification after implementation:

caddy validate --config deploy/caddy/Caddyfile --adapter caddyfile make
route-table-test make p5a-native-http-check make dev-https-transport-check make
dev-https-public-check

Root-owned shared paths: Makefile, .tool-versions, dependency manifests/locks,
generated route snapshot, and container build workflow.

## 3. Runtime coordination contract

Runtime author owns apps/server migrations, sqlc source/generated files,
coordination packages, readiness/drain implementation, connection allocation,
and its implementation contract. Topology consumes the frozen typed adapter for
join/ready/drain receipts, quiescence plus per-category next-due receipts, and
external fence-proof insert. It must not add HTTP endpoints or database tables
to satisfy infrastructure guesses.

Required topology integration tests use runtime fakes for: unacked transition
fails before business mutation; session loss closes local admission but proves
no death; SIGKILL needs external proof; stale/replayed instance proof rejected;
quiescence proof invalidated by a writer; and exact next due prevents late wake.

Root-owned shared paths: migrations/schema head, sqlc generated snapshot,
OpenAPI and generated clients if any public contract changes, Makefile, go.work
and go.work.sum.

## 4. Private base topology

Proposed private paths:

modules/compute/**modules/ecs-replica/** modules/edge/**modules/iam/**
environments/{uat,production}/**
tests/contracts/{launch-template,task-boundary,placement,edge}/**

Implement the launch template and SGs, three DAEMON services, one ops task,
digest-only task definitions, dedicated roles/secrets, min1/max2/no-surge ASG,
termination hook CONTINUE, complete-replica target registration, CloudFront
Function custom-header overwrite, origin request/cache policies, ACM/ALB HTTPS,
fixed deny and rotating-secret rules, and disabled-before-activation defaults.

Static tests assert no EIP, no NAT, no third node, no awsvpc task, no public Go/
Nuxt/print listener, no cross-task secret grant, no wildcard PassRole, exact
memory/CPU/UID/caps/root settings, no mutable image tag, and no ALB target-cert
validation claim.

Non-cloud verification, not run:

```sh
tofu fmt -check -recursive
tofu validate
tflint --recursive
checkov -d .
conftest test <saved-tofu-plan-and-task-definitions>
```

Exact commands depend on the approved private repository toolchain. Plan and
state files must never be copied into the public repository.

## 5. Lifecycle and external fencing controller

Proposed private paths:

controllers/lifecycle/**modules/lifecycle/** tests/lifecycle/**
runbooks/replica-lifecycle.txt

Implement the Standard Step Functions generation state machine and only the AWS
calls listed in topology-lifecycle sections 6 and 7. Use ASG SetInstanceHealth
for failed nodes, exact `TerminateInstanceInAutoScalingGroup` with decrement for
normal scale-in, lifecycle CONTINUE, and an independent exact-instance EC2
termination reconciler. Split controller and proof-writer roles. Never grant
TerminateInstances or runtime proof mutation.

Fake-API tests cover duplicated/out-of-order EventBridge delivery, controller
crash at every mutation, throttling, hook timeout, drain timeout, deregistration
timeout, instance-ID mismatch/reuse, eventual terminated observation, proof
write retry/conflict, max-two invariant, and restoration order. Each mutation
must be replay-safe after a crash. Prove one scheduled execution per environment
generation, parallel instance observers cannot alter desired state, ambiguous
calls are described before retry, and RDS outage does not trigger node loops.

Keep maintenance as the existing one-shot EC2 ops task on the active complete
replica with the runtime contract's narrow existing-table/column SQL grants. Use
public-subnet Fargate for lifecycle-command and fence-proof, each with its own
SSM SecureString credential and EXECUTE-only new lifecycle functions. Proof
repeats exact terminated DescribeInstances immediately before its transaction.
Test exact digest, stale evidence, same-evidence replay, all removals audited,
and missing-left claims released only after proof. Static tests reject Lambda,
RDS IAM auth, NAT, endpoints, always-on instances, wildcard mutation, unlisted
wildcard read, secret-bearing workflow input, and proof-table DML. PassRole is
allowed only for exact command task/execution roles with PassedToService ECS.

Use conditional S3 writes in the existing private state bucket for one durable
environment owner/generation. Test overrun, duplicate, concurrent autoscale,
manual recovery, StopExecution transfer, redrive, stale ETag, and an in-flight
exact-instance call. No lease timeout can steal ownership.

Extend the public ops image with pinned OpenTofu and a generic policy wrapper;
keep private config outside the image. A separate infra-apply Fargate task makes
a fresh plan each transition, validates its JSON against the reviewed closed
resource/action policy, and immediately applies that file. Tests reject stale
serial/lineage, drift, provisioners/external execution, unknown resources,
controller direct ALB/DNS API calls, and scheduled private Actions use.

Non-cloud verification, not run:

```sh
go test -race ./controllers/lifecycle/... -count=1
conftest test <saved-IAM-and-lifecycle-plan-JSON>
```

Language and exact paths remain a private-repo owner decision.

## 6. UAT booking, maintenance, and cleanup controller

Proposed private paths:

controllers/uat-schedule/**modules/uat-schedule/** tests/uat-schedule/**
runbooks/uat-booking.txt

Implement explicit DISABLED, CAMPAIGN, TEST_WINDOW, CAMPAIGN_TAIL, STOPPED, and
MAINTENANCE states. CAMPAIGN keeps RDS available. TEST_WINDOW alone creates the
ALB/origin route. Hourly ops run a no-ALB node. STOPPED requires the atomic
runtime receipt. Schedule the earliest real category deadline and a less-than-
seven-day RDS guard. Missing proof keeps RDS available and alerts.

Tests use a fake clock and fake AWS/runtime adapters for 10/14-day calendars,
24-hour last-writer tail reset, DST/time-zone boundaries, every retention
category deadline, empty/no-empty queues, proof invalidation, RDS automatic-
start guard, partial ALB deletion, failed wake, delayed startup, and cleanup.
Assert optional test hours are the first budget reduction.

Cost check must run the saved pricing CSV and repository calculation script,
show maintenance separately from test hours, and reproduce the selected gross
2-day/16-hour USD 27.636091 total and USD 2.101179 controller component. A
passing calculation is not approval or a duration guarantee. Preserve the
verified gross inputs: 500 five-minute lifecycle/proof tasks at 0.25 vCPU/0.5
GiB, 10 ten-minute infra tasks at 0.5 vCPU/1 GiB, 50,000 Step Functions
transitions, 10,000 S3 PUT-equivalent and 10,000 retained-KMS calls, plus four
node/four RDS recovery hours. Assert controller USD 2.101179 and total
2-day/16-hour UAT USD 27.636091 with no allowance. Durations remain unproved.

Non-cloud verification, not run:

```sh
python3 -B docs/research/aws-cost/uat-lifecycle.py --check
go test ./controllers/uat-schedule/... -count=1
conftest test <saved-UAT-plan-JSON>
```

The [public cost model](../../../research/aws-cost/uat-lifecycle.md) owns saved
price provenance, inputs and generated results. Private plan fixtures remain
inside the infrastructure repository.

## 7. Hosted proof and cleanup

Only the integration owner runs these after local gates and explicit AWS and
Cloudflare authorization. Record resource IDs, timestamps, state transitions,
and redacted evidence; never record secret values or personal data.

- Prove Function viewer header overwrite on cache miss and hit, one origin
  value, viewer spoof rejection, ALB XFF irrelevance, direct-origin denial,
  origin secret rotation, CloudFront origin TLS validation, and ALB-to-Caddy
  encryption without claiming target-cert validation.
- Prove each node receives exactly one task from each DAEMON service, no task
  crosses pairing routes, mixed/partial release remains unregistered, and the
  second node becomes ready without desired/max exceeding two.
- Inject graceful, hung, SIGKILL, ECS-control-plane, database-session, and EC2
  observation failures. Show only exact EC2 terminated state writes proof and
  that unresolved incarnations block reclamation.
- Measure RDS start, EC2 launch, ECS convergence, readiness, jobs, drain,
  deregistration, termination, proof, and cleanup distributions. Recompute the
  forecast before booking. Do not shorten privacy cadence to fit the budget.
- End with public disabled; ASG 0 for UAT; no UAT instance, ENI, EBS volume,
  public IPv4, ALB, target group, listener, origin DNS, or stale secret rule;
  RDS stopped only with valid proof; retained storage/logs/alarms inventoried.

## 8. Phase gates

The integration owner runs all gates at one unchanged candidate commit after
Phase 9 merge and after hosted cleanup:

make docs-fmt make ci make scan

Also run the approved public Caddy/image checks, private tofu/static/controller
checks, hosted contract harness, cost recalculation, and the Phase 10 exit list.
Any failing gate or changed candidate invalidates the evidence.

## Remaining implementation inputs and proof

- Freeze typed runtime interfaces after local implementation passes.
- Create the private infrastructure repository and resolve its approval feature
  within the existing no-paid-plan constraint before private workflow wiring.
- Verify full-chain Caddy/ALB behavior, runtime adapters, placement, connection
  use, and startup/drain timing under hosted conditions.
- Recalculate the saved cost model from measured task durations and retained
  telemetry before booking optional testing.
- Set campaign calendars, alert delivery and recovery procedures without
  exposing secrets. Hosted evidence and production approval remain separate
  gates.
