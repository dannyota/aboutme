# CrowdSec retention reload evaluation

Status: blocked proposal, 2026-11-02. Keep stopped reconciliation. A graceful
reload alone does not prove cache deletion with the pinned components. No live
path or ADR 0026 amendment is approved by this evaluation.

The [privacy contract](crowdsec-http-privacy.md#live-retention-and-failure) owns
all retention, lease, SQL, and failure bounds. This proposal changes none of
them. The owner chose [batched stopped cleanup](crowdsec-retention-batching.md).
Zero downtime requires consumer changes and remains outside that scope.

## Smallest approach and its limits

Local first-party source establishes these boundaries:

| Pinned source                                                                                | Finding                                                                                      |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| Bouncer `crowdsec/crowdsec.go:137`, `internal/core/core.go:108`, `internal/core/store.go:29` | Provision creates a new core with a new empty store; no shared store is reused.              |
| Bouncer `internal/bouncer/stream.go:51`, `internal/core/core.go:194`                         | The new stream starts with `startup=true`, asynchronously.                                   |
| Bouncer `crowdsec/crowdsec.go:301`, `internal/core/decisions.go:104`                         | Start does not wait for the snapshot. Empty-cache lookups allow requests before bans arrive. |
| Bouncer `crowdsec/crowdsec.go:286`, `internal/core/core.go:209`                              | Cleanup cancels and joins workers, but does not clear the old store.                         |
| Caddy `caddy.go:362`, `modules/caddyhttp/app.go:853`                                         | New apps start before old apps stop; reload does not wait for old HTTP requests to drain.    |

Consequently, reload success proves neither enforcement readiness nor absence
from every old cache. Long requests may retain old handlers and stores. Forcing
those connections closed would violate the uninterrupted-request goal. A config
ID, load timestamp, module uptime, or registered LAPI pull cannot supply the
missing proof. `/crowdsec/check` returns only a blocked boolean, so overlapping
fresh decisions can hide stale ones. Health checks perform a separate lookup.
The module instance ID is generated from whole-second time and is not a unique
reload acknowledgement (`internal/core/core.go:255`).

Both production Caddyfiles set `admin off`. Enabling administration would add a
control surface without fixing these defects. Do not expose an admin endpoint or
introduce a socket for this proposal. The source inspection supports graceful
listener reuse, not a claim of zero failed requests in this deployment.

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

Read from the local module cache; URLs were not fetched. Source inspection is
not runtime verification. No secondary-source claim supports this decision.

- [Pinned bouncer core](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/ffdcb7c6f861/internal/core/core.go)
- [Store](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/ffdcb7c6f861/internal/core/store.go)
- [Decision processing](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/ffdcb7c6f861/internal/core/decisions.go)
- [Stream](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/ffdcb7c6f861/internal/bouncer/stream.go)
- [Lifecycle](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/ffdcb7c6f861/crowdsec/crowdsec.go)
- [Admin API](https://github.com/hslatman/caddy-crowdsec-bouncer/blob/ffdcb7c6f861/crowdsec/adminapi.go)
- [Caddy 2.11.9 configuration lifecycle](https://github.com/caddyserver/caddy/blob/v2.11.9/caddy.go#L362-L375)
- [Caddy HTTP shutdown](https://github.com/caddyserver/caddy/blob/v2.11.9/modules/caddyhttp/app.go#L853-L876)
