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
operation lock. PostgreSQL configuration plus a passing database gate makes the
helper return without stopping services.

For the first switch, the helper records the active edge service and stops both
edge modes, CrowdSec, its firewall bouncer, and database maintenance scheduling.
It provisions the separate database, applies the pinned catalog policy, selects
PostgreSQL, and deletes only the three named SQLite database files. Old alerts
and decisions are discarded. Decisions regenerate from live traffic.

The helper registers the local machine before starting CrowdSec. Startup
retention clears owned cache and firewall state and writes a lease for the new
database. Both maintenance timers start before CrowdSec. The helper registers
`aboutme-caddy` through the host secrets helper and `aboutme-firewall` through
its private key file, then starts the firewall bouncer and the recorded edge
service. The switch causes a short edge outage.

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

A failed helper stops consumers and both timers, prints
`crowdsec_migrate_refused`, and fails the deploy. Inspect fixed service results
and the database gate. Correct the cause and rerun the ordinary deploy. Do not
remove the application operation fence or bypass the gate to start services. Use
explicit reset when the database cannot pass its pinned policy. A failure after
PostgreSQL activation can leave a passing database gate with stopped consumers.
The no-op condition still applies on a rerun; use `--reset` to repeat
registration and startup in that state.

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
