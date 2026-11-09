# vMonitor Synthetic quota renewal

Approved 2026-10-14, updated 2026-11-01, pending implementation: GitHub Actions
renews the free Basic Synthetic Test quota for the
[Vietnam alarm](vietnam-production.md#logs-metrics-and-alarms). Telegram handles
notifications without quota processing. The
[ADR 0026 amendment](../adr/0026-vietnam-hosted-production.md#amendment-synthetic-quota-renewal-in-github-actions-2026-11-01)
authorizes the sole cloud-credential exception.

## Timing and budget

The [runbook](../runbooks/production.md#alarms) owns allowances and live expiry.
Basic cannot auto-renew; free renewal opens in the last seven days [Synthetic].
Use the last 72 hours until successful renewal proves whether it extends expiry
or starts a new term. Early renewal may lose unused days. A 409 window refusal
is a sent, definite refusal, not proof of no send.

After live validation, schedule `37 5,17 * * *` UTC. Compute time left from
fresh provider reads and current UTC, never schedule timestamps or rounded days.
Fail at <= 172800 seconds after processing. Expiry stops checks; recovery stays
human. GitHub may delay/drop runs and disables idle public-repository schedules
after 60 days [schedule]. Operators check enabled state and recent runs: silence
sends no alert. The uptime workflow remains a backstop.

Renew only when `0 < seconds-left <= 259200`, with a free quote. Synthetic has
no notification-quota action budget. Provider rate limits and refusal costs to
Synthetic allowances remain unverified. Never retry renewal within a run.
Uncertain sends hold later runs; confirmed renewal needs no human clearance.

## Workflow and trust boundary

`.github/workflows/vmonitor-renew.yml` has one `renew` job on `ubuntu-24.04`:

- Permit only `workflow_dispatch` initially; add `schedule` through the release
  gate. Dispatch input `action` chooses `renew` (default) or `check`; schedule
  selects `renew`. Pass it through step `env`, validate in the script, never
  interpolate into shell source. `check` uses only reads with `--read-only`.
- Set `permissions: {}` with only job-level `issues: write`. Guard repository
  `dannyota/aboutme`, ref `refs/heads/main`, and the allowed event. Workflow
  concurrency is `vmonitor-renew`, `cancel-in-progress: false`; job timeout is
  10 minutes. This serializes the workflow, not console actions.
- Use environment `vmonitor-renew`, with selected **Branch** rule `main`, no tag
  rule. Its three secrets are `VNGCLOUD_CLIENT_ID`, `VNGCLOUD_CLIENT_SECRET`,
  and `VNGCLOUD_PROJECT_ID`. Secret mapping masks the project identifier before
  the runner prints step environment values. `VNGCLOUD_REGION` names a shared
  provider location and stays a variable. Keep values out of documentation and
  do not duplicate secrets in repository or organization settings. Environment
  policy evaluates the run ref; a nonmatching job fails before running
  [branch-failure]. Protection rules gate secret access even if branch code
  removes the job guard [environments]. Inspect this configuration before
  credentialed use.
- Forks lack secrets; ordinary pull-request refs do not match `main`.
  Environments are not bound to workflow files: trusted-main
  `pull_request_target` or `workflow_run` code could request access. Never
  execute PR-controlled code here. Review workflow/test changes before merge;
  green candidate CI is no boundary.
- SHA-pinned `actions/checkout` reads public `dannyota/aboutme`,
  `refs/heads/main`, with `persist-credentials: false`. No candidate checkout or
  local action runs. Main may be newer than the event commit; log the checkout
  SHA. SHA-pinned `actions/setup-go` mirrors `.tool-versions`, with
  `cache: false` [setup-go]. Pins carry version comments and use grouped
  Dependabot updates.
- Install and verify the
  [pinned CLI](vmonitor-quota-renewal-cli.md#pinned-cli-and-evidence) before
  providing cloud secrets, using fresh temporary install, module-cache, and
  build-cache directories; never restore or save caches. Review CLI/dependency
  changes: hashes prove identity, not safety [gomod].
- Only the final script step, with ID `renew`, receives `VNGCLOUD_CLIENT_ID`,
  `VNGCLOUD_CLIENT_SECRET`, `VNGCLOUD_REGION`, `VNGCLOUD_PROJECT_ID`, and
  `GITHUB_TOKEN`. No cloud secrets reach checkout or installation. No username,
  password, profile, endpoint override, or credential arguments are permitted.

## Pinned CLI and script contract

The [renewal CLI contract](vmonitor-quota-renewal-cli.md) pins the `vngcloud`
CLI and its evidence, and defines the script's reads, renewal call, output
validation, and guard closure.

## Persistent hold

The owner creates one dedicated, initially open issue in `dannyota/aboutme`; pin
its number in the main script. Closed arms; open blocks sends. Every schedule,
dispatch, and rerun reads it first. Open/missing/invalid state or API failure
fails `renew` before invocation. `check` reports open holds and may pass healthy
reads; missing/invalid state or API failure fails. No other workflow may use
this key.

After validated reads, immediately before renewal invocation, reopen the issue
and read back open state. Uncertain marker writes abort renewal. Keep the guard
across cancellation or runner loss; a script skip before opening leaves it
closed. Only the opening run may close it automatically under the
[CLI predicate](vmonitor-quota-renewal-cli.md#script-and-cli-contract), then
confirm closed state. Never close inherited holds, delete/replace the issue, or
store provider bodies or credentials in it.

Missing evidence after interruption is uncertain, even between guard opening and
CLI invocation. Failed closure blocks later sends; extended expiry cannot clear
holds. Only a human reconciles: disable the workflow, cancel queued/active runs,
wait for termination, reconcile quota and settlement with the provider, then
close the issue and re-enable. Unchanged expiry cannot settle ambiguity. Manual
renewal uses this disabled window. The owner repairs missing issues.

The issue API grants repository-wide, not per-issue, `issues: write` [issues].
Grant no `actions: write`: self-disabling misses abrupt termination and adds
workflow control. Actions-write tokens can dispatch, re-enable, or cancel
[actions-api], but cannot close issues or edit trusted main. Dispatches stay
blocked; disabling alarms remains possible. Trust issue writers: they and stolen
job tokens can bypass holds. Runner/action compromise exposes the cloud key;
revoke credentials and disable the workflow.

Both Issues and Actions write allow hold closure and main dispatch. Trusted-main
code still enforces zero price; repeated free renewals may lose days or exhaust
provider limits. This risk is accepted. Stolen cloud keys bypass the CLI price
guard and may incur cost. Inspect provider-side limits with IAM.

## Logging, custody, and limits

Capture stdout/stderr privately. Logs, annotations, summaries, and the issue may
contain only fixed quota labels, validated UTC times, allowlisted
statuses/actions, numeric prices, and exit codes; never raw errors, bodies,
identifiers, `OrderID`, `NextSafeCommand`, or tokens. No tracing, `--debug`,
artifacts, or token cache uploads. Delete temporary responses and `HOME` on
exit; runner disposal covers interruption. GitHub masking is only a backstop
[secrets]. Write `Synthetic quota: healthy` on success or
`Synthetic quota: failed` on failure to `GITHUB_STEP_SUMMARY`. Failures also
emit a fixed `::error::`; catchable termination writes the failure summary.
Timeout/setup failures remain failed jobs. Keep Actions failure notifications
enabled for schedule and dispatch actors, separate from Telegram-only vMonitor
alarms. Each failure adds one diagnostic line to logs and the summary with an
allowlisted stage and environment reason, CLI exit (0-255) and documented code
or `other` for CLI stages, or issue HTTP class (`2xx`, `4xx`, `5xx`, `none`).

`aboutme-vmonitor-renew` has no console login. Allow only these `vmonitor`
actions on resource `*`: `GetSyntheticQuota`, `GetSyntheticTestQuotaDetail`,
`GetSyntheticUsage`, `GetQuotaUsages`, `ListDetailedQuotasInTrash`, and
`RenewSyntheticQuota`. No notification actions, Recover, Buy, or other writes.
Before enabling renewal, verify required reads/quote and effective IAM against
the pin. Never probe forbidden mutations or widen permissions to pass;
unexpected dependencies need a reviewed design change.

Keep an age-encrypted backup in `aboutme-infra`. Replace the environment key
through a secret-safe pipe, run `check`, then revoke the old key. Revoke on
suspected compromise, loss of trusted-main access, or compromise of both the
infrastructure backup and its decryption key. App backups hold no copy. Tokens
reportedly last 1800 seconds; verify whether revocation invalidates them sooner.
Trust main writers, admins, the runner, and pinned actions/CLI with the key.

The credential never reaches the host, renderer, or app data. Anonymous
visitors, signed-in users, hostile resumes, stolen app sessions/MCP tokens,
database writers, leaked app backups, and compromised host containers have no
route to it. Verify TLS for provider and GitHub calls: network attackers may
interrupt, but cannot supply trusted outcomes without defeating TLS. Uncertainty
fails closed through the guard. No app API, schema, client, or data migration
changes. Older CLIs are unsupported; pin mismatch aborts before credentials,
with no older-outcome fallback.

Rollback disables the workflow and reconciles holds before manual renewal; never
discard an uncertain hold. Check data stays unchanged; expiry/deletion recovery
follows the runbook. Laptop timers need uptime; host timers expose the key to
compromise. Paid packages were rejected. Telegram removes notification quota;
Synthetic still needs renewal. Scope: one small workflow, one script, focused
tests, and no new service or durable store beyond the hold issue.

## Verification and release

Extend `scripts/test/workflow-contract.test.mjs` with exact renewal workflow
assertions for event/input, guards, permissions, environment, concurrency,
timeout, checkout, pins, toolchain, caches, install/hash-check order, clean CLI
environment, and final-step variable/secret mappings. Reject extra jobs/actions,
secret expressions in `run`, `continue-on-error`, bypassed hash checks, debug,
artifacts, and any PR-controlled execution. Include negative fixtures for each
boundary. Static YAML tests cannot prove server-side environment settings or
IAM; inspect nonsecret metadata separately.

Change `scripts/test/workflow-safety-test.sh` only at `assert_workflow_secrets`
and its fixtures (the assertion beginning with "No workflow may reference a
secret except GitHub's per-run GITHUB_TOKEN"). Its `property === "GITHUB_TOKEN"`
allowlist needs one contextual exception: exact file
`.github/workflows/vmonitor-renew.yml`, job `renew`, final step ID `renew`,
environment `vmonitor-renew`, and that step's three exact `env` mappings from
`secrets.VNGCLOUD_CLIENT_ID`, `secrets.VNGCLOUD_CLIENT_SECRET`, and
`secrets.VNGCLOUD_PROJECT_ID` to the same names. Require the trusted workflow
contract, not merely the filename. Reject those names in another file/job/step,
job-level env, `run`, YAML keys, computed access, or aggregate secret
expressions; keep existing hostile expression fixtures and the proof-job secret
prohibition unchanged. Add a positive exact mapping fixture and negative
context/mapping fixtures. Keep release-check and shared-image-action no-secret
assertions in `scripts/test/workflow-contract.test.mjs` unchanged. This
exception implements ADR 0026; it does not grant secrets to `ci.yml` or its
reusable jobs.

Add `scripts/test/vmonitor-renew-test.sh`, with fake CLI, issue API, and clock.
Cover 72/48-hour boundaries, delayed runs, invalid
action/read/status/class/time, expiry, mismatched reads, check-only behavior,
and every [outcome/exit](vmonitor-quota-renewal-cli.md#script-and-cli-contract).
Use pinned golden JSON and types. Reject table aliases, wrong casing, missing or
duplicate Synthetic rows, incomplete snapshots, `none`, and `unavailable`. Cover
null fields, `TimeLeftSeconds` null/-1/0, stale `AsOf`/counter values,
fractional seconds, and equivalent offsets; thresholds must use `EndTime` and
current time. Cover retained `unknown` confirmation, `accepted`, preflight
auth/config failures, canceled login, error operations, and `NotSettled` 200.
Assert exact argv, preserved expected-end string, at most one renewal invocation
and zero resends, no writes before verified guard creation, and strictly later
read-back for success. Test confirmed output followed by failed health read:
guard closure is allowed, success is not. Test nonzero safe outcomes: guard
closes, job fails. Exercise missing/invalid/contradictory JSON, 401/404, exit
2/3, timeouts, 5xx, and `NotSettled`; a status alone never clears a guard.
Assert fixed, redacted success and failure summaries and persistent holds after
contradictory pending payment.

Persist the fake issue across runs. Cover interruption before/after send, failed
marker read/write/closure, inherited holds despite later extended expiry, and
automatic skips after confirmed renewal. Check that only human reconciliation
permits a later send after uncertainty. Inject hostile output, fake credentials,
override variables, and cached profiles; assert no disclosure or
credential-source fallback. Wire the new test into the existing hosted contracts
checks and shell syntax checks in `Makefile`; the renewal workflow must never
become a credential-bearing dependency of `ci.yml`.

Release order:

1. Land the accepted ADR amendment and reviewed design. CLI evidence and JSON
   are pinned in the
   [CLI contract](vmonitor-quota-renewal-cli.md#pinned-cli-and-evidence); verify
   IAM reads/quote dependencies, environment metadata, and narrowed grants.
2. Create the initially open hold issue. Merge the workflow, script, and tests
   with **no schedule trigger**, independent adversarial review, and green CI.
3. Run `action=check` on main with credentials. It may pass healthy reads while
   leaving the initial hold open. After it passes, the owner closes that hold
   and coordinates a due `action=renew`; no console renewal may overlap.
4. For the runbook's current term, the provider window starts 2026-11-24; the
   72-hour threshold starts 2026-11-28T06:54:54Z. Keep the schedule absent until
   successful due dispatch proves later expiry by read-back and records term
   arithmetic. November 24 dispatch must stay not due. Unconfirmed sends require
   reconciliation, not another attempt.
5. Add the cron trigger in a separate reviewed, green change under the owner's
   standing approval. Keep coordinated manual renewal during rollout. Record
   live evidence and update the runbook's expiry after success.

No further product choice needs owner approval. The owner controls initial
arming and hold recovery. Pin changes, wider permissions or credential
exceptions, paid renewal, and automated recovery need a new reviewed decision.
Verify revocation timing, provider limits, and live term arithmetic.

[Synthetic]:
  https://docs.greennode.ai/vmonitor/dashboards/synthetics/lam-viec-voi-synthetic-test-quota
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
