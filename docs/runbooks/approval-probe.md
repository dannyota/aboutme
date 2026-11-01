# Anonymous approval probe

This harmless workflow measures the
[anonymous prerequisite](../design/production-pull-deploy/README.md#anonymous-verification-prerequisite).
It has no production connection, checkout, actions, credentials, or request
asset. Both jobs only echo fixed text and have empty token permissions. The
environment name and enrolled ID stand in for the production gate. The synthetic
`v0.0.0` comment authorizes nothing outside this measurement.

## Prepare

The primary lands the independently reviewed workflow on `main`, then creates
`approval-probe` under repository **Settings → Environments** before
dispatching:

1. Add `dannyota` as a required reviewer. Leave **Prevent self-review**
   unchecked. Disable administrator bypass. Add no secrets or variables.
2. Choose **Selected branches and tags**. Add exactly one **Branch** rule named
   `main`, with no tag rule. Save and inspect the settings again.
3. Fill [the pins](../../scripts/approval-probe.json) with positive numeric
   `repository_id`, `workflow_id`, `environment_id`, and `maintainer_ids`.
   `null` and the empty list deliberately refuse before any network request.
   Keep enrolled IDs in that file alone; review its changes before measuring.
4. Obtain public IDs from the repository, workflow, environment, and user REST
   endpoints below. Use an anonymous curl invocation with `--disable` first,
   `--proxy ''`, and no authentication header. Filter only the ID fields into
   the config; do not save or publish raw responses, profiles, or headers.

```text
GET /repos/dannyota/aboutme
GET /repos/dannyota/aboutme/actions/workflows/approval-probe.yml
GET /repos/dannyota/aboutme/environments/approval-probe
GET /users/dannyota
```

GitHub supports one required reviewer and optional self-review prevention.
Configure these settings explicitly; the three probe endpoints do not attest the
environment's protection settings. See
[environment protections](https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments)
and
[environment management](https://docs.github.com/en/rest/deployments/environments).

Use a separately reviewed checkout to run the checker, with Bash, Python 3, and
curl on `PATH`. The script discards inherited environment variables for curl,
disables curl configuration and proxies, follows no redirects, and sends no
token. It does not use `gh` or a credential store. Run it only in the primary
session under the repository's bounded-command rules.

## Inspect a run

Choose and record `head_sha` independently from trusted `main` **before** the
dispatch. Do not derive the expected value from the run under examination.
Compute a harmless request hash once:

```sh
request_hash=$(printf '%s' 'aboutme approval feasibility only' | shasum -a 256 | awk '{print $1}')
```

In **Actions → Approval probe → Run workflow**, select `main`. Copy its run ID
from the run URL into `run_id`. At the waiting environment gate, the reviewer
must enter this exact comment, including the synthetic tag:

```sh
printf 'approve production deploy v0.0.0 run %s attempt 1 sha256:%s\n' \
  "$run_id" "$request_hash"
```

After the scenario settles, inspect anonymously:

```sh
bash scripts/approval-probe.sh "$run_id" "$head_sha" "$request_hash"
```

Exit 0 means `accept`; exit 1 means `refuse`. Standard output is one JSON object
with a fixed `reason`, `requests_used`, and the latest valid response's numeric
rate limit, remaining count, and reset epoch. No response body, user identity,
comment, URL, or curl error is printed. Counters start null until headers
verify. Capture only this verdict and the public run URL in the evidence record.

The checker applies the design table to the probe's enrolled identities. It
requires both successful attempt-one jobs in dependency order. It reads every
approval page, counts relevant reviews without deduplication, and never selects
a newest approval: the documented review object has no review timestamp. Any
relevant rejection or wrong comment refuses regardless of list order.
Self-approval by an enrolled human is accepted deliberately.

## Scenarios

Run each case separately. Wait for a terminal result in the UI; avoid API
polling while waiting. Set the expected SHA and hash before each new dispatch.

| Case             | Procedure                                                                                                         | Expected result                                                     |
| ---------------- | ----------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------- |
| Approve          | An enrolled maintainer dispatches; the owner approves with the exact comment; both jobs finish.                   | `ACCEPTED`                                                          |
| Self-approve     | The owner dispatches and approves with the exact comment.                                                         | `ACCEPTED`                                                          |
| Reject           | The owner rejects at the waiting gate.                                                                            | `APPROVAL_REJECTED`                                                 |
| Cancel           | Cancel a fresh run while it waits for approval; wait for the run to settle.                                       | `RUN_CANCELLED`                                                     |
| Rerun            | Rerun all jobs of a finished approved run; inspect the same run ID after the attempt changes.                     | `RUN_ATTEMPT`                                                       |
| Wrong comment    | Approve a fresh dispatch with harmless text that differs from the expected comment.                               | `APPROVAL_COMMENT`                                                  |
| Missing approval | Inspect a fresh run still waiting, then cancel it. Fixtures separately cover missing history on a successful run. | `RUN_INCOMPLETE`; fixture: `APPROVAL_MISSING`                       |
| Pagination       | Use the procedure below and retain the actual observed response shape.                                            | Acceptance only with exactly one relevant approval across all pages |

For approval by someone other than the dispatcher, use another enrolled
maintainer to dispatch. If none is available, mark that observation unverified;
the self-approval case alone does not prove distinct identities.

For pagination, pass a fourth argument of `1` to request one approval per page:

```sh
bash scripts/approval-probe.sh "$run_id" "$head_sha" "$request_hash" 1
```

The checked-in workflow offers one environment gate, so a normal live run has
only one review. The primary can try repeated reviews of the same pending gate
before it settles, but GitHub may refuse a second review. If multiple relevant
reviews are visible, the expected result is `APPROVAL_COUNT`, or a conflicting
comment/rejection refusal. Never treat that run as accepted.

The fake-curl fixture exercises an unrelated review on page one and the sole
relevant approval on page two, including `Link` traversal. Its execution awaits
hosted checks. GitHub's
[review-history reference](https://docs.github.com/en/rest/actions/workflow-runs#get-the-review-history-for-a-workflow-run)
does not document `per_page` or `page` for this endpoint. A single live page
does **not** prove pagination support. Record unavailable live pagination as
unverified and return the prerequisite to the owner; do not claim the full
anonymous prerequisite has passed or build deployment machinery on that basis.
Do not add another environment or workflow shape silently to manufacture it.

HTTP failures, malformed JSON, missing fields, unsafe or looping links,
duplicate identities, and unexpected jobs refuse. The hosted fixtures cover
these paths, 403/429 rate exhaustion, and conflicting approvals in either order.
Do not exhaust the shared public rate limit to test 403 manually.

## Budget and evidence

GitHub's anonymous core limit is
[60 requests per hour per originating IP](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api).
All anonymous clients sharing that IP consume the same allowance. Use response
headers to measure it; never assume a fresh allowance. A successful probe uses
`1 run + P approval pages + 1 jobs page = P + 2` requests, normally three. Each
invocation permits at most 12 requests and preserves at least 12 remaining
requests. It makes no retries and stops on 403, 429, or insufficient budget.
Each request has a 5-second connect and 15-second total timeout.

A proposed hourly complete-cycle envelope is:

| Work                                                                | Maximum requests |
| ------------------------------------------------------------------- | ---------------: |
| Discover approval runs twice                                        |                2 |
| Verify selected approval run, including up to two history pages     |                4 |
| Coordinator and release CI run discovery and detail                 |                4 |
| Publication run discovery, detail, and jobs                         |                3 |
| Release metadata and digest asset                                   |                2 |
| Repeat mutable approval, main-ref, and CI evidence before admission |                7 |
| Observer GitHub reads                                               |                8 |
| Other clients sharing the IP                                        |                6 |
| Total                                                               |               36 |
| Remaining from 60                                                   |               24 |

This is a budget hypothesis, not measured feasibility. The primary must count
every actual request, redirects and pages included, for a complete cycle. Record
observer use and other clients on the same IP, plus starting/ending remaining
counters and reset epochs. A larger actual count replaces this estimate. Cache
immutable bytes, bound discovery frequency, and stop before the reserve is
consumed. The probe alone cannot prove the CI, publication, or observer
allowance. Revalidation within 120 seconds remains the
[host contract](../design/production-pull-deploy/evidence.md).

Fill [docs/research/approval-probe.md](../research/approval-probe.md) with
sanitized observations. Include exact tested commit, config commit reference,
public run URLs, UI-observed protections, scenario results, fixed verdict JSON,
request counts, rate counters, and unresolved mappings. Store no raw API bodies,
headers, review comments, login/profile data, tokens, or local environment.
Distinguish hosted fixture evidence from live GitHub observations. The evidence
template starts empty and makes no claim of success.

## Remove the probe

After the primary records the evidence and the owner decides the transport:

1. Disable **Approval probe** in Actions and cancel remaining probe runs.
2. Remove `.github/workflows/approval-probe.yml` through a reviewed change,
   updating its focused workflow assertions together.
3. Delete `approval-probe` in **Settings → Environments** after the workflow is
   gone. Confirm that no pending run can recreate an unprotected environment.
4. Retain the sanitized evidence. If the checker and fixtures are also removed,
   remove their config and Makefile entries in the same reviewed change.

## API references

- [Run and review history](https://docs.github.com/en/rest/actions/workflow-runs):
  public anonymous reads and the fields used for run and approval binding.
- [Attempt-one jobs](https://docs.github.com/en/rest/actions/workflow-jobs#list-jobs-for-a-workflow-run-attempt):
  the attempt-specific endpoint and documented jobs pagination.
- [Pagination](https://docs.github.com/en/rest/using-the-rest-api/using-pagination-in-the-rest-api):
  `Link` relations and page-size parameters where supported.
- [Rate headers and exhaustion](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api):
  authoritative remaining/reset headers and 403/429 handling.
