# CrowdSec PostgreSQL

CrowdSec uses a separate PostgreSQL database and role over the host Unix socket.
The [privacy design](../design/crowdsec-http-privacy.md) owns storage, feed,
logging, and backup limits. The privacy-notice activation must already be
complete before changed ingestion starts.

## First deploy

Use the ordinary reviewed release deploy described in
[production](production.md#deploy). The installer installs the pinned packages,
configuration, service requirements, and helpers, then runs:

```sh
sudo /usr/local/lib/aboutme/crowdsec-migrate.sh
```

The helper runs during every ordinary installation under the existing deploy
bundle lock, `/srv/data/host-operations/.bundle.lock`. Direct calls hold the
same lock; installer children reuse its open descriptor. Every call refuses an
owned fence, unresolved operation, or running operation unit before stopping
anything, and requires the existing notice activation admission.

A no-op requires PostgreSQL configuration, a passing database gate, a mode 0600
local machine credential file whose machine is validated in the current
database, both named bouncer registrations, a valid retention lease and timers,
and both CrowdSec services enabled. It changes no service state, including when
those services are stopped.

For the first switch, the helper records the active edge service and stops both
edge modes, CrowdSec, its firewall bouncer, the watcher timer and service, and
database maintenance scheduling. It joins those trigger sources, then stops and
joins `aboutme-crowdsec-retention-failure.service` before provisioning or
clearing startup denial. It provisions the separate database, applies the pinned
catalog policy, selects PostgreSQL, and deletes only the three named SQLite
database files. Old alerts and decisions are discarded. Decisions regenerate
from live traffic.

The helper registers the local machine before starting CrowdSec. Startup
retention clears owned cache and firewall state and writes a lease for the new
database. Both maintenance timers start before CrowdSec. The helper registers
`aboutme-caddy` through the host secrets helper and `aboutme-firewall` through
its private key file, then starts the firewall bouncer and the recorded edge
service. The watcher timer resumes after normal admission passes. When neither
edge service was active, neither starts and `crowdsec_migrate_no_edge` appears
on stderr; restore the intended edge through its normal guards. Zero exit does
not prove serving. The switch causes a short edge outage.

## Engine and hub upgrades

`deploy/vn/edge/crowdsec/config.yaml.local` pins `cscli.hub_branch: master` so
offline activation can reuse cached detection without calling
`https://version.crowdsec.net/latest` to select a branch. The pin selects a
branch, not content versions, and does not make uncached operations work
offline. `cscli hub update` needs network and fetches the `master` index
regardless of the installed engine version; `cscli hub upgrade` needs network
for downloads and uses that index. See the CrowdSec 1.8.1
[branch selection](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/cmd/crowdsec-cli/core/require/branch.go)
and
[hub commands](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/cmd/crowdsec-cli/clihub/hub.go).

Refresh the hub only with a reviewed engine package upgrade, never during a
release switch. Re-prove the
[version-specific behavior](../design/crowdsec-http-privacy.md#live-retention-and-failure)
for that upgrade. On the host:

1. Hold `/srv/data/host-operations/.bundle.lock` and use the existing fence and
   operation admission rules. Follow the privacy design's
   [stop and startup order](../design/crowdsec-http-privacy.md#migration-loss-and-release-order):
   record the active edge, stop both edge modes, the firewall bouncer, CrowdSec,
   the watcher, and maintenance timers, then join services and verify
   quiescence.
2. Upgrade the reviewed engine package with package startup suppressed as in the
   documented installation path.
3. Run `sudo cscli hub update`, then `sudo cscli hub upgrade`.
4. Confirm `sudo cscli hub list`. Resume startup retention, maintenance timers,
   and the engine through the documented guards; confirm
   `sudo cscli lapi status` before starting the firewall bouncer and recorded
   edge and resuming the watcher through that path. A migration no-op leaves
   stopped services stopped.

## Verification

Run these commands on the host. Retain fixed results and service state only:

```sh
sudo /usr/local/lib/aboutme/crowdsec-db.sh preflight-ready </dev/null
sudo cscli lapi status
sudo systemctl is-active crowdsec.service crowdsec-firewall-bouncer.service
sudo systemctl is-enabled aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer
sudo systemctl is-active aboutme-crowdsec-retention.timer aboutme-crowdsec-vacuum.timer
sudo /usr/local/lib/aboutme/crowdsec-retention-lease-check.sh
```

Confirm the expected edge mode serves and the ordinary deployment smoke passes.
Do not print keys, credentials, database rows, request targets, or raw error
logs. `crowdsec-migrate-test.sh` covers host ordering with fake commands;
`crowdsec-db-test.sh` covers the pinned database gate and isolated reset;
`crowdsec-integration-test.sh` covers real consumers and retention.

## Failure recovery

A failed helper stops and joins consumers, the watcher, and both maintenance
timers, prints `crowdsec_migrate_refused step=<step>`, and fails the deploy. The
step is a fixed code: `arguments`, `lock`, `idle`, `notice`, `stop`,
`fresh-admission`, `provision`, `activate`, `activate-config`,
`activate-offline`, `activate-gate`, `activate-config-proof`,
`activate-detection`, `activate-files`, `discard-sqlite`, `machine`,
`startup-admission`, `startup-retention`, `maintenance-timers`, `unmask`,
`enable`, `engine`, `caddy-register`, `firewall-register`, `firewall-start`,
`firewall-ready`, `edge-start`, `lapi`, `final-gate`, `lease`, or `watch`. The
code identifies the failed step without exposing command output. A shutdown that
cannot establish quiescence reports `crowdsec_migrate_stop_failed`; keep the
host closed and resolve the remaining process or job before recovery.

Correct the cause and rerun before schema initialization. `fresh` requires an
absent database or a verified empty schema. An occupied catalog with incomplete
activation refuses with consumers stopped, even when the catalog gate passes.
Use explicit `--reset` after schema initialization or incomplete startup. Never
remove an application operation fence or bypass startup guards. A complete no-op
leaves stopped services stopped; use reset to repeat startup.

Routine startup and runtime retention cannot clear a retained failure marker.
The admitted migration path clears it only after successful fresh provisioning
or explicit reset and activation. A later migration failure revokes the lease
and retains the marker again.

### Interrupted deploy

A deploy interrupted during an edge switch can leave the engine stopped and the
lease invalid. Both edge modes, including maintenance, must stay closed while
`/run/aboutme/crowdsec-retention-failed` exists. Never remove the failure marker
or lease, disable safety timers, or repeatedly start Caddy to reopen the site.

1. Record unit names, states, job IDs, and fixed failure results. Keep raw
   CrowdSec logs, request feeds, and database rows on the host.
2. Prove the deploy coordinator, host steps, database commands, and media
   writers have exited. Inspect the fence and any mutation marker. Recover the
   owning operation before
   [clearing its fence](production.md#passkey-release-fence); never delete fence
   files.
3. Recover incomplete startup with explicit reset below. A service restart or
   migration no-op cannot renew an invalid lease. An existing catalog requires
   reset, which discards CrowdSec history and registrations.
4. Run the verification commands above, then redeploy the intended eligible tag
   through the ordinary deployer. Confirm public readiness, security headers,
   the test ban, and healthy timers. Timing a retry between timer ticks is not a
   repair.

```sh
sudo /usr/local/lib/aboutme/crowdsec-migrate.sh --reset
```

Reset discards live CrowdSec alerts, bans, statistics, and registrations. It
preserves application data and recreates the private tmpfs log directory removed
by failure cleanup. Keep the application stopped until verification completes.
If reset reports `crowdsec_migrate_no_edge`, no edge service was restored; let
the deployer start the intended mode through its normal guards. Never fabricate
a lease by hand.

## Reset after a cluster restore

Keep consumers stopped after restoring the PostgreSQL cluster. Complete the
application and privacy-notice restore procedure under its existing owner.
Before attack protection resumes, run:

```sh
sudo /usr/local/lib/aboutme/crowdsec-migrate.sh --reset
```

Reset drops and recreates only the CrowdSec database. It discards restored rows,
planner statistics, local machine credentials, and bouncer registrations.
Startup retention clears the bouncer's owned cache and nftables state and
initializes its firewall before consumers start. The helper registers fresh
credentials and uses the same timer and service order as the first switch. Reset
does not restore the application database. Automation never discards restored
CrowdSec data. Keep consumers stopped if notice authorization still blocks
startup; follow the [notice runbook](crowdsec-privacy-notice.md).

## Retention and maintenance

Startup and five-minute runtime retention target records older than 23 hours and
verify that no eligible record reaches 23 hours 30 minutes. Decisions last at
most 22 hours. Every pass uses the database gate and bounded SQL batches. Only a
complete pass renews the database-bound lease. Both timers must be enabled and
active; the watchdog rejects missing or stale leases.

Edge changes join the current retention pass or watcher safety check under the
retention lock. App readiness waits run outside that lock. Retention and vacuum
timers stay enabled and active; their deadlines and lease checks still apply
during deploy, rollback, and activation. The watcher releases its locks before
disk, backup, and journal checks.

Hourly vacuum uses `VACUUM (ANALYZE FALSE)` with its own deadline. Never run
`ANALYZE` or `VACUUM ANALYZE` against CrowdSec. Statistics contamination
requires explicit database reset. Backups cover the whole cluster, including
CrowdSec; live deletion does not erase retained backups or WAL. Follow the
privacy design's backup lifetime and the ordinary backup alarms.
