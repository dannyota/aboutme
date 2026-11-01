# Anonymous approval probe evidence

Status: unverified. Template only; no live scenarios or checks recorded.

## Context

- Observation date (UTC):
- Tested commit:
- Reviewed pins commit (IDs remain in `scripts/approval-probe.json`):
- Independent expected main SHA source:
- Required reviewer setting inspected:
- Self-review prevention disabled:
- Administrator bypass disabled:
- Selected branch `main`, no tag rule:
- Environment secrets and variables absent:
- Hosted contracts run URL and exact commit:

## Live observations

Use the [runbook](../runbooks/approval-probe.md). Repeat this block for approve,
self-approve, reject, cancel, rerun, wrong comment, missing approval, and
pagination. Keep fixture results separate.

- Scenario:
- Public run URL and attempt:
- Expected fixed reason:
- Observed verdict JSON:
- Requests used:
- Starting and ending remaining/reset counters:
- UI-observed result:
- Verified or unverified, with precise limitation:

## Complete-cycle budget

- Observation window and shared-IP client use:
- Discovery requests:
- Approval requests and page count:
- CI and publication requests:
- Release and asset requests:
- Mutable evidence revalidation requests:
- Observer requests:
- Other requests, redirects, and pagination:
- Total requests:
- Starting and ending remaining/reset counters:
- Reserve maintained:
- Measured cycle or arithmetic hypothesis:

## Decision

- Unverified fields, ordering, or pagination:
- Owner transport decision:
- Evidence supporting that decision:
- Probe workflow and environment removal:

## Sources

- [Design prerequisite](../design/production-pull-deploy/README.md#anonymous-verification-prerequisite)
- [Run and review history API](https://docs.github.com/en/rest/actions/workflow-runs#get-the-review-history-for-a-workflow-run)
- [Attempt jobs API](https://docs.github.com/en/rest/actions/workflow-jobs#list-jobs-for-a-workflow-run-attempt)
- [Pagination](https://docs.github.com/en/rest/using-the-rest-api/using-pagination-in-the-rest-api)
- [Anonymous rate limit](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api)
