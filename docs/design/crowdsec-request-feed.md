# CrowdSec request feed and detection

This page holds the request feed and detection rules of the
[CrowdSec HTTP privacy](crowdsec-http-privacy.md) design. Its
[Sources](crowdsec-http-privacy.md#sources) section lists the cited pages.

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
source address and coarse fields, never by target text; maintenance 503 is
neutral. The configured 30-minute scenario check does not cap bucket lifetime;
[history assumptions and overdue graphs](crowdsec-retention-batching.md#deadline-arithmetic)
define the retention response.

No GeoIP, autonomous-system, reverse-DNS, or intelligence enrichment exists.
CrowdSec persists alert samples, not the request feed or an access log; samples
hold the seven fields plus scenario, counts, timestamps, and decision expiry;
unsampled requests disappear with feed cleanup. HTTP scenario names start with
`aboutme/http-`. The bouncer enforces current IP decisions with 403, which never
feeds status-based ban escalation. Community API, console, intelligence clients,
and online credentials are absent, so no attack data reaches threat-intelligence
recipients. The Caddy bouncer pulls decisions only from the local API; its
[community pull flags](crowdsec-retention-reload.md#pinned-bouncer-behavior)
reach no central service. The local API listens only on `127.0.0.1:8095`. Pin
`cscli.hub_branch: master` to reuse cached detection without release lookup.
