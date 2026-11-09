# CrowdSec retention reload evaluation

Status: blocked proposal, 2026-11-02. Keep stopped reconciliation. A graceful
reload alone does not prove cache deletion with the pinned components. No live
path or ADR 0026 amendment is approved by this evaluation.

The [privacy contract](crowdsec-http-privacy.md#live-retention-and-failure) owns
all retention, lease, SQL, and failure bounds. This proposal changes none of
them. The owner chose [batched stopped cleanup](crowdsec-retention-batching.md).
Zero downtime requires consumer changes and remains outside that scope.

## Smallest approach and its limits

First-party source for bouncer v0.15.0 and Caddy 2.11.9 establishes these
boundaries:

| Pinned source                                                                                | Finding                                                                                      |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Bouncer `crowdsec/crowdsec.go:143`, `internal/core/core.go:149`, `internal/core/store.go:29` | Provision creates a new core with a new empty store; no shared store is reused.              |
| Bouncer `internal/bouncer/stream.go:110`, `internal/core/core.go:242`                        | The new stream starts with `startup=true`, asynchronously.                                   |
| Bouncer `crowdsec/crowdsec.go:324`, `internal/core/decisions.go:109`                         | Start does not wait for the snapshot. Empty-cache lookups allow requests before bans arrive. |
| Bouncer `crowdsec/crowdsec.go:309`, `internal/core/core.go:257`                              | Cleanup cancels and joins workers, but does not clear the old store.                         |
| Caddy `caddy.go:362`, `modules/caddyhttp/app.go:853`                                         | New apps start before old apps stop; reload does not wait for old HTTP requests to drain.    |

Consequently, reload success proves neither enforcement readiness nor absence
from every old cache. Long requests may retain old handlers and stores. Forcing
those connections closed would violate the uninterrupted-request goal. A config
ID, load timestamp, module uptime, registered LAPI pull, or the
[populated flag](#pinned-bouncer-behavior) cannot supply the missing proof.
`/crowdsec/check` returns only a blocked boolean, so overlapping fresh decisions
can hide stale ones. Health checks perform a separate lookup. The module
instance ID is generated from whole-second time and is not a unique reload
acknowledgement (`internal/core/core.go:303`).

Both production Caddyfiles set `admin off`. Enabling administration would add a
control surface without fixing these defects. Do not expose an admin endpoint or
introduce a socket for this proposal. The source inspection supports graceful
listener reuse, not a claim of zero failed requests in this deployment.

## Pinned bouncer behavior

`deploy/caddy/production/build/go.mod` pins `caddy-crowdsec-bouncer` v0.15.0,
tag commit `4a6ce1d`, whose module hash matches `go.sum`. Production sets only
`api_url`, `api_key`, and `ticker_interval 10s`
(`deploy/caddy/production/entrypoint.sh`). Compared with the previous pin
`ffdcb7c6f861`, v0.15.0 changes startup timing, stream request flags, and HTTPS
transport and adds a populated flag; the reload limits above hold for both.

| Behavior                   | v0.15.0 source                                          | Effect here                                                                                                                                                                                                                                                   |
| -------------------------- | ------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Empty-cache startup        | `internal/core/decisions.go:109`                        | Unchanged. Until the first pull is applied, lookups miss its bans and allow those requests. A one-time warning marks the first lookup before then.                                                                                                            |
| Delta pulls                | `internal/bouncer/stream.go:167`                        | Unchanged. One pull every `ticker_interval` (10 s), each with the 10 s default timeout of the unset `lapi_timeout`. A failed pull is logged and the cache is kept.                                                                                            |
| Initial connection retries | `internal/bouncer/stream.go:77`, `:127`                 | Changed from a fixed 10 s wait to full jitter: a random wait below 10 s, with the bound doubling per failure to a two-minute cap.                                                                                                                             |
| Initial snapshot timeout   | `internal/bouncer/stream.go:63`, `:189`                 | Changed from 10 s to the larger of 60 s and six times `lapi_timeout`: 60 s here.                                                                                                                                                                              |
| Deletions and cleanup      | `internal/core/store.go`, `internal/core/core.go:248`   | Unchanged. Stream deletions remove store entries; shutdown joins workers without clearing the store.                                                                                                                                                          |
| `enable_hard_fails`        | `internal/core/core.go:141`                             | Unchanged and still omitted, so Caddy serves and retries while the local API is down.                                                                                                                                                                         |
| Populated state            | `internal/core/decisions.go:78`, `crowdsec/admin.go:23` | Added. `DecisionStorePopulated` and an info line follow the first applied pull, even when some inserts failed. Serving does not wait for it, it says nothing about older caches, the admin API is off, and production logs reduce the line to a fixed marker. |
| AppSec                     | `internal/core/appsec.go:79`                            | Unchanged. It runs only with `appsec_url`, which production never sets, so the bouncer sends nothing to AppSec.                                                                                                                                               |
| Address logging            | `internal/core/logging.go`                              | Unchanged path. The added retry, warning, and populated lines use the `crowdsec` logger and reach only the [private safe-metadata log](crowdsec-http-privacy.md#log-privacy).                                                                                 |
| Community decisions        | `internal/bouncer/stream.go:48`                         | Requests stop sending `community_pull=false` and `additional_pull=false`. Local API 1.8.1 ignores both in stream filters, and the host has no online client, so pulled decisions are unchanged and nothing is uploaded.                                       |
| LAPI transport             | `internal/bouncer/client.go`                            | Changed for `https` URLs only, which honor proxy variables. The entrypoint accepts only loopback HTTP.                                                                                                                                                        |

During local API trouble at edge start, the empty-cache window can last minutes:
a hanging snapshot attempt can take 60 s and the next retry can wait up to two
minutes. With `ffdcb7c6f861`, each attempt and each wait took at most 10 s. The
[deploy smoke](../runbooks/deploy.md#deploy) 403 wait does not cover that case.

## Limits and decision

Firewall-only restart cannot retire Caddy caches. The firewall selects SSH
scenarios, but its owned input rules have no port filter. Clearing them removes
protection for those addresses across input ports, not just SSH. SQL locks
exclude database writers but cannot fence an already fetched stream response.
Stopping LAPI alone neither clears Caddy caches nor preserves new protection.
Cache-free lookup adds per-request LAPI dependence and allows requests on lookup
failure unless configured to terminate Caddy.

Keep stopped reconciliation. A future live path needs supported consumer changes
proving applied initial bans before serving, complete retirement of old caches,
and candidate absence through SQL commit despite delayed responses. Prove both
Caddy modes, overlapping decisions, revocation, concurrency, and firewall
rebuild with pinned real components before any rollout. HTTPS probes, long
requests, and continuous active-ban checks supplement that proof; passing HTTP
alone does not establish cache deletion. No live implementation, production
proof, new admin surface, or ADR amendment is authorized here.

## Sources

Bouncer findings compare the v0.15.0 tag with `ffdcb7c6f861`; the tag's files
match the module cache copy. Caddy and CrowdSec findings come from the pinned
module cache. Source inspection is not runtime verification. No secondary-source
claim supports this decision.

- [Bouncer changes from the previous pin](https://github.com/hslatman/caddy-crowdsec-bouncer/compare/ffdcb7c6f861...v0.15.0)
- [Pinned bouncer core](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/v0.15.0/internal/core/core.go)
- [Store](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/v0.15.0/internal/core/store.go)
- [Decision processing](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/v0.15.0/internal/core/decisions.go)
- [Stream](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/v0.15.0/internal/bouncer/stream.go)
- [Lifecycle](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/v0.15.0/crowdsec/crowdsec.go)
- [Admin API](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/v0.15.0/crowdsec/adminapi.go)
- [Logging hook](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/v0.15.0/internal/core/logging.go)
- [AppSec](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/v0.15.0/internal/core/appsec.go)
- [LAPI client](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/v0.15.0/internal/bouncer/client.go)
- [CrowdSec 1.8.1 stream filters](https://github.com/crowdsecurity/crowdsec/blob/v1.8.1/pkg/database/decisionfilter.go)
- [Caddy 2.11.9 configuration lifecycle](https://github.com/caddyserver/caddy/blob/v2.11.9/caddy.go#L362-L375)
- [Caddy HTTP shutdown](https://github.com/caddyserver/caddy/blob/v2.11.9/modules/caddyhttp/app.go#L853-L876)
