# vMonitor quota renewal

Approved direction (2026-10-14), pending implementation: GitHub renews the free
Synthetic Test and Email quotas behind the [Vietnam alarm](vietnam-production.md#logs-metrics-and-alarms).
The renew-only service account is the approved cloud-credential exception.
Amend ADR 0027 before enabling writes.

## Timing and budget

The [runbook](../runbooks/production.md#alarms) owns allowances, expiry dates,
and the provider's three notification-quota actions per month. Provider docs
limit renewal to the last seven days ([Synthetic], [Email]). Use the last
72 hours until live renewal confirms the allowed window and whether renewal
extends the old expiry or starts a new term. Early renewal may lose unused days.

Schedule `37 5,17 * * *` UTC. Compute remaining time from fresh provider reads
and current UTC time, never the scheduled timestamp or rounded calendar days.
[GitHub may delay or drop runs][schedule]; no minimum run count is guaranteed.
Fail if either quota has at most 48 hours left after processing. Expiry stops
checks or mail; recovery stays human. The uptime workflow remains a backstop.
GitHub disables idle public-repository schedules after 60 days [schedule]; the
runbook must check enabled state and recent runs, since silence sends no alert.

Normally email needs one renewal per term, possibly two in a calendar month
when renewing early. This is an expectation, not an enforced monthly ceiling.
Other notification-quota actions share the three-action budget. Whether refused
or failed requests consume it is unverified. On any failure, the primary disables
this workflow before investigation and cancels queued runs; never rerun an
ambiguous write. Disabling is a human response, not an atomic failure latch:
another scheduled run could start first and consume another action. Before
re-enabling, reconcile provider state, settlement, and remaining action budget.

## Workflow and trust boundary

`.github/workflows/vmonitor-renew.yml` has one `renew` job on `ubuntu-24.04`:

- Only `schedule` and `workflow_dispatch`; dispatch input `action` is a required
  choice of `renew` (default) or `check`. Schedule selects `renew`. Pass the
  action through step `env`, validate it in the script, never interpolate input
  into shell source. `check` calls only read commands with `--read-only`.
- `permissions: {}` with no job override; job guard requires repository
  `dannyota/aboutme`, ref `refs/heads/main`, and one of the two allowed events.
  Workflow concurrency is `vmonitor-renew`, `cancel-in-progress: false`;
  job timeout is 10 minutes. This serializes this workflow, not console actions.
- `environment: vmonitor-renew`, already configured with the selected **Branch**
  rule `main`, no tag rule. `VNGCLOUD_REGION` comes from `vars`; client ID and
  client secret come from environment secrets. Do not duplicate them in
  repository or organization secrets. Environment policy evaluates the run ref,
  not the checkout ref: a nonmatching job [fails before running][branch-failure],
  rather than running without secrets. [Protection rules][environments] gate
  secret access even if a malicious branch edits away the job guard.
- Forks do not inherit these secrets; ordinary pull-request refs do not match
  `main`. An environment is not bound to one workflow file. Trusted-main
  `pull_request_target` or `workflow_run` code could request it: never execute
  PR-controlled code with this environment. Review workflow and contract-test
  changes before merge; green candidate CI alone is not a security boundary.
- SHA-pinned `actions/checkout` reads `dannyota/aboutme`, `refs/heads/main`,
  `persist-credentials: false`. No candidate checkout or local action runs.
  The main script may be newer than the event commit; log the checkout SHA.
- SHA-pinned `actions/setup-go` mirrors `.tool-versions`, with `cache: false`
  ([default caching][setup-go] must be disabled). All action pins carry version
  comments and use grouped Dependabot updates.
- Install `danny.vn/vngcloud/cmd/vngcloud@<pinned-version>` with
  `GOTOOLCHAIN=local`, `GOSUMDB=sum.golang.org`, empty `GOPRIVATE`, `GONOSUMDB`,
  and `GOINSECURE`. Use a fresh temporary install directory and no restored
  module or build cache. Assert the exact module path, version, and reviewed
  `h1:` sum in `go version -m` before executing that same absolute binary.
  [Go verifies downloaded dependencies][gomod]; review CLI and dependency pin
  changes, since hashes establish identity rather than safe behavior.
- Only the final script step receives `VNGCLOUD_CLIENT_ID`,
  `VNGCLOUD_CLIENT_SECRET`, and `VNGCLOUD_REGION`. No username, password, profile,
  or alternate endpoint configuration. No cloud secret during checkout/install.

## Script and CLI contract

`scripts/vmonitor-renew.sh` uses JSON and `jq`. Validate both quota reads before
any write: `monitor get-synthetic-quota` and
`monitor get-notification-quota --kind Email`. Missing quotas, invalid JSON,
unknown/non-active status, unparseable `EndTime`, or expiry fail closed. Match the
pinned CLI's documented field names and active status; do not guess aliases.

Process Synthetic then Email. In `renew` mode, when `0 < seconds-left <= 259200`,
call `monitor renew-synthetic-quota` or
`monitor renew-notification-quota --kind Email`, respectively, with the exact
read value as `--expected-end-time`, `--max-days-left 3`, and `--max-price 0`.
Otherwise skip. The CLI must re-read and reject a stale expected end, refuse
missing/nonzero prices, and send at most once. Neither script nor CLI retries
a write, including authentication errors, timeouts, 429s, or 5xx responses.
Bounded polling may only read. A nonzero CLI exit stops processing and fails;
never conceal `NotSettled` with a later successful read or continue to Email.

After success, read again: a sent renewal requires a strictly later end time;
all quotas must remain active with more than 172800 seconds left for a pass.
Apply this threshold in `check` mode too. `IN-TRASH` or expiry never invokes
Recover or Buy. A later successful run reads the extended term and skips it.
`--expected-end-time` detects stale reads; it is not a durable idempotency key
or a proven server-side compare-and-swap. An unchanged end after an ambiguous
request does not prove no write occurred. Human renewal must not overlap a run.

Capture stdout/stderr privately; emit only fixed quota labels, validated UTC
times, known statuses/actions, numeric prices, and exit codes. Never forward raw
errors, bodies, identifiers, or tokens to logs, annotations, or summaries. No
tracing, `--debug`, credential arguments, artifacts, or token-cache uploads.
Remove temporary responses/token files on exit; runner disposal covers abrupt
termination. [GitHub masking][secrets] is only a backstop. Script failures emit a
fixed `::error::` and summary; timeout/setup failures remain failed jobs. Keep
Actions failure notifications enabled for schedule and dispatch actors.

## Custody and limits

`aboutme-vmonitor-renew` has no console login. Allow only these `vmonitor` actions
on resource `*`:
`GetSyntheticQuota`, `GetSyntheticTestQuotaDetail`, `GetSyntheticUsage`,
`GetEmailQuota`, `GetEmailUsage`, `GetQuotaUsages`, `ListDetailedQuotasInTrash`,
`RenewSyntheticQuota`, and `RenewEmailQuota`. No Recover, Buy, or other writes.
Prove reads/quotes work and inspect effective IAM for excluded writes before
enabling renewal. No mutation probes or permission widening to make the CLI pass.

Keep an age-encrypted backup in `aboutme-infra`. Replace the environment key
through a secret-safe pipe, run `check`, then revoke the old key. Revoke on
suspected compromise or loss of trusted-main access. Issued tokens reportedly
last 1800 seconds; verify whether key revocation invalidates them sooner. A stolen
key can spend the action budget and bypass the CLI price guard. Main writers,
admins, the runner, and pinned actions/CLI are trusted with this credential.

Visitors, users, hostile resumes, stolen app sessions/MCP tokens, database
writers, leaked application backups, and compromised host containers have no
path to this key; it never reaches the host or application data. Require TLS
verification. No app API, schema, client, or data migration changes. Rollback
returns to manual renewal. Laptop timers need uptime; host timers expose the key
to host compromise. Paid packages were rejected; changing channels still leaves
Synthetic renewal.

## Verification and release

Extend `scripts/test/workflow-contract.test.mjs` to assert the exact trigger,
input, guard, permission, environment, concurrency, timeout, checkout, action
pins, toolchain, cache, install/hash-check order, and step-only secret/variable
mapping above. Reject extra jobs/actions, secret-bearing expressions in `run`,
`continue-on-error`, bypassed hash checks, artifacts, and debug flags. Include
negative fixtures that weaken each boundary. Static YAML tests cannot prove
server-side environment settings or IAM; inspect their nonsecret metadata.

`scripts/test/vmonitor-renew-test.sh` uses a fake CLI and clock: test 72/48-hour
boundaries, delayed runs, `check` never renewing, invalid action/read/status/time,
expiry, price refusal, stale end, all nonzero exits, failed settlement with zero
resends, no Email write after Synthetic failure, and a successful subsequent run
skipping extended terms. Inject hostile/error output, fake secrets and tokens;
assert stdout, stderr, annotations, and summaries disclose none of them.

Release order: publish CLI version/sum and JSON contract; verify narrowed IAM
and environment metadata; amend ADR 0027 and the runbook's credential rule;
merge after independent design/adversarial implementation review and green CI;
dispatch `action=check` on main. Keep manual renewal until live success confirms
term arithmetic. No owner choice is open; pins, provider settlement/budget
behavior, and live evidence remain due.

[Synthetic]: https://docs.greennode.ai/vmonitor/dashboards/synthetics/lam-viec-voi-synthetic-test-quota
[Email]: https://docs.greennode.ai/vmonitor/dashboards/notification/lam-viec-voi-email-notification-quota
[schedule]: https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#schedule
[branch-failure]: https://github.blog/changelog/2021-02-17-github-actions-limit-which-branches-can-deploy-to-an-environment/
[environments]: https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments
[setup-go]: https://github.com/actions/setup-go#caching-dependency-files-and-build-outputs
[gomod]: https://go.dev/ref/mod#authenticating
[secrets]: https://docs.github.com/en/actions/concepts/security/secrets
