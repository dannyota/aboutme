# AWS hosting cost recommendation

The owner approved part-time UAT and production autoscaling on 2026-09-06:
**$20–30/month for UAT**, **$140–170/month for production**, and
**$160–200/month combined**, in AWS Singapore (`ap-southeast-1`). Amounts are
USD before tax. These are workload estimates, not a measured bill or a promise
that arbitrary traffic fits the range.

[ADR 0034](../../adr/0034-scheduled-uat-and-production-autoscaling.md) records
the decision. The operating plan replaces the proposed
$100, 14-day UAT
campaign and separate $8/month retained-resource allowance. Use
**$30/month** as the UAT operating ceiling, including retained and allocated
shared costs. Production launch still requires Phase 11 approval under
[ADR 0031](../../adr/0031-aws-cost-research-and-hosted-uat.md). This research
has not provisioned resources or purchased a plan.

## Selected configuration

| Resource           | UAT                                                                                       | Production target                                                                                            |
| ------------------ | ----------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------ |
| Region and runtime | Singapore ECS on EC2 Graviton                                                             | Same                                                                                                         |
| Application host   | One `t4g.small` during test windows; temporary larger replicas for production-shape proof | `t4g.medium`, minimum one, initial maximum two; automatic scale-out and scale-in                             |
| Root disk          | 30 GiB gp3 per active node; deleted with the node                                         | 30 GiB gp3 per node                                                                                          |
| Database           | Private Single-AZ `db.t4g.micro`, 20 GiB gp3; stop between tests                          | Private Single-AZ `db.t4g.small`, 50 GiB gp3; fixed compute size                                             |
| Edge               | Same load-balancer path during test windows; remove the load balancer when idle           | CloudFront through an Application Load Balancer to the application fleet                                     |
| Network            | Auto-assigned node addresses are released on termination                                  | Two load-balancer public addresses and one outbound public address per node; no NAT gateway in this estimate |
| Media              | Private S3; Go authorizes every read                                                      | Same                                                                                                         |
| Monitoring         | App/job metrics and alarms, 180-day logs; stop-aware heartbeat checks                     | Same inventory plus scaling and load-balancer health                                                         |
| Build              | Free standard public ARM64 image build and smoke                                          | Promote UAT-proven image digests                                                                             |

These are starting sizes, not proven hosted capacity. The
[workload record](workload.md) distinguishes local render and server-sent event
(SSE) measurements from unmeasured whole-application load. Each server task
retains its 512 MiB Go-plus-Chromium cgroup. Jobs need their own reservations
and host headroom. Database connections and fleet-wide limits must fit the
[numeric budgets](../../design/budgets.md).

The current code has process-local revocation leases, render jobs, SSE
subscriptions, and limiters. Adding a second ECS replica is not yet safe. Task
10.18 defines the shared coordination, routing, placement, and draining
contracts before dependent implementation. Phase 10 must prove scale-out and
scale-in under writes, revocations, printing, and SSE before enabling the
production target. RDS compute does not change with ECS capacity.

EC2 remains the selected application compute because it preserves the existing
runtime boundaries at the modeled cost. Its owner must patch the host image,
verify replacement and draining, watch CPU credits and disk pressure, and keep
job headroom. RDS, S3, SES, CloudFront, and Scheduler retain managed-service
roles. Multi-AZ RDS, NAT gateways, paid support, and commitments are not
selected or included in the launch estimate.

## Reproducible monthly estimate

Use [monthly-inputs.json](monthly-inputs.json), [monthly.py](monthly.py), and
[monthly-results.csv](monthly-results.csv) for the approved operating model. It
separates production, standalone UAT, and UAT's incremental cost beside
production. Shared mail monitoring, keys, registry, and state are allocated
once. Recurring account allowances are applied once to combined usage; they are
not independent allowances for each environment.

The production estimate assumes 730 baseline server-hours and 100–200 extra
server-hours monthly, 10–100 GB of edge traffic, and the saved low to expected
operational workload. Each extra node includes compute, disk, and its outbound
public address. The load balancer includes two Availability Zones, two public
addresses, and one modeled capacity unit. More traffic, scaling hours, CPU
surplus, log cardinality, or cross-zone traffic changes the result. No page-view
count is evidence of server capacity.

UAT starts with 40 billable hours over ten test days. The 160-hour case is a
sensitivity, not an unconditional runtime entitlement. The load balancer runs
throughout each booked UAT window. Production-shape tests, including a second
node, consume the same monthly allowance. Book those windows and required
retention first; shorten optional tests if the forecast would exceed $30. A 50
GiB production-shape database is a separate disposable target. Do not shrink the
20 GiB UAT database back after an in-place expansion.

The calculation uses dated [prices](pricing.csv), including ongoing
[CloudFront allowances](https://aws.amazon.com/cloudfront/pricing/pay-as-you-go/)
and [CloudWatch allowances](https://aws.amazon.com/cloudwatch/pricing/). It does
not assume promotional EC2/RDS credits, reserved capacity, or Savings Plans.
Phase 10 checks actual shared allowance use before activation. If those
allowances are consumed by other workloads, use the gross result and reduce
optional work to fit the ceiling.

The selected 40-hour UAT case is about
$22/month. Expected production is about
$163/month, and their combined account
estimate is about $187/month. The
160-hour UAT sensitivity exceeds both the $30
UAT ceiling and, with expected production, the $200 combined range. Dashboard
charges are gross because their free-allowance eligibility is unproven. The
model also reserves a full month of root-disk and node-address cost for UAT;
those are conservative cost reserves, not resources retained by the normal
shutdown path.

The original [comparison](comparison.md), [scenarios](scenarios.json), and
[results](results.csv) retain the gross 14-day, production, and alternative
service calculations. They explain the service choice; their campaign totals are
not the selected monthly operating budget.

## Alerts and operating controls

Before activation, Task 10.15 records the owner, monthly cost allocation, booked
test windows, shutdown times, resource inventory, and cleanup path. Use a
monthly AWS budget covering active, stopped, retained, and allocated shared
resources. Include global edge charges and untagged shared costs.

Verify that the
[budget filter](https://docs.aws.amazon.com/cost-management/latest/userguide/budgets-create-filters.html)
actually captures the intended charges. A tag filter requires an activated
cost-allocation tag. Until then, use a broader account budget and reconcile it
against the owned inventory. Budget monitoring and notifications are
[free](https://aws.amazon.com/aws-cost-management/aws-budgets/pricing/); no paid
budget reports or actions are selected.

- Alert at $20 and $25 actual monthly UAT cost, and a forecast of $30.
- At $25 actual cost or a forecast of $30, stop optional tests and reserve the
  remaining amount for required cleanup, storage, logs, and shutdown work.
- Start each test window only when actual cost plus the remaining conservative
  forecast fits $30. End compute at the booked shutdown time. A larger monthly
  spend requires a priced extension.
- Reconcile the inventory and forecast daily. AWS billing data is delayed; an
  alert does not stop services and is not a technical hard cap.
- Preserve an operator-run shutdown and cleanup path if private Actions quota is
  exhausted. No paid Actions usage or subscription is approved.

Production's range is a planning input for Phase 11. It does not authorize
production activation, scale-to-zero, or an automatic database resize.

## UAT stop, resume, and final cleanup

Stopping between tests preserves UAT data. Final teardown removes the disposable
environment. These operations have different costs and data-loss effects.

1. Before stopping, close test access and new writes, drain active work, and
   complete any pending privacy work that must meet its deadline. Do not leave
   media-deletion jobs past the 24-hour target while their worker is disabled.
2. Disable application capacity reconciliation and scheduled jobs in the
   reviewed order, then scale the owned EC2 group to zero and stop RDS. ECS and
   its Auto Scaling Group must not replace deliberately removed capacity. Remove
   the temporary load balancer at the end of every test window.
3. Node termination deletes its root disk and releases its auto-assigned public
   address. Clean up any orphaned volumes or addresses. Retain the primary
   database, state, keys, required logs, images, and authorized UAT media.
   Charge their storage and other retained costs to the monthly ceiling. RDS
   [automatically restarts after seven days](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_StopInstance.html);
   prove a restart safeguard outside the stopped application before calling
   scheduled shutdown operational.
4. On resume, start the database and recreate capacity and the load balancer,
   verify readiness, run due bounded maintenance, restore the job schedules, and
   reopen test access. Record the actual billable hours, including operational
   overhead.
5. At final teardown, retain only redacted evidence and the explicitly required
   persistent resources. Empty only the owned synthetic UAT media bucket. Delete
   disposable EC2, volumes, addresses, load balancers, edge resources, UAT DNS,
   and RDS targets under the no-final-snapshot contract with
   `delete_automated_backups = true`. Verify no owned restore target or manual
   backup remains. Preserve the shared email stack and Google DNS.
6. Reconcile the remaining state, keys, release images, and 180-day logs against
   their retention requirements. Their cost remains inside the same
   $30/month
   ceiling; the previous separate $8 allowance does not apply.
   Review retained resources after 30 days and remove eligible expired
   resources.

## GitHub and mail handoff

[ADR 0033](../../adr/0033-public-image-builds-private-deployment.md) keeps all
four image builds and native smoke in public `aboutme`. Private `aboutme-infra`
validates artifacts, publishes to ECR, and deploys using its available quota.
Public workflows receive no AWS credentials. Keep public caches within 10 GiB
and private artifacts limited to release metadata. The old private Actions
columns are overage sensitivities, not approved spend.

GitHub private-repository required-reviewer eligibility remains a Phase 10
input. Verify an existing eligible plan or design a replacement approval
contract that preserves provenance, scoped AWS roles, and recorded owner
approval. No subscription purchase is selected. A missing paid feature must not
silently remove the approval gate.

The [email runbook](../../runbooks/email.md) records Singapore SES in sandbox,
the `aboutme-auth` configuration set, and the existing `aboutme-email` stack.
Adopt that stack as a persistent unit; CloudFormation remains the sole owner of
its children. Do not duplicate its SES, SNS, SQS, CloudWatch, or DNS resources
in disposable UAT state.

Phase 10 checks sandbox limits, verified recipients, runtime SES IAM, and the
feedback consumer. Price a new consumer and polling before activation. Simulator
delivery is separate from real verification/reset workflows. Preserve Google
Workspace records and the existing MAIL FROM subdomain.

The [runtime handoff](../../plans/phase-10/runtime-refresh.md) carries the
remaining startup, private print, ARM64, access, and workflow contracts. Local
checks precede AWS activation. Hosted UAT proves load, scaling, mail, restore,
rollback, alarms, origin-secret rotation, and concurrent migrations. A failed
capacity check needs a priced correction within the approved ceiling.

Reproduce the gross comparison with
`python3 -B docs/research/aws-cost/calculate.py` and the operating model with
`python3 -B docs/research/aws-cost/monthly.py --check`. After changing
quantities or rates, regenerate its CSV with `--write`.
