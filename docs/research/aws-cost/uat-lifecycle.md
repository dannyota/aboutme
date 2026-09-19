# UAT lifecycle cost

Status: historical model. ADR 0037 removed hosted UAT from the first release.
The figures below preserve the earlier planning record and do not describe the
live production service.

The two-day planning campaign with 16 test hours costs **USD 27.636091 per
730-hour month before tax**, without free-tier allowances. It leaves USD
2.363909 below the approved USD 30 ceiling. This is a planning envelope under
[ADR 0035](../../adr/0035-replica-coordination-and-uat-lifecycle.md), pending
local lifecycle proof and hosted timing measurements.

## Reproduction and scope

Run `python3 -B docs/research/aws-cost/uat-lifecycle.py --check` at the
repository root. The [inputs](uat-lifecycle-inputs.json) and
[generated line items](uat-lifecycle-results.csv) use the saved gross rates in
[pricing.csv](pricing.csv). Use `--write` after an intentional input change. The
Phase 9 model and its historical results remain separate.

The model retains the Phase 9 baseline's traffic, media, logs, alarms,
dashboards, backups, keys, state and image storage. It conservatively retains 20
restore-instance hours, four production-shape drill hours, and four second-node
proof hours even though the planned campaign is shorter. UAT bears the full
retained/shared inventory in this standalone estimate.

Application nodes terminate between windows; their root disks and public IPv4
charges are prorated only if cleanup proves their absence. RDS remains available
for the campaign and its final 24-hour writer tail. Later daily wake reserves
assume the accepted next-due proof permits those stopped intervals.

| Quantity                                           | Planning envelope                                                       |
| -------------------------------------------------- | ----------------------------------------------------------------------- |
| Public test hours                                  | 16 over two calendar days                                               |
| Campaign plus final writer tail                    | 72 hours                                                                |
| Later maintenance wakes                            | 28; 15 node minutes and two RDS hours each                              |
| Off-test campaign maintenance                      | 56 hourly windows of ten node minutes                                   |
| Recovery reserve                                   | Four node hours and four RDS hours                                      |
| Primary node hours, including maintenance/recovery | 36.333333                                                               |
| Primary RDS hours, including maintenance/recovery  | 132                                                                     |
| Scheduler invocations                              | 800, covering 730 hourly evaluations and 70 other deliveries            |
| Controller log allowance                           | Within the existing 2 GiB ingestion and 12 GiB-month archive quantities |

## Controller reserve

| Item                    | Quantity                                                  | Gross USD |
| ----------------------- | --------------------------------------------------------- | --------: |
| Lifecycle/proof Fargate | 500 tasks, five minutes, 0.25 vCPU/0.5 GiB, public IPv4   |  0.721771 |
| Infrastructure Fargate  | Ten tasks, ten minutes, 0.5 vCPU/1 GiB, public IPv4       |  0.049408 |
| Step Functions Standard | 50,000 transitions, including retries                     |  1.250000 |
| S3 requests             | 10,000 operations, conservatively charged at the PUT rate |  0.050000 |
| Additional KMS requests | 10,000                                                    |  0.030000 |
| Controller total        | No allowance deductions                                   |  2.101179 |

Singapore Step Functions SKU `Q77S8DTHK7YGTP6Z` was verified on 2026-09-06 at
USD 0.000025 per transition in the
[AWS regional Price List](https://pricing.us-east-1.amazonaws.com/offers/v1.0/aws/AmazonStates/current/ap-southeast-1/index.json).
Retries are billable transitions under
[Step Functions pricing](https://aws.amazon.com/step-functions/pricing/). The
saved [Fargate](https://aws.amazon.com/fargate/pricing/),
[public IPv4](https://aws.amazon.com/vpc/pricing/),
[S3](https://aws.amazon.com/s3/pricing/) and
[KMS](https://aws.amazon.com/kms/pricing/) rates retain their source URLs and
retrieval dates in the price table.

Lifecycle and proof tasks are independent Fargate tasks. Application serving and
maintenance remain on EC2. Credentials use standard SSM `SecureString`
parameters under the existing retained KMS key. This model adds no Secrets
Manager secret, KMS key, NAT gateway, VPC endpoint, paid Actions use, or
subscription.

## Booking controls

The USD 25 threshold concerns **actual cost**; USD 30 concerns **forecast
cost**. Optional testing stops at either threshold. Reserved cleanup and due
privacy work remain required. Billing data arrives with delay, so these controls
are not an instantaneous spending cap.

Task durations include image download, startup, execution, and shutdown. The
quantities are reserves, not measured guarantees. Startup or job overruns, extra
recoveries, more transitions, missing teardown, additional retained storage, or
excess logs require a revised forecast before optional work continues. The
shorter test window is not proof that all Phase 10 workflows will fit it.
Production launch and its account-wide cost forecast remain separate Phase 11
gates.
