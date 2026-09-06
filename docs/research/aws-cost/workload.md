# AWS cost workload

Status: Phase 9 Task 9.1 research input. Prices and service facts were retrieved
on 2026-09-06. Amounts are US dollars before tax.

## Cost boundary

The estimate covers one AboutMe environment in AWS Singapore (`ap-southeast-1`)
and its global dependencies. It uses On-Demand public rates without Savings
Plans, reservations, Spot, credits, or free-tier deductions. Eligibility
adjustments are listed separately because account history, other workloads, and
the GitHub plan are unknown.

The accepted application topology remains the baseline:

- one ECS cluster on one Graviton EC2 host;
- one Caddy task and one Go task in host network mode, communicating over
  loopback;
- one bridge-isolated Nuxt task reached by Caddy on a fixed host port;
- no Application Load Balancer and no horizontal scaling;
- one private, encrypted, Single-AZ RDS for PostgreSQL instance with gp3
  storage;
- one private S3 media bucket reached only through Go;
- CloudFront in front of a stable Elastic IP origin; and
- the existing Singapore SES/SNS/SQS email stack adopted as a persistent unit.

AWS lists ECS, RDS DB instances, RDS clusters, S3, EventBridge Scheduler, and
CloudFront CloudFormation resources as available in Singapore as of the
retrieval date. CloudFront is global; its distribution metrics appear in
`us-east-1`. The regional availability result is saved in the Phase 9 ignored
evidence directory.

The estimate does not treat the current runtime as ready for ARM64 deployment.
The pinned Playwright image currently names an x86-64 Chromium path. Phase 10
owns the native ARM64 image smoke and the missing private print redemption
route. Those are activation prerequisites, not modeled speedups.

## Fixed bounds and measured evidence

Configured bounds, local measurements, and hosted unknowns have different
meanings:

| Kind           | Evidence                                                                                                                             | Cost use                                                                                  |
| -------------- | ------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------- |
| Configured     | The server task has a 512 MiB task-level cgroup for Go plus Chromium. The current ECS reservation is 512 CPU units.                  | Preserve the bound. Do not infer an ARM speedup or lower hosted CPU need.                 |
| Configured     | Caddy reserves 128 MiB and 128 CPU units. Nuxt reserves 256 MiB and 256 CPU units.                                                   | The accepted host must carry all three tasks and scheduled jobs.                          |
| Measured       | The export runbook passed 156 serial Chromium calls at 512 MiB, no swap, and 0.5 CPU. Peak memory was 324,501,504 bytes (309.5 MiB). | Supports the current bounded starting point only.                                         |
| Measured       | Seven of nine queued render calls completed. Two reached the 20-second deadline. Cleanup joined within 27 ms.                        | Queue misses remain a UAT latency and capacity risk.                                      |
| Configured     | The shared heavy-work permit prevents in-process photo normalization and Chromium from overlapping.                                  | Separate CLI and ops ECS tasks are outside this permit and need measured host headroom.   |
| Configured     | One task accepts at most 2,000 server-sent event connections. Heartbeats are 25 seconds.                                             | The stress workload reaches the configured cap.                                           |
| Measured       | A local test held 2,000 server and client connections in one process.                                                                | It does not prove whole-task, EC2, or CloudFront capacity.                                |
| Hosted unknown | Request mix, render concurrency, database I/O, logs, cache ratio, and sustained CPU are not measured in AWS.                         | Scenario values are explicit inputs and UAT must replace them with billing and telemetry. |

T4g small and medium each have two vCPUs, a 20% baseline per vCPU, and 24 CPU
credits per hour. T4g launches in unlimited mode by default. The 14-day high
case prices 33.6 surplus vCPU-hours. The 30-day stress case prices 73. Each is
equivalent to 0.1 vCPU above the aggregate baseline for every active hour. Low
and expected cases assume no surplus. This is a sensitivity, not a forecast from
request counts. RDS T4g credits use the same explicit hours at
`$0.075/vCPU-hour`; hosted RDS CPU use is also unknown. See the
[T4g specification](https://aws.amazon.com/ec2/instance-types/general-purpose/)
and
[unlimited-mode billing behavior](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/burstable-performance-instances-unlimited-mode.html).

## Environment sizes

| Input                                  |            UAT |     Production |
| -------------------------------------- | -------------: | -------------: |
| EC2 host                               |    `t4g.small` |   `t4g.medium` |
| EC2 root gp3                           |          30 GB |          30 GB |
| RDS class                              | `db.t4g.micro` | `db.t4g.small` |
| RDS gp3                                |          20 GB |          50 GB |
| Automated backup retention             |        30 days |        30 days |
| Public origin addresses                |              1 |              1 |
| Availability Zones for the application |              1 |              1 |
| Horizontal capacity                    |           none |           none |

Thirty-day automated backup retention follows the approved privacy lifecycle in
both environments. RDS grants backup storage up to the account's total
provisioned database storage in the Region at no extra charge. Actual
changed-block volume is unknown. Low cases use zero excess. Expected and high
cases price explicit nonzero sensitivities up to 20 GB-month for UAT and 50
GB-month for production. Any RDS excess costs `$0.095/GB-month`. AWS documents
[the regional allowance](https://aws.amazon.com/rds/faqs/) and
[RDS billing components](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/User_DBInstanceBilling.html).

## Scenario inputs

UAT low, expected, and stress lifetimes are 72 hours, 336 hours, and 730 hours.
The expected case is a proposed 14-day rehearsal. A separate 14-day high case
uses stress traffic and retention assumptions without extending the lifetime. It
also includes a four-hour `t4g.medium` host-rate increment and a separate
temporary `db.t4g.small` database with 50 GB storage for a production-shape
drill. The temporary database is deleted by exact ownership after the drill. The
model does not assume RDS storage can shrink from 50 GB back to 20 GB.
Production cases are steady-state 730-hour months. All compared hosting options
consume the same workload row.

| Input                      | UAT low | UAT expected | UAT 14-day high | UAT stress |  Prod low | Prod expected | Prod stress |
| -------------------------- | ------: | -----------: | --------------: | ---------: | --------: | ------------: | ----------: |
| Active hours               |      72 |          336 |             336 |        730 |       730 |           730 |         730 |
| HTTPS requests             | 100,000 |    1,000,000 |      10,000,000 | 10,000,000 | 1,000,000 |    10,000,000 | 100,000,000 |
| CloudFront invalidations   |      10 |          100 |           1,000 |      1,000 |       100 |         1,000 |      10,000 |
| CloudFront viewer GB       |       1 |           10 |             100 |        100 |        10 |           100 |       1,000 |
| CloudWatch ingest GB       |     0.5 |            2 |              10 |         10 |         2 |            10 |         100 |
| S3 media GB-month          |       1 |            5 |              25 |         25 |         5 |            50 |         500 |
| S3 GET requests            |   1,000 |       10,000 |         100,000 |    100,000 |    10,000 |       100,000 |   1,000,000 |
| S3 PUT/LIST requests       |     100 |        1,000 |          10,000 |     10,000 |     1,000 |        10,000 |     100,000 |
| SES recipients             |     100 |          500 |           5,000 |      5,000 |     1,000 |        10,000 |     100,000 |
| Render calls               |      25 |          250 |           2,500 |      2,500 |       100 |         2,000 |      20,000 |
| Peak SSE connections       |      10 |          100 |           2,000 |      2,000 |        50 |           500 |       2,000 |
| Nightly restore runs       |       3 |           14 |              14 |         30 |        30 |            30 |          30 |
| Hours per restore          |       1 |            2 |               4 |          4 |         1 |             2 |           4 |
| ECR peak GB                |       4 |            6 |              20 |         20 |         6 |            12 |          40 |
| Route 53 zone bill months  |       1 |            1 |               1 |          1 |         1 |             1 |           1 |
| RDS excess backup GB-month |       0 |            5 |              20 |         20 |         0 |            25 |          50 |
| Cross-AZ database GB       |     0.1 |            1 |              10 |         10 |         1 |            10 |         100 |
| Other internet egress GB   |     0.1 |            1 |              10 |         10 |         1 |            10 |         100 |
| Logs Insights scan GB      |       1 |           10 |             100 |        100 |        10 |           100 |       1,000 |
| EC2/RDS surplus vCPU-hours |     0/0 |          0/0 |       33.6/33.6 |      73/73 |       0/0 |           0/0 |       73/73 |
| Aurora average ACU         |     0.5 |         0.75 |               2 |          2 |       0.5 |          0.75 |           2 |
| Aurora I/O requests        | 100,000 |    1,000,000 |      10,000,000 | 10,000,000 | 1,000,000 |    10,000,000 | 100,000,000 |

Requests, renders, and SSE connections are separate inputs. The calculator does
not invent a conversion between them. Media bytes, database I/O, logs, and email
feedback are also independent because no hosted measurements establish those
ratios.

All viewer bytes conservatively use CloudFront's Asia rate. Transfer from AWS
services to CloudFront is free, and S3 transfer to services in the same Region
is free. ECR pushes and same-Region runtime pulls are also free. S3 requests and
CloudFront viewer charges still apply. The estimate separately prices assumed
cross-AZ database bytes and non-CloudFront host egress. Same-AZ database traffic
would reduce the former to zero. See the
[CloudFront FAQ](https://aws.amazon.com/cloudfront/faqs/) and
[S3 transfer explanation](https://repost.aws/knowledge-center/s3-data-transfer-costs),
[ECR pricing](https://aws.amazon.com/ecr/pricing/), and
[RDS transfer rules](https://aws.amazon.com/rds/pricing/).

Publish and revocation create CloudFront invalidation paths. The gross model
applies the `$0.005/path` marginal rate to every assumed path. The first 1,000
paths each month are a separate service allowance and are not deducted.

## Scheduled lifecycle work

Seven schedules are active at UAT activation and production launch:

| Job                       | Cadence       |             Modeled duration |
| ------------------------- | ------------- | ---------------------------: |
| Idempotency expiry        | hourly        |                    5 minutes |
| Media deletion            | hourly        |                    5 minutes |
| Media orphan cleanup      | weekly        |                   10 minutes |
| Session and audit privacy | daily         |                    5 minutes |
| Restore verification      | nightly       | 1, 2, or 4 hours by scenario |
| Origin TLS expiry         | daily         |                    5 minutes |
| CloudFront CIDR drift     | every 6 hours |                    5 minutes |

The first four jobs use the server image. The last three use the ops image. All
run as separate ECS tasks on the same EC2 host in the accepted option. They need
task reservations and placement headroom even though they add no EC2 charge
while they fit. The in-process heavy-work permit covers only photo normalization
and Chromium in the Go API; it does not serialize these CLI and ops tasks.
Concurrent UAT measurements must prove host headroom, and missed schedules must
alarm. The conditional Fargate estimate prices a 0.25-vCPU, 0.5-GB task and one
ephemeral public IPv4 address for each assumed duration. Every modeled task runs
longer than the 60-second IPv4 billing minimum. AWS bills standard public IPv4
addresses per second after that minimum; it does not round these jobs to a full
hour. See [Amazon VPC pricing](https://aws.amazon.com/vpc/pricing/).

Nightly restore verification creates a real private RDS instance from the latest
automated snapshot. It uses the source instance's class and storage, runs schema
checks, and deletes the target without a final snapshot. The cost model prices
both temporary instance hours and storage. A failed run can leave the target
until operator adjudication. Each extra full day costs the restore class's 24
instance-hours plus one day of storage; an orphan lasting a full month
approaches another primary database bill.

## Monitoring inventory

The baseline enables standard ECS Container Insights, 180-day CloudWatch log
retention, one dashboard, and the Phase 10 alarm inventory. Standard Container
Insights emits custom metrics and performance log events, so both metric and log
charges apply. AWS documents the
[billing path](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/cloudwatch_billing.html)
and
[ECS metric set](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/Container-Insights-metrics-ECS.html).

The initial environment count is 30 Container Insights metric streams, 20
application and job custom metrics, 18 standard alarms, and one dashboard. The
persistent email stack adds an assumed seven SES event metrics and seven alarms.
Cardinality depends on the implemented cluster, service, task, dimension set,
and existing email stack. At Singapore rates, every additional continuously
active custom metric adds `$0.30/month` and every standard alarm adds
`$0.10/month`. UAT must inventory billed streams and the adopted stack before
the production estimate is accepted. Scenario inputs also price CloudWatch API
calls and Logs Insights scans.

The CloudFront 5xx alarm uses a default distribution metric at no extra charge.
CloudFront publishes it in `us-east-1`. No optional additional metrics are
assumed. If Phase 10 enables them, CloudFront can send up to eight charged
metrics; the calculator includes the `$0.30/metric-month` input at a zero count.
See
[CloudFront metric pricing behavior](https://docs.aws.amazon.com/AmazonCloudFront/latest/DeveloperGuide/viewing-cloudfront-metrics.html).

## Build and plan eligibility

The public `dannyota/aboutme` repository uses one standard `ubuntu-24.04-arm`
build-and-smoke job for one sequential build of all four images and one native
ARM64 smoke pass. It uploads the final OCI bundle and evidence only after the
smoke succeeds. Standard GitHub-hosted runners for public repositories are free.
The private `aboutme-infra` repository retains credential-free public artifact
validation, credentialed publication, speculative plan, and apply/compatibility
jobs. It uses four standard ARM64 job budgets per cycle at `$0.005/minute` after
any shared private-account allowance. Private artifact storage costs
`$0.25/GB-month` after its shared allowance. Public artifacts keep the prior
count, size, and retention inputs at a zero gross charge. Public caches are
capped at the separate 10 GB per-repository allowance, so no paid cache
extension is configured. See
[Actions billing](https://docs.github.com/en/billing/concepts/product-billing/github-actions),
[runner pricing](https://docs.github.com/en/billing/reference/actions-runner-pricing),
and
[runner specifications](https://docs.github.com/en/actions/reference/runners/github-hosted-runners).

| Scenario                 | Cycles | Minutes/image | Public combined job budgets | Private jobs | Public artifacts/cycle | GB/artifact | Retention | Public cache peak |
| ------------------------ | -----: | ------------: | --------------------------: | -----------: | ---------------------: | ----------: | --------: | ----------------: |
| UAT low                  |      1 |            10 |    1 job: 4 images, 1 smoke |            4 |                      2 |           2 |    7 days |              5 GB |
| UAT expected             |      2 |            15 |    1 job: 4 images, 1 smoke |            4 |                      2 |           3 |   14 days |              8 GB |
| UAT 14-day high / stress |      6 |            20 |    1 job: 4 images, 1 smoke |            4 |                      2 |           3 |   14 days |             10 GB |
| Production low           |      2 |            15 |    1 job: 4 images, 1 smoke |            4 |                      2 |           3 |   14 days |              8 GB |
| Production expected      |      4 |            15 |    1 job: 4 images, 1 smoke |            4 |                      2 |           3 |   14 days |             10 GB |
| Production stress        |     12 |            20 |    1 job: 4 images, 1 smoke |            4 |                      2 |           3 |   14 days |             10 GB |

The combined public job has separate duration budgets: sequential-build minutes
are `cycles × 4 images × minutes/image`, and smoke-pass minutes are
`cycles × 1 × minutes`. Private minutes are `cycles × 4 × minutes`. Public
artifact GB-month remains `cycles × artifacts × GB × retention_days / 30` at a
zero rate. Private storage contains only one 0.01 GiB metadata artifact per
cycle with the same retention. There are no private OCI artifact copies and no
private cache baseline.

The residual-private gross sensitivity is shown before shared private-account
allowances. The plan selects zero paid Actions initially, conditional on quota.
The owner must verify the private repository's plan, remaining shared minutes,
and remaining artifact storage before activation; any overage needs a priced
decision. For an illustration only, if a Free account has its full unused
allowance, the largest modeled campaign uses 960 private minutes against 2,000
included minutes and 12 × 0.01 GiB = 0.12 GiB peak metadata storage (0.056
GB-month modeled accrual) against 500 MB shared artifact storage. Other private
repositories and GitHub Packages can consume those shared allowances, so this
does not predict the bill.

Included allowances apply only after the owner proves the plan and checks use by
every private repository on the account:

| Plan             | Included minutes/month | Artifact storage | Cache storage |
| ---------------- | ---------------------: | ---------------: | ------------: |
| Free             |                  2,000 |           500 MB |         10 GB |
| Pro              |                  3,000 |             1 GB |         10 GB |
| Team             |                  3,000 |             2 GB |         10 GB |
| Enterprise Cloud |                 50,000 |            50 GB |         10 GB |

Public application-repository Actions are free, while private use draws from the
owner's shared allowance. The read-only account projection is `User` with no
plan name, so private eligibility and remaining quota remain unknown.

GitHub's current environment documentation says required reviewers and wait
timers are unavailable for private repositories on Free, Pro, and Team. Phase 10
requires a recorded human reviewer and protected-environment approval after the
speculative plan. GitHub advertises Enterprise Cloud as starting at
`$21/user-month` for the first 12 months. That is not a verified renewal rate,
single-month checkout, or seat commitment. It normally requires moving the
private repository from the current personal namespace into an organization
governed by an enterprise account. The license is a conditional excluded line,
not an approved purchase. See
[environment limits](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments),
[GitHub pricing](https://github.com/pricing), and
[enterprise user billing](https://docs.github.com/en/billing/concepts/enterprise-billing/billing-for-enterprises).

## Lifecycle and retained resources

Setup includes public zero-rate build usage and residual-private Actions
sensitivity. Active cost includes compute, database, edge, requests, logs,
metrics, mail, schedules, and KMS use. Restore cost is shown separately.
Retained cost includes ECR, the state bucket, KMS keys, 180-day logs, and any
backup bytes above the regional allowance.

Synthetic UAT teardown empties only UAT media and destroys the environment with
no final or manual RDS snapshot. The model assumes teardown deletes retained
automated backups; retaining either backup type would continue the explicit
`$0.095/GB-month` excess sensitivity until deletion. Teardown does not destroy
the state bucket/key, the UAT secrets key, ECR, or the adopted shared-email
stack. The UAT totals charge peak ECR and state storage for the active fraction
of a 730-hour month plus one post-destroy month. They use that same horizon to
allocate the assumed existing shared-email metrics and alarms. This allocation
is a gross share of an existing persistent cost, not a new UAT-only resource.
The two retained KMS keys also include one post-destroy month. The totals
include the full six-month storage liability for the UAT run logs and expose its
one-month run rate separately. An ordinary stopped RDS instance is different:
storage and backups remain charged, and RDS automatically restarts a stopped
instance after seven days. Stopping is not the modeled disposal path.

The conditional Fargate rows assume one Route 53 private hosted-zone billing
month for every selected scenario. The UAT start and end dates are not fixed. A
run can use one month only when the zone is created and deleted within one
sufficiently long calendar billing month; crossing another month adds `$0.50`.
The 730-hour UAT row is a monthly stress comparison, not a promise that every
dated 730-hour interval incurs one zone fee. Production uses one steady billing
month. Private hosted-zone DNS queries are free. See
[Route 53 pricing](https://aws.amazon.com/route53/pricing/).

The accepted DNS-only record stays in the existing Cloudflare `aboutme.vn` zone.
Cloudflare offers free DNS on every plan and does not charge DNS queries on
Free, Pro, or Business; Enterprise query volume informs a custom quote. Adding a
record has zero modeled incremental price while the zone remains under its
plan-dependent record quota. The existing zone subscription is outside the
incremental UAT estimate, and Phase 10 must verify quota before mutation. See
[Cloudflare's DNS FAQ](https://developers.cloudflare.com/dns/faq/) and
[record quotas](https://developers.cloudflare.com/dns/manage-dns-records/#dns-records-quota).

The email feedback queue has no consumer. Empty polling and consumer runtime are
zero in the baseline, and a consumer contract must price both before activation.
Long polling reduces empty receives but every poll is still an SQS request. See
[SQS polling behavior](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-short-and-long-polling.html).

Production is a steady-state month. It includes three active customer-managed
keys: the shared state key plus retained UAT and production secrets keys. Its
log storage uses six monthly ingestion cohorts as the 180-day steady-state upper
model. Fresh Phase 10 keys have zero billable rotated versions. At the first
rotation, two retained UAT keys add `$2/month`; three retained production-era
keys add `$3/month`. A second rotation doubles those additions. Later rotations
do not raise the KMS storage charge further under the current pricing rule.

The machine-readable inputs are in `scenarios.json`. `calculate.py` reads those
inputs and `pricing.csv`, then writes every line item and subtotal to
`results.csv` using decimal arithmetic.
