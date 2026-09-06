# Go server

`apps/server` is the Go API module. Intended component boundaries live in the
[system design](../../docs/design/system.md) and
[repository design](../../docs/design/repository.md). The
[current-state architecture](../../docs/architecture.md) records the landed
slice.

## Implemented surface

- `GET` and `HEAD` liveness and readiness probes.
- Google and LinkedIn OpenID Connect plus GitHub OAuth login, and
  email-and-password registration, verification, login, reset,
  reauthentication, and add/change.
- Authenticated provider linking and reauthentication starts.
- Opaque PostgreSQL-backed sessions, rotation with bounded predecessor grace,
  CSRF, current-user lookup, logout, session listing, per-session revoke, and
  logout-everywhere.
- Request and body bounds, trusted-proxy client-IP extraction, rate limiting,
  security and cache headers, JSON envelopes, and structured logging.
- Resume and idempotency tables, bounded document validation and codec,
  owner-scoped store operations, revision compare-and-swap, transactional
  idempotency, pure projection, and compare-and-swap backfill.
- The private resume HTTP surface: resume, entry, section, structure,
  customization, and photo operations over private filesystem or S3 media.
- Publish, unpublish, rename, and slug delete, with public JSON, photo, HTML,
  Markdown, sitemap, robots, and llms.txt reads gated by publish state.
- First-party OAuth 2.1 (dynamic client registration, S256 consent, token
  rotation and revocation, discovery) and a bearer-authenticated MCP server
  with fifteen resume tools.
- Encrypted transactional authentication mail delivered through SES or a
  loopback capture server.

Server-Sent Events, private print redemption, and direct public rendering are
implemented across the Go and web components; Chromium is initialized only by
the normal server path. Four privacy and media lifecycle jobs are available as
one-shot commands:
`idempotency-expiry-sweep`, `media-deletion-sweep`,
`media-orphan-sweep` (with optional `--dry-run`), and
`privacy-retention-sweep`. They run before HTTP and Chromium initialization,
load only the database configuration they need (media commands also load the
media backend), and emit a fixed identifier-free JSON result. Each run is
bounded to 30 minutes, uses an advisory overlap lock, and joins work after
cancellation. The [privacy runbook](../../docs/runbooks/privacy.md) describes
the budgets, failure handling, and Phase 10 scheduler handoff.

## Data sources

Hand-written, append-only files in `migrations/` are the sole relational schema
source. Goose applies them through `cmd/migrate`, and sqlc reads the same files
with `sql/queries.sql`. [ADR 0010](../../docs/adr/0010-goose-only-migrations.md)
records this rule.

The root `go.work` connects this module to the generated schema module at
`packages/schema/gen/go`. Run Go commands from `apps/server` so the workspace
is found upward.

## Configuration

The server requires `DATABASE_URL`, `ENV`, and `PUBLIC_ORIGIN`. `PORT` defaults
to `8080`, `LISTEN_HOST` to `127.0.0.1`, and `LOG_LEVEL` to `info`.
`TRUSTED_PROXY_CIDRS` and provider credentials are required in staging and
production. See [`.env.example`](../../.env.example) and
[`internal/config/config.go`](internal/config/config.go) for the validated
contract.

## Development

Use the repository's
[native development runbook](../../docs/runbooks/native-development.md) for the
one-origin stack. For server-only checks:

```sh
go build ./...
go vet ./...
go test ./...
```

Run these commands from this directory. Database-backed gates use the one
shared container started by `make test-db-up` from the repository root.

Run a one-shot job from this directory with the same environment as the
server. The command exits nonzero on an invalid command, invalid configuration,
or failed job; its stdout still contains the fixed result when the worker
returns one. Media commands require the configured private media backend.

```sh
go run ./cmd/server idempotency-expiry-sweep
go run ./cmd/server media-deletion-sweep
go run ./cmd/server media-orphan-sweep --dry-run
go run ./cmd/server privacy-retention-sweep
```
