# CrowdSec HTTP privacy

The Vietnam direct edge supplies CrowdSec with a bounded abuse-detection feed.
The owner approved the feed on 2026-10-10, PostgreSQL storage, backups, and
conditional target samples on 2026-10-18, and the one-step storage switch that
discards SQLite history on 2026-10-21.

## Request feed

The [request feed and detection](crowdsec-request-feed.md) design defines the
seven-field Caddy feed, route and probe classes, target samples, tmpfs files,
and the CrowdSec parser and scenarios.

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
role, the CrowdSec 1.8.2 catalog manifest (tables, columns, types, constraints,
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

The request feed stays in tmpfs under the
[cleanup schedule](crowdsec-request-feed.md#request-feed). Feed and live
database attack records have a 24-hour logical retention and access bound;
backup copies have the separate lifetime below. Attack data never enters release
artifacts, support bundles, or diagnostic copies. Decisions last at most 22
hours, including SSH and manual tests. HTTP bans start at 15 minutes, then one
hour and four hours for repeat offenses under the
[active-decision count](crowdsec-retention-batching.md#escalation), limiting
harm to shared addresses. The status scenario counts 400, 404, 405, and
unknown-route scanner signals; rate detection excludes static assets and health
checks; the HTTP profile precedes and stops catch-all evaluation. The host
targets records older than 23 hours. Effective engine settings are
`db_config.flush.max_age: 0s` and `max_items: null`: pinned CrowdSec 1.8.2 skips
age and count deletion with those values, so its minute flush cannot delete
decisions before bouncer reconciliation; its orphan cleanup still runs, and
orphan decisions are invalid state, not a delivery mechanism. Re-prove these
version-specific behaviors on every upgrade.

Startup requires the data mount, synchronized clock, and
`postgresql@18-main.service`; consumers stay stopped during cleanup and require
a successful gate, not unit ordering alone. PostgreSQL restart or recovery
invalidates the gate and lease; restart needs a fresh pass, not saved success.
The owner accepted batching stopped reconciliation on 2026-11-02. The proposed
[batch schedule and arithmetic](crowdsec-retention-batching.md) use 03:00 ICT
plus forced age cleanup; one nightly batch alone cannot meet the bounds. Every
five-minute pass still deletes stale graphs without decisions and orphan
events/metadata live, trims feeds, validates state, and checks the lease.
Expired decisions remain stream-visible until a stopped batch; the 23-hour
target also uses older parent/event/metadata times. Startup keeps full stopped
cleanup. Missing schema never renews the lease or triggers initialization.

Serialize passes with a host lock. Use fixed SQL identifiers, UTC cutoffs, short
transactions, and at most 1,000 candidates per batch; statement timeout is 10
seconds, lock timeout 2 seconds, and the pass deadline bounds cascades and
loops. Check each alert, decision, event, and metadata timestamp, including
occurrence time; purge stale graphs; reject missing or non-finite times,
dangling links, and excessive decision duration. Verification finds no record
aged 23 hours 30 minutes or more while fresh records and registrations remain.
Never interpolate request text or accept an exit code as cleanup proof.

Natural expiry and supported explicit revocation travel through the stream;
neither proves applied cache deletion. Before physically deleting any decision,
stop both Caddy modes, the firewall bouncer, and the engine; prove quiescence,
clear caches and owned nftables state, recheck exact candidates, delete, verify,
and rebuild previously active consumers. Leave unrelated firewall rules intact.
Keep bounded 20-second reconciliation/rebuild interfaces within the whole-pass
deadline. [Reload alone is insufficient](crowdsec-retention-reload.md). Batching
owns the SQL contention rule and deferred-expiry verification change; it
provides no new cache acknowledgement or concurrent-Caddy stream isolation. SQL
targets the pinned schema, without SQLite, forced vacuum, or WAL waits.

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
hold fixed operational messages only. Caddy writes its `crowdsec` and
`http.handlers.crowdsec` loggers only to the private bouncer file through
`safe_metadata`, which keeps the time, level, and fixed `crowdsec_event` marker;
that covers the
[pinned bouncer's](crowdsec-retention-reload.md#pinned-bouncer-behavior) retry,
warning, and populated lines. Caddy and CrowdSec disable core dumps; the host
uses zram and has no disk swap.

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
restored. The operator keeps every consumer stopped until the explicit
[`--reset`](crowdsec-storage-migration.md) succeeds. Restore also revokes notice
authority: complete the
[notice reconciliation and activation rules](crowdsec-privacy-notification.md#send-authority-and-restore-fence)
before consumer startup. Reset neither clears notice denial nor grants
activation. Never rewind the application cluster for CrowdSec.

## Migration, loss, and release order

[CrowdSec storage migration](crowdsec-storage-migration.md) owns the
SQLite-to-PostgreSQL switch, its approved loss, the no-op and `--reset` paths,
and release order.

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

Prove every feed, detection, gate, retention, log, and offline rule above and in
the [request feed](crowdsec-request-feed.md) with eligible and forbidden
markers, including forced SQL errors and stale graphs. Integration runs
`crowdsec-migrate.sh` via a systemctl adapter; fakes prove order. Required cases
in `crowdsec-migrate-test.sh`: missing registrations, failure at each step,
pre-schema retry, occupied-schema refusal, late no-op with stopped services, and
reset with no edge active. Exercise real switch/reset effects through
`crowdsec-integration-test.sh`: discarded SQLite files and old rows/statistics,
only new registrations, rejection of old machine and both bouncer credentials,
intact application data, lease/timer guards, and passing LAPI/database checks.
Prove direct-call bundle-lock and notice refusal before mutation. Distinguish
these requirements from existing assertions.

Require exact-commit CI and independent adversarial review before deployment.
Production proof reports service state, counts, lease, backups, resource bounds,
and a banned address's 403, never rows or credentials. Observe absent central
API traffic; a TCP probe or missing credentials file alone is insufficient.

## Sources

- [CrowdSec 1.8.2 database configuration](https://github.com/crowdsecurity/crowdsec/blob/v1.8.2/pkg/csconfig/database.go)
- [CrowdSec 1.8.2 schema initialization](https://github.com/crowdsecurity/crowdsec/blob/v1.8.2/pkg/database/database.go)
- [CrowdSec 1.8.2 flush implementation](https://github.com/crowdsecurity/crowdsec/blob/v1.8.2/pkg/database/flush.go)
- [CrowdSec 1.8.2 decision expiry and deletion](https://github.com/crowdsecurity/crowdsec/blob/v1.8.2/pkg/database/decisions.go)
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
