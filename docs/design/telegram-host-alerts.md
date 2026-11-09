# Telegram host alerts

Status: accepted (2026-11-02), by the owner under the
[ADR amendment](../adr/0026-vietnam-hosted-production.md#amendment-host-alerts-through-telegram-2026-11-02).
Production cutover is not yet done. Mail remains current until cutover; app
contracts do not change.

The owner accepted Telegram only, host SMTP removal after confirmed receipt, and
the narrow transfer exception in the ADR amendment. Keep app SMTP. vMonitor
remains the external readiness backstop but shares Telegram delivery risk and
misses healthy-site disk/backup faults. GitHub uptime is slower.

**Transport and custody.** Keep the one-shot root sender and existing curl, jq,
and CA roots; no new daemon/package. Each unsuppressed call makes one POST to
`https://api.telegram.org/bot<TOKEN>/sendMessage`, with JSON `chat_id` and
`text`, subject to the [unit-failure retry exception](#unit-failure-retries).
Verify certificates/hostnames; use `--connect-timeout 5`, `--max-time 15`, HTTPS
only. Curl itself has no retries, no redirects, and no proxy. First argument
`-q` disables curl config. No traces, verbose output, webhook, polling daemon,
or inbound firewall opening.

The owner reuses the existing bot and vMonitor supergroup through the
[runbook's pipe-only imports](../runbooks/production.md#host-telegram-setup-and-rotation).
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
`rejected`, `invalid_response`, `stamp_failed`) and validated keys reach the
journal. Never log values, URLs, request/response bodies, or provider
descriptions.

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
Concurrent calls for one key/day send at most one in-flight request. Suppress
only an existing empty, private regular stamp. A creation failure returns
nonzero with `stamp_failed`. Keep the stamp on confirmed success; remove this
invocation's stamp on failure or caught termination. Failures return nonzero;
the watcher continues its other checks. Later calls retry ongoing faults,
normally at the next five-minute tick. No queue: transient events can be lost,
unknown delivery can duplicate later, and SIGKILL/power loss can suppress until
the next UTC day. Preserve stamps at cutover. Every non-test send requires the
active Telegram selector and matching credential approval while holding the
shared credential lock, including calls from a watcher that selected its sender
before rollback. Bind proof and approval to a SHA-256 digest of the validated
credential pair, computed from memory through a pipe under that lock. Keep the
digest only in root-only state. Metadata-only changes preserve approval; imports
and replacements explicitly clear proof and approval.

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

The [runbook](../runbooks/production.md#host-telegram-setup-and-rotation) owns
setup, rotation, and rollback. Order: owner acceptance, source verification,
implementation/regressions, green exact-commit CI, staged installation and
proof. Stage `alert-telegram.sh` beside active SMTP `alert.sh`. Its `--test`
sends fixed `[aboutme-prod] host alert delivery test`, no caller text, same
transport and credentials, one send per invocation, no daily-stamp read/write.
Only owner receipt permits switching `watch.sh` and `aboutme-alert@.service` to
Telegram. Every credential/destination change also needs receipt; failure blocks
cutover, never stops serving or rolls back the app. Ordinary app deploys do not
send tests. Older app images keep current host helpers; no data migration or
alert replay.

First-party references, unfetched under the network prohibition:
[API requests](https://core.telegram.org/bots/api#making-requests),
[sendMessage](https://core.telegram.org/bots/api#sendmessage),
[BotFather](https://core.telegram.org/bots/features#botfather), and
[curl options](https://curl.se/docs/manpage.html). Before implementation, verify
result semantics, limits, revocation, and installed curl bounds.

## Unit-failure retries

Only unit-failure alerts (`unit-*` keys) may make up to three attempts, with 10
seconds between attempts. Retry only when curl exit 6, 7, or 28, HTTP `000`,
zero request and upload bytes, and an empty response body together prove nothing
was sent. Missing proof, any request or upload bytes, any response, or any other
failure ends the invocation without retry. Watcher alerts and receipt tests get
one attempt. Curl's own retries remain disabled.

The alert unit independently sets `Wants=network-online.target` and
`After=network-online.target`. The [target](https://systemd.io/NETWORK_ONLINE/)
concerns link configuration, not Telegram reachability, and depends on the
configured waiter.
[Netplan 1.0.1](https://github.com/canonical/netplan/blob/1.0.1/src/generate.c#L60-L85)
disables its wait-online generation. The host's waiter state is unverified.

The sender allows a 20-second credential-lock wait, three requests of at most 15
seconds each, and two 10-second sleeps: 85 seconds before outcome cleanup. The
alert service uses `TimeoutStartSec=100s` for this budget and cleanup, plus
`TimeoutStopSec=5s` to stop. It discards stdout and sends sanitized sender
outcomes to its own journal.

Retention failure dispatches `aboutme-alert@aboutme-crowdsec-retention.service`
with `systemctl --no-block start`. Its five-second dispatch bound does not share
the sender's process group or locks. Dispatch failure makes the retention action
fail. Shutdown keeps its 60-second stop bound and 75-second cleanup bound.
Delivery failure does not reopen retention admission; its daily key is
`unit-aboutme-crowdsec-retention`.

## Operator reference

The [runbook](../runbooks/production.md#host-telegram-setup-and-rotation) owns
implemented setup, receipt confirmation, cutover, rotation, and rollback
commands. A successful HTTP response does not prove owner receipt or production
activation.

The owner's choice reuses the existing bot from local `TELEGRAM_BOT` and the
vMonitor `aboutme-alerts` supergroup. Add that bot with send permission and no
admin rights; leave VNG's bot and its notifications intact. Coordinate
shared-bot revocation with every sender. Keep application SMTP and working host
SMTP during initial staging. No automatic SMTP fallback is configured. With the
reviewed bundle copied, the host administrator installs it before the runbook's
`stage`:

```sh
sudo bash <bundle>/host/install.sh <bundle>
```

Do not run setup with tracing or terminal recording enabled.

`telegram-setup.sh` reads the literal local `.env` value without sourcing it. It
filters `vngcloud monitor list-channels --output json` for exactly one Telegram
channel named `aboutme-alerts` with a numeric supergroup address, then pipes
both values to the host importer without newlines. Missing, duplicate,
malformed, or failed results stop setup. `--replace` invalidates proof and
approval while keeping Telegram selected. Automatic sends stay disabled until a
successful fresh test and `confirm-receipt --received`. A failed test clears its
proof. Destination changes require owner approval and the same receipt sequence;
never accept API-suggested destination changes.

Inspect names and metadata only on the host:

```sh
sudo stat -c '%u:%a %n' /etc/aboutme/secrets \
  /etc/aboutme/secrets/telegram-bot-token \
  /etc/aboutme/secrets/telegram-chat-id
```

Expect directory `0:700` and files `0:400`. Never print, inspect, tee, or save
values or raw provider responses. Telegram secrets never enter Podman or
database backups. For each encrypted custody refresh, set `AGE_RECIPIENT` and a
fresh `ENCRYPTED_BACKUP` path on the trusted laptop; the helper refuses
overwrite and creates no plaintext intermediate. Record only UTC receipt time
and success/failure, with no values or screenshots.

After successful initial cutover, remove host-alert `ALERT_TO` on the host:

```sh
sudo sed -i '/^ALERT_TO=/d' /etc/aboutme/smtp.env
```

The installer requires msmtp only while SMTP is selected. Package removal is
operator cleanup; retain application SMTP settings and secrets. Before rollback,
restore the reviewed SMTP configuration and `ALERT_TO`, ensure `msmtp` and SMTP
secrets exist, then follow the runbook's `stage` and fresh-key mail proof. The
same command removes the Telegram override, selector, proof, and approval and
reloads systemd. Preserve daily stamps and other host safety changes; no app
restart is needed. After restore, verify membership and current credentials
without displaying values; rotate exposed tokens and repeat test, owner receipt,
and explicit confirmation. Inspect only fixed journal outcomes:

```sh
sudo journalctl -u 'aboutme-alert@*' -u aboutme-watch --since -30min -o cat
```
