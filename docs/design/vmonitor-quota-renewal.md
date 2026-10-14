# vMonitor quota renewal

Proposal (2026-10-14, owner decision). A scheduled GitHub workflow renews the
two free GreenNode vMonitor quotas behind the site-down alarm in the
[Vietnam production design](vietnam-production.md#logs-metrics-and-alarms),
using one GreenNode service account whose only permissions are those renewals.
This amends ADR 0027's "GitHub without cloud credentials" rule for this one
credential. Nothing here changes the check, the channel, or the production host.

## Quotas

The Synthetic Test quota (10 checks) and the Email Notification package
`emailfree` (20 emails a month) are free, last at most one month, and cannot
auto-renew; the [production runbook](../runbooks/production.md#alarms) records
their current end times. GreenNode allows renewal only within 7 days before
expiry and one quota of each kind at a time ([Synthetic quota], [Email quota]).
At expiry the quota moves to trash (`IN-TRASH`), the checks stop, and the path
becomes Recover, not Renew. Billing allows three notification-quota actions a
month; one was used on 2026-10-14.

**Unconfirmed until the first live renewal on 2026-10-24:** how early GreenNode
accepts a renewal inside the 7-day window, and whether the new end time counts
from the old end or from the renewal. Until then the workflow renews only in the
last 3 days.

## Trigger and timing

- `on.schedule`: `37 5,17 * * *`. GitHub delays scheduled runs under load and
  "high load times include the start of every hour" [schedule]; `uptime.yml`
  runs arrived 3 to 10 hours late. Two runs a day keep at least two attempts
  inside the 3-day window; a run finds nothing to do on most days.
- `on.workflow_dispatch` with one input `action`: `renew` (default) or `check`
  (reads only, with `--read-only`). No other trigger. Scheduled runs use the
  latest `main` commit [schedule]; a dispatch on another branch cannot reach the
  environment (below).
- Not due: when more than 3 days remain, the CLI exits 0 without sending and the
  run passes with "skipped" in the step summary.
- Alert: when 2 days or fewer remain after the renewal step, the run fails.
- `concurrency: { group: vmonitor-renew, cancel-in-progress: false }`, so two
  runs never read one end time and both send. `timeout-minutes: 10`.
- GitHub disables a schedule in a public repository after 60 days without
  activity [schedule]; the runbook's healthy-state list checks the workflow.

## Workflow shape

`.github/workflows/vmonitor-renew.yml`, one job `renew`:

1. `permissions: {}` at the top level and no job override, so `GITHUB_TOKEN` can
   do nothing [permissions].
2. `environment: vmonitor-renew`. The owner (repository admin) creates it with
   deployment branches set to `main` only and stores the secrets there.
   Environment secrets reach only jobs that reference the environment, and only
   matching branches can deploy to it [environments].
3. `actions/checkout` of `dannyota/aboutme` at `refs/heads/main` with
   `persist-credentials: false`, as in `release-images.yml`. It supplies
   `scripts/vmonitor-renew.sh` only; no other code runs.
4. `actions/setup-go` with `go-version` from `.tool-versions`, then
   `GOTOOLCHAIN=local go install danny.vn/vngcloud/cmd/vngcloud@vX.Y.Z` with the
   version in a workflow `env` value, never `@latest`. Go verifies every
   downloaded module against `sum.golang.org` and refuses a mismatch [gomod].
   The step then requires `go version -m` to show
   `mod danny.vn/vngcloud vX.Y.Z h1:<pinned sum>`, so the workflow pins the
   module hash as well as the version. Releases carry no binaries, so this is
   the only install path.
5. One step runs `bash scripts/vmonitor-renew.sh "$ACTION"` with step `env:`
   `VNGCLOUD_CLIENT_ID`, `VNGCLOUD_CLIENT_SECRET`, and `VNGCLOUD_REGION`
   (`hcm-3`) from the environment. Nothing else is set: never
   `VNGCLOUD_USERNAME`, `VNGCLOUD_PASSWORD`, or `VNGCLOUD_PROFILE`.

Every external action keeps a full commit pin with a version comment, and
Dependabot groups their updates weekly, as for every workflow.

## Renewal procedure

`scripts/vmonitor-renew.sh` runs these steps for the Synthetic quota, then the
Email quota, with `--output json` and `jq`:

1. Read `monitor get-synthetic-quota` or
   `monitor get-notification-quota --kind Email`; keep `Status` and `EndTime`.
2. `IN-TRASH`, missing, or unparsable: fail the run. Recovery is a human step
   (`Recover*` from the laptop) in this release; see Open decisions.
3. In `renew` mode, run `monitor renew-synthetic-quota` or
   `monitor renew-notification-quota --kind Email` with
   `--expected-end-time <EndTime>` from step 1, `--max-days-left 3`, and the
   default `--max-price 0`. The CLI exits 0 without sending when more than 3
   days remain; otherwise it quotes, refuses any price above 0 VND, sends
   exactly once with no retry, and reads until the new end time shows.
4. Read again. If the end time did not move and 2 days or fewer remain, fail.
5. Append one summary line per quota: status, old end, new end, action.

| CLI exit | Meaning                                              | Run result        |
| -------- | ---------------------------------------------------- | ----------------- |
| 0        | Renewed, or skipped because not due                  | Pass unless alert |
| 1        | `PriceAboveMax`, `Unpriced`, `NotSettled`, API error | Fail              |
| 2        | `InvalidUsage` or `ReadOnly`                         | Fail              |
| 3        | Authentication failure                               | Fail              |
| 4        | `NotFound`                                           | Fail              |

Guards: one send at most per quota per run; the CLI never resends after a
timeout or 5xx (`NotSettled` fails the run; the next run reads first and treats
a moved end time as done); a stale `--expected-end-time` is refused, so a rerun
of an old run cannot send twice. The 3-day window and the one-month term bound
normal use to one renewal per quota per month, within the budget of two email
renewals a month. On any failed run the owner disables the workflow
(`gh workflow disable vmonitor-renew.yml`) before investigating, so repeated
attempts cannot spend the three monthly notification-quota actions.

The script and the CLI print quota names, statuses, end times, prices, and exit
codes only. GitHub redacts registered secret values, but "this redaction is not
guaranteed" for transformed values [secrets], so the CLI must never print a
token or credential, including in its one-line JSON errors, and the workflow
never uses `--debug`, uploads artifacts, or caches `~/.vngcloud`.

## Credential custody

- Principal: GreenNode service account `aboutme-vmonitor-renew`, no console
  login, token lifetime 1800 s. A service account signs in with a client ID and
  secret; an IAM-user login needs the root sign-in page's reCAPTCHA and cannot
  be automated [vngcloud-auth].
- Policy `aboutme-vmonitor-renew`: one allow statement on resource `*` with 11
  actions: `GetSyntheticQuota`, `GetSyntheticTestQuotaDetail`,
  `GetSyntheticUsage`, `RenewSyntheticQuota`, `RecoverSyntheticQuota`,
  `GetEmailQuota`, `GetEmailUsage`, `RenewEmailQuota`, `RecoverEmailQuota`,
  `GetQuotaUsages`, `ListDetailedQuotasInTrash`. No buy, delete, auto-renew,
  check, channel, metric, log, compute, network, storage, or IAM action. Probed
  2026-10-14: quota reads and zero-price quotes return 200; check list, metric
  quota, and central billing return 403.
- Storage: GitHub environment `vmonitor-renew`, secrets `VNGCLOUD_CLIENT_ID`,
  `VNGCLOUD_CLIENT_SECRET`, `VNGCLOUD_REGION`; an age-encrypted copy in
  `aboutme-infra`, like other host secrets.
- Rotation: create a new key for the service account, pipe it from its source
  file into `gh secret set --env vmonitor-renew VNGCLOUD_CLIENT_SECRET`, run the
  workflow with `action=check`, then delete the old key. Rotate on any suspected
  leak and after any change to who can push to `main`.
- Revocation: delete the key or the service account in GreenNode IAM. The 1800 s
  token lifetime bounds a stolen token; a stolen secret stays valid until
  revoked. A revoked credential fails the run with exit 3, and the manual
  console renewal in the runbook still works.

## Threats

- Stolen `GITHUB_TOKEN`: `permissions: {}` leaves it no repository, package, or
  workflow write.
- Stolen GreenNode secret: can read the two quotas and renew or recover them;
  cannot buy, delete, change checks or channels, or touch compute, storage, or
  IAM. Residual risk: IAM has no condition keys, so if the email package ever
  became paid, a renewal at that price would charge the credit wallet; the
  `--max-price 0` guard lives only in the CLI. Denial of service: spending the
  three monthly notification-quota actions could block the owner's own renewal
  for the month; mitigations are rotation and the GitHub uptime backstop.
- Malicious pull request: no `pull_request` trigger; a fork receives no secrets
  [fork-secrets]; the environment admits `main` only, and `main-required-ci`
  governs what lands there. A changed workflow acts only after it lands.
- Compromised dependency: actions pinned by commit, Go toolchain pinned, the CLI
  pinned by version and module hash under `GOTOOLCHAIN=local`, no npm. A bad CLI
  release needs a reviewed pin bump; the CLI maintainer is the owner.
- Public logs: see Renewal procedure.
- Compromised container or host: no change; the host holds no GreenNode
  management credential.

## Failure behavior

A failed scheduled run notifies the user who last modified the cron line, or who
re-enabled the workflow; a failed dispatch notifies the actor [notify]. Both are
the owner, who keeps Actions failure notifications on. Every failure exits
non-zero with an `::error::` line and a step summary naming the quota. A pass
prints both end times. Expiry stops the checks; the uptime backstop keeps
running.

## Alternatives

- Laptop `systemd` timer: the credential already lives there, but the laptop is
  not always on, and one missed 3-day window stops the checks.
- Production host timer: puts a GreenNode management credential on the host,
  where a compromised container or host could use it; the host keeps only static
  vStorage keys.
- Telegram or webhook channel: GreenNode caps only email and SMS [notification],
  so this would remove the email renewal, but the Synthetic quota still needs
  one. A possible later simplification; owner decision.
- Paid packages: cost for a two-email-a-month alarm. Rejected by the owner.

## Tests

`scripts/test/workflow-contract.test.mjs` asserts for `vmonitor-renew.yml`:
triggers are exactly `schedule` and `workflow_dispatch`; top-level `permissions`
is `{}` and the job sets none; the job has `environment: vmonitor-renew`, the
named `concurrency` group with `cancel-in-progress: false`, and
`timeout-minutes` at most 10; every `actions/checkout` names
`repository: dannyota/aboutme`, `ref: refs/heads/main`, and
`persist-credentials: false`; the install line matches
`danny.vn/vngcloud/cmd/vngcloud@v\d+\.\d+\.\d+` and an `h1:` sum is asserted;
secrets appear only in step `env:`, never in `run:`; no `upload-artifact`, cache
action, or `--debug`. The existing pin and `.tool-versions` mirror checks cover
the actions and `go-version`.

`scripts/test/vmonitor-renew-test.sh` runs the script with a fake `vngcloud` on
`PATH` and asserts: no send when not due; one send per quota when due; CLI exit
1 fails the run; `IN-TRASH` fails; a moved end time on the second read passes; 2
days left after renewal fails; a rerun sends nothing; stdout never contains the
fake secret.

## Release order

1. The CLI release with the four commands and its `h1:` sum (peer).
2. The environment, branch rule, and secrets (owner, in progress).
3. One devops change: script, tests, workflow, runbook, backlog line, ADR
   history. Adversarial review before merge.
4. First run: `workflow_dispatch` with `action=check` on `main`.
5. The manual renewal on 2026-10-24 stands unless step 4 passed by 2026-10-22
   21:43 UTC, when the Synthetic 3-day window opens. The first live renewal
   settles the two unconfirmed facts; record them in the runbook.

## Open decisions

- Automatic `Recover*` after an expiry, or human only (recommended: human only
  until one recovery has been observed).
- Store `VNGCLOUD_REGION` as an environment variable rather than a secret, so
  GitHub does not redact `hcm-3` from logs (recommended).

[Synthetic quota]:
  https://docs.greennode.ai/vmonitor/dashboards/synthetics/lam-viec-voi-synthetic-test-quota
[Email quota]:
  https://docs.greennode.ai/vmonitor/dashboards/notification/lam-viec-voi-email-notification-quota
[notification]: https://docs.greennode.ai/vmonitor/dashboards/notification
[schedule]:
  https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#schedule
[permissions]:
  https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax#permissions
[environments]:
  https://docs.github.com/en/actions/how-tos/deploy/configure-and-manage-deployments/manage-environments
[secrets]: https://docs.github.com/en/actions/concepts/security/secrets
[fork-secrets]:
  https://docs.github.com/en/actions/security-for-github-actions/security-guides/using-secrets-in-github-actions
[notify]:
  https://docs.github.com/en/actions/concepts/workflows-and-actions/notifications-for-workflow-runs
[gomod]: https://go.dev/ref/mod#authenticating
[vngcloud-auth]: https://github.com/dannyota/vngcloud/wiki/Authentication
