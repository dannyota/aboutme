# CrowdSec HTTP privacy

The Vietnam direct edge supplies CrowdSec with a bounded abuse-detection feed.
The owner approved the initial feed on 2026-10-10 and PostgreSQL storage,
backups, and conditional target samples on 2026-10-18. Approval is not
deployment evidence; the release and hosted proofs below must pass.

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
constructs this allowlist, never copies an access event. It excludes queries,
headers, bodies, host, port, TLS details, duration, and user or resume fields.
Only the direct listener's socket supplies `source_ip`, with IPv4-in-IPv6
unmapping; forwarding headers cannot supply it. Unknown methods become `OTHER`.
Invalid base fields drop the record. Errors emit fixed reason codes only.

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

Only `route_class=unknown` with status 400, 404, or 405 permits a target sample.
Every other request emits `probe_class: "none"` and `target_path: ""`, including
known routes returning those statuses. Valid public resume route syntax stays
`public` even when the resume does not exist. Match protected route classes
ASCII case-insensitively against the original parsed path and every exclusion
view: the escaped path's first two decoded views and their dot-segment/slash
normalizations. This matches pinned Caddy path behavior. If any view matches,
emit that class by the table's priority and no target sample.

Capture the original parsed URL before rewrites. For an eligible valid URL,
`target_path` equals Go `URL.EscapedPath()`: preserve valid original escapes and
their case, encoded slashes, double encoding, and UTF-8 encoding. Do not clean,
decode, truncate, or normalize the stored path. Never use `URL.String()` or
`RequestURI()` because they can include forbidden fields. `RawQuery`, host,
userinfo, and fragment never enter this field. A literal encoded `%3F` remains
path data; an actual query delimiter and everything after it are excluded.

Validate before capture: a nonempty absolute path starting with `/`, consistent
`RawPath` when present, valid percent triplets, and valid UTF-8 without control
characters in the decoded views. Authority-only, opaque, or asterisk targets,
malformed escapes, inconsistent URL fields, and controls yield
`probe_class: "invalid"`, `target_path: ""`. A valid escaped path longer than
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
contain visitor-supplied text; this trade-off does not anonymize paths.

Serving and maintenance write `serving.json` and `maintenance.json` under
`/var/log/caddy/crowdsec`, mounted from no-swap tmpfs `/run/aboutme/caddy-log`.
Each private file caps at 1 MiB plus one uncompressed size roll; timed rotation
is off. Precreate exact paths and tail without globs. At startup and every five
minutes, `crowdsec-retention.sh` truncates request/bouncer feeds and removes
rolls; prove parser continuity after truncation. Reboot removes the feeds. Caddy
excludes both access logger names and removes URI data from runtime errors. The
CloudFront listener emits no feed, even in a shared image.

## Detection

CrowdSec acquisition labels both files `type: aboutme-http`. The parser rejects
wrong keys, types, classes, bounds, escapes, or combinations. `none` requires an
ineligible request and empty path. `invalid`/`oversized` require eligibility and
an empty path. Other classes require an eligible valid path and matching fixed
classification. Never reconstruct a missing path. Scenarios group by source
address and coarse fields, never by target text. HTTP and SSH windows last at
most 30 minutes; maintenance 503 is neutral.

No GeoIP, autonomous system, reverse DNS, or intelligence enrichment is added.
CrowdSec persists alert samples, not the full request feed or an access log.
Samples may contain the seven fields plus scenario, counts, timestamps, and
decision expiry. Unsampled requests disappear with feed cleanup. HTTP scenario
names start with `aboutme/http-`. The bouncer uses current IP decisions and
returns 403; that response never contributes to status-based ban escalation.

Community API, console, intelligence clients, and online credentials are absent.
No attack data is shared with threat-intelligence recipients. The local API
listens only on `127.0.0.1:8095`; backups follow the separate contract below.

## Database and authorization

Use a separate `crowdsec` database and role in the existing PostgreSQL 18
cluster. CrowdSec owns its schema, separately from application migrations.
Sharing application tables would mix these authorities; sharing the cluster
still shares resources, WAL, backups, and failures. A second cluster would add
isolation but another service on the bounded host.

Effective `db_config`: `type: pgx`, `db_name: crowdsec`, `user: crowdsec`,
`db_path: /run/postgresql` (socket directory), empty `host`, `port: 0`, and
`sslmode: disable`. No password, TCP, SQLite path, or `use_wal` setting may be
inherited. Set `max_open_conns: 4` and role connection limit 8. Retention CLI
and SQL steps run sequentially within that budget.

HBA permits `crowdsec` only into its database through a named peer map from OS
`root`, the existing engine identity. No wildcard, application identity, or
`trust` rule is allowed. Bootstrap uses `postgres`; runtime and retention use
`crowdsec`. Revoke public database/schema access. The role owns its database and
objects, has no memberships, and has no superuser, database/role-creation,
replication, or bypass-RLS privilege. Refuse conflicting ownership or grants.

Generate machine and bouncer credentials into protected encrypted-volume files,
never arguments or logs. Revocation removes the registration and replaces the
consumer's credential and cache; prove old credentials fail before restart. Host
root remains trusted. Keep the [host budgets](vietnam-host-memory.md) and
monitor volume, connections, and backup age; alert count is not a hard size cap.

Before every schema-capable `cscli` or CrowdSec action, including flushes,
starts, config checks, and registration, run side-effect-free `psql -X` with
`ON_ERROR_STOP=1` in a read-only transaction. Check database identity, current
role, and the expected CrowdSec 1.8.1 catalog manifest (tables, columns, types,
constraints, and indexes), ownership, grants, logging, and statistics policy. Do
not invent a vendor schema-version table: the reviewed manifest and pinned
binary identify the expected version. Failure prevents the binary action. Repeat
preflight before SQL cleanup; an exit from `cscli` is not evidence that missing
schema was safe. Only the fenced provisioner may accept a verified empty
database, initialize it, apply policy, and pass the full gate. All schema
changes require stopped ingestion, reviewed policy, and fresh proofs.

## Planner statistics and vacuum

During fenced fresh provisioning, before any attack ingestion, set
`autovacuum_enabled=false` on every CrowdSec table and `STATISTICS 0` on every
user column. This includes IPs, decision values, serialized events and target
paths, metadata, and credential fields. Reject unexpected relations, expression
indexes, and extended-statistics objects. The gate checks table reloptions and
column targets, and requires aggregate absence of CrowdSec rows in `pg_stats`
without selecting sampled values. It also rejects extended-statistics state.

PostgreSQL's table switch disables ordinary automatic VACUUM and ANALYZE
together; large analyze thresholds are not an off switch. Retain ordinary
`VACUUM (ANALYZE FALSE)` in a separate hourly maintenance job with a five-minute
deadline, no overlapping vacuum runs, and fixed-name, fixed-reason alarms.
Wraparound protection remains enabled. Never run manual ANALYZE or
`VACUUM ANALYZE` for CrowdSec. Zero targets prevent per-column collection but do
not prove removal of old statistics; even analyzing an empty table can retain
them. Any statistics contamination fails the gate and requires fenced database
replacement, not catalog edits. Restore always uses the fresh-state boundary
below. Lower planner accuracy and explicit vacuum maintenance are trade-offs.

## Live retention and failure

The request feed stays in tmpfs and follows the cleanup schedule above. Feed and
live database attack records have a 24-hour logical retention and access bound.
Backup copies have the separate lifetime below. Attack data never enters release
artifacts, support bundles, or routine diagnostic copies.

Decisions last at most 22 hours, including SSH and manual tests. HTTP bans start
at 15 minutes, then one hour and four hours for repeat offenses while prior
decisions remain. This limits harm to shared addresses. The status scenario
counts 400, 404, 405, and unknown-route scanner signals; rate detection excludes
static assets and health checks. The HTTP profile precedes and stops catch-all
evaluation. The host targets records older than 23 hours. Effective engine
settings are `db_config.flush.max_age: 0s` and `max_items: null`: pinned
CrowdSec 1.8.1 skips age/count deletion with those values, preventing its minute
flush from deleting decisions before bouncer reconciliation. Its orphan cleanup
still runs; orphan decisions are invalid state, not a supported delivery
mechanism. Re-prove these version-specific behaviors on every upgrade.

Startup requires the data mount, synchronized clock, and
`postgresql@18-main.service`; consumers stay stopped during cleanup. CrowdSec,
the firewall bouncer, and both Caddy modes require a successful gate, not just
unit ordering. PostgreSQL restart or recovery invalidates the gate and lease;
consumer restart requires a fresh pass, not saved oneshot success.

After preflight, runtime selects an exact bounded candidate set and reconciles
its decisions with every active bouncer before SQL physically removes stale
graphs, expired decisions, and orphan events or metadata. Only stopped-consumer
cleanup may use `cscli alerts flush --max-age 23h --max-items 5000`, after its
preflight. Startup uses SQL without the engine or local API. Missing schema
never renews the lease or silently triggers initialization.

Serialize passes with a host lock. Use fixed SQL identifiers, UTC cutoffs, short
transactions, and at most 1,000 candidates per batch. Statement timeout is 10
seconds, lock timeout 2 seconds; the pass deadline bounds cascades and loops.
Check each alert, decision, event, and metadata timestamp, including occurrence
time. Purge stale graphs; reject missing or non-finite times, dangling links,
and excessive decision duration. Verification finds no record aged 23 hours 30
minutes or more. Preserve fresh records and registrations. Never interpolate
request text or accept an exit code as cleanup proof.

Natural expiries and supported explicit revocations first travel through the
LAPI/bouncer stream. Allow at most 20 seconds within the runtime deadline to
obtain consumer acknowledgement that each candidate was removed or expired from
all active Caddy caches and the SSH nftables set. Prove the acknowledgement
mechanism against the pinned consumers; a stream response or `last_pull` alone
does not establish that consumers applied deletion. Recheck the same candidate
IDs and eligibility before deletion; do not expand the set or run a blanket
flush while consumers serve. No unreconciled decision means no routine Caddy
restart.

Missed polls, unavailable acknowledgement, early revocation, or invalid active
decisions that remain unreconciled at the deadline close the gate. Stop both
Caddy modes, the firewall bouncer, and engine; prove quiescence, clear in-memory
caches and the bouncer-owned nftables state, then physically delete. Verify and
rebuild from current decisions before reopening. Unrelated firewall rules stay
intact. This fallback is mandatory if the pinned consumers expose no sufficient
acknowledgement. SQL targets the pinned schema, not a stable vendor API.
Retention never waits for SQLite, database checkpoints, forced vacuum, or WAL
archival.

| Control                       | Bound       |
| ----------------------------- | ----------- |
| Runtime interval              | 300 seconds |
| Runtime whole-pass deadline   | 60 seconds  |
| Startup whole-pass deadline   | 300 seconds |
| Successful lease maximum age  | 360 seconds |
| Independent watchdog interval | 300 seconds |
| Consumer stop deadline        | 60 seconds  |

Only full success renews the root-owned lease. The independent watchdog rejects
missing, stale, future-dated, or malformed leases and disabled cleanup timers,
except for the exact [cutover mode](crowdsec-cutover.md#cutover-mode). The
lease, watchdog, and stop bounds total 12 minutes, inside the 30-minute
verification margin. Any clock, authentication, schema, or cleanup failure
blocks startup or stops all consumers. Confirm quiescence, remove tmpfs feeds,
and alarm with fixed codes. Keep PostgreSQL available to the app. Failed cleanup
keeps the gate closed until verified recovery; stopping ingestion is not
deletion.

## Log privacy

CrowdSec, firewall-bouncer, and Caddy bouncer logs that can contain an HTTP
source address or event stay on tmpfs and use the feed's retention bound. Their
journals contain fixed operational messages only. Caddy and CrowdSec have core
dumps disabled. The host uses zram and has no disk swap.

PostgreSQL errors, parameters, rows, and context must not leak attack values to
durable logs. `log_statement = none` alone is insufficient. Before any CrowdSec
connection set role-specific `log_min_messages = panic`,
`log_min_error_statement = panic`, `log_error_verbosity = terse`,
`log_statement = none`, `log_min_duration_statement = -1`,
`log_min_duration_sample = -1`, `log_statement_sample_rate = 0`,
`log_transaction_sample_rate = 0`, `log_parameter_max_length = 0`, and
`log_parameter_max_length_on_error = 0`. Do not grant the role permission to
weaken protected logging settings. Keep debug SQL logging disabled in CrowdSec.
Client diagnostics go only to the bounded private log path or are discarded;
journal and alarm output contain fixed reason codes and aggregate counts.
Forced-error proofs must cover the database server as well as the client.

## Backups and restore

pgBackRest copies the whole cluster, including CrowdSec records, credentials,
and WAL, to encrypted private vStorage in Vietnam. Selective restore does not
exclude data from backups. Keys stay in protected pgBackRest configuration; the
CrowdSec role receives none. A decrypted backup exposes stored attack data. The
30-day recovery window and weekly fulls normally retain copies for about five
weeks after live deletion. Backup/expiration failures require alarms; this is
not a strict physical erasure deadline. Backups serve recovery only. Live
deletion does not promise forensic erasure of pages, WAL, or backups. Encryption
protects an unpowered host; cleanup must precede consumer restart.

Every cluster or point-in-time restore keeps consumers fenced. Drop the restored
`crowdsec` database and fresh-provision schema, statistics policy, and
credentials; row cleanup cannot remove all old statistics or revoked identities.
Pre-cutover restores also provision fresh state. Clear caches and owned nftables
state, verify empty attack state and new credentials, then pass the gate. All
restored history and bans are discarded. Never rewind the app cluster solely to
repair CrowdSec.

## Migration, loss, and release order

Cutover imports no SQLite data: history, bans, detection windows, and
registrations start fresh. One approved local incident snapshot has restricted
access on encrypted storage and expires 30 days after capture. It is neither a
fallback nor a recurring backup; never copy it into support, CI, the repository,
or the backup bucket.

Complete the [notice and email gate](crowdsec-privacy-notification.md) before
ingestion. The [cutover contract](crowdsec-cutover.md) owns exact start permits,
quiescent bootstrap, manual safety-pass order, watchdog coverage, atomic
completion, and failure recovery under the existing operation fence. Its narrow
same-invocation runtime admission preserves ordinary disabled-timer refusal;
only completed safety passes establish the later cutover mode.

Provision authentication and PostgreSQL configuration before CrowdSec runs. Only
the fenced provisioner may initialize verified empty schema. Apply the
statistics policy and pass the full gate before registration or ingestion.
Generate fresh credentials in protected files and synchronize the Caddy secret.
The [restore composition](crowdsec-cutover.md#restore-composition) retains the
notice restore owner through fresh CrowdSec provisioning and private completion.

Installers, helpers, units, and tests have no SQLite checkpoint or fallback.
Keep non-database assets and credential directories. Image rollback retains the
reviewed PostgreSQL host bundle and compatible notice, with no SQLite rollback.
No client API or resume schema changes. Deploy parser and encoder together under
the fence, draining tmpfs first; mixed five/seven-field records are rejected.

## Security boundaries

Visitors, hostile content, and network attackers cannot add feed fields or forge
socket addresses. Users and stolen app tokens gain no CrowdSec DB access. App
containers cannot have both the socket and host UID 0; test their actual
identities. A CrowdSec DB writer can damage security state, not access app data
or keys. Invalid state closes the gate; host root and superusers remain trusted.
Backup controls cover leaked copies and revived credentials.

## Public notice and release

The notices in `apps/web/app/i18n/legal-en.ts` and `legal-vi.ts` use this exact
attack-protection copy. It replaces the blanket path exclusion as well as the
retention sentences:

English:

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

Vietnamese:

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

The production notices name GreenNode and Bizfly in Vietnam. Cloudflare provides
DNS-only records; requests reach the Vietnam host directly.

## Required proof

Hosted tests use distinct markers in eligible paths and forbidden fields:

- the feed/parser enforce seven keys, fixed empty values, classes, eligibility,
  and socket address; only eligible alert samples persist exact escaped paths;
- percent and double encoding, encoded slashes, Unicode, control characters,
  malformed and oversized paths, and 512-byte boundaries follow the contract;
- valid resume handles, nonexistent valid resume routes, auth/API paths, and
  every other protected route class emit no target, including mixed-case and
  percent-encoded case aliases in every decoded/normalized exclusion view;
- query, header, body, host, and forbidden-path markers reach no feed or alert
  sample; target markers reach no durable log or alarm, including forced errors;
- installed detection creates 15-minute and repeat one-hour bans enforced by
  Caddy 403; a benign sequence creates no decision;
- CrowdSec 1.8.1 initializes and operates against PostgreSQL 18 through the
  configured Unix socket and peer map, with no TCP or password fallback;
- application roles and container identities cannot access CrowdSec, and the
  CrowdSec role cannot access application data or privileged database actions;
- seeded 23-hour-30-minute HTTP and SSH graphs, stale child events, expired
  decisions, and orphans disappear from live tables in runtime and startup paths
  while fresh records and machine and bouncer credentials remain;
- non-finite timestamps, invalid durations, missing schema, incorrect grants,
  lock contention, clock failure, and database loss fail closed within the
  configured bounds;
- missing tables, columns, manifest mismatch, or changed statistics policy stop
  every schema-capable command before it runs, without recreating anything;
- fresh provisioning establishes zero column targets and table policy before
  ingestion; aggregate checks show no value-bearing planner statistics after
  sustained events and ordinary VACUUM; contamination closes the gate;
- natural expiry, explicit revocation, missed polls, and concurrent expiry
  reconcile the exact deletion set; failed acknowledgement clears consumer state
  before deletion, while healthy reconciled cleanup needs no restart;
- missing/stale leases and ordinary disabled timers stop all consumers; the
  [cutover and restore proofs](crowdsec-cutover.md#required-proof) cover the
  bounded exceptions, invocation identity, watchdog coverage, and completion;
- a forced cleanup failure blocks startup or stops consumers without a database
  checkpoint, forced vacuum, or archive-completion dependency;
- forced SQL errors, including bad values and constraint failures, leak no
  attack fields into PostgreSQL logs, process logs, the journal, or alarm mail;
- bootstrap interruption, reboot, PostgreSQL restart, and restored old state
  cannot bypass cleanup or resurrect revoked registrations or cached bans;
- a cluster restore with old rows, statistics, and revoked credentials replaces
  the CrowdSec database before startup; the application database stays valid;
- fresh registrations, Caddy bans, firewall state, and expiry work after
  cutover, with no SQLite database created or command executed; and
- central API credentials and traffic remain absent.

Pinned hosted integration and static checks cover these contracts. Require
exact-commit CI and independent adversarial review before deployment. Production
proofs report metadata, service state, counts, leases, backups, and enforcement,
never rows or credentials. A TCP-only database probe is insufficient.

## Sources

- [CrowdSec 1.8.1 database configuration](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/csconfig/database.go)
- [CrowdSec 1.8.1 schema initialization](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/database.go)
- [CrowdSec 1.8.1 flush implementation](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/flush.go)
- [CrowdSec 1.8.1 decision expiry and deletion](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/decisions.go)
- [Caddy access logging and file rotation](https://caddyserver.com/docs/caddyfile/directives/log)
- [Caddy access-log fields](https://caddyserver.com/docs/formatters#access-logs)
- [CrowdSec alert flush command](https://docs.crowdsec.net/docs/cscli/cscli_alerts_flush/)
- [CrowdSec machine registration](https://docs.crowdsec.net/docs/cscli/cscli_machines_add/)
- [CrowdSec bouncer registration](https://docs.crowdsec.net/docs/cscli/cscli_bouncers_add/)
- [PostgreSQL 18 peer authentication](https://www.postgresql.org/docs/18/auth-peer.html)
- [PostgreSQL 18 user maps](https://www.postgresql.org/docs/18/auth-username-maps.html)
- [PostgreSQL 18 logging controls](https://www.postgresql.org/docs/18/runtime-config-logging.html)
- [PostgreSQL 18 table maintenance settings](https://www.postgresql.org/docs/18/sql-createtable.html#SQL-CREATETABLE-STORAGE-PARAMETERS)
- [PostgreSQL 18 statistics collection and persistence](https://www.postgresql.org/docs/18/sql-analyze.html)
- [PostgreSQL 18 statistics view](https://www.postgresql.org/docs/18/view-pg-stats.html)
- [PostgreSQL 18 VACUUM](https://www.postgresql.org/docs/18/sql-vacuum.html)
- [Go escaped URL paths](https://pkg.go.dev/net/url#URL.EscapedPath)
- [RFC 3986 percent encoding](https://www.rfc-editor.org/rfc/rfc3986#section-2.1)
- [pgBackRest backup, retention, and restore](https://pgbackrest.org/user-guide.html)
- [systemd dependency semantics](https://www.freedesktop.org/software/systemd/man/latest/systemd.unit.html)
