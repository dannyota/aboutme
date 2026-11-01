# Anonymous approval probe evidence

Status: partially verified on 2026-11-01. Live approval, rejection, comment,
rerun, cancellation, and missing-approval mappings behaved as designed.
Pagination and approval by a second maintainer remain unverified, and anonymous
reads were observed to be cached for up to 60 seconds.

## Context

- Observation date (UTC): 2026-11-01, about 14:00 to 14:25.
- Tested commit: `f1bd30a2` (checker, workflow, and runbook on `main`).
- Reviewed pins commit (IDs remain in `scripts/approval-probe.json`):
  `9b529813`.
- Independent expected main SHA source: `git rev-parse origin/main` after
  `git fetch`, taken before each dispatch.
- Required reviewer setting inspected: one required reviewer, `dannyota`.
- Self-review prevention disabled: yes (`prevent_self_review` false).
- Administrator bypass disabled: yes (`can_admins_bypass` false).
- Selected branch `main`, no tag rule: yes, one branch policy `main`.
- Environment secrets and variables absent: yes, zero of each.
- Hosted contracts run URL and exact commit:
  [run 41314714851](https://github.com/dannyota/aboutme/actions/runs/41314714851)
  on `f1bd30a2`, success.

## Live observations

The owner account dispatched every run and reviewed it through the REST pending
deployments endpoint. Each checker invocation was anonymous.

| Scenario                             | Run and attempt                                                                        | Expected                              | Observed verdict              | Requests | Remaining after | Status                           |
| ------------------------------------ | -------------------------------------------------------------------------------------- | ------------------------------------- | ----------------------------- | -------: | --------------: | -------------------------------- |
| Self-approve                         | [41314873657](https://github.com/dannyota/aboutme/actions/runs/41314873657), attempt 1 | `ACCEPTED`                            | `accept`, `ACCEPTED`          |        4 |              48 | Verified                         |
| Pagination, `per_page=1`             | 41314873657, attempt 1                                                                 | `ACCEPTED`, one approval across pages | `accept`, `ACCEPTED`          |        4 |              41 | Unverified: one review, one page |
| Rerun                                | 41314873657, attempt 2 waiting                                                         | `RUN_ATTEMPT`                         | `refuse`, `RUN_ATTEMPT`       |        4 |              37 | Verified                         |
| Reject                               | [41315177792](https://github.com/dannyota/aboutme/actions/runs/41315177792), attempt 1 | `APPROVAL_REJECTED`                   | `refuse`, `APPROVAL_REJECTED` |        2 |              35 | Verified                         |
| Wrong comment                        | [41315273143](https://github.com/dannyota/aboutme/actions/runs/41315273143), attempt 1 | `APPROVAL_COMMENT`                    | `refuse`, `APPROVAL_COMMENT`  |        2 |              33 | Verified                         |
| Missing approval, waiting            | [41315486803](https://github.com/dannyota/aboutme/actions/runs/41315486803), attempt 1 | `RUN_INCOMPLETE`                      | `refuse`, `RUN_INCOMPLETE`    |        1 |              32 | Verified                         |
| Cancel, checked at once              | 41315486803, attempt 1                                                                 | `RUN_CANCELLED`                       | `refuse`, `RUN_INCOMPLETE`    |        1 |              31 | Stale anonymous read             |
| Cancel, rechecked after cache window | 41315486803, attempt 1                                                                 | `RUN_CANCELLED`                       | `refuse`, `RUN_CANCELLED`     |        1 |              27 | Verified                         |

Approval by a maintainer other than the dispatcher: unverified. Only one
maintainer is enrolled, so the self-approval case does not prove distinct
identities.

### Anonymous caching

Right after cancellation, the authenticated view reported `completed` and
`cancelled`, while the anonymous checker still saw an unfinished run. The
anonymous run response carried `cache-control: public, max-age=60, s-maxage=60`.
A later anonymous read returned `completed` and `cancelled`. The checker refused
in both reads, so the stale read was safe, but the reason code was wrong until
the cache expired. A host must wait at least 60 seconds after a terminal run
state, or treat anonymous answers younger than that window as provisional.

## Complete-cycle budget

- Observation window and shared-IP client use: one hour window resetting at
  epoch 1793545205. Other anonymous reads from the same IP that hour included
  the pin discovery and two header reads.
- Approval requests and page count: one page per run (`P = 1`).
- Successful verification cost: 4 requests (`P + 3`), matching the runbook.
- Refusals cost 1 to 4 requests, depending on where the checker stopped.
- Total checker requests in this session: 19. The remaining counter fell from 48
  after the first check to 27.
- CI, publication, release, observer, and revalidation requests: not measured;
  the probe covers only the approval read path.
- Measured cycle or arithmetic hypothesis: the approval path is measured. The
  full 37-request cycle remains a hypothesis.

## Decision

- Unverified fields, ordering, or pagination: live pagination (the review
  history endpoint does not document `per_page` or `page`), and distinct
  dispatcher and approver identities.
- Owner transport decision (2026-11-01): accept with conditions, recorded in
  [ADR 0026](../adr/0026-vietnam-hosted-production.md#amendment-anonymous-approval-pull-deployment-2026-11-01).
  Anonymous approval reads and Ansible pull may proceed under its cache wait and
  single-page, single-review conditions. Solo owner approval is accepted; laptop
  deployment stays available in parallel. This decision does not turn unverified
  pagination, second-maintainer approval, or the full-cycle budget into verified
  evidence.
- Evidence supporting that decision: this record and the hosted fixtures.
- Probe workflow and environment removal: planned in the
  [design release order](../design/production-pull-deploy/README.md#release-order-and-owner-decisions),
  including removal of the checker and its supporting files after review.

## Sources

- [Design prerequisite](../design/production-pull-deploy/README.md#anonymous-verification-prerequisite)
- [Run and review history API](https://docs.github.com/en/rest/actions/workflow-runs#get-the-review-history-for-a-workflow-run)
- [Attempt jobs API](https://docs.github.com/en/rest/actions/workflow-jobs#list-jobs-for-a-workflow-run-attempt)
- [Pagination](https://docs.github.com/en/rest/using-the-rest-api/using-pagination-in-the-rest-api)
- [Anonymous rate limit](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)
