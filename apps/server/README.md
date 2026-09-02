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

Server-Sent Events, print and image rendering, and the privacy lifecycle
workers are not implemented. The
[current-state architecture](../../docs/architecture.md) records the exact
boundary.

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
