# vMonitor renewal CLI contract

This page holds the pinned CLI and script contract of the
[vMonitor Synthetic quota renewal](vmonitor-quota-renewal.md) design, which owns
the workflow, trust boundary, and
[persistent hold](vmonitor-quota-renewal.md#persistent-hold).

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

Supplied release evidence satisfies this pin's maintainer gate: verified signed
annotated tag [v0.89.0][release] points to
`2eddd4a00198f71c6ad3db6f631402b2f4d6603f`; successful [master CI][cli-ci]
includes `go test -race ./...`. Real-transport tests with retries enabled live
in `internal/cli/svc_monitor_ci_test.go`:

- `TestSyntheticMutationCLIRealTransport`
- `TestSyntheticMutationCLISendFailures`
- `TestSyntheticMutationCLIRetainedAttemptOutcomes`
- `TestSyntheticMutationPreSDKOutcome`
- `TestSyntheticQuotaCIEnvironmentOnlyAuth`

These supply evidence for request counts, retained attempt outcomes, preflight
refusal, and environment-only auth; fake-CLI tests here cannot prove transport
safety. Live smoke found a 0 VND quote and not-due at 30 days left, with no
send. Live renewal and term arithmetic remain unverified. Sources and JSON
goldens below pin v0.89.0; wiki section names locate guidance.

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

`scripts/vmonitor-renew.sh` uses JSON and `jq`. With `umask 077`, create a
mode-0700 temporary directory and a fresh child `HOME` for CLI processes. Their
clean environment admits only the four named cloud variables and required
process settings; exclude `VNGCLOUD_PROFILE`, config/credential paths,
endpoint/debug overrides, and the issue API token. Never load profiles or reuse
token caches.

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

Open and verify the guard first. The CLI re-reads, refuses stale expected expiry
or missing/nonzero price, and sends at most once. Script and CLI never resend
for auth, timeout, 429, 5xx, or settlement failure; bounded polling only reads.
`--expected-end-time` is not durable idempotency or proven server-side
compare-and-swap. Human renewal must not overlap a run. No Recover, Buy, or
notification commands.

Renew stdout is one object with exactly `Sent`, `Stage`, `Error`,
`PreviousEndTime`, `Quota`, `OrderID`, `PaymentPending`, `Renewed`. `Error` is
null or has exactly `Code`, `Operation`, `Status`. Validate exact JSON keys,
types, and pinned **Unattended CI** combinations below; never search arbitrary
output for a safe field. Missing, invalid, contradictory, unknown, or
unclassified SDK output retains the opened guard. Capture exit status
separately; nonzero does not imply no send.

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
Only `confirmed` has `Renewed == true`. `PaymentPending` must be false outside
`unconfirmed`, including every closing outcome and `accepted`. Validate these
flags together with the table's `Sent`, `Stage`, `Error`, and exit combinations
before closure. `accepted` cannot pass this workflow, which never sets `NoWait`.
Preflight failures emit the full object; stderr's lowercase `error` is separate.

Close only for valid output with `Sent == "no"` and stage `not-due` or
`preflight`, or stage `refused`, or stage `confirmed` with `Renewed == true`.
Otherwise keep the hold. HTTP status and CLI exit alone never authorize closure;
interrupted or broken output may lack usable JSON. Refusal proves rejection, not
absence of a send.

Only valid not-due or confirmed output with exit 0 proceeds to read-only status
read-back; all other outcomes fail despite healthy later reads. Sent renewal
needs an end instant strictly later than `expiry`. Every passing run, including
`check` and not-due, needs active Basic status and > 172800 seconds left. Valid
confirmation allows closure despite failed final health, but the run fails.
Later reads cannot make `unconfirmed` succeed or clear its hold; response bodies
alone cannot pass.

On nonzero CLI exit, classify captured output, close only if allowed, and fail
with that exit (1, 2, 3, or 4); shell fail-fast must not skip classification.
Script validation, health, or issue API failures exit 1. Unknown CLI exits fail
and retain an opened guard. Failed guard closure cannot turn nonzero CLI results
into success. `check` never changes the hold or sends renewal.

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
