# CrowdSec HTTP privacy

The Vietnam direct edge supplies CrowdSec with a bounded abuse-detection feed.
The owner approved the feed on 2026-10-10, PostgreSQL storage, backups, and
conditional target samples on 2026-10-18, and the one-step storage switch that
discards SQLite history on 2026-10-21.

## Request feed

Caddy writes JSON lines to tmpfs with exactly these fields:

| Field         | Type   | Value                                                                  |
| ------------- | ------ | ---------------------------------------------------------------------- |
| `ts`          | string | UTC RFC 3339 timestamp with nanoseconds                                |
| `source_ip`   | string | Canonical unmapped socket IPv4 or IPv6 address, without port           |
| `method`      | string | `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`, or `OTHER` |
| `status`      | number | Integer from 100 through 599                                           |
| `route_class` | string | One of the route classes below                                         |
| `probe_class` | string | One of the fixed classes below                                         |
| `target_path` | string | Eligible escaped path, at most 512 bytes, or the empty string          |

All seven keys are mandatory, with no extra or duplicate keys. The encoder
constructs this allowlist and never copies an access event; it excludes queries,
headers, bodies, host, port, TLS details, duration, and user or resume fields.
Only the direct listener's socket supplies `source_ip`, with IPv4-in-IPv6
unmapping; forwarding headers cannot. Unknown methods become `OTHER`; invalid
base fields drop the record; errors emit fixed reason codes only.

Route classification uses the first matching row:

| Class         | Routes                                                                                        |
| ------------- | --------------------------------------------------------------------------------------------- |
| `maintenance` | Every request handled by maintenance Caddy                                                    |
| `system`      | Health, readiness, well-known, robots, sitemap, `llms.txt`, print, and internal-render routes |
| `static`      | `/_nuxt` and its children                                                                     |
| `auth`        | App, authorize, OAuth, login, registration, password reset, and verification routes           |
| `api`         | `/api` and its children                                                                       |
| `mcp`         | `/mcp` and its children                                                                       |
| `public`      | Root, valid public resume routes, guide, privacy, terms, showcase, and templates routes       |
| `unknown`     | Every other path, including reserved names and probes                                         |

Only `route_class=unknown` with status 400, 404, or 405 permits a target sample;
every other request emits `probe_class: "none"` and `target_path: ""`, including
known routes returning those statuses. Valid public resume route syntax stays
`public` even when the resume does not exist. Match protected route classes
ASCII case-insensitively against the original parsed path and every exclusion
view (the escaped path's first two decoded views and their dot-segment and slash
normalizations), matching pinned Caddy path behavior. If any view matches, emit
that class by table priority and no target sample.

Capture the original parsed URL before rewrites. For an eligible valid URL,
`target_path` equals Go `URL.EscapedPath()`: preserve valid original escapes and
their case, encoded slashes, double encoding, and UTF-8 encoding; do not clean,
decode, truncate, or normalize it. Never use `URL.String()` or `RequestURI()`,
which can include forbidden fields; `RawQuery`, host, userinfo, and fragment
never enter this field. A literal `%3F` remains path data; a query delimiter and
everything after it are excluded.

Validate before capture: a nonempty absolute path starting with `/`, consistent
`RawPath` when present, valid percent triplets, and valid UTF-8 without control
characters in the decoded views. Authority-only, opaque, or asterisk targets,
malformed escapes, inconsistent URL fields, and controls yield
`probe_class: "invalid"`, `target_path: ""`; a valid escaped path longer than
512 bytes before JSON encoding yields `oversized` and the empty string. Never
repair an invalid raw path through `EscapedPath()` fallback. If parsing fails
before the encoder runs, emit no record or raw diagnostic. Validation and
classification use bounded transient buffers; no decoded view is persisted.

For eligible paths passing those checks, use the first matching class below.
Match ASCII case-insensitively on the original and at most two decoded views;
normalize backslashes to slashes for matching only. A decode error is invalid.

| Class           | Fixed matching rule                                                                                                                                                           |
| --------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `traversal`     | A slash-delimited `..` component                                                                                                                                              |
| `secret-config` | Component `.env` or `.env.*`; basename `config.json`, `config.yml`, `config.yaml`, `config.php`, `wp-config.php`, `id_rsa`, `id_ed25519`; path `/etc/passwd` or `/etc/shadow` |
| `repository`    | Component `.git`, `.svn`, or `.hg`                                                                                                                                            |
| `web-shell`     | Basename `shell.php`, `cmd.php`, `c99.php`, `r57.php`, `wso.php`, `webshell.php`, `shell.jsp`, or `shell.aspx`                                                                |
| `framework`     | First component `wp-admin`, `wp-content`, `wp-includes`, `phpmyadmin`, `phpunit`, `vendor`, `actuator`, or `cgi-bin`; basename `wp-login.php` or `xmlrpc.php`                 |
| `other`         | Any other eligible valid path                                                                                                                                                 |

The complete allowlist adds only `none`, `invalid`, and `oversized`. Classes
contain no attacker text and do not themselves trigger bans. Target samples can
contain visitor-supplied text; this trade-off does not anonymize paths. Serving
and maintenance write `serving.json` and `maintenance.json` under
`/var/log/caddy/crowdsec`, mounted from no-swap tmpfs `/run/aboutme/caddy-log`.
Each private file caps at 1 MiB plus one uncompressed size roll; timed rotation
is off. Precreate exact paths and tail without globs. At startup and every five
minutes, `crowdsec-retention.sh` truncates request and bouncer feeds and removes
rolls; prove parser continuity after truncation. Reboot removes the feeds. Caddy
excludes both access logger names, removes URI data from runtime errors, and
emits no feed from the CloudFront listener, even in a shared image.

## Detection

CrowdSec acquisition labels both files `type: aboutme-http`. The parser rejects
wrong keys, types, classes, bounds, escapes, or combinations: `none` requires an
ineligible request and empty path; `invalid` and `oversized` require eligibility
and an empty path; other classes require an eligible valid path and matching
fixed classification. Never reconstruct a missing path. Scenarios group by
source address and coarse fields, never by target text; HTTP and SSH windows
last at most 30 minutes, and maintenance 503 is neutral.

No GeoIP, autonomous-system, reverse-DNS, or intelligence enrichment exists.
CrowdSec persists alert samples, not the request feed or an access log; samples
hold the seven fields plus scenario, counts, timestamps, and decision expiry;
unsampled requests disappear with feed cleanup. HTTP scenario names start with
`aboutme/http-`. The bouncer enforces current IP decisions with 403, which never
feeds status-based ban escalation. Community API, console, intelligence clients,
and online credentials are absent, so no attack data reaches threat-intelligence
recipients. The local API listens only on `127.0.0.1:8095`. Pin
`cscli.hub_branch: master` to reuse cached detection without release lookup.

## Database and authorization

Use a separate `crowdsec` database and role in the existing PostgreSQL 18
cluster; CrowdSec owns its schema apart from application migrations. Sharing
application tables would mix authorities; sharing the cluster still shares
resources, WAL, backups, and failures; a second cluster would add a service.

Effective `db_config`: `type: pgx`, `db_name: crowdsec`, `user: crowdsec`,
`db_path: /run/postgresql` (socket directory), empty `host`, `port: 0`,
`sslmode: disable`, and `max_open_conns: 4`, with role connection limit 8. No
password, TCP, SQLite path, or `use_wal` setting may be inherited. Retention CLI
and SQL steps run sequentially within that budget.

HBA permits `crowdsec` only into its database through a named peer map from OS
`root`, the existing engine identity; no wildcard, application identity, or
`trust` rule. Bootstrap uses `postgres`; runtime and retention use `crowdsec`.
Revoke public database and schema access. The role owns its database and
objects, has no memberships, and no superuser, database or role creation,
replication, or bypass-RLS privilege; refuse conflicting ownership or grants.
Generate machine and bouncer credentials into protected encrypted-volume files,
never arguments or logs. Revocation removes the registration and replaces the
consumer's credential and cache; prove old credentials fail before restart. Keep
the [host budgets](vietnam-host-memory.md) and monitor volume, connections, and
backup age; alert count is not a hard size cap.

Before every schema-capable `cscli` or CrowdSec action, including flushes,
starts, config checks, and registration, run side-effect-free `psql -X` with
`ON_ERROR_STOP=1` in a read-only transaction. Check database identity, current
role, the CrowdSec 1.8.1 catalog manifest (tables, columns, types, constraints,
and indexes), ownership, grants, logging, and statistics policy; the reviewed
manifest and pinned binary identify the version without a vendor schema-version
table. Failure prevents the binary action. Repeat preflight before SQL cleanup;
a `cscli` exit does not prove missing schema was safe. Only
`crowdsec-provision.sh` may accept a verified empty database, initialize it,
apply policy, and pass the full gate. Schema changes require stopped ingestion,
reviewed policy, and fresh proofs.

## Planner statistics and vacuum

During fresh provisioning, before any attack ingestion, set
`autovacuum_enabled=false` on every CrowdSec table and `STATISTICS 0` on every
user column, including IPs, decision values, serialized events and target paths,
metadata, and credential fields; reject unexpected relations, expression
indexes, and extended-statistics objects. The gate checks table reloptions and
column targets and requires no CrowdSec rows in `pg_stats`, in aggregate.

PostgreSQL's table switch disables automatic VACUUM and ANALYZE together; large
analyze thresholds are not an off switch. Run `VACUUM (ANALYZE FALSE)` in a
separate hourly job with a five-minute deadline, no overlapping runs, and
fixed-name, fixed-reason alarms; wraparound protection stays enabled. Never run
manual ANALYZE or `VACUUM ANALYZE` for CrowdSec. Zero targets prevent collection
but do not remove old statistics; even analyzing an empty table can retain them,
so contamination fails the gate and requires `crowdsec-migrate.sh --reset`, not
catalog edits. Lower planner accuracy and explicit vacuum are the trade-offs.

## Live retention and failure

The request feed stays in tmpfs under the cleanup schedule above. Feed and live
database attack records have a 24-hour logical retention and access bound;
backup copies have the separate lifetime below. Attack data never enters release
artifacts, support bundles, or diagnostic copies. Decisions last at most 22
hours, including SSH and manual tests. HTTP bans start at 15 minutes, then one
hour and four hours for repeat offenses while prior decisions remain, limiting
harm to shared addresses. The status scenario counts 400, 404, 405, and
unknown-route scanner signals; rate detection excludes static assets and health
checks; the HTTP profile precedes and stops catch-all evaluation. The host
targets records older than 23 hours. Effective engine settings are
`db_config.flush.max_age: 0s` and `max_items: null`: pinned CrowdSec 1.8.1 skips
age and count deletion with those values, so its minute flush cannot delete
decisions before bouncer reconciliation; its orphan cleanup still runs, and
orphan decisions are invalid state, not a delivery mechanism. Re-prove these
version-specific behaviors on every upgrade.

Startup requires the data mount, synchronized clock, and
`postgresql@18-main.service`; consumers stay stopped during cleanup and require
a successful gate, not unit ordering alone. PostgreSQL restart or recovery
invalidates the gate and lease; restart needs a fresh pass, not saved success.
After preflight, runtime selects an exact bounded candidate set and reconciles
its decisions with every active bouncer before SQL physically removes stale
graphs, expired decisions, and orphan events or metadata. Only stopped-consumer
cleanup may use `cscli alerts flush --max-age 23h --max-items 5000`, after its
preflight; startup uses SQL without the engine or local API. Missing schema
never renews the lease or triggers initialization.

Serialize passes with a host lock. Use fixed SQL identifiers, UTC cutoffs, short
transactions, and at most 1,000 candidates per batch; statement timeout is 10
seconds, lock timeout 2 seconds, and the pass deadline bounds cascades and
loops. Check each alert, decision, event, and metadata timestamp, including
occurrence time; purge stale graphs; reject missing or non-finite times,
dangling links, and excessive decision duration. Verification finds no record
aged 23 hours 30 minutes or more while fresh records and registrations remain.
Never interpolate request text or accept an exit code as cleanup proof.

Natural expiries and supported explicit revocations first travel through the
LAPI/bouncer stream. Allow at most 20 seconds within the runtime deadline for
consumer acknowledgement that each candidate left all active Caddy caches and
the SSH nftables set; prove that mechanism against the pinned consumers, since a
stream response or `last_pull` alone does not establish applied deletion.
Recheck the same candidate IDs and eligibility before deletion; never expand the
set or run a blanket flush while consumers serve. No unreconciled decision means
no routine Caddy restart. Missed polls, unavailable acknowledgement, early
revocation, or invalid active decisions still unreconciled at the deadline close
the gate: stop both Caddy modes, the firewall bouncer, and the engine; prove
quiescence, clear in-memory caches and the bouncer-owned nftables state, then
physically delete; verify and rebuild from current decisions before reopening,
leaving unrelated firewall rules intact. This fallback is mandatory if the
pinned consumers expose no sufficient acknowledgement. SQL targets the pinned
schema, not a vendor API. Retention never waits for SQLite, checkpoints, forced
vacuum, or WAL archival.

Bounds: runtime interval 300 seconds, runtime whole-pass deadline 60 seconds,
startup whole-pass deadline 300 seconds, lease maximum age 360 seconds, watchdog
interval 300 seconds, and consumer stop deadline 60 seconds. Only full success
renews the root-owned lease; the independent watchdog rejects missing, stale,
future-dated, or malformed leases and disabled cleanup timers. The lease,
watchdog, and stop bounds total 12 minutes, inside the 30-minute verification
margin. Any clock, authentication, schema, or cleanup failure blocks startup or
stops all consumers: confirm quiescence, remove tmpfs feeds, alarm with fixed
codes, and keep PostgreSQL available to the app. Failed cleanup keeps the gate
closed until verified recovery; stopping is not deletion.

## Log privacy

CrowdSec, firewall-bouncer, and Caddy bouncer logs that can contain an HTTP
source address or event stay on tmpfs under the feed's retention bound; journals
hold fixed operational messages only. Caddy and CrowdSec disable core dumps; the
host uses zram and has no disk swap.

PostgreSQL errors, parameters, rows, and context must not leak attack values to
durable logs; `log_statement = none` alone is insufficient. Before any CrowdSec
connection set role-specific `log_min_messages = panic`,
`log_min_error_statement = panic`, `log_error_verbosity = terse`,
`log_statement = none`, `log_min_duration_statement = -1`,
`log_min_duration_sample = -1`, `log_statement_sample_rate = 0`,
`log_transaction_sample_rate = 0`, `log_parameter_max_length = 0`, and
`log_parameter_max_length_on_error = 0`. Do not grant the role permission to
weaken them. Keep debug SQL logging disabled in CrowdSec. Client diagnostics go
only to the bounded private log path or are discarded; journal and alarm output
contain fixed reason codes and aggregate counts. Forced-error proofs must cover
the database server as well as the client.

## Backups and restore

pgBackRest copies the whole cluster, including CrowdSec records, credentials,
and WAL, to encrypted private vStorage in Vietnam; selective restore does not
exclude data from backups. Keys stay in protected pgBackRest configuration; the
CrowdSec role receives none, and a decrypted backup exposes stored attack data.
The 30-day recovery window and weekly fulls normally retain copies for about
five weeks after live deletion; backup and expiration failures require alarms,
and this is not a strict erasure deadline. Backups serve recovery only; live
deletion promises no forensic erasure of pages, WAL, or backups, and encryption
protects only an unpowered host. A cluster or point-in-time restore brings back
CrowdSec rows, planner statistics, and registrations; row cleanup cannot remove
old statistics or revoked identities. Neither automated restore nor retention
resets this state, and a passing gate does not prove the database was never
restored. The operator keeps every consumer stopped until the explicit `--reset`
below succeeds. Restore also revokes notice authority: complete the
[notice reconciliation and activation rules](crowdsec-privacy-notification.md#send-authority-and-restore-fence)
before consumer startup. Reset neither clears notice denial nor grants
activation. Never rewind the application cluster for CrowdSec.

## Migration, loss, and release order

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

## Security boundaries

Root owns migration, peer authentication, and credentials; no new principal
exists. Administrative SQL uses `runuser` as `postgres` with an empty
environment. After a kill or reboot, normal startup guards apply, but no
completion guard exists. Restore safety relies on the operator stopping
consumers until reset succeeds. Visitors, signed-in users, hostile resume
content, and stolen session/MCP tokens have no host-command surface. Eligible
path text stays bounded untrusted data, never SQL or command input. TLS protects
traffic; forwarding headers cannot select `source_ip`. A compromised container
lacks the host UID 0 plus socket access that peer authentication needs.
Compromised Caddy can read decisions and ignore bans; its key cannot write
alerts. A CrowdSec database writer can corrupt protection but has no application
grants. An app database writer can falsify notice outcomes. Host root and
database superusers stay trusted; loopback LAPI cannot contain them.

## Public notice and release

`apps/web/app/i18n/legal-en.ts` and `legal-vi.ts` carry this exact copy in place
of the blanket path exclusion and the retention sentences:

> Attack protection uses your IP address, request time, method, response status,
> broad route class, and a fixed probe category. Only for unrecognized routes
> returning 400, 404, or 405, attack alert samples may include the exact escaped
> target path, without the query, up to 512 bytes. We do not record target paths
> for known routes, including account, API, and valid public resume routes.
> Invalid or oversized targets use a fixed category and no path. Queries,
> headers, and bodies are never included. The request feed stays in RAM;
> CrowdSec stores attack alert samples and bans, not a full access log. Live
> records are kept for at most 24 hours. Encrypted database backups in Vietnam
> may retain earlier records for about five weeks after deletion, under the
> backup policy below. Backups are used only for disaster recovery. Restored
> CrowdSec data is discarded before attack protection resumes. None of this data
> is shared with the CrowdSec community.
>
> Hệ thống chống tấn công dùng địa chỉ IP, thời điểm yêu cầu, phương thức, mã
> phản hồi, nhóm tuyến truy cập và nhóm dấu hiệu thăm dò cố định. Chỉ với tuyến
> không được nhận diện trả về mã 400, 404 hoặc 405, mẫu cảnh báo tấn công mới có
> thể chứa nguyên đường dẫn đích đã mã hóa ký tự, bỏ phần truy vấn, tối đa 512
> byte. Chúng tôi không ghi đường dẫn đích của các tuyến đã biết, gồm tuyến tài
> khoản, API và CV công khai hợp lệ. Đích không hợp lệ hoặc quá dài chỉ được gán
> nhóm cố định, không ghi đường dẫn. Tham số truy vấn, tiêu đề và nội dung yêu
> cầu không được ghi. Dữ liệu yêu cầu nằm trong RAM; CrowdSec lưu mẫu cảnh báo
> tấn công và lệnh chặn, không lưu nhật ký đầy đủ mọi lượt truy cập. Bản ghi
> đang hoạt động được giữ tối đa 24 giờ. Bản sao lưu cơ sở dữ liệu được mã hóa
> tại Việt Nam có thể giữ bản ghi cũ trong khoảng năm tuần sau khi xóa, theo
> chính sách lưu bản sao bên dưới. Bản sao lưu chỉ dùng để khôi phục sau sự cố.
> Dữ liệu CrowdSec được khôi phục sẽ bị loại bỏ trước khi hệ thống chống tấn
> công hoạt động trở lại. Không dữ liệu nào được chia sẻ với cộng đồng CrowdSec.

## Required proof

Paths without a directory are under `deploy/vn/tests/`; jobs are in
`vn-infra.yml` unless `ci.yml` is named. A listing is not evidence of a pass.

| Test files                                                                                                                                                                           | CI job                                                                    | Scope                                                                                                                                                                                                                         |
| ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `deploy/caddy/production/build/crowdsec_feed_test.go`                                                                                                                                | `ci.yml` / `server-integration` through `deploy/caddy/production/test.sh` | Encoder keys, route exclusions, socket address, escapes, and bounds                                                                                                                                                           |
| `deploy/caddy/production/test/crowdsec_feed.sh`, `deploy/caddy/production/test/direct_maintenance.sh` through `deploy/caddy/production/test.sh`                                      | `ci.yml` / `server-integration`                                           | Serving and maintenance feed/log privacy with production Caddy                                                                                                                                                                |
| `crowdsec-config-test.sh`, `crowdsec-retention-test.sh`, `crowdsec-lease-test.sh`, `crowdsec-vacuum-test.sh`, `crowdsec-window-test.sh`, `crowdsec-offline-test.sh`, `watch-test.sh` | `check`                                                                   | Configuration, cleanup, lease, vacuum, window, offline, and watchdog contracts                                                                                                                                                |
| `crowdsec-db-test.sh derive` and `integration`, `crowdsec-retention-test.sh postgresql`, `crowdsec-offline-test.sh packages`                                                         | `crowdsec-postgresql`                                                     | Pinned catalog, peer/grant isolation, logging, statistics, bounded SQL, reset isolation, package suppression                                                                                                                  |
| `crowdsec-migrate-test.sh`                                                                                                                                                           | `crowdsec-postgresql`                                                     | Fake-command no-op, switch/reset ordering, provisioning-failure stop                                                                                                                                                          |
| `crowdsec-integration-test.sh`, `crowdsec-config-test.sh`, `crowdsec-offline-test.sh packages`                                                                                       | `crowdsec-integration`                                                    | Real parser, bans, retention, consumer rebuild, database loss; migration entry point via systemctl adapter: switch, no-op, reset, old machine credentials and both old bouncer keys refused, failure requiring explicit reset |
| `crowdsec-privacy-notice-test.sh`                                                                                                                                                    | `crowdsec-privacy-notice`                                                 | Notice authority and restore denial                                                                                                                                                                                           |

Prove every feed, detection, gate, retention, log, and offline rule above with
eligible and forbidden markers, including forced SQL errors and stale graphs.
Integration runs `crowdsec-migrate.sh` via a systemctl adapter; fakes prove
order. Required cases in `crowdsec-migrate-test.sh`: missing registrations,
failure at each step, pre-schema retry, occupied-schema refusal, late no-op with
stopped services, and reset with no edge active. Exercise real switch/reset
effects through `crowdsec-integration-test.sh`: discarded SQLite files and old
rows/statistics, only new registrations, rejection of old machine and both
bouncer credentials, intact application data, lease/timer guards, and passing
LAPI/database checks. Prove direct-call bundle-lock and notice refusal before
mutation. Distinguish these requirements from existing assertions.

Require exact-commit CI and independent adversarial review before deployment.
Production proof reports service state, counts, lease, backups, resource bounds,
and a banned address's 403, never rows or credentials. Observe absent central
API traffic; a TCP probe or missing credentials file alone is insufficient.

## Sources

- [CrowdSec 1.8.1 database configuration](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/csconfig/database.go)
- [CrowdSec 1.8.1 schema initialization](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/database.go)
- [CrowdSec 1.8.1 flush implementation](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/flush.go)
- [CrowdSec 1.8.1 decision expiry and deletion](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/decisions.go)
- [Caddy access logging and file rotation](https://caddyserver.com/docs/caddyfile/directives/log)
- [CrowdSec database backends and backend switch](https://docs.crowdsec.net/docs/local_api/database)
- [CrowdSec `db_config` reference](https://docs.crowdsec.net/docs/configuration/crowdsec_configuration)
- [CrowdSec alert flush command](https://docs.crowdsec.net/docs/cscli/cscli_alerts_flush/)
- [CrowdSec machine registration](https://docs.crowdsec.net/docs/cscli/cscli_machines_add/)
- [CrowdSec bouncer registration](https://docs.crowdsec.net/docs/cscli/cscli_bouncers_add/)
- [CrowdSec local API status check](https://docs.crowdsec.net/docs/cscli/cscli_lapi_status/)
- [PostgreSQL 18 peer authentication](https://www.postgresql.org/docs/18/auth-peer.html)
- [PostgreSQL 18 user maps](https://www.postgresql.org/docs/18/auth-username-maps.html)
- [PostgreSQL 18 logging controls](https://www.postgresql.org/docs/18/runtime-config-logging.html)
- [PostgreSQL 18 table maintenance settings](https://www.postgresql.org/docs/18/sql-createtable.html#SQL-CREATETABLE-STORAGE-PARAMETERS)
- [PostgreSQL 18 statistics collection and persistence](https://www.postgresql.org/docs/18/sql-analyze.html)
- [PostgreSQL 18 VACUUM](https://www.postgresql.org/docs/18/sql-vacuum.html)
- [Go escaped URL paths](https://pkg.go.dev/net/url#URL.EscapedPath)
- [pgBackRest backup, retention, and restore](https://pgbackrest.org/user-guide.html)
