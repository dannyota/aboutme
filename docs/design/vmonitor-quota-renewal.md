# vMonitor Synthetic quota renewal

Approved direction (2026-10-14), updated 2026-11-01, pending implementation:
GitHub Actions renews the free Basic Synthetic Test quota behind the
[Vietnam alarm](vietnam-production.md#logs-metrics-and-alarms). Notifications go
to Telegram. The
[ADR 0026 amendment](../adr/0026-vietnam-hosted-production.md#amendment-synthetic-quota-renewal-in-github-actions-2026-11-01)
authorizes the sole cloud-credential exception. This design has no notification
quota processing.

## Timing and budget

The [runbook](../runbooks/production.md#alarms) owns allowances and live expiry.
Basic cannot auto-renew. The provider permits free renewal only in the last
seven days [Synthetic]. Use the last 72 hours until a successful renewal proves
whether it extends the old expiry or starts a new term. Early renewal may lose
unused days. A 409 window refusal is a sent, definite refusal, not proof that no
request left the client.

After live validation, schedule `37 5,17 * * *` UTC. Compute remaining time from
fresh provider reads and current UTC time, never the schedule timestamp or
rounded calendar days. Fail if the quota has at most 172800 seconds left after
processing. Expiry stops checks; recovery stays human. GitHub may delay or drop
runs and disables idle public-repository schedules after 60 days [schedule]. The
operator checks enabled state and recent runs, since silence sends no alert. The
uptime workflow remains a backstop.

Renew only when `0 < seconds-left <= 259200`. A free quote is mandatory; this
design does not apply a notification-quota action budget to Synthetic. Provider
rate limits and whether refusals consume any Synthetic allowance remain
unverified. Never retry a renewal invocation within a run. An uncertain send
blocks later runs through the persistent hold; confirmed renewal needs no human
clearance.

## Workflow and trust boundary

`.github/workflows/vmonitor-renew.yml` has one `renew` job on `ubuntu-24.04`:

- Initially only `workflow_dispatch`; add `schedule` through the release gate.
  These are the only permitted events. Dispatch input `action` is a choice of
  `renew` (default) or `check`. Schedule selects `renew`. Pass the action
  through step `env`, validate it in the script, never interpolate it into shell
  source. `check` uses only read commands with `--read-only`.
- Set `permissions: {}` with only job-level `issues: write`. Guard repository
  `dannyota/aboutme`, ref `refs/heads/main`, and the allowed event. Workflow
  concurrency is `vmonitor-renew`, `cancel-in-progress: false`; job timeout is
  10 minutes. This serializes the workflow, not console actions.
- Use environment `vmonitor-renew`, with selected **Branch** rule `main`, no tag
  rule. Client ID and client secret are environment secrets; region and project
  are environment variables `VNGCLOUD_REGION` and `VNGCLOUD_PROJECT_ID`. Keep
  their values out of documentation. Do not duplicate credentials in repository
  or organization secrets. Environment policy evaluates the run ref, not the
  checkout ref; a nonmatching job fails before running [branch-failure].
  Protection rules gate secret access even if branch code removes the job guard
  [environments]. Inspect this configuration before credentialed use.
- Forks do not inherit secrets; ordinary pull-request refs do not match `main`.
  An environment is not bound to one workflow file. Trusted-main
  `pull_request_target` or `workflow_run` code could request it: never execute
  PR-controlled code with this environment. Review workflow and test changes
  before merge; green candidate CI alone is not a security boundary.
- SHA-pinned `actions/checkout` reads public `dannyota/aboutme`,
  `refs/heads/main`, with `persist-credentials: false`. No candidate checkout or
  local action runs. Main may be newer than the event commit; log the checkout
  SHA. SHA-pinned `actions/setup-go` mirrors `.tool-versions`, with
  `cache: false` [setup-go]. Pins carry version comments and use grouped
  Dependabot updates.
- Install and verify the CLI as specified below before providing cloud secrets.
  Use fresh temporary install, module-cache, and build-cache directories; do not
  restore or save caches. Review CLI and dependency changes: hashes establish
  identity, not safe behavior [gomod].
- Only the final script step, with ID `renew`, receives `VNGCLOUD_CLIENT_ID`,
  `VNGCLOUD_CLIENT_SECRET`, `VNGCLOUD_REGION`, `VNGCLOUD_PROJECT_ID`, and
  `GITHUB_TOKEN`. No cloud secrets reach checkout or installation. No username,
  password, profile, endpoint override, or credential arguments are permitted.

## Pinned CLI and evidence

Use `danny.vn/vngcloud/cmd/vngcloud@v0.89.0`. Install with `GOTOOLCHAIN=local`,
`GOSUMDB=sum.golang.org`, and empty `GOPRIVATE`, `GONOSUMDB`, and `GOINSECURE`:

```sh
go install danny.vn/vngcloud/cmd/vngcloud@v0.89.0
```

Before executing that same absolute binary, assert its module identity using
`go version -m <bin>`:

```text
danny.vn/vngcloud v0.89.0 h1:o9YirnefFhEqpEJtalX41uHci6PR6P4AcotwPrWiqkA=
```

The maintainer evidence gate is satisfied for this pin, from the supplied
release evidence: verified signed annotated tag [v0.89.0][release] points to
commit `2eddd4a00198f71c6ad3db6f631402b2f4d6603f`; its successful [master CI
run][cli-ci] includes `go test -race ./...`. The real-transport tests with
retries enabled live in `internal/cli/svc_monitor_ci_test.go`:

- `TestSyntheticMutationCLIRealTransport`
- `TestSyntheticMutationCLISendFailures`
- `TestSyntheticMutationCLIRetainedAttemptOutcomes`
- `TestSyntheticMutationPreSDKOutcome`
- `TestSyntheticQuotaCIEnvironmentOnlyAuth`

These are the supplied evidence for request counts, retained attempt outcomes,
preflight refusal, and environment-only authentication. Fake-CLI tests in this
repository cannot prove transport safety. Live smoke established a 0 VND quote
and a not-due result at 30 days left with nothing sent. A live successful
renewal and its term arithmetic remain unverified; the response body alone is
not proof of success.

These contract sources and JSON goldens are pinned to v0.89.0. Wiki section
names locate the matching guidance.

- **Quota status snapshot:** [monitor/quotas_status.go][status-source], lines
  16–34, defines `AsOf`, `Items`, `Complete`, and row keys `Kind`, `ClassName`,
  `PackageName`, `Status`, `Quota`, `Price`, `StartTime`, `EndTime`,
  `TimeLeftSeconds`, `AutoRenew`, `RenewWindowOpen`, `NextSafeCommand`, `Error`.
  Fields from `Quota` through `RenewWindowOpen` and `Error` are nullable.
  `Error` has `Code`, `Operation`, and integer `Status`. `TimeLeftSeconds` is a
  signed integer or null ([status golden][status-golden], lines 5–32: `-1`,
  null). Source lines 169–180 truncate seconds at `AsOf` toward zero.
- **Quota status snapshot:** that source, lines 40–74, 90–131, and 155–165,
  gives `Kind: "Synthetic"`, `ClassName: "Basic"`, and empty `PackageName`.
  Present rows preserve raw status; [monitor/quotas.go][quota-source], lines
  24–28, defines `ACTIVE` and `IN-TRASH`. Absence gives `none`, null data, and
  exit 0; component failure gives `unavailable`, `Complete: false`, and exit 1
  (`QuotaStatusIncomplete`), even for 401/404. See [Quota status
  snapshot][wiki-status] and [internal/cli/errors.go][cli-errors], lines 203–207
  and 570–573. The window is known only for Basic, price 0, 10 API tests, one
  location; true needs active, future expiry within seven days. It grants no
  write permission. `NextSafeCommand` is a read hint.
- **Quota reads:** JSON uses `.Quota.EndTime` and `.Quota.Status`, with status
  `ACTIVE` in the [get golden][get-golden], lines 2–16. The output type is in
  [monitor/quotas.go][quota-source], lines 128–131.
  [internal/cli/svc_monitor.go][cli-render], lines 395–407 and 621–656, passes
  SDK values to JSON rendering. `Class`, `PriceVND`, `StartUTC`, `EndUTC`, and
  `TimeLeft` are table labels, never JSON aliases. See [Quota
  reads][wiki-reads].
- **Unattended CI:** [monitor/quotas_renew.go][renew-source], lines 17–32,
  defines renewal input and output; [monitor/quotas_outcome.go][outcome-source],
  lines 13–72, defines `Sent`, `Stage`, and nullable `Error`; keys and
  combinations are below. `PreviousEndTime` is an RFC 3339 timestamp or null;
  `Quota` is an object or null; `OrderID` is a possibly empty string;
  `PaymentPending` and `Renewed` are booleans; only confirmation sets `Renewed`
  true. `Error.Operation` is `monitor.RenewSyntheticQuota`, including pre-SDK
  failures ([internal/cli/main.go][cli-main], lines 46–115). `Error.Status` is
  an integer, with 0 when absent; `NotSettled` can retain 200, as the [renew
  golden][renew-golden], lines 1–14, shows. The [Unattended CI][wiki] table,
  lines 308–323, and [internal/cli/errors.go][cli-errors], lines 570–670, own
  exit mappings. **Synthetic recovery and renewal**, lines 248–275, covers
  flags, eligibility, quote, and send-once rules;
  [internal/cli/main.go][cli-main], lines 117–154, enforces price consent and
  refuses `--query`. **Unattended CI**, lines 279–284 and 333–354, covers
  credentials and guard closure.

## Script and CLI contract

`scripts/vmonitor-renew.sh` uses JSON and `jq`. Create a mode-0700 temporary
directory with `umask 077` and assign a fresh child directory to `HOME` for CLI
processes. Pass only the four named cloud variables and required process
settings through a clean child environment. Unset `VNGCLOUD_PROFILE` and all
config/credential path overrides; the clean environment must also exclude
endpoint and debug overrides. Do not load a profile or reuse a token cache. Keep
the issue API token out of the CLI child environment.

Before any possible write, read with the verified absolute binary:

```sh
vngcloud --read-only monitor get-synthetic-quota --output json
vngcloud --read-only monitor quota-status --kind synthetic --output json
```

The raw expiry is `.Quota.EndTime`; preserve its exact string as `expiry`.
Require `.Quota.Status == "ACTIVE"`. For `quota-status --kind synthetic`,
require `Complete == true` and exactly one item, identified by
`Kind == "Synthetic"`. Require its `Error == null`, `Status == "ACTIVE"`,
`ClassName == "Basic"`, numeric `Price == 0`, and non-null parseable `StartTime`
and `EndTime`. Require agreement between both expiry instants. Validate the
pinned types without replacing null with zero, false, or an empty string.
Missing quota, unknown status/class, expired quota, or malformed/mismatched
reads fail closed. Never parse table labels or execute `NextSafeCommand`.
Compute both thresholds from the parsed `EndTime` minus current UTC time,
preserving fractional seconds and accepting equivalent RFC 3339 offsets.
`TimeLeftSeconds` may be null or negative and reflects `AsOf`, not the decision
time; never use it for thresholds or require it to be positive.

In `renew` mode, only a due quota with `RenewWindowOpen` true may invoke:

```sh
vngcloud monitor renew-synthetic-quota \
  --expected-end-time "$expiry" --max-days-left 3 --max-price 0 --output json
```

Open and verify the guard before this command. The CLI re-reads, refuses stale
expected expiry or missing/nonzero price, and sends at most once. Neither the
script nor CLI resends a mutation for auth, timeout, 429, 5xx, or settlement
failure; bounded polling may only read. `--expected-end-time` is not durable
idempotency or proven server-side compare-and-swap. Human renewal must not
overlap a run. No Recover, Buy, or notification command is permitted.

Renew stdout has exactly these top-level fields: `Sent`, `Stage`, `Error`,
`PreviousEndTime`, `Quota`, `OrderID`, `PaymentPending`, `Renewed`. `Error` is
null or has exactly `Code`, `Operation`, and `Status`. Validate one JSON object,
its exact keys, types and allowed combinations in the table against the pinned
contract; never search arbitrary output for a safe field. Missing, invalid,
contradictory, or unknown output retains an opened guard. Capture exit status
separately; a nonzero exit does not imply no send.

This table allows the pinned wiki's **Unattended CI** combinations. Other SDK
outputs remain unclassified and retain the hold under this workflow's policy.

| Outcome                                       | `Sent`                  | `Stage`     | `Error.Code` (`Error.Status`)                                                                                                                                                                                | CLI exit               | Guard          |
| --------------------------------------------- | ----------------------- | ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ---------------------- | -------------- |
| Outside positive MaxDaysLeft                  | no                      | not-due     | null `Error`                                                                                                                                                                                                 | 0                      | Close          |
| Confirmed, `Renewed: true`                    | yes or retained unknown | confirmed   | null `Error`                                                                                                                                                                                                 | 0                      | Close          |
| Price refused                                 | no                      | preflight   | PriceAboveMax or Unpriced (0)                                                                                                                                                                                | 1                      | Close          |
| Stale guard or changed final terms            | no                      | preflight   | QuotaChanged (0)                                                                                                                                                                                             | 1                      | Close          |
| Mismatched unresolved prior attempt           | retained yes or unknown | unconfirmed | NotSettled (retained status or 0)                                                                                                                                                                            | 1                      | Keep           |
| Definite mutation refusal                     | yes                     | refused     | Conflict (409), BadRequest (400), Forbidden (403), Throttled (429)                                                                                                                                           | 1                      | Close          |
| Mutation auth or missing quota refusal        | yes                     | refused     | Unauthorized (401), NotFound (404)                                                                                                                                                                           | 3, 4 respectively      | Close          |
| Accepted NoWait                               | yes                     | accepted    | null `Error`                                                                                                                                                                                                 | 0                      | Keep; fail run |
| Response seen, settlement unconfirmed         | yes                     | unconfirmed | NotSettled (mutation status, else cause status or 0)                                                                                                                                                         | 1                      | Keep           |
| Possible send without a response              | unknown                 | unconfirmed | NotSettled (0)                                                                                                                                                                                               | 1                      | Keep           |
| Proven not sent                               | no                      | preflight   | RequestFailed (0)                                                                                                                                                                                            | 1                      | Close          |
| Input, consent, read-only, config refusal     | no                      | preflight   | InvalidUsage, ReadOnly, InvalidConfig (0)                                                                                                                                                                    | 2                      | Close          |
| Missing credentials or token exchange failure | no                      | preflight   | NoCredentials or ServiceAccountLoginFailed (status or 0)                                                                                                                                                     | 3; canceled exchange 1 | Close          |
| Other pre-send failure                        | no                      | preflight   | TokenCacheFailed, RequestFailed, UnexpectedStatus (0); NotFound (404); BadRequest, Unauthorized, Forbidden, Conflict, Throttled, ServerError, ClientError, InvalidResponse, ResponseTooLarge (source status) | 1; auth 3, NotFound 4  | Close          |

`Sent` strings are `no`, `yes`, `unknown`; retained describes prior evidence.
Only `confirmed` has `Renewed == true`. `accepted` cannot pass this workflow,
which never sets `NoWait`. Preflight failures emit the full object. The
lowercase stderr `error` envelope is separate from the stdout outcome.

The only closure predicate for valid output is `Sent == "no"` with stage
`not-due` or `preflight`, or stage `refused`, or stage `confirmed` with
`Renewed == true`. Anything else keeps the hold. HTTP status and CLI exit alone
never authorize closure; interrupted or broken output may lack usable JSON. A
definite refusal proves the request was rejected, not that it was unsent.

Only valid not-due or confirmed output with CLI exit 0 can proceed toward
success; invalid output or any other outcome fails even if later reads look
healthy. After either valid zero-exit outcome, repeat the read-only status
command. A sent renewal passes only if read-back proves a strictly later end
instant than `expiry`. Require active Basic status and more than 172800 seconds
left in every passing run, including `check` and not-due runs. A valid confirmed
result permits guard closure under the predicate even if this final health check
fails; the run still fails. Never use a later read to turn `unconfirmed` into
success or clear its hold. Response-body claims alone cannot pass the run.

Stop on a nonzero CLI exit, classify its captured output, close only if allowed,
and fail with that exit (1, 2, 3, or 4). Do not use shell fail-fast behavior
that skips classification. Script validation, health, or issue API failures exit
1; unknown CLI exits also fail and retain an opened guard. A guard-closure
failure cannot replace a nonzero CLI result with success. `check` may report an
open hold and pass healthy reads, but must never change the hold or send a
renewal.

## Persistent hold

The owner creates one dedicated, initially open issue in `dannyota/aboutme`; pin
its number in the main script. Closed means armed; open blocks sends. Every
schedule, dispatch, and rerun reads it before processing. Open/missing/invalid
state or API failure fails `renew` before any renewal invocation. `check`
reports an open hold and may continue read-only checks; missing/invalid state or
API failure still fails the run. No other workflow may use this key.

After validated reads, immediately before a renewal invocation, reopen the issue
and read back its open state. An uncertain marker write aborts without renewal.
Keep the guard across cancellation or runner loss. A script-level skip before
opening it leaves the issue closed. Only the run that opened the guard may close
it automatically, using the CLI closure predicate above, then confirm closed
state. Never close a hold inherited from another run, delete or replace the
issue, or store provider bodies or credentials in it.

Missing evidence after interruption is uncertain, even between opening the guard
and invoking the CLI. Failed guard closure also blocks later sends. A later read
showing extended expiry does not clear a retained hold. Only a human reconciles
it: disable the workflow, cancel queued and active runs, wait for termination,
reconcile the quota and settlement with the provider, then close the issue and
re-enable. An unchanged end time cannot settle an ambiguous send. Manual renewal
uses this same disabled-workflow window. A missing issue needs owner repair.

The issue API grants repository-wide `issues: write`, not per-issue scope
[issues]. Grant no `actions: write`. Self-disabling misses abrupt termination
and adds workflow control. An Actions-write token can dispatch, re-enable, or
cancel runs [actions-api], but cannot close the issue or edit trusted main.
Repeated dispatches remain blocked; disabling alarms remains possible. Issue
writers can bypass the hold and must be trusted. A stolen job token can clear
it; runner/action compromise already exposes the cloud key. Revoke credentials
and disable the workflow on that compromise.

A token with both Issues and Actions write can close the hold and dispatch on
main. Through trusted-main code the zero-price guard still applies; repeated
free renewals may lose days or exhaust provider limits. This accepted residual
risk does not imply a stolen cloud key cannot incur cost: it bypasses the CLI
price guard. Provider-side limits remain part of the IAM inspection.

## Logging, custody, and limits

Capture stdout/stderr privately; emit only fixed quota labels, validated UTC
times, allowlisted statuses/actions, numeric prices, and exit codes. Never emit
raw errors, bodies, identifiers, `OrderID`, `NextSafeCommand`, or tokens to
logs, annotations, summaries, or the issue. No tracing, `--debug`, artifacts, or
token cache uploads. Delete temporary responses and `HOME` on exit; runner
disposal covers interruption. GitHub masking is only a backstop [secrets].
Failures emit a fixed `::error::` and summary; timeout/setup failures remain
failed jobs. Keep Actions failure notifications enabled for schedule and
dispatch actors; they are distinct from the Telegram-only vMonitor alarm
channel.

`aboutme-vmonitor-renew` has no console login. Allow only these `vmonitor`
actions on resource `*`: `GetSyntheticQuota`, `GetSyntheticTestQuotaDetail`,
`GetSyntheticUsage`, `GetQuotaUsages`, `ListDetailedQuotasInTrash`, and
`RenewSyntheticQuota`. No notification actions, Recover, Buy, or other writes.
Verify the required reads/quote and effective IAM against the pinned CLI before
enabling renewal. Do not probe forbidden mutations or widen permissions to make
the CLI pass; an unexpected dependency needs a reviewed design change.

Keep an age-encrypted backup in `aboutme-infra`. Replace the environment key
through a secret-safe pipe, run `check`, then revoke the old key. Revoke on
suspected compromise or loss of trusted-main access. Issued tokens reportedly
last 1800 seconds; verify whether revocation invalidates them sooner. Main
writers, admins, the runner, and pinned actions/CLI are trusted with the key.
Application backups contain no copy; compromise of the encrypted infrastructure
backup and its decryption key requires cloud-key revocation too.

Anonymous visitors, signed-in users, hostile resumes, stolen app sessions/MCP
tokens, database writers, leaked app backups, and compromised host containers
have no route to the credential. It never reaches the host, renderer, or app
data. Require TLS verification for provider and GitHub calls; a network attacker
may interrupt them but cannot supply trusted outcomes without defeating TLS.
Uncertain results fail closed through the guard. No app API, schema, client, or
data migration changes. Older CLI versions are unsupported; a pin mismatch
aborts before credentials. No fallback to an older outcome contract.

Rollback disables the workflow and follows hold reconciliation before manual
renewal; never discard an uncertain hold. Existing check data stays unchanged;
expiry and deletion recovery follow the runbook. Laptop timers need uptime; host
timers expose the key to host compromise. Paid packages were rejected. Telegram
removes the notification quota, but Synthetic still needs renewal. The
implementation is one small workflow, one script, and focused tests, with no new
service or durable store beyond the hold issue.

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
environment `vmonitor-renew`, and that step's two exact `env` mappings from
`secrets.VNGCLOUD_CLIENT_ID` and `secrets.VNGCLOUD_CLIENT_SECRET` to the same
names. Require the trusted workflow contract, not merely the filename. Reject
those names in another file/job/step, job-level env, `run`, YAML keys, computed
access, or aggregate secret expressions; keep existing hostile expression
fixtures and the proof-job secret prohibition unchanged. Add a positive exact
mapping fixture and negative context/mapping fixtures. Keep release-check and
shared-image-action no-secret assertions in
`scripts/test/workflow-contract.test.mjs` unchanged. This exception implements
ADR 0026; it does not grant secrets to `ci.yml` or its reusable jobs.

Add `scripts/test/vmonitor-renew-test.sh`, with fake CLI, issue API, and clock.
Cover 72/48-hour boundaries, delayed runs, invalid
action/read/status/class/time, expiry, mismatched reads, check-only behavior,
and every outcome/exit above. Use pinned golden JSON and types. Reject table
aliases, wrong casing, missing or duplicate Synthetic rows, incomplete
snapshots, `none`, and `unavailable`. Cover null fields, `TimeLeftSeconds`
null/-1/0, stale `AsOf`/counter values, fractional seconds, and equivalent
offsets; thresholds must use `EndTime` and current time. Cover retained
`unknown` confirmation, `accepted`, preflight auth/config failures, canceled
login, error operations, and `NotSettled` 200. Assert exact argv, preserved
expected-end string, at most one renewal invocation and zero resends, no writes
before verified guard creation, and strictly later read-back for success. Test
confirmed output followed by failed health read: guard closure is allowed,
success is not. Test nonzero safe outcomes: guard closes, job fails. Exercise
missing/invalid/contradictory JSON, 401/404, exit 2/3, timeouts, 5xx, and
`NotSettled`; a status alone never clears a guard.

Persist the fake issue across runs. Cover interruption before/after send, failed
marker read/write/closure, inherited holds despite later extended expiry, and
automatic skips after confirmed renewal. Check that only human reconciliation
permits a later send after uncertainty. Inject hostile output, fake credentials,
override variables, and cached profiles; assert no disclosure or
credential-source fallback. Wire the new test into the existing hosted contracts
checks and shell syntax checks in `Makefile`; the renewal workflow must never
become a credential-bearing dependency of `ci.yml`.

Release order:

1. Land the accepted ADR amendment and this reviewed design. The pinned CLI
   release-evidence gate is satisfied and JSON details are pinned above; verify
   IAM reads/quote dependencies, environment metadata, and narrowed grants.
2. Create the initially open hold issue. Merge the workflow, script, and tests
   with **no schedule trigger**, independent adversarial review, and green CI.
3. Run `action=check` on main with credentials. It may pass healthy reads while
   leaving the initial hold open. After it passes, the owner closes that hold
   and coordinates a due `action=renew`; no console renewal may overlap.
4. The provider window starts 2026-11-24 for the runbook's current term; this
   design's 72-hour threshold starts 2026-11-28T06:54:54Z. Keep the schedule
   absent until a successful due dispatch proves later expiry by read-back and
   records term arithmetic. A November 24 dispatch must remain not due. An
   unconfirmed send requires reconciliation, not another attempt.
5. Add the cron trigger in a separate reviewed, green change under the owner's
   standing approval. Keep coordinated manual renewal during rollout. Record
   live evidence and update the runbook's expiry after success.

No additional product choice needs owner approval. The owner controls initial
arming and hold recovery. Pin changes, permission widening, paid renewal,
recovery automation, or a wider credential exception require a new reviewed
decision. Revocation timing, provider limits, and live term arithmetic remain
explicit verification items.

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
[release]: https://github.com/dannyota/vngcloud/releases/tag/v0.89.0
[cli-ci]: https://github.com/dannyota/vngcloud/actions/runs/41290218076
[wiki]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/docs/wiki/Monitor-Quotas.md#unattended-ci
[wiki-status]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/docs/wiki/Monitor-Quotas.md#quota-status-snapshot
[wiki-reads]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/docs/wiki/Monitor-Quotas.md#quota-reads
[status-source]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/monitor/quotas_status.go
[quota-source]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/monitor/quotas.go
[renew-source]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/monitor/quotas_renew.go
[outcome-source]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/monitor/quotas_outcome.go
[cli-render]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/internal/cli/svc_monitor.go
[cli-main]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/internal/cli/main.go
[cli-errors]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/internal/cli/errors.go
[status-golden]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/internal/cli/testdata/golden/monitor-quota-status.json.golden
[get-golden]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/internal/cli/testdata/golden/monitor-get-synthetic-quota.json.golden
[renew-golden]:
  https://github.com/dannyota/vngcloud/blob/v0.89.0/internal/cli/testdata/golden/monitor-renew-synthetic-quota.json.golden
