# CrowdSec HTTP privacy

The Vietnam production edge gives CrowdSec a small HTTP request feed so it can
detect repeated abuse without keeping ordinary access logs. This target is
approved by the owner (2026-10-10). It becomes production behavior only at the
Vietnam cutover. Until then, production keeps the AWS privacy contract.

## Request feed

Caddy writes newline-delimited JSON to tmpfs. One valid request record has
exactly these fields:

| Field         | Type   | Value                                                                  |
| ------------- | ------ | ---------------------------------------------------------------------- |
| `ts`          | string | UTC RFC 3339 timestamp with nanoseconds                                |
| `source_ip`   | string | Canonical unmapped socket IPv4 or IPv6 address, without port           |
| `method`      | string | `GET`, `HEAD`, `POST`, `PUT`, `PATCH`, `DELETE`, `OPTIONS`, or `OTHER` |
| `status`      | number | Integer from 100 through 599                                           |
| `route_class` | string | One of the route classes below                                         |

The encoder is an allowlist, not a filtered copy of Caddy's access event. It
never emits the raw path or URI, query, request or response headers, body, host,
port, TLS details, byte counts, duration, user or resume handle, token, or other
identifier. It takes `source_ip` only from the direct listener's socket and
canonicalizes it with IPv4-in-IPv6 unmapping. Forwarding headers, including
repeated headers, cannot supply it. An unknown method becomes `OTHER`. A missing
or invalid timestamp, address, status, or route class drops the record. An
encoder error names only a fixed reason code and never the bad value or source
event.

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

Serving writes `/var/log/caddy/crowdsec/serving.json`; maintenance writes
`/var/log/caddy/crowdsec/maintenance.json`. The host mounts both below
`/run/aboutme/caddy-log`, a tmpfs with no swap backing. Each file is private to
Caddy, caps at 5 MiB, rotates on every UTC clock hour, and keeps one
uncompressed roll. An active file plus one roll normally holds less than two
hours, but Caddy's roll settings are not the retention authority. The host
precreates both exact current paths before CrowdSec starts; acquisition tails
them without a glob. The retention unit enforces the 24-hour bound independently
of size rotation and proves parser continuity after an active-file restart. A
reboot removes both. Caddy's default logger excludes both access logger names
and deletes the URI from runtime errors, so request data cannot fall through to
stderr or journald.

The feed exists only on the direct listener. The AWS CloudFront listener keeps
its current no-access-log behavior, including in an image that contains both
listeners.

## Detection

CrowdSec acquisition labels both files `type: aboutme-http`. A local parser
accepts only the five-field schema above. Local scenarios group requests by
source address and the coarse method, status, and route class. They cover
volume, repeated failures, and scanning across the `unknown` class. Every
enabled HTTP and SSH scenario accumulates events for at most 30 minutes. The
feed does not enable a stock parser or scenario that needs a raw path or
user-agent. Maintenance rules treat the expected 503 status as neutral.

The parser and scenarios do not add GeoIP, autonomous system, reverse DNS, or
threat-intelligence data to an HTTP event. CrowdSec may store the five feed
fields, scenario name, counts, timestamps, and a decision expiry. The Caddy
bouncer uses only a current IP decision and returns 403. The status scenario
does not count 403, so the bouncer's own answers to a banned address never
extend its ban. HTTP scenario names start with `aboutme/http-`, which identifies
their stored records and profiles.

Community API, console, and threat-intelligence clients stay disabled. The
installation has no online credentials. No HTTP attack feed, event, alert, or
decision is sent to CrowdSec or another remote recipient. The local API listens
only on `127.0.0.1:8095`.

## Retention and failure

The raw feed remains only in tmpfs and normally holds less than two hours under
hourly rotation. CrowdSec's SQLite database and WAL live on the encrypted
Vietnam data volume. All feed and database attack records have the 24-hour
logical retention bound. They are not part of pgBackRest, vStorage, release,
support, or diagnostic copies.

Every HTTP, SSH, and manual test decision lasts at most 22 hours. Many customers
can share one address (mobile carrier NAT, public Wi-Fi), so an HTTP ban starts
at 15 minutes and escalates to one hour, then four, only when the same address
offends again while its earlier decisions are still in the database. The status
scenario counts only scanner signals (400, 404, 405, and unknown routes), and
the rate scenario leaves out static assets and health checks, and a separate
static-flood scenario bans only far beyond page-load bursts (3,000 requests,
then 50 a second). The HTTP profile precedes the default catch-all and stops
profile evaluation. `db_config.flush.max_age` is 23 hours, which shortens all
alert history to 23 hours. CrowdSec 1.8.1 runs its alert flush every minute, but
that setting is only one control: its code keeps an old alert while the alert
has an active decision. Events and metadata follow their alert by cascade. The
decision and scenario-window limits make every child event eligible before 24
hours.

A host retention unit runs at startup and every five minutes. Startup waits for
a valid synchronized clock and cleans stopped SQLite before CrowdSec, either
bouncer, or Caddy may start. Those services require the successful gate and run
after it; ordering alone is insufficient. Runtime cleanup asks CrowdSec to flush
alerts older than 23 hours, then removes expired decisions and orphan records,
checkpoints and truncates the SQLite WAL, and verifies every IP-bearing alert,
event, metadata row, and decision by its own timestamp. No logical record or
feed copy may reach 23 hours 30 minutes. The cleanup has a five-minute deadline.

Offline cleanup enables SQLite foreign keys and selects bounded alert ID batches
across HTTP and SSH attack graphs. It removes their expired decisions, events,
and metadata while leaving machine and bouncer credentials intact. It checks
`foreign_key_check`, every related table, and the WAL checkpoint busy result.
The implementation may disable WAL if pinned CrowdSec proves that mode safe. A
successful `sqlite3` exit alone is not proof.

Any cleanup, checkpoint, or verification failure closes the gate. At startup,
the dependent services do not start. At runtime, the unit stops Caddy and
CrowdSec, removes the tmpfs feed, and keeps the services stopped before a
persistent record can cross 24 hours. Stopping ingestion does not count as
database deletion, so recovery must complete cleanup and verification before
restart. Each success renews a root-owned five-minute lease. An independent
watchdog applies the same stop when the lease is missing or stale, including
when the cleanup timer is disabled. The unit stops both serving and maintenance
Caddy and sends a fixed reason-code alarm. Journald and alarm mail receive no
address, event JSON, row value, or feed content.

CrowdSec, firewall-bouncer, and Caddy bouncer logs that can contain an HTTP
source address or event stay on tmpfs and use the feed's retention bound. Their
journals contain fixed operational messages only. Caddy and CrowdSec have core
dumps disabled. The host uses zram and has no disk swap.

An unpowered host cannot rewrite disk blocks on a schedule. The encrypted volume
protects those bytes while the host is off. The startup gate deletes expired
logical rows and truncates the WAL before any process can read or use them. The
24-hour promise concerns logical records and access, not forensic erasure of
encrypted storage sectors.

## Public notice and release

The English and Vietnamese notices live in `apps/web/app/i18n/legal-en.ts` and
`legal-vi.ts`. They disclose that attack protection in Vietnam uses the IP
address, method, response status, and broad route class; excludes raw paths,
queries, headers, and bodies; keeps the request feed, attack alerts, and bans
for at most 24 hours; and does not share HTTP attack data with the CrowdSec
community.

The same edit replaces the current AWS hosting and transfer text with the
approved GreenNode and Bizfly text. The notice is held with the Vietnam cutover
release and is never deployed to AWS production. It becomes effective when the
cutover switches the public Route 53 records to the verified Vietnam host. The
temporary rehearsal hostname uses fictional data and is not announced.

## Required proof

Hosted tests use a marker in a URI, query, header, and body. They prove:

- the feed contains the exact five keys and the trusted socket address;
- no marker or raw request field reaches the feed, CrowdSec event, process logs,
  journal, or alarm mail while upstream and Caddy bouncer errors are forced;
- a sanitized record passes the installed parser and scenario, creates a first
  decision of 15 minutes and a repeat decision of one hour, and makes the Caddy
  bouncer return 403;
- a benign sequence through the same pinned CrowdSec 1.8.1 path creates no
  decision;
- seeded 23-hour-30-minute HTTP and SSH records, child events, and WAL content
  disappear in the runtime and startup paths while fresh records and machine and
  bouncer credentials remain;
- a missing, stale, or disabled cleanup timer expires the independent lease and
  stops both Caddy modes and CrowdSec;
- a forced cleanup or checkpoint failure blocks startup or stops the serving
  services; and
- central API credentials and traffic remain absent.

## Sources

- [CrowdSec 1.8.1 database configuration](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/csconfig/database.go)
- [CrowdSec 1.8.1 flush implementation](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/flush.go)
- [CrowdSec 1.8.1 alert schema](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/ent/schema/alert.go)
- [CrowdSec 1.8.1 event schema](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/ent/schema/event.go)
- [CrowdSec 1.8.1 decision schema](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/ent/schema/decision.go)
- [Caddy access logging and file rotation](https://caddyserver.com/docs/caddyfile/directives/log)
- [Caddy access-log fields](https://caddyserver.com/docs/formatters#access-logs)
- [CrowdSec alert flush command](https://docs.crowdsec.net/docs/cscli/cscli_alerts_flush/)
