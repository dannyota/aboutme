# AWS hosting cost comparison

Status: Phase 9 Task 9.2 comparison. Prices were retrieved on 2026-09-06.
Amounts are US dollars before tax.

## Result

The historical fixed-host ECS-on-EC2 and RDS baseline was the lowest gross-cost
option in every expected and stress campaign case. The owner then selected the
scheduled UAT and autoscaled production target in
[ADR 0034](../../adr/0034-scheduled-uat-and-production-autoscaling.md). The
target still depends on UAT proving ARM64 Chromium, concurrent job headroom,
render latency, replica safety, server-sent event (SSE) behavior, and the
private print path.

The owner approved a revised operating estimate of
**$20–30 per month** for
part-time UAT, **$140–170 per month** for future
autoscaled production, and **$160–200 per month** when both run in one account.
Production activation remains a separate Phase 11 decision. The deterministic
[monthly model](monthly-inputs.json) gives these unrounded reference points:

| Operating case                            |  Standalone | Increment beside production |
| ----------------------------------------- | ----------: | --------------------------: |
| UAT, 40 hours over 10 days                |  $22.122210 |                  $20.826628 |
| UAT high, 160 hours over 20 days          |  $35.818148 |                  $37.800648 |
| Production low, 100 extra node-hours      | $142.942180 |                           — |
| Production expected, 200 extra node-hours | $162.603564 |                           — |
| Combined low                              |           — |                 $163.768808 |
| Combined expected                         |           — |                 $186.630193 |
| Combined high                             |           — |                 $200.404213 |

Combined expected pairs expected production with the selected 40-hour UAT
schedule. Combined high pairs expected production with the unselected 160-hour
UAT sensitivity.

With every allowance set to zero, the same UAT schedules cost
$30.629128 and
$44.403148. The selected 40-hour baseline is within the approved
range when the modeled allowances are available. Verify account use before
activation. The 160-hour forecast exceeds the UAT range, and its combined high
case exceeds $200. Neither is selected or a spending entitlement; reduce
optional test hours or obtain a revised cost decision.

Production uses one `t4g.medium` node normally and allows a second node for
100–200 hours per month. A persistent public Application Load Balancer (ALB),
one ALB capacity unit, and its two public IPv4 addresses cost
$31.536000 per
month. Each extra node includes its public IPv4 address and prorated 30 GiB gp3
root volume; 100 hours cost $5.134521.
The database stays fixed-size Single-AZ RDS PostgreSQL. Multi-AZ RDS, NAT
gateways, and paid VPC endpoints are excluded. All capacity remains unproven
until hosted load and recovery checks run.

The account-wide CloudFront and CloudWatch monthly allowances are applied to
merged quantities once. For example, expected production's 10 million HTTPS
requests use the request allowance, so the combined high case bills UAT's
additional 1 million requests. The result does not add two separately netted
environment totals. The saved quantities come from the official
[CloudFront pay-as-you-go pricing](https://aws.amazon.com/cloudfront/pricing/pay-as-you-go/)
and [CloudWatch pricing](https://aws.amazon.com/cloudwatch/pricing/) pages,
retrieved on 2026-09-06. The combined rows also count shared SES monitoring, KMS
keys, OpenTofu state, and ECR storage once. Public standard-runner GitHub
Actions cost zero and no Actions overage or subscription is included in AWS
totals. Generic CloudWatch API requests are priced gross because the operation
mix is unknown. The one-million-request allowance is operation-sensitive;
`GetMetricData`, `GetInsightRuleReport`, and `GetMetricWidgetImage` are outside
it. Replace the gross input only after Phase 10 records eligible operations.
Dashboards are also priced gross because the three-dashboard allowance requires
each dashboard to reference at most 50 metrics, which this model has not proved.
Custom-metric allowance billing by emitted metric-hours remains an estimate; the
gross column is the fallback until the first live invoice confirms it.

Aurora Serverless v2 is a viable managed database alternative in Singapore, but
it costs more at the modeled capacity and changes database, backup, and restore
contracts. Fargate is available and priced, but it is not a drop-in replacement
for the historical separate-service, fixed-host comparison topology. Either
alternative needs an ADR and new UAT evidence before it can replace the
baseline.

## Comparable gross totals

The selected monetary totals are AWS plus residual private Actions gross
sensitivity. Public build, smoke, artifact, and cache usage has zero gross
charge and is recorded separately in `results.csv`. The totals are not an actual
GitHub bill: private plan eligibility and remaining shared minute and artifact
allowance must be verified before activation. Paid Actions is selected at zero
initially; any overage needs a priced decision. Totals exclude the conditional
GitHub Enterprise Cloud license, free allowances, discounts, credits,
commitments, and tax. Each option uses the same traffic, storage, email,
monitoring, restore, and build inputs from `scenarios.json`.

| Scenario                                            | EC2 + RDS | EC2 + Aurora | Fargate + RDS |
| --------------------------------------------------- | --------: | -----------: | ------------: |
| UAT low, 72 hours                                   |    $13.00 |       $18.59 |        $17.60 |
| UAT expected, 14 days (superseded operating choice) |    $44.18 |       $87.96 |        $63.99 |
| UAT high, same 14 days                              |    $91.09 |      $219.33 |       $110.04 |
| UAT stress, 730 hours                               |   $135.05 |      $412.53 |       $175.72 |
| Production low, monthly                             |   $115.46 |      $151.46 |       $142.10 |
| Production expected, monthly                        |   $158.89 |      $232.98 |       $186.05 |
| Production stress, monthly                          |   $568.88 |      $840.83 |       $594.16 |

These gross campaign rows remain useful option and traffic comparisons. The
part-time monthly schedule above supersedes the 14-day campaign as the operating
choice. The UAT high row was the prior budget case. It keeps the proposed 14-day
lifetime while applying stress traffic, logs, backups, credits, restore
duration, build frequency, and a four-hour production-shape drill. Its selected
historical option is `$88.68` AWS plus `$2.41` residual private Actions gross
sensitivity, for `$91.09`. That sensitivity is not a purchase estimate: private
plan eligibility and shared quota must be checked before activation. AWS budget
data can be delayed, and stopping resources leaves storage, address, log, key,
and registry charges. Task 9.3 must state any control as proposed and define a
direct teardown path.

## Lifecycle detail

The expected UAT totals separate setup, active operation, retained resources,
and restore work. The seven-day idle sensitivity is excluded from each total.
Fargate restore includes the scheduled task while it waits for RDS.

| Expected 14-day UAT | Setup | Active | Seven-day idle | Retained | Restore/jobs | Gross total |
| ------------------- | ----: | -----: | -------------: | -------: | -----------: | ----------: |
| EC2 + RDS           | $0.93 | $33.72 |          $2.14 |    $8.72 |        $0.81 |      $44.18 |
| EC2 + Aurora        | $1.03 | $75.68 |         $18.81 |    $8.36 |        $2.88 |      $87.96 |
| Fargate + RDS       | $0.84 | $52.03 |          $6.72 |    $8.72 |        $2.39 |      $63.99 |

The setup column includes public zero-rate workflow usage, residual private
Actions sensitivity, and the production-shape drill. The drill does not resize
the 20 GB UAT database in place. It prices a separate 50 GB `db.t4g.small` for
four hours and the EC2 host-class rate difference, then requires exact-owner
deletion. RDS allocated storage cannot be shrunk back to 20 GB.

Retained UAT cost charges peak ECR and state storage for the active fraction of
a month plus one post-destroy month. The assumed existing shared SES metrics and
alarms use the same allocation horizon. This is a gross allocation of the
persistent stack, not a new UAT-only charge. The two customer-managed KMS keys
include one post-destroy month. Retained cost also includes backup-growth
sensitivity and the full six-month storage liability for the run's logs. In the
expected case, one month of those UAT logs is `$0.06`; the included six-month
liability is `$0.36`. The adopted email stack persists outside UAT state. Its
actual metric and alarm inventory must replace the assumed seven of each.

An idle environment is not a safe indefinite savings mode. The EC2 + RDS
seven-day figure retains the root disk, public IPv4 address, and database
storage while compute is stopped. RDS restarts a stopped instance automatically
after seven days. The figure excludes ongoing KMS, ECR, logs, email monitoring,
and backup growth. Synthetic UAT teardown is cheaper after validation because it
removes the primary database and environment resources, creates no final or
manual snapshot, and deletes retained automated backups in the model. Any
retained backup continues at the excess-backup rate until deletion. The named
bootstrap roots remain.

Expected production lifecycle cost is:

| Expected production month | Setup |  Active | Retained |  Restore/jobs | Gross total |
| ------------------------- | ----: | ------: | -------: | ------------: | ----------: |
| EC2 + RDS                 | $1.20 | $142.88 |   $11.18 |         $3.63 |     $158.89 |
| EC2 + Aurora              | $1.20 | $215.95 |    $9.38 |         $6.45 |     $232.98 |
| Fargate + RDS             | $1.20 | $166.61 |   $11.18 | $3.43 + $3.63 |     $186.05 |

## Historical option 1: fixed-host ECS on EC2 with RDS

This gross comparison option preserves the former one-host topology. ADR 0034
supersedes its single-host routing and placement as the production target:

- Caddy and Go remain separate host-network tasks with loopback origin trust.
- Nuxt remains bridge-isolated and cannot reach Go loopback.
- CloudFront reaches one reassociated Elastic IP; no load balancer is added.
- Go and Chromium retain their 512 MiB task cgroup and current 512 CPU-unit
  reservation.
- RDS remains private, Single-AZ PostgreSQL with gp3 and 30-day backups.
- Scheduled tasks use the same paid host when their reservations fit.

The local 309.5 MiB render peak and 156 serial calls support a starting point,
not hosted capacity. Two of nine queued calls already reached the 20-second
deadline. The in-process heavy permit serializes only Chromium and photo work;
it does not serialize separate server CLI or ops tasks. UAT must measure the
simultaneous reservations and memory of the three services, migrations, and
scheduled jobs. A `t4g.small` that cannot place or run that set must be resized.

T4g requires CPU-credit monitoring. The high and stress rows price explicit EC2
and RDS surplus-credit hours. No application speedup is attributed to ARM. The
existing runtime image hardcodes an x86-64 Chromium path, so Phase 10 must
produce and smoke-test a native ARM64 image before activation.

The operating cost is host ownership. The owner must patch the ECS-optimized
Amazon Machine Image, observe ECS agent and disk health, test Auto Scaling group
replacement and Elastic IP reassociation, and retain task placement headroom.
The managed RDS service still owns database host patching and automated backup
machinery. Single-node compute and Single-AZ RDS retain planned downtime and
availability limits.

Amazon ECS charges no separate EC2-launch-type service fee. Standard Parameter
Store parameters and integrated, non-exportable ACM public certificates also
have no additional fee. The dated catalog records those zero-price dependencies
instead of treating them as absent.

## Option 2: ECS on EC2 with Aurora Serverless v2

This option keeps the historical fixed-host EC2 application topology and
replaces RDS PostgreSQL with Aurora PostgreSQL Serverless v2 standard storage.
Singapore supports RDS clusters. The estimate uses explicit average capacity of
0.5, 0.75, and 2 Aurora capacity units (ACUs) and does not assume auto-pause.
Each ACU combines about 2 GiB of memory with CPU and networking. Actual scaling
is a hosted unknown. AWS documents
[capacity behavior](https://aws.amazon.com/blogs/database/understanding-how-acu-minimum-and-maximum-range-impacts-scaling-in-amazon-aurora-serverless-v2/).

Aurora increases expected UAT by `$43.78` and expected production by `$74.10`
over EC2 + RDS. It can scale without choosing an instance class and can pause on
supported versions when configured with a zero minimum, but the always-running
application and nightly work make pause eligibility uncertain. Storage and I/O
remain charged while paused.

Adoption needs an ADR that changes the approved database resource, capacity
policy, storage and I/O billing model, recovery-time assumptions, and nightly
restore implementation. Phase 10 would need PostgreSQL compatibility tests,
OpenTofu cluster resources, snapshot restore ownership, and real restore timing.
The modeled nightly restore uses an assumed 0.5 ACU for the scenario duration;
it is not measured evidence.

Aurora is a managed alternative, but its scaling feature does not offset its
modeled cost or contract changes for this small steady service. Keep it as a
future capacity option unless UAT shows RDS instance sizing cannot meet the
workload.

## Option 3: ECS on Fargate with RDS

Fargate requires `awsvpc` network mode. The historical comparison design
requires separate Caddy and Go tasks sharing host loopback, plus a
bridge-isolated Nuxt task on a fixed host port. Separate Fargate tasks do not
share loopback. Containers in one Fargate task can communicate over localhost,
but co-location changes task ownership and resource bounds. See the
[Fargate task rules](https://docs.aws.amazon.com/AmazonECS/latest/developerguide/fargate-tasks-services.html)
and
[network namespace guidance](https://docs.aws.amazon.com/AmazonECS/latest/bestpracticesguide/fargate-security-considerations.html).

The priced redesign is deliberately explicit: one combined Caddy + Go task at
0.5 vCPU and 1 GB, one Nuxt task at 0.25 vCPU and 0.5 GB, one single-AZ Network
Load Balancer with one capacity unit, three long-running public IPv4 addresses,
one ephemeral public IPv4 for each scheduled task's duration, and a Route 53
private discovery zone for Caddy to find Nuxt. The current 512 MiB Go plus
Chromium task limit cannot be carried into that combined task unchanged without
deciding whether Caddy shares the limit or the task memory increases. Fargate
allows 512 MiB only with 0.25 vCPU; 0.5 vCPU starts at 1 GB. A 0.25-vCPU form is
unmeasured, not proven to fail.

The public-task-IP estimate changes the exposure model and still needs security
group and discovery design. Standard public IPv4 addresses are billed per second
with a 60-second minimum; every modeled scheduled task exceeds that minimum. See
[Amazon VPC pricing](https://aws.amazon.com/vpc/pricing/). The gross NAT gateway
and processing components are `$20.59` for expected UAT and `$50.74` for the
stress 730-hour case when the image-volume proxy and other egress pass through
it. Those figures are not net private-topology increments: private tasks remove
service and job public IPs but require a NAT public IP and a complete network
recalculation. Interface endpoints are cataloged at `$0.013/endpoint-hour` plus
processing, but the required ECR, Logs, SSM, and other endpoint set is not
fixed. Either network form needs an ADR and a complete price update. Route 53
private hosted-zone queries are free, while the `$0.50` zone fee is not
prorated. The model selects one billing month; each extra month crossed by the
as-yet-undated UAT run adds `$0.50`. See
[Route 53 pricing](https://aws.amazon.com/route53/pricing/).

Fargate can add only `SYS_PTRACE` as a Linux capability. The application keeps
Chromium sandboxing enabled. The current image, user namespace, seccomp, and
sandbox behavior have not been proven on Fargate ARM64. See the
[kernel capability constraint](https://docs.aws.amazon.com/AmazonECS/latest/APIReference/API_KernelCapabilities.html).

Fargate removes EC2 host patching and task placement against a fixed instance.
It adds NLB health and capacity, service discovery, per-task address and
security-group policy, and per-run job compute. It remains single-task capacity
because horizontal scaling is not authorized. Expected production costs `$27.16`
more than EC2 + RDS before a private-network redesign.

## Shared invariants and edge behavior

All options keep private S3 media behind Go, revocation checks, 30-day database
backups, 180-day logs, nightly real restores, and the 2,000-SSE cap. None may
make S3 a public or CloudFront origin. None may overlap photo normalization and
Chromium in the Go process.

The planned 25-second SSE heartbeat is below the 60-second CloudFront origin
read timeout. CloudFront measures that timeout between response packets, so the
configuration is plausible. It still needs a real edge test with 2,000 clients;
the local one-process test does not prove CloudFront, network, file-descriptor,
or whole-task capacity. AWS documents the
[origin timeout behavior](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/RequestAndResponseBehaviorCustomOrigin.html).

The private print path is unresolved before any option can activate. Nuxt must
reach Go's private redemption endpoint without gaining a route that can forge
the trusted client address. Pricing does not waive that security prerequisite.
The gross request price includes one CloudFront Function call per viewer
request, but the proposed UAT Basic Authorization gate and its header conflict
remain an unapproved Phase 10 prerequisite. Removing the gate to save function
cost is not an accepted resolution.

OpenTofu can install providers from registries and is designed for Terraform
configuration compatibility. The current HashiCorp AWS provider documentation
exposes the chosen
[ECS task definition](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_task_definition),
[ECS service](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecs_service),
[RDS instance](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/db_instance),
[Aurora cluster](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/rds_cluster),
[S3 bucket](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket),
[Scheduler schedule](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/scheduler_schedule),
and
[CloudFront distribution](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudfront_distribution)
resources. The ECS task resource documents `host`, `bridge`, and `awsvpc` modes
and Fargate compatibility. The Aurora resource documents Serverless v2 scaling.
The CloudFront resource documents the origin read timeout and function
association.

One concrete compatibility gap remains. The provider's RDS resource says its
write-only `password_wo` argument requires Terraform 1.11 or later. The Phase 10
contract requires that argument, but generic OpenTofu configuration
compatibility does not prove the exact provider/plugin protocol behavior. Phase
10 must pin OpenTofu and the AWS provider, run the write-only password test, and
lock the tested provider checksum before implementation. S3 encryption and
lifecycle settings also use separate provider resources. See
[provider requirements](https://opentofu.org/docs/language/providers/requirements/)
and the [OpenTofu FAQ](https://opentofu.org/faq/).

## Cost sensitivities

These increments come from `pricing.csv` and decimal arithmetic in the saved
calculator inputs:

| Change                                                         | Gross increment |
| -------------------------------------------------------------- | --------------: |
| 1 million CloudFront HTTPS requests plus viewer-function calls |           $1.30 |
| 10 GB CloudFront Asia egress                                   |           $1.20 |
| 1 GB logs ingested and held for 180 days                       |           $0.88 |
| 10 GB-month S3 Standard                                        |           $0.25 |
| 10 GB-month RDS gp3                                            |           $1.38 |
| 10 GB-month Aurora storage                                     |           $1.10 |
| 10 GB-month excess RDS backup                                  |           $0.95 |
| 10 GB-month excess Aurora backup                               |           $0.23 |
| 73 EC2 surplus vCPU-hours                                      |           $2.92 |
| 73 RDS surplus vCPU-hours                                      |           $5.48 |
| 1,000 CloudFront invalidation paths at the gross marginal rate |           $5.00 |
| One continuously active custom metric                          |     $0.30/month |
| One standard alarm                                             |     $0.10/month |
| Extra Route 53 private hosted-zone billing month               |           $0.50 |
| First KMS rotation for three retained keys                     |     $3.00/month |

Render count has no independent per-render charge on the historical fixed EC2
host. It becomes a cost only when measured concurrency forces a larger host or
creates CPU-credit charges. The calculator therefore does not invent a linear
render price.

The restore schedule is material even when its normal cost is small. A failed
nightly target left for a full month costs about `$21.01` for the UAT RDS shape
or `$44.13` for the production shape, before backup growth. The four-hour job
bound and stale-target adjudication must alarm and delete only the exact owned
target.

## External plan and unresolved inputs

Public standard-runner Actions are free. Private allowance eligibility, quota,
and shared artifact use remain unknown, so the residual private gross rows are
conservative sensitivity rather than an actual bill. If a Free account has a
full unused allowance, the largest modeled campaign's 960 private minutes and 12
× 0.01 GiB = 0.12 GiB peak metadata storage (0.056 GB-month modeled accrual) fit
within 2,000 minutes and 500 MB. Other private repositories and GitHub Packages
can consume the shared allowance. Required environment reviewers and wait timers
are unavailable for private repositories on Free, Pro, or Team. GitHub
advertises Enterprise Cloud as starting at `$21/user-month` for the first 12
months. That is neither a verified renewal rate nor a verified single-month or
seat commitment. A private repository in the current personal namespace would
also need an organization and enterprise governance decision. The conditional
line is excluded from every gross total, and no purchase or workflow weakening
is authorized.

The existing SES account is in sandbox. Production access, recipient volume,
mail feedback consumption, and the adopted stack's exact alarms remain Phase 10
inputs. The queue has no consumer, so the baseline prices feedback events but
uses zero empty polls and zero consumer runtime. Any consumer must add its task
runtime and polling cadence before activation.

Cloudflare documents free DNS on every plan. The added DNS-only record has zero
modeled incremental cost while the existing zone remains under its
plan-dependent record quota. The existing zone subscription is excluded, and
Phase 10 must verify quota before mutation. The model also needs UAT
measurements for Container Insights cardinality, Logs Insights scans,
non-CloudFront egress, cross-AZ database placement, backup bytes, image size,
CPU credits, build duration, restore duration, and NLB capacity. `results.csv`
keeps these assumptions visible so the production row can be recalculated rather
than accepted as a quote.
