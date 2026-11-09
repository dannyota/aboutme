# CrowdSec retention batching

Status: accepted, 2026-11-02. The owner accepted fewer stopped reconciliations,
preferably at night. Recommend a 03:00 ICT batch plus age-triggered batches.
Keep the [privacy bounds](crowdsec-http-privacy.md#live-retention-and-failure)
and the existing stopped-consumer proof. This is not zero downtime.

## What needs a stop

`deploy/vn/host/crowdsec-retention.sh` already calls `retention_reconcile` only
when its selected rows include kind `d`. Its SQL deletes stale alert graphs and
orphan events/metadata live otherwise. The pinned catalog's foreign keys cascade
alert deletion into decisions, events, and metadata. Therefore:

- Expired decisions, revoked decisions, and decisions attached to a stale graph
  require stopped reconciliation before physical deletion.
- A stale alert graph with no decisions, and orphan events or metadata, need no
  consumer stop. Keep their five-minute cleanup.
- An old event in a graph that still has decisions makes the whole graph due. Do
  not detach that event or ignore it to postpone the graph's deadline.

These are source findings, also covered by existing no-decision and stale-graph
fixtures in `deploy/vn/tests/crowdsec-retention-test.sh`; no tests ran here.

## Deadline arithmetic

For a decision `d`, let `a` be the earliest of its `created_at`, `updated_at`,
the parent alert's four timestamps, every attached event's three timestamps, and
every attached metadata row's two timestamps. Use actual database values, not
`until`, insertion time alone, or a remembered last-batch timestamp.

The graph's cleanup target is `T = a + 23 h`; its verification deadline is
`H = a + 23 h 30 min`. Expiry `e = until` does not restart either clock. An
already expired decision can wait until a batch before `T`; an active decision
whose graph reaches `T` must be reconciled and deleted with that graph, as in
the current code. Fresh graph rows can be lost through that existing cascade.

One nightly batch cannot satisfy the bounds. A decision created at 02:59 ICT and
expiring at 03:14 survives the 03:00 batch. Next night's batch sees it at age 24
h 1 min, beyond both bounds, even without an older event.

After a batch at `B`, all expired decisions are gone. With sustained traffic,
duration `D`, and graph history `L` before decision creation, the oldest
survivor was created just after `B - D`. Its target arrives just after
`B + 23 h - D - L`. This is the available interval between forced batches:

| Duration `D` | `L = 0`     | `L = 30 min` |
| ------------ | ----------- | ------------ |
| 15 min       | 22 h 45 min | 22 h 15 min  |
| 1 h          | 22 h        | 21 h 30 min  |
| 4 h          | 19 h        | 18 h 30 min  |
| 22 h         | 1 h         | 30 min       |

The estimates assume graph history `0 <= L <= 30 min` at decision creation,
including ingestion delay. This is a workload assumption, not a configured
lifetime bound. `capacity * leakspeed` bounds neither a leaky bucket's lifetime
nor its alert history: each event resets its idle timer, and alert construction
uses the bucket's first timestamp. See the pinned
[bucket lifecycle](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/leakybucket/bucket.go#L183)
and
[alert construction](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/leakybucket/overflows.go#L283).
One qualifying 404 every 10 seconds for 25 hours, followed by a burst, can
therefore create a graph already beyond `H`, even with prompt ingestion.

Treat such a graph as immediately due, without changing its timestamps or
waiting for the nightly batch. The first pass that observes it uses the existing
stopped path if it has decisions; decision-free graphs retain live cleanup.
Delete and verify before rebuilding consumers or renewing the lease. If it
arrives after selection and remains at final verification, fail the pass, stop
consumers, and require full stopped recovery. Keep the original pass deadline;
never renew a lease with an over-`H` row. Add no bucket cap or engine fork.

This exceptional path does not prevent admission of an already-old graph or
retroactively meet its deadline. Under normal scheduling, discovery can take
another five minutes. Clear the retention breach and emit the fixed reason
`crowdsec_retention_overdue`; this does not meet the occurrence-age bound.
Expected rarity is not a guarantee: a visitor can sustain such buckets
deliberately. The 361-second cleanup bound below applies to records present
before `T`; privacy deadlines remain requirements, not an unconditional
guarantee.

## Schedule and live passes

Keep one five-minute timer and the existing `startup`/`runtime` interface.
Change its `AccuracySec` from 30 seconds to 1 second. In the helper use UTC:
20:00 UTC is 03:00 ICT the next day. The runtime pass whose start falls in
`[20:00, 20:05)` is the nightly opportunity; if missed, the age trigger still
protects retention. No extra timer, daily marker, daemon, or persisted queue.

At each runtime pass, after the existing gates and feed trim:

1. If any decision's `a <= pass_time - 23 h`, run a forced batch. Otherwise, run
   a nightly batch only in the nightly opportunity and only if expired decision
   candidates exist. Never restart for an empty batch.
2. In a batch, use the original candidate predicate: all expired decisions, old
   decisions, stale graphs, and orphans. Stop consumers once, clear owned
   nftables state once, drain the bounded SQL batches, verify, and rebuild only
   the consumers previously active. Unexpired decisions on fresh graphs stay.
3. On other passes, select only stale graphs with no decisions and orphans. Keep
   expired decisions in the database, where streams can still deliver removals.
   Do not equate retained expired rows with active bans.
4. Verification distinguishes intentionally deferred expired decisions from due
   work. Require no stale graphs, age-due decisions, or orphans at the captured
   cutoff. Permit younger expired decisions on a live pass only. Independently
   check every row against `H` using a fresh verification time, repeat
   preflight, and renew the existing lease only after full success.

This starts forced cleanup at the 23-hour target, rather than consuming the
failure margin to approach 23 h 30 min. A record crossing `T` just after a pass
is normally deleted within `300 + 1 + 60 = 361 seconds`, at most 23 h 6 min 1 s
old. The existing 12-minute lease/watchdog/stop allowance still fits before `H`:
`6 min 1 s + 12 min < 30 min`. Timer accuracy is not a hard scheduling
guarantee; delayed or failed passes keep the existing fail-closed behavior.
Stopping prevents access but does not erase database rows. Recovery must
complete deletion before reopening; an outage is not successful cleanup.

Expected successful reconciliation cycles, each restarting the previously active
Caddy, engine, and firewall bouncer once:

- No decisions: zero per day. Sparse traffic: usually zero or one, sometimes two
  if a pending graph reaches its target before the next night.
- Sustained 15-minute to 4-hour bans with `L <= 30 min`: two per day, one
  nightly and one forced. At `L = 30 min`, the forced batch is around 21:30 ICT
  for 4-hour bans, or 01:15 ICT for 15-minute bans.
- Sustained 22-hour decisions: approximately 24 per day for `L = 0`, or 48 per
  day for `L = 30 min`. One isolated 22-hour decision needs only one
  reconciliation; duration alone does not imply 48 daily restarts.
- Long-lived buckets or delayed/backdated graphs can force every five-minute
  pass, 288 scheduled cycles per day. Failure/recovery/manual starts are outside
  these estimates. Overload can fail the bounded pass instead of completing it.

A deadline-only rule removes the nightly opportunity and has fewer restarts
under sustained short bans: roughly `24 / (23 - D - L)` per day, about 1.08 to
1.30 with `L = 30 min`. Its outages drift through business hours. Recommend
nightly plus forced batches for the owner's night preference; it is not the
absolute minimum count. A fixed twice-daily schedule fits ordinary short bans
but cannot cover the 22-hour case. Reload is
[insufficient without consumer changes](crowdsec-retention-reload.md).

## Concurrency and existing limits

Keep the host lock and candidate IDs, row versions, graph membership, limits,
timeouts, and epoch validation. For live SQL deletion acquire the complete
existing table-lock set with `NOWAIT`. On contention roll back the entire
transaction, releasing partial locks, and reselect; never wait while holding
`alerts` for an ingestion transaction that already holds `decisions`. Allow at
most three attempts within the original pass deadline. Exhaustion, unexpected
SQL errors, or changed candidates follow existing failure handling; no partial
deletion or lease renewal. Test real ingestion paused between child and parent
inserts. Stopped batches keep the same acquisition rule.

`until <= pass_time` means eligible for a stopped batch, not proven natural
expiry: explicit revocation can shorten `until`. Both remain stream-visible
until the stopped batch. An operator needing proven immediate revocation must
use stopped reconciliation immediately, not wait for the nightly schedule. No
live-deletion exemption or natural-expiry discriminator is introduced.

Both Caddy modes currently share `crowdsec-bouncer-key` and host networking;
their streaming cursors are not independent. Stopping both proves retirement of
their old caches for deletion, but does not fix missed fresh bans when both
serve concurrently. Do not create a new overlap or use maintenance as an outage
mask. Independent registrations for concurrent modes require a separate key
provisioning/rotation change outside this small patch. Resolve and prove that
existing enforcement defect before claiming safe concurrent serving; neither
batching nor the bouncer's
[populated flag](crowdsec-retention-reload.md#pinned-bouncer-behavior) provides
that proof or a startup-readiness guarantee.

## Escalation

Retained expired rows must not increase ban duration. In
`deploy/vn/edge/crowdsec/profiles.yaml`, change `duration_expr` to evaluate
`GetActiveDecisionsCount(Alert.GetValue())` exactly once per evaluation and bind
the result with syntax supported by the pinned expr language, such as `let`.
Reuse that single value for both comparisons; never query again between them.
Otherwise, expiry can change one active decision to zero between calls and
incorrectly select 4 hours. Keep the filters, decision type, 15-minute / 1-hour
/ 4-hour values, profile order, and `on_success` unchanged. Count zero active
prior decisions as 15 minutes, one as 1 hour, and two or more as 4 hours. Keep
the helper's existing address/range matching and scenario scope; add no history
store or new time window.

The required helper contract is `until > evaluation_time`; equality and earlier
expiry, including shortened expiry through revocation, do not count. The pinned
[expression helpers](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/exprhelpers/helpers.go#L719)
and
[count query](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/decisions.go)
own that behavior. Local 1.7.8 source shows the active helper selecting
`CountDecisionsByValue(..., nil, true)` and `UntilGT(time.Now().UTC())`; verify
that contract and expression registration on 1.8.1 before release. A helper or
query error must fail the proof, never justify reverting to all retained rows.
The supplied review confirms that pinned 1.8.1 returns count-query errors;
[duration evaluation](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/csprofiles/csprofiles.go#L110)
logs evaluation errors and retains the configured 15-minute fallback. This
runtime fallback does not make a failed rollout proof acceptable.

Eager five-minute deletion can leave just-expired rows counted until its next
pass. Active-only counting removes that incidental grace period; it cannot
increase escalation from retained expired history. Offenses at 08:00, 12:00, and
18:00 with no active prior bans each receive 15 minutes. This preserves the
accepted short-ban policy for shared addresses, including mobile carrier CGNAT;
it needs no new owner decision. A creation-time lookback is less suitable:
decision durations differ, so one window cannot represent all active bans.

## Exact implementation boundary and proof

Only these implementation files need edits; none are edited by this proposal:

- `deploy/vn/host/crowdsec-retention.sh`: batch eligibility, live/batch
  candidate selection and verification, fresh final age check, nonblocking SQL
  locks and bounded contention retries, and the fixed overdue alarm. Preserve
  startup's complete stopped cleanup, feed trimming, lease format, consumer
  restart order, and watchdog contract.
- `deploy/vn/host/etc/systemd/system/aboutme-crowdsec-retention.timer`:
  `AccuracySec=1s`; keep five-minute `OnCalendar`, persistence, and service.
- `deploy/vn/edge/crowdsec/profiles.yaml`: the single bound active count above.
- `deploy/vn/tests/crowdsec-retention-test.sh` and
  `deploy/vn/tests/crowdsec-retention-fixture.sh`: deterministic time inputs,
  schedule boundaries and counts, deferred-expiry lease success, live graph
  cleanup, fresh final verification, missed night, startup, contention, changed
  child rows, oversized graphs, already-overdue graphs at selection and after
  selection, and existing failure cases.
- `deploy/vn/tests/crowdsec-integration-test.sh`: real expiry and pre-selection
  revocation retained until batch; stopped deletion and rebuild of both modes;
  fresh-ban preservation, delayed stream responses, paused ingestion, and
  active-only escalation and slow-trickle bucket lifetime while rows remain.
- `deploy/vn/tests/crowdsec-config-test.sh`: timer accuracy and the exact active
  helper expression, keeping the three duration values and profile order.

Use pinned engine 1.8.1 with a controlled clock/replay to feed one qualifying
404 every 10 seconds for 25 hours, then a 21-request burst. Prove the old first
timestamp survives despite `cache_size: 5`; a capacity-times-leakspeed assertion
alone is insufficient. Pass the resulting graph through retention with and
without a decision: prove immediate eligibility, stopped cache retirement when
needed, graph deletion, the fixed alarm, and lease renewal only after fresh
verification. Also insert an over-`H` graph after selection and prove failure,
no lease renewal, and stopped recovery. Advance time deterministically rather
than sleeping for 25 hours; direct SQL fixtures alone do not prove bucket life.

Compare identical seeded histories under eager deletion and batching: retained
expired rows must never yield a longer ban. Cover 08:00/12:00/18:00,
zero/one/two active prior decisions, `until` just before/equal/after evaluation
time, revocation, overlapping active and expired decisions, and before/after a
batch. The active-only expression must produce the same duration before and
after physical deletion of expired rows. Prove expression compilation and
evaluation on 1.8.1; missing helper or count-query failure blocks rollout. Add
an expiry-between-calls regression: return one active decision on the first
helper call, then advance the controlled clock past its expiry before the second
comparison, so a second call would return zero. Assert exactly one helper call
and a 1-hour result, never 4 hours. Separately inject a count-query error and
assert the configured 15-minute fallback.

Test all four durations, decisions just before/after 03:00, old parent/event/
metadata clocks, `T` equality, `H` equality rejection, and continuous synthetic
traffic. Count one stop/rebuild per pass regardless of SQL batch count. Existing
hosted `check`, `crowdsec-postgresql`, and `crowdsec-integration` jobs own
proof; no new CI job or production test is authorized by this document.

## Security, size, and release

Keep
[credential custody and recovery](crowdsec-http-privacy.md#security-boundaries).
Anonymous visitors, signed-in users, hostile resume content, and stolen app/MCP
tokens cannot select batch times or authorize SQL. A visitor can create more
legitimate candidates and force the bounded failure path; timers never depend on
visitor text. Network attackers gain no endpoint. Compromised containers gain no
host command, peer identity, or lease authority. Database writers cannot renew
the root-owned lease; corrupted dates/graphs fail validation or demand earlier
cleanup. Host root and database superusers remain trusted. Leaked backups retain
the existing exposure and restore-reset requirement.

No new key, schema, API, client fallback, registration change, or runtime
service is needed for batching. Retaining decisions until age cleanup increases
rows and can make each batch larger; keep the 1,000-candidate and whole-pass
limits, host budgets, fixed-code alarms, and fail-closed exhaustion. Do not
promise a fixed memory bound from the restart estimates.

The owner's batching acceptance remains valid; no longer escalation history is
proposed. Ship helper, timer, profile, and tests as one host bundle after
exact-commit CI and independent adversarial review. Preserve the startup sweep
on install/reboot; it may cause a one-time reconciliation. Rollback restores
eager deletion with the stopped path and five-minute checks; retain active-only
counting. No database reset or additional data loss is authorized. Privacy
notice and ADR 0026 bounds remain unchanged.

## Sources and verification limits

Repository code/catalog/tests above and the supplied prior independent review
are the evidence for current behavior. No network or runtime proof ran. Engine
1.8.1 source was not available locally. Bucket findings above come from the
supplied review's pinned citations; active-helper details were inspected in
local 1.7.8 only. Confirm both against the linked 1.8.1 source and pinned CI
fixture before release, along with ingestion, revocation, and cursor behavior.
The first-party links identify required sources, not newly fetched proof:

- [Engine insertion](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/alerts.go)
- [Expiry, revocation, and counting](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/decisions.go)
- [Stream cursor](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/apiserver/controllers/v1/decisions.go)
- [Bouncer identity](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/apiserver/middlewares/v1/api_key.go)
- [PostgreSQL locking](https://www.postgresql.org/docs/18/explicit-locking.html)
- [systemd timer accuracy](https://www.freedesktop.org/software/systemd/man/latest/systemd.timer.html)
