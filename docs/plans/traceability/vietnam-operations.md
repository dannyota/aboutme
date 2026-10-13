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

## Alarm acceptance remains open

AC-OPS-019 follows the [Vietnam alarm inventory](../../design/vietnam-production.md#logs-metrics-and-alarms) and [production alarm runbook](../../runbooks/production.md#alarms): host conditions and unit failures use Bizfly SMTP, and external readiness failures use GitHub workflow notifications.

The 2026-10-13 03:44:20 UTC collection found daily alert stamps for host and unit conditions. The latest `failed-units` stamp was 2026-10-13 00:00:00 UTC; the latest `coraza-unparsed` stamp was 2026-10-13 00:10:04 UTC. A stamp is created before sending and normally removed when `msmtp` fails. An interrupted send can retain it. SMTP output is discarded, so stamps alone do not prove SMTP acceptance or mailbox delivery. The watcher ignores send failures, so a successful watcher exit also does not prove delivery.

The owner reported receiving operations mail. No per-alarm receipt metadata was collected. Acceptance still needs a documented safe trigger for each applicable alarm, evidence that its detector or failure action ran, and a matching mailbox receipt timestamp. A direct synthetic mail proves the sender path only. Production retention failures stop the edge and must not be induced solely for notification proof.

## External readiness monitoring

On 2026-10-13, `uptime.yml` was active and had eight successful scheduled executions since 2026-10-11. The latest three starts were 2026-10-12 09:28:59 UTC, 2026-10-12 19:15:15 UTC, and 2026-10-13 01:23:44 UTC. These gaps do not match its five-minute cron and leave monitoring cadence unproved. A separate public `/readyz` check returned 200 at about 2026-10-13 03:49 UTC. Successful samples establish readiness only when sampled; they do not establish uninterrupted availability or notification delivery.

## Supporting host metadata

At 2026-10-13 03:44:20 UTC, the watcher, CrowdSec retention, and three backup timers were enabled and active. Root used 28% and the data volume used 2%. The retention lease was root-owned with mode `0600` and had a 03:40:00 UTC modification time. PostgreSQL answered on its socket; archive metadata reported no failures and `archive_failing=false`. The backup repository reported status code 0, 16 backups, and a newest backup age of about 3.59 hours. The latest weekly verification exited 0 at 2026-10-12 00:01:12 UTC. These facts do not establish a completed restore drill.
