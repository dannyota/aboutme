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
database maintenance scheduling. It joins the stopped services and verifies
quiescence. It provisions the separate database, applies the pinned catalog
policy, selects PostgreSQL, and deletes only the three named SQLite database
files. Old alerts and decisions are discarded. Decisions regenerate from live
traffic.

The helper registers the local machine before starting CrowdSec. Startup
retention clears owned cache and firewall state and writes a lease for the new
database. Both maintenance timers start before CrowdSec. The helper registers
`aboutme-caddy` through the host secrets helper and `aboutme-firewall` through
its private key file, then starts the firewall bouncer and the recorded edge
service. The watcher timer resumes after normal admission passes. When neither
edge service was active, neither starts and `crowdsec_migrate_no_edge` appears
on stderr; restore the intended edge through its normal guards. Zero exit does
not prove serving. The switch causes a short edge outage.

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
`startup-retention`, `maintenance-timers`, `unmask`, `enable`, `engine`,
`caddy-register`, `firewall-register`, `firewall-start`, `firewall-ready`,
`edge-start`, `lapi`, `final-gate`, `lease`, or `watch`. The code identifies the
failed step without exposing command output. A shutdown that cannot establish
quiescence reports `crowdsec_migrate_stop_failed`; keep the host closed and
resolve the remaining process or job before recovery.

Correct the cause and rerun before schema initialization. `fresh` requires an
absent database or a verified empty schema. An occupied catalog with incomplete
activation refuses with consumers stopped, even when the catalog gate passes.
Use explicit `--reset` after schema initialization or incomplete startup. Never
remove an application operation fence or bypass startup guards. A complete no-op
leaves stopped services stopped; use reset to repeat startup.

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

Hourly vacuum uses `VACUUM (ANALYZE FALSE)` with its own deadline. Never run
`ANALYZE` or `VACUUM ANALYZE` against CrowdSec. Statistics contamination
requires explicit database reset. Backups cover the whole cluster, including
CrowdSec; live deletion does not erase retained backups or WAL. Follow the
privacy design's backup lifetime and the ordinary backup alarms.
