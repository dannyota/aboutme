# Vietnam operations evidence

Read-only production metadata collected on 2026-10-13 supports the successful-run requirement in AC-INF-006. [ADR 0027](../../adr/0027-vietnam-hosted-production.md) and the [Vietnam scheduled-job design](../../design/vietnam-production.md#scheduled-jobs) define the four application jobs.

## Successful application jobs

Each `aboutme-job@<name>.service` reported `Result=success`, `ExecMainCode=1` (normal process exit), and `ExecMainStatus=0` for its latest invocation. Corresponding journal reports had `success=true` and `dryRun=false`. All timestamps below are UTC.

|Job|Started|Exited|Exit status|
|-|-|-|-|
|`idempotency-expiry-sweep`|2026-10-13 03:03:53|2026-10-13 03:03:54|0|
|`media-deletion-sweep`|2026-10-13 03:02:24|2026-10-13 03:02:25|0|
|`privacy-retention-sweep`|2026-10-13 00:01:39|2026-10-13 00:01:40|0|
|`media-orphan-sweep`|2026-10-12 00:02:50|2026-10-12 00:02:51|0|

At 2026-10-13 03:44:20 UTC, all four application-job timers were enabled and active. Duplicate journal reports shared invocation IDs, so report totals are not invocation counts. This evidence records one successful invocation of each job; it does not prove overlap handling or failure notification.

## Alarm acceptance

AC-OPS-019 follows the [Vietnam alarm inventory](../../design/vietnam-production.md#logs-metrics-and-alarms) and [production alarm runbook](../../runbooks/production.md#alarms): host conditions and unit failures use Bizfly SMTP, and external readiness failures use GitHub workflow notifications.

The 2026-10-13 03:44:20 UTC collection found daily alert stamps for host and unit conditions. The latest `failed-units` stamp was 2026-10-13 00:00:00 UTC; the latest `coraza-unparsed` stamp was 2026-10-13 00:10:04 UTC. A stamp is created before sending and normally removed when `msmtp` fails. An interrupted send can retain it. SMTP output is discarded, so stamps alone do not prove SMTP acceptance or mailbox delivery. The watcher ignores send failures, so a successful watcher exit also does not prove delivery.

The owner supplied a screenshot of the `coraza-matches` alert received at 13:40 Vietnam time on 2026-10-13. The message records event time `2026-10-13T06:40:01Z` and a count of 122. This verifies receipt for the Coraza match-threshold alert. The watcher counts rule-match records, not distinct requests, so the subject's use of "requests" is inaccurate. The live serving container's configuration was checked and uses `SecRuleEngine DetectionOnly`.

On 2026-10-13 the owner accepted AC-OPS-019 on this evidence: host and unit alarms share the Bizfly SMTP sender that delivered this alert, and the alert wording was corrected from it. Per-alarm triggers are not required. Production retention failures stop the edge and must not be induced for notification proof. External readiness alerts depend on the scheduled workflow, whose cadence gap remains open below.

### Coraza alert at 06:40 UTC

A read-only aggregate of the three retained RAM-only WAF log files reproduced all 122 matches in the hour ending `2026-10-13T06:40:01Z`. The matches occurred between `06:37:56.786Z` and `06:37:57.916Z`. There were no invalid or unparsed records in that window. The watcher correctly excluded 231 engine diagnostics from the match count. Public readiness returned HTTP 200 during the investigation.

|Rule|Records|Meaning in the pinned CRS 4.25.0 rules|
|-|-|-|
|930130|55|Restricted file path|
|930140|2|AI coding assistant artifact path|
|920440|7|Restricted filename extension|
|920500|1|Backup or working-file path|
|949110|57|Inbound anomaly score exceeded|

The filename rules and short burst are consistent with an automated file scan. This is an inference; the retained metadata has no request identifiers, response status, raw paths, or client addresses. It cannot establish distinct request totals, blocked responses, or successful access. Coraza's live detection-only configuration does not block on these matches.

The received subject incorrectly labels rule records as requests. Its generic footer points to `journalctl`, but Coraza rule metadata is in `/run/aboutme/caddy-log/waf/` on tmpfs, not the journal. The alert implementation now says "rule matches" and directs only the three Coraza alert keys to safe metadata inspection; unit and host alerts keep their journal guidance. Rule counts, thresholds, throttling, and the detection mode are unchanged.

2026-10-14: the owner decided that `coraza-matches` and `coraza-unparsed` must not send mail. The [alarm inventory](../../design/vietnam-production.md#logs-metrics-and-alarms) keeps their hourly counts in the watcher journal and retains `coraza-log-invalid` mail. This decision replaces the Coraza notification behavior described above; the 2026-10-13 delivery evidence and acceptance remain historical records.

## External readiness monitoring

On 2026-10-13, `uptime.yml` was active and had nine successful scheduled executions since 2026-10-11. The latest starts were 2026-10-12 19:15:15 UTC, 2026-10-13 01:23:44 UTC, and 2026-10-13 07:55:21 UTC. The eight successive gaps range from 3:12:21 to 9:46:16. Each run started when created and completed within 7 to 10 seconds. There were no failed, pending, skipped, or canceled runs in the complete ten-run inventory, which also included one successful manual execution. The missing intervals precede recorded run creation; the API does not establish the scheduler's internal cause.

These gaps do not match the requested five-minute cron and leave monitoring cadence unproved. [GitHub documents possible delay or dropped scheduled runs](https://docs.github.com/en/actions/reference/workflows-and-actions/events-that-trigger-workflows#schedule), without a maximum delivery delay. Changing cron offsets would not establish the interval. A separate public `/readyz` check returned 200 at about 2026-10-13 03:49 UTC. Successful samples establish readiness only when sampled; they do not establish uninterrupted availability or notification delivery.

## Historical readiness failure

Retained metadata for the 2026-10-10 19:12 UTC boot shows the network, web, and PostgreSQL start jobs completing before the server starts. Before the server's explicit restart at 19:18 UTC, 41 `/readyz` samples returned 503 while all 27 collected `/healthz` samples returned 200. The first readiness failure took 5,040 milliseconds; the next 40 took less than one reported millisecond. After restart, readiness returned 503 once, then 200. No PostgreSQL stop coincided with that restart.

Those timings are consistent with a cached browser readiness failure, but they do not identify the failing dependency. The private browser probe intentionally caches its result for the process lifetime. An earlier boot recorded a browser initialization failure and recovered through an automatic restart; that separate event does not prove this failure's cause. Current green readiness does not close the incident. Recovery policy or closed reason diagnostics require a separate reviewed change; no boot-order or retry change was made from this evidence.

## Restore prerequisites

An earlier read-only `storage.ListProjects` call with the management account returned HTTP 403 on 2026-10-13; its cause was not established. Later that day, with `vngcloud` updated to v0.81.1, the read-only `aboutme` profile listed one storage project, `aboutme` in HCM04, and read `aboutme-tfstate` versioning as `{"Enabled":false,"Status":"Off"}`. `deploy/vn/scripts/drill-state-proof.sh` then passed with that profile and the existing state key. Its probe `restore-drill/probes/71ec062a-5ac4-4d3f-b06d-9ff13e23e6d9.json` was created, refused a second conditional write, read back unchanged, and was deleted; the final check found it absent.

The owner confirmed on 2026-10-13, from building the `vngcloud` CLI, that a GreenNode delete is permanent and leaves no recoverable copy. That settles the permanent-deletion requirement in the [restore design](../../design/restore-drill-networking.md). No restore resources were allocated, and backup restoration remains unproved.

## Supporting host metadata

At 2026-10-13 03:44:20 UTC, the watcher, CrowdSec retention, and three backup timers were enabled and active. Root used 28% and the data volume used 2%. The retention lease was root-owned with mode `0600` and had a 03:40:00 UTC modification time. PostgreSQL answered on its socket; archive metadata reported no failures and `archive_failing=false`. The backup repository reported status code 0, 16 backups, and a newest backup age of about 3.59 hours. The latest weekly verification exited 0 at 2026-10-12 00:01:12 UTC. These facts do not establish a completed restore drill.
