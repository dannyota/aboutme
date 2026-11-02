# Telegram host alerts

Status: proposed (2026-11-02), pending owner acceptance of the
[ADR amendment](../adr/0026-vietnam-hosted-production.md#amendment-host-alerts-through-telegram-2026-11-02).
Mail remains current until cutover; app contracts do not change.

Recommend Telegram only; fallback adds maintenance and duplicates. Keep app
SMTP. vMonitor remains the external readiness backstop but shares Telegram
delivery risk and misses healthy-site disk/backup faults. GitHub uptime is
slower.

**Transport and custody.** Keep the one-shot root sender and existing curl, jq,
and CA roots; no new daemon/package. Each unsuppressed call makes one POST to
`https://api.telegram.org/bot<TOKEN>/sendMessage`, with JSON `chat_id` and
`text`. Verify certificates/hostnames; use `--connect-timeout 5`,
`--max-time 15`, HTTPS only, no retries/redirects/proxy. First argument `-q`
disables curl config. No traces, verbose output, webhook, polling daemon, or
inbound firewall opening.

The owner reuses the existing bot and vMonitor supergroup through the
[runbook's pipe-only imports](../runbooks/production.md#host-telegram-setup-and-rotation-draft).
Files live under `/etc/aboutme/secrets`, root-owned 0400, under the root-only
directory; reject symlinks, wrong permissions, empty or malformed values. Token:
`[0-9]+:[A-Za-z0-9_-]+`, at most 256 ASCII bytes. Chat ID: `-?[0-9]{1,20}`, as a
JSON string. Reject whitespace/newlines. Treat chat ID as personal data. Extend
import/replacement allowlists; exclude `telegram-bot-token` and
`telegram-chat-id` from Podman, including unconditional `--replace` sync. Only
the host sender reads them; keep age-encrypted custody. Feed the token-bearing
URL and constructed JSON through curl config on stdin, using shell builtins and
anonymous pipes from the host secrets. No intermediate secret-bearing files,
command arguments, exported variables, or inspection. Bound replies to 16 KiB in
memory even without Content-Length; pipe them to the validator, suppress raw
curl/jq errors, and discard bodies without writing files. Success requires curl
exit 0, HTTP 200, and a JSON object with boolean `ok=true`. Only fixed outcomes
(`sent`, `suppressed`, `invalid_input`, `invalid_secret`, `transport_failed`,
`rejected`, `invalid_response`) and validated keys reach the journal. Never log
values, URLs, request/response bodies, or provider descriptions.

**Content and deduplication.** `alert-telegram.sh` keeps `<key> <subject>` and
bounded stdin. Keys retain `[A-Za-z0-9@._-]{1,120}`. Subjects are 1 to 200
printable ASCII bytes under `LC_ALL=C`. Read at most 4096 detail bytes, keep
printable ASCII and newlines, and cap the included detail at 2048 bytes with a
fixed truncation notice. Total `text` is at most 3000 ASCII bytes including
fixed guidance. Include `[aboutme-prod]`, key, UTC time, subject, and detail;
omit hostname. Plain text, no parse mode: only unit names, counts, percentages,
fixed guidance. No log lines, request URLs, resume/account data, credentials, or
chat ID. Callers must select approved metadata; printable filtering cannot
remove personal data. Preserve the Coraza metadata-location guidance for
`coraza-log-invalid`. Watcher subjects use `failed units: <count>`, with bounded
names and an omitted count in detail; never concatenate unit names into the
subject.

Keep atomic noclobber stamps at `/var/lib/aboutme/alerts/<key>.<UTC-date>`.
Concurrent calls for one key/day send at most one in-flight request. Keep the
stamp on confirmed success; remove this invocation's stamp on failure or caught
termination. Failures return nonzero; the watcher continues its other checks.
Later calls retry ongoing faults, normally at the next five-minute tick. No
queue: transient events can be lost, unknown delivery can duplicate later, and
SIGKILL/power loss can suppress until the next UTC day. Preserve stamps at
cutover.

**Trust and egress.** Existing open egress permits resolver DNS and TCP 443 to
`api.telegram.org`; no IP pin or circumvention proxy. Telegram receives
metadata, chat ID, and host source IP outside the Vietnam boundary;
location/retention are unverified. The existing supergroup's members can read
alerts. Our bot needs send permission, no admin rights. The owner controls
BotFather and membership; revoking this shared bot's token affects every sender
using it.

Visitors, signed-in users, hostile resumes, stolen sessions/MCP tokens, and DB
writers have no alert endpoint or control of destination/body. App containers
have no secret mount/root sender access; root compromise defeats this boundary.
Network attackers can block delivery; TLS prevents impersonation. DB backups
exclude bot secrets; decrypted age copies expose them. Never apply API redirects
or chat migration hints. Revocation cannot erase sent messages or member copies.

**Verification and rollout.** Hosted VN infrastructure tests use synthetic
credentials and a loopback fake endpoint, never Telegram. Permit an endpoint
override only with the existing non-root test-root mode; root rejects all test
routing. Cover success, HTTP/JSON rejection including 429, redirects, DNS/TLS
errors, timeouts, controls/locales, malformed replies, unsafe secrets, private
files/argv/logs, oversized replies, stamp races/retry, rotation without Podman,
SMTP-active staging, receipt-gated caller switching, and the watcher-to-sender
path with a failure list exceeding 200 characters.

The [runbook](../runbooks/production.md#host-telegram-setup-and-rotation-draft)
owns setup, rotation, and rollback. Order: owner acceptance, source
verification, implementation/regressions, green exact-commit CI, staged
installation and proof. Stage `alert-telegram.sh` beside active SMTP `alert.sh`.
Its `--test` sends fixed `[aboutme-prod] host alert delivery test`, no caller
text, same transport and credentials, one send per invocation, no daily-stamp
read/write. Only owner receipt permits switching `watch.sh` and
`aboutme-alert@.service` to Telegram. Every credential/destination change also
needs receipt; failure blocks cutover, never stops serving or rolls back the
app. Ordinary app deploys do not send tests. Older app images keep current host
helpers; no data migration or alert replay.

First-party references, unfetched under the network prohibition:
[API requests](https://core.telegram.org/bots/api#making-requests),
[sendMessage](https://core.telegram.org/bots/api#sendmessage),
[BotFather](https://core.telegram.org/bots/features#botfather), and
[curl options](https://curl.se/docs/manpage.html). Before implementation, verify
result semantics, limits, revocation, and installed curl bounds.

## Operator reference

Proposed only. Use after owner acceptance and reviewed implementation of the
host alert contract.

Commands require the proposed helper support. Origin HTTP success does not prove
alert delivery.
