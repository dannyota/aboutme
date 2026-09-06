# AWS UAT cost recommendation

Recommend a 14-day Singapore UAT on the existing ECS-on-EC2, RDS PostgreSQL, and
private S3 design. The expected modeled cost is
**$46.43**, including gross
GitHub Actions usage. The same 14-day high-usage case is **$97.53**.
Prices were retrieved on 2026-09-06; amounts are USD before tax.

Status: **proposed; spending amount awaits the owner**. Region, UAT hostname,
and supporting Cloudflare DNS scope are already authorized by
[ADR 0031](../../adr/0031-aws-cost-research-and-hosted-uat.md). This research
has not provisioned resources or purchased a plan.

## Selected configuration

| Resource         | UAT starting point                                                    | Production planning assumption                     |
| ---------------- | --------------------------------------------------------------------- | -------------------------------------------------- |
| Application host | One `t4g.small`, 2 vCPU, 2 GiB                                        | One `t4g.medium`, 2 vCPU, 4 GiB                    |
| Root disk        | 30 GiB gp3                                                            | 30 GiB gp3                                         |
| Database         | Private Single-AZ `db.t4g.micro`, 20 GiB gp3                          | Private Single-AZ `db.t4g.small`, 50 GiB gp3       |
| Backups          | 30-day automated retention, nightly isolated restore                  | Same retention and restore schedule                |
| Media            | Private, unversioned S3; Go authorizes every read                     | Same                                               |
| Edge             | CloudFront to one Elastic IP origin over HTTPS; Cloudflare DNS-only   | Same topology, separate production resources       |
| Server limit     | 512 MiB task cgroup for Go and Chromium; 512 CPU units                | Same hard memory bound; repeat hosted measurements |
| Monitoring       | Standard Container Insights, app/job metrics and alarms, 180-day logs | Same inventory, measured cardinality               |
| Build            | Native `ubuntu-24.04-arm` in private `aboutme-infra`                  | Promote the UAT-proven image digests               |

These are starting sizes, not proven hosted capacity. The
[workload record](workload.md) distinguishes the local render and SSE
measurements from unmeasured whole-application load. Scheduled jobs need their
own task reservations and host headroom. They do not share the request process's
heavy-work permit.

Reserve a four-hour production-shape drill inside the 14 days. Temporarily use
the larger host class and a separate private 50 GiB `db.t4g.small` database.
Keep the ordinary UAT database intact, then delete only the owned drill target.
Do not model a reversible in-place reduction from 50 GiB back to 20 GiB.

EC2 remains self-managed because it preserves the current network and trust
boundaries at the lowest modeled cost. Its owner must patch the host image, test
replacement and Elastic IP reassociation, watch CPU credits and disk pressure,
and maintain job placement headroom. RDS, S3, SES, CloudFront, and Scheduler
keep their managed-service roles. The [comparison](comparison.md) prices Aurora
and Fargate and identifies the ADR and runtime changes each would need. No
topology change is selected here.

## Spending proposal

| Scenario                  |     AWS | Gross Actions usage | Combined |
| ------------------------- | ------: | ------------------: | -------: |
| Expected 14-day UAT       |  $43.57 |               $2.86 |   $46.43 |
| High-usage 14-day UAT     |  $88.68 |               $8.85 |   $97.53 |
| Expected production month | $157.68 |               $5.30 |  $162.98 |
| Stress production month   | $564.07 |              $17.00 |  $581.07 |

The proposed campaign ceilings are **$100 for AWS** and **$10 for Actions
usage**, with a **14-day active lifetime**. These are spending decisions for the
owner, not existing controls. They cover the modeled campaign and stated
retention charges. They exclude tax and a new GitHub subscription. Production
figures are planning scenarios and do not authorize launch or production spend.

The totals include the full six-month log-storage liability, keys and
registry/state storage through active UAT and one month after teardown, shared
email monitoring over that same period, backup growth, nightly restore
resources, and the production-shape drill. They are not a quote or an upper
bound on arbitrary traffic or resource drift. Read
[scenarios.json](scenarios.json), [pricing.csv](pricing.csv), and the line items
in [results.csv](results.csv) before changing a workload assumption.

After the modeled retention month, allow **up to
$8/month** for the retained
footprint and review it after 30 days. At the expected retained quantities the
run rate is about $5.46/month;
at the high quantities it is about $7.10/month. These rates include two KMS
keys, state storage, ECR, remaining logs, and the assumed shared mail metrics
and alarms. Shared mail is an existing cost, and its actual inventory must
replace the assumption. Log storage expires on its 180-day schedule; retained
release images and keys have separate lifetimes.

### Alerts and controls

Before activation, Task 10.15 records the owner, start time, expiry time, exact
resource inventory, and budget scope. Separate AWS UAT charges from other
account workloads. Record shared-resource allocation explicitly.

Verify that each
[budget filter](https://docs.aws.amazon.com/cost-management/latest/userguide/budgets-create-filters.html)
captures its intended charges. A tag filter requires an activated
cost-allocation tag. If the tag is not active, start with a broader account
budget and reconcile it against the UAT inventory; do not create a filter that
silently misses new charges. Include global edge charges and untagged shared
costs in the ledger.

Use a custom-period AWS cost budget from activation through the modeled first
retention month, plus a monthly retained-resource budget after that. The 14-day
active expiry remains a separate scheduled operator check. Track the remaining
six-month log-storage liability even after the campaign budget ends. AWS
[supports custom project periods](https://docs.aws.amazon.com/cost-management/latest/userguide/budgets-managing-costs.html);
budget monitoring and notifications are
[free](https://aws.amazon.com/aws-cost-management/aws-budgets/pricing/). No paid
budget reports or budget actions are selected.

- Alert at $50 and $75 actual AWS campaign cost, and at $90 forecast cost. The
  owner checks resource counts and the revised forecast at each alert.
- At $90 actual AWS cost, or a forecast above $100, stop optional test work and
  execute the scoped cleanup unless the owner approves an extension.
- End active UAT at day 14 unless an extension is recorded. Cleanup is an
  operator action through the reviewed infrastructure workflow.
- Set the Actions usage budget to stop paid usage at $10 where the account
  supports it. Reserve an operator-run cleanup path so a blocked Actions run
  cannot leave UAT resources running.
- For retained resources, alert at
  $6 actual monthly cost and a forecast above
  $8. Remove eligible expired
  resources and seek a priced extension before the next month if required
  retention would exceed the limit. Preserve keys and images still needed for
  decryption, promotion, or rollback.

Check the inventory and forecast daily; use the saved model if AWS cannot yet
produce a forecast. AWS budget data typically refreshes every 8–12 hours, so
[costs can exceed a threshold before its alert arrives](https://docs.aws.amazon.com/cost-management/latest/userguide/budgets-managing-costs.html).
An alert does not stop a service, and stopping EC2 or RDS still leaves charges.
The mechanism that removes active AWS charges is the verified destruction of the
disposable UAT resources. No automatic budget-triggered stop or teardown exists
yet. Phase 10 must prove the selected controls and the direct cleanup path
before calling them operational.

## Cleanup and remaining resources

1. Record the synthetic data-loss scope, stop new writes, and complete pending
   media deletion work. Save only the required redacted evidence.
2. Stop the environment's schedules and tasks. Verify that no restore or
   production-shape drill target remains. Delete only targets with the expected
   environment and ownership tags; adjudicate stale ownership explicitly.
3. Empty only the UAT media bucket. Destroy the disposable environment under its
   no-final-snapshot contract with `delete_automated_backups = true`, including
   its primary RDS instance, EC2 capacity, root volumes, Elastic IP, edge
   resources, and UAT-only DNS. Confirm that no owned manual or retained
   automated backup remains. Retaining one changes this cost model and needs a
   priced retention decision.
4. Verify the remaining AWS and DNS inventory against the planned retained list.
   Preserve the state bucket and keys, retained UAT secret key, protected
   release images, required logs, and the shared email stack and Google DNS.
5. Record the residual monthly cost and review it after 30 days. Remove
   unreferenced images when their release window closes; preserve every UAT,
   promotion, and rollback digest while its release record requires it.

A seven-day stopped EC2/RDS UAT still has about $2.14 of infrastructure charges
before keys, registry, logs, email monitoring, and backup growth. RDS restarts
after seven days. This is a sensitivity for short maintenance, not the chosen
post-UAT lifecycle. Do not pause cleanup schedules while pending deletion work
still needs to meet its 24-hour target.

## GitHub and mail inputs

The current read-only account projection did not provide a plan name. GitHub's
[environment rules](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
do not offer private-repository required reviewers on Free, Pro, or Team. Tasks
10.1 and 10.12 currently require that approval mechanism.

Verify existing Enterprise Cloud eligibility and the private repository's
organization/namespace before those tasks activate. If it is unavailable, review
a replacement approval contract that preserves artifact provenance, scoped AWS
roles, and recorded owner approval. Do not silently omit the gate. GitHub
advertises Enterprise as starting at $21/user-month for the first 12 months;
renewal price, seat terms, and checkout commitment are unverified. That
conditional subscription is excluded from the ceilings above. This
recommendation does not request a subscription purchase.

The [email runbook](../../runbooks/email.md) records Singapore SES in sandbox,
the `aboutme-auth` configuration set, and the existing `aboutme-email` stack.
Adopt shared email as a persistent stack unit so CloudFormation remains the sole
owner of its children. Do not duplicate or destroy its SES, SNS, SQS,
CloudWatch, or DNS resources in disposable UAT state.

Phase 10 needs the current sandbox limits, approved verified recipients, the
runtime SES IAM policy, and a decision and implementation for feedback
consumption. Simulator delivery is separate from real verification/reset
workflows. The queue has no consumer today; any new consumer adds its runtime
and polling cost before activation. Preserve Google Workspace records and the
existing MAIL FROM subdomain.

## Phase 10 handoff and decision record

The [runtime handoff](../../plans/phase-10/runtime-refresh.md) and task
contracts carry the startup, private print, ARM64, access-control, and workflow
gaps. Before activation, prove mixed render/media/SSE load, API/SSR latency, job
headroom, the nightly restore, rollback, alarms, and origin-secret rotation on
the selected hardware. A failed capacity check needs a priced correction within
the approved ceiling or a new spending decision.

Owner budget decision: **pending** for the $100 AWS campaign ceiling, $10
Actions ceiling, 14-day lifetime, and up to $8/month retained footprint. GitHub
plan eligibility and any replacement approval mechanism remain separate Phase 10
inputs. No cloud activation is permitted until the required inputs, local gates,
and recorded spending decision are complete.

Reproduce the totals with `python3 -B docs/research/aws-cost/calculate.py`. The
phase's independent review and final local checks are recorded by the
integration owner before closure.
