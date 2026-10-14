# vMonitor quota renewal

Approved direction (2026-10-14), pending implementation: GitHub renews the free
Synthetic Test and Email quotas behind the
[Vietnam alarm](vietnam-production.md#logs-metrics-and-alarms). The renew-only
service account is the approved cloud-credential exception. Amend ADR 0027
before enabling writes.

## Timing and budget

The [runbook](../runbooks/production.md#alarms) owns allowances, expiry dates,
and the provider's three notification-quota actions per month. Provider docs
limit renewal to the last seven days ([Synthetic], [Email]). Use the last 72
hours until live renewal confirms the allowed window and whether renewal extends
the old expiry or starts a new term. Early renewal may lose unused days.

Schedule `37 5,17 * * *` UTC. Compute remaining time from fresh provider reads
and current UTC time, never the scheduled timestamp or rounded calendar days.
[GitHub may delay or drop runs][schedule]; no minimum run count is guaranteed.
Fail if either quota has at most 48 hours left after processing. Expiry stops
checks or mail; recovery stays human. The uptime workflow remains a backstop.
GitHub disables idle public-repository schedules after 60 days [schedule]; the
runbook must check enabled state and recent runs, since silence sends no alert.

Normally email needs one renewal per term, possibly two in a calendar month when
renewing early. This is an expectation, not an enforced monthly ceiling. Other
notification-quota actions share the three-action budget. Whether refused or
failed requests consume it is unverified. An unconfirmed send leaves the
[persistent hold](#persistent-hold) set until human reconciliation. Confirmed
renewals need no human clearance, so monthly renewal remains unattended.

## Workflow and trust boundary

`.github/workflows/vmonitor-renew.yml` has one `renew` job on `ubuntu-24.04`:

- Initially only `workflow_dispatch`; add `schedule` through the release gate
  below. These are the only permitted events. Dispatch input `action` requires a
  choice of `renew` (default) or `check`. Schedule selects `renew`. Pass the
  action through step `env`, validate it in the script, never interpolate input
  into shell source. `check` calls only read commands with `--read-only`.
- `permissions: {}` with only job-level `issues: write`; job guard requires
  repository `dannyota/aboutme`, ref `refs/heads/main`, and an allowed event.
  Workflow concurrency is `vmonitor-renew`, `cancel-in-progress: false`; job
  timeout is 10 minutes. This serializes this workflow, not console actions.
- `environment: vmonitor-renew`, already configured with the selected **Branch**
  rule `main`, no tag rule. `VNGCLOUD_REGION` comes from `vars`; client ID and
  client secret come from environment secrets. Do not duplicate them in
  repository or organization secrets. Environment policy evaluates the run ref,
  not the checkout ref: a nonmatching job [fails before
  running][branch-failure], rather than running without secrets. [Protection
  rules][environments] gate secret access even if a malicious branch edits away
  the job guard.
- Forks do not inherit these secrets; ordinary pull-request refs do not match
  `main`. An environment is not bound to one workflow file. Trusted-main
  `pull_request_target` or `workflow_run` code could request it: never execute
  PR-controlled code with this environment. Review workflow and contract-test
  changes before merge; green candidate CI alone is not a security boundary.
- SHA-pinned `actions/checkout` reads `dannyota/aboutme`, `refs/heads/main`,
  `persist-credentials: false`. No candidate checkout or local action runs. The
  main script may be newer than the event commit; log the checkout SHA.
- SHA-pinned `actions/setup-go` mirrors `.tool-versions`, with `cache: false`
  ([default caching][setup-go] must be disabled). All action pins carry version
  comments and use grouped Dependabot updates.
- Install `danny.vn/vngcloud/cmd/vngcloud@<pinned-version>` with
  `GOTOOLCHAIN=local`, `GOSUMDB=sum.golang.org`, empty `GOPRIVATE`, `GONOSUMDB`,
  and `GOINSECURE`. Use a fresh temporary install directory and no restored
  module or build cache. Assert the exact module path, version, and reviewed
  `h1:` sum in `go version -m` before executing that same absolute binary. [Go
  verifies downloaded dependencies][gomod]; review CLI and dependency pin
  changes, since hashes establish identity rather than safe behavior.
- Only the final script step receives `VNGCLOUD_CLIENT_ID`,
  `VNGCLOUD_CLIENT_SECRET`, and `VNGCLOUD_REGION`. No username, password,
  profile, or alternate endpoint configuration. No cloud secret during
  checkout/install.

## Persistent hold

The owner creates one dedicated, initially open issue in `dannyota/aboutme`; pin
its number in the main script. Closed means armed; open blocks sends. Pass
`GITHUB_TOKEN` to the final script step for the issue API. Every run, including
schedules, dispatches, and reruns, reads the issue before quota processing.
Reject open/missing/invalid state or API failure before any renewal command;
`check` reports the hold without changing it and may continue read-only checks.
Workflow concurrency covers this sequence; no other workflow may use this key.

After both quota reads validate, immediately before the first renewal command
that may send, reopen the issue as a temporary guard and read back its open
state. An uncertain marker write aborts without renewal. This guard must precede
the send: a failure handler alone cannot cover cancellation or runner loss. A
run that is not due or refuses before reaching this guard leaves no hold.

The same run closes its own guard when each renewal request is either confirmed
by readback of a strictly later end time or proven unsent. This includes a
confirmed send followed by a proven-unsent price refusal or stale expected end
time. Closing the guard does not turn a failed job into a pass; GitHub failure
notifications still apply. Require documented, machine-readable CLI evidence for
that distinction; an exit code or unchanged provider end time alone is
insufficient. Confirm closed state before reporting completion. Never delete or
replace the issue, or close a hold inherited from an earlier run. Store no
credentials or provider response bodies in the issue.

Any possibly sent request without confirmation leaves the guard open as a
persistent hold. This includes timeout, 5xx, `NotSettled`, authentication or
transport failure after the request may have left, and interruption after an
unconfirmed send. A nonzero exit retains the hold only if a request remains
unconfirmed. Later successful reads do not clear that failure. Missing send
evidence after interruption is uncertain, even if termination occurred between
opening the guard and sending. Failed guard closure also blocks later sends
until reconciled. These failures need human action; a completed, confirmed
renewal leaves no hold and needs no human clearance.

Only a human clears a persistent hold: disable the workflow, cancel queued and
active runs, wait for termination, reconcile both quotas, settlement, and
remaining action budget, then close the issue and re-enable. An unchanged end
time cannot resolve an ambiguous response; keep the hold until the provider
resolves the outcome. Missing issues fail closed and require owner repair.
Manual renewal uses the same disabled-workflow window.

The [issue API][issues] adds repository-wide issue mutation power, with no
per-issue `GITHUB_TOKEN` scope. Grant no `actions: write`: self-disabling after
failure misses abrupt termination and adds workflow control. A stolen token with
only [Actions write][actions-api] can dispatch, re-enable, or cancel runs, but
cannot close the hold or edit trusted main. Repeated dispatches remain blocked;
disabling alarms remains possible. Issue writers can bypass this hold and must
be trusted. A stolen job token can clear it; runner/actions compromise already
exposes the cloud key. Revoke credentials and disable the workflow on that
compromise; the hold does not contain it.

Residual risk: a stolen token with both [Issues write][issues] and [Actions
write][actions-api] can close the hold issue and dispatch a renewal on main.
Through this path, the service account can only read and renew the free quotas,
and the trusted-main CLI requires a 0 VND quote. The worst case is spending the
monthly notification-quota action budget and a possibly early renewal, never
cost. The primary accepts this residual risk for the owner.

## Script and CLI contract

`scripts/vmonitor-renew.sh` uses JSON and `jq`. Validate both quota reads before
any write: `monitor get-synthetic-quota` and
`monitor get-notification-quota --kind Email`. Missing quotas, invalid JSON,
unknown/non-active status, unparseable `EndTime`, or expiry fail closed. Match
the pinned CLI's documented field names and active status; do not guess aliases.

Process Synthetic then Email. In `renew` mode, when
`0 < seconds-left <= 259200`, call `monitor renew-synthetic-quota` or
`monitor renew-notification-quota --kind Email`, respectively, with the exact
read value as `--expected-end-time`, `--max-days-left 3`, and `--max-price 0`.
Otherwise skip. The CLI must re-read and reject a stale expected end, refuse
missing/nonzero prices, and send at most once. Neither script nor CLI retries a
write, including authentication errors, timeouts, 429s, or 5xx responses.
Bounded polling may only read. A nonzero CLI exit stops processing and fails;
never conceal `NotSettled` with a later successful read or continue to Email.

After success, read again: a sent renewal requires a strictly later end time;
all quotas must remain active with more than 172800 seconds left for a pass.
Apply this threshold in `check` mode too. `IN-TRASH` or expiry never invokes
Recover or Buy. A later run skips confirmed extended terms without human action.
`--expected-end-time` detects stale reads; it is not a durable idempotency key
or a proven server-side compare-and-swap. An unchanged end after an ambiguous
request does not prove no write occurred. Human renewal must not overlap a run.

Capture stdout/stderr privately; emit only fixed quota labels, validated UTC
times, known statuses/actions, numeric prices, and exit codes. Never forward raw
errors, bodies, identifiers, or tokens to logs, annotations, or summaries. No
tracing, `--debug`, credential arguments, artifacts, or token-cache uploads.
Remove temporary responses/token files on exit; runner disposal covers abrupt
termination. [GitHub masking][secrets] is only a backstop. Script failures emit
a fixed `::error::` and summary; timeout/setup failures remain failed jobs. Keep
Actions failure notifications enabled for schedule and dispatch actors.

## Custody and limits

`aboutme-vmonitor-renew` has no console login. Allow only these `vmonitor`
actions on resource `*`: `GetSyntheticQuota`, `GetSyntheticTestQuotaDetail`,
`GetSyntheticUsage`, `GetEmailQuota`, `GetEmailUsage`, `GetQuotaUsages`,
`ListDetailedQuotasInTrash`, `RenewSyntheticQuota`, and `RenewEmailQuota`. No
Recover, Buy, or other writes. Prove reads/quotes work and inspect effective IAM
for excluded writes before enabling renewal. No mutation probes or permission
widening to make the CLI pass.

Keep an age-encrypted backup in `aboutme-infra`. Replace the environment key
through a secret-safe pipe, run `check`, then revoke the old key. Revoke on
suspected compromise or loss of trusted-main access. Issued tokens reportedly
last 1800 seconds; verify whether key revocation invalidates them sooner. A
stolen key can spend the action budget and bypass the CLI price guard. Main
writers, admins, the runner, and pinned actions/CLI are trusted with this
credential.

Visitors, users, hostile resumes, stolen app sessions/MCP tokens, database
writers, leaked application backups, and compromised host containers have no
path to this key; it never reaches the host or application data. Require TLS
verification. No app API, schema, client, or data migration changes. Rollback
returns to manual renewal. Laptop timers need uptime; host timers expose the key
to host compromise. Paid packages were rejected; changing channels still leaves
Synthetic renewal.

## Verification and release

Extend `scripts/test/workflow-contract.test.mjs` to assert the release-stage
trigger, input, guard, permission, environment, concurrency, timeout, checkout,
action pins, toolchain, cache, install/hash-check order, and step-only
secret/variable mapping above. Reject extra jobs/actions, secret-bearing
expressions in `run`, `continue-on-error`, bypassed hash checks, artifacts, and
debug flags. Include negative fixtures that weaken each boundary. Static YAML
tests cannot prove server-side environment settings or IAM; inspect their
nonsecret metadata.

`scripts/test/vmonitor-renew-test.sh` fakes the CLI, issue API, and clock. Test
72/48-hour boundaries, delayed runs, `check` never renewing, invalid
action/read/status/time, expiry, price refusal, stale end, all nonzero exits,
failed settlement with zero resends, no Email write after Synthetic failure, and
a subsequent run skipping confirmed extended terms without human clearance.
Assert exact argv and call counts for every CLI command, including
`--read-only`, Email kind, the unchanged expected end time, `--max-days-left 3`,
and `--max-price 0`. Assert one renewal invocation per due quota reached, zero
on earlier refusal or failure. Persist the fake issue across runs: confirmed
success leaves it closed; not due, pre-send refusal, price refusal, and stale
expected end time leave no hold when no request was sent. Unconfirmed sends,
including timeout, 5xx, `NotSettled`, authentication/transport failure, and
interruption after an unconfirmed send, leave it open. A nonzero exit retains
the hold only if a request remains unconfirmed. Assert zero sends from every
later schedule, dispatch, and rerun while that hold exists, even if provider
reads show an extended term. Test interruption between guard creation and send
with missing send evidence, failed guard closure, and partial batch success.
Only human reconciliation permits a later run to send. Test confirmed Synthetic
renewal followed by a machine-readable Email price refusal proving no send: the
guard closes, the job still fails, and neither renewal is retried. Failed marker
reads/writes or missing issues must send zero renewals; `check` never clears the
hold. Inject hostile output, fake secrets and tokens; assert stdout, stderr,
annotations, and summaries disclose none of them.

Before credentialed use, obtain the CLI maintainer's passing transport-test
evidence for the exact pinned release: state changed after the script's read and
missing/nonzero prices send zero renewal requests; ambiguous responses such as
authentication errors, disconnects/timeouts, 429s, 5xx, and `NotSettled`, cause
zero resends. The evidence must also prove the CLI's machine-readable no-send
result for refusals; missing or ambiguous results must never clear the guard.
Evidence names the release commit, test command, run URL, and request-count
assertions using the real CLI transport path. Fake-CLI tests cannot prove this.

Release order: publish CLI version/sum, JSON contract, and transport evidence;
verify narrowed IAM and environment metadata; amend ADR 0027 and the runbook's
credential rule; create the hold issue. Merge with **no schedule trigger** after
independent design/adversarial implementation review and green CI. The first
credentialed run is `action=check` on main. After it passes, the owner clears
the initial hold and coordinates one due `action=renew` dispatch with manual
operators; no console renewal may overlap. Keep the schedule absent until live
success confirms term arithmetic. A confirmed successful dispatch leaves no
hold; an unconfirmed send requires the human reconciliation procedure above. The
primary adds the cron trigger in a separate reviewed, green change authorized by
the owner. Keep manual renewal during rollout using the hold procedure.
Job-level `issues: write` and human clearance only for unconfirmed sends are
approved (2026-10-14); `actions: write` remains excluded. Pins, CLI no-send
evidence, provider settlement/budget behavior, and live evidence remain due.

[Synthetic]:
  https://docs.greennode.ai/vmonitor/dashboards/synthetics/lam-viec-voi-synthetic-test-quota
[Email]:
  https://docs.greennode.ai/vmonitor/dashboards/notification/lam-viec-voi-email-notification-quota
[schedule]:
  https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#schedule
[branch-failure]:
  https://github.blog/changelog/2021-02-17-github-actions-limit-which-branches-can-deploy-to-an-environment/
[environments]:
  https://docs.github.com/en/actions/reference/workflows-and-actions/deployments-and-environments
[setup-go]:
  https://github.com/actions/setup-go#caching-dependency-files-and-build-outputs
[gomod]: https://go.dev/ref/mod#authenticating
[secrets]: https://docs.github.com/en/actions/concepts/security/secrets
[issues]: https://docs.github.com/en/rest/issues/issues#update-an-issue
[actions-api]: https://docs.github.com/en/rest/actions/workflows
