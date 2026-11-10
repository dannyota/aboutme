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

## Host bundle reinstall

An exact package pin is installed with either dpkg status `install ok installed`
or `hold ok installed`. Keep the holds. Checking only the first status can send
a healthy host through offline package installation, disabling and masking
CrowdSec. Migration then stops the edge and refuses the occupied catalog with
`crowdsec_migrate_refused step=fresh-admission`.

Stop rerunning that bundle. Resolve any owned operation, then follow
[failure recovery](#failure-recovery), including explicit `--reset` and its
CrowdSec data loss. Run the verification commands. If reset reports
`crowdsec_migrate_no_edge`, start the intended previously active edge through
its normal guards, for example `sudo systemctl start aboutme-caddy.service`, and
confirm readiness. A successful reset alone does not restore serving.

After recovery, copy the reviewed fixed bundle and rerun
`sudo bash <bundle>/host/install.sh <bundle>`. This completes settings and unit
activation skipped after the failed migration. Confirm the installer reports
`install: done`, the intended edge serves, and the timers are active.

## Engine upgrade to 1.8.2

Follow the [upgrade design](../design/crowdsec-1.8.2-raise.md). Reset loses
CrowdSec alerts, bans, windows, and registrations; application data stays
intact. Require independent review, green exact-commit main-push CI, a patch
tag, and successful release-image publication under the
[release rules](../../instructions/releases.md). Before installation, take the
encrypted incremental backup on the host using the workload lock and release
annotation. Require completion and healthy WAL archiving; keep keys on the host.
The deployer's later backup is too late.

```sh
sudo /usr/local/lib/aboutme/workload-lock.sh 21600 \
  runuser -u postgres -- pgbackrest --stanza=aboutme --type=incr \
  --annotation="aboutme-release=<tag>" backup
```

1. Record the active edge and keep the reviewed 1.8.1 bundle and package. Copy
   `deploy/vn` from clean reviewed main and run
   `sudo bash <bundle>/host/install.sh <bundle>`. Accept only
   `crowdsec_migrate_refused step=fresh-admission`, after checking 1.8.2 is
   installed and held, its helpers and catalog match the bundle, and consumers
   are stopped. Diagnose any other failure before proceeding.
2. After the installer exits and releases its lock, run
   `sudo /usr/local/lib/aboutme/crowdsec-migrate.sh --reset`. Restore only the
   recorded edge through normal guards, for example
   `sudo systemctl start aboutme-caddy.service`.
3. Rerun the same installer until `install: done`. Allow for a brief PostgreSQL
   restart. Run `DEPLOY_HOST=<host> bash deploy/vn/scripts/deploy.sh <tag>`.
4. Run the checks below. Verify the package and hold, public readiness, ban
   enforcement and removal, PostgreSQL health, backup and WAL state, and a
   successful scheduled retention and vacuum.

A failed reset revokes its lease, retains failure denial, and stops consumers.
Diagnose the reported step, repair the cause, rerun `--reset`, restore the
recorded edge, and finish the installer. Never clear fences or markers by hand.

To roll back, use the reviewed 1.8.1 bundle's installer and authenticated
package with its matching helpers and catalog. At occupied-catalog refusal,
reset under that old bundle, restore the recorded edge, and finish its
installer. CrowdSec state is lost again. Preserve notice and fence fixes; never
restore the application cluster for CrowdSec. Image rollback alone retains the
host bundle. Do not refresh the hub as part of this upgrade. The
`cscli.hub_branch: master` pin selects the branch, not content versions;
uncached operations still need network. See the
[branch selection](https://github.com/crowdsecurity/crowdsec/blob/v1.8.2/cmd/crowdsec-cli/core/require/branch.go)
and
[hub commands](https://github.com/crowdsecurity/crowdsec/blob/v1.8.2/cmd/crowdsec-cli/clihub/hub.go).

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
