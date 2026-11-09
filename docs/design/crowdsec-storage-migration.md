# CrowdSec storage migration, loss, and release order

This page holds the storage switch of the
[CrowdSec HTTP privacy](crowdsec-http-privacy.md) design. Its
[Sources](crowdsec-http-privacy.md#sources) section lists the cited pages.

`deploy/vn/host/crowdsec-migrate.sh` switches SQLite to PostgreSQL. `install.sh`
runs it as root on every ordinary installation after installing packages,
configuration, units, and helpers, under the installer's bundle lock
`/srv/data/host-operations/.bundle.lock`. Package hooks cannot initialize,
register, or start CrowdSec or the bouncer during installation; a fresh bouncer
package receives a private, unregistered bootstrap key only to suppress its
automatic registration, and the migration replaces that key before the bouncer
starts. The approved loss covers SQLite alerts, decisions, detection windows,
and registrations; CrowdSec documents no backend migration. One approved
incident snapshot stays on restricted encrypted local storage, expires after 30
days, and never reaches support, CI, the repository, or backups. The migration
holds the bundle lock for its whole run, inherited from `install.sh` or acquired
by a direct call such as `--reset`, refuses while any fence operation is owned
or an operation unit runs, and never takes fence ownership itself. Every path
that takes fence operation ownership, including `deploy.sh` and restore
admission, first takes this bundle lock and releases it once ownership is
recorded: bundle lock, then fence lock, never the reverse. No new lock or marker
exists. Install the host bundle before the separate image deploy.

Notice admission, PostgreSQL configuration, passing `preflight-ready`, and a
registered machine and both bouncers make the step a no-op; otherwise:

1. Stop `aboutme-caddy.service`, `aboutme-maintenance.service`,
   `crowdsec-firewall-bouncer.service`, `crowdsec.service`,
   `aboutme-watch.timer`, `aboutme-watch.service`, and the retention and vacuum
   timers. Join their services and the startup retention oneshot before any
   migration effect; an admitted watchdog tick would run
   `crowdsec-retention-fail.sh` and tear down the lease and runtime directories.
   Record active edge services before stopping; accept the short edge outage.
2. `crowdsec-provision.sh fresh`: role, protected logging settings, database,
   verified empty schema, pinned catalog, statistics policy, full gate.
3. Activate the PostgreSQL configuration and delete the SQLite files.
4. Gate `cscli machines add -a --force --error`; write the machine credential
   file `/etc/crowdsec/local_api_credentials.yaml` as root-only mode 0600.
5. `crowdsec-retention.sh startup`: gate, clear caches and owned nftables state,
   initialize firewall tables, remove feeds, and write the database-bound lease.
6. Enable and start both maintenance timers, as the lease check requires; only
   then re-enable `aboutme-watch.timer`, after startup retention and the timers
   establish ordinary admission. Enable both CrowdSec services and start the
   engine through its normal startup guards.
7. Register `aboutme-caddy` through `secrets-host.sh bouncer-register` (key in
   the `crowdsec-bouncer-key` host and Podman secrets) and `aboutme-firewall`
   (key in `/srv/data/crowdsec/firewall-bouncer.key` and the bouncer
   configuration); keys travel through protected files only.
8. Start the firewall bouncer, verify its owned nftables state was rebuilt, then
   start only the edge services recorded in step 1.
9. Verify `cscli lapi status` and `crowdsec-db.sh preflight-ready`.

Failure stops and joins consumers/timers, prints `crowdsec_migrate_refused`, and
exits non-zero. `fresh` accepts an absent database or verified empty schema,
never an existing catalog, even without attack rows. Rerun before schema
initialization; afterwards a failed no-op check needs explicit `--reset`. A
passing check leaves stopped services stopped. Use reset for incomplete startup.
The active-edge record dies with the invocation; the operator restores the
intended edge through normal guards. Zero exit does not prove serving.

`--reset` bypasses the no-op, uses the same lock and stop set, and substitutes
`crowdsec-provision.sh restore-reset` for step 2: drop and recreate only the
CrowdSec database without `FORCE`, so an unaccounted connection blocks
replacement. Continue from step 3, replacing the machine file, both bouncer
keys, firewall configuration key, and Caddy Podman secret before their consumers
start. Old registrations must fail authentication. Only explicit reset discards
PostgreSQL history, bans, statistics, and registrations, preserving application
data and the notice ledger. Invalid role grants still require repair.

Require the [notice activation gate](crowdsec-privacy-notification.md) before
mutation or a no-op on every install. Release the seven-field encoder, parser,
and host bundle together; stop writers and clear feeds before the parser rejects
five-field lines. Image rollback retains the PostgreSQL bundle and compatible
notice; SQLite never resumes. No client API or resume schema changes.
