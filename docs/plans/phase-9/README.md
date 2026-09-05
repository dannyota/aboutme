# Phase 9 — AWS Singapore cost research

Status: **Planned** (2026-09-05). Research has not run.

**Goal:** choose a costed UAT deployment at `uat.aboutme.vn` in AWS Singapore
(`ap-southeast-1`) and estimate later production costs separately.

**Authority:** [ADR 0031](../../adr/0031-aws-cost-research-and-hosted-uat.md),
[deployment design](../../design/deployment.md),
[resource budgets](../../design/budgets.md), and the
[roadmap](../implementation-plan.md). Existing infrastructure tasks are a
comparison baseline, not a researched cost recommendation.

Use OpenTofu for infrastructure. Prefer managed AWS services when they meet the
workload, security, and cost requirements; account for operating effort when
comparing them with self-managed options.

## Inputs and scope

- Use the final Phase 6–8 runtime, render/media measurements, jobs, retention,
  and security requirements. Early research may gather prices; final sizing
  waits for those measurements.
- Use Singapore prices with official source URLs and retrieval dates. Separate
  global edge/certificate dependencies from regional application/data costs.
- Model short-lived UAT, idle UAT, and continuous production separately. State
  assumptions rather than inventing expected users or an approved budget.
- Read the [email runbook](../../runbooks/email.md). It records the Singapore
  SES sandbox, sender/configuration set, existing stack and missing runtime IAM.
  Price the missing integration and feedback processing explicitly. Preserve
  existing mail and DNS resources; no blanket rebuild is planned.
- Research is read-only. Public pricing and documentation are sufficient to
  start; do not provision benchmarks or buy commitments in this phase.

## Tasks

| Task                                  | Deliverable                                       | Predecessor                                 |
| ------------------------------------- | ------------------------------------------------- | ------------------------------------------- |
| [9.1](task-01-workload-and-prices.md) | Workload model and dated price inventory          | Final Phase 6–8 measurements for completion |
| [9.2](task-02-compare-options.md)     | Cost and operating comparison                     | 9.1                                         |
| [9.3](task-03-recommendation.md)      | Recommendation, spending limits, Phase 10 handoff | 9.2                                         |

The integration owner owns `docs/research/aws-cost/`, proposed ADR/design
changes, and the phase exit record. Use one researcher at a time on this laptop.
A fresh Sol reviewer checks the completed comparison and recommendation.

## Outputs and handoff

Task outputs live in `docs/research/aws-cost/`: `workload.md`, `pricing.csv`,
`comparison.md`, and `recommendation.md`. These are future deliverables, not
existing evidence. Account identifiers, private invoices, credentials, and
billing exports do not belong in this public repository.

The recommendation states a spending ceiling and UAT lifetime, including cleanup
and retained-resource cost. The owner resolves any budget amount from the priced
recommendation. The authorized region, UAT hostname, and DNS scope do not
require another general permission request.

Phase 10 cannot copy old sizing defaults before this phase finishes. Changes to
accepted architecture need a follow-up ADR and consistent design, plan, and
acceptance rows before implementation. See [exit criteria](exit-criteria.md).
