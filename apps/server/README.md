# apps/server

Go API server, module `github.com/dannyota/aboutme/apps/server` (Go 1.26).
Intended layout and responsibilities are in the
[design spec](../../docs/specs/aboutme-design.md); the current repository state
is in [`docs/architecture.md`](../../docs/architecture.md).

## Implemented surface

- `/healthz` liveness and `/readyz` database readiness.
- Google and LinkedIn OIDC plus GitHub OAuth2 login, explicit provider linking,
  and verified-email collision protection.
- Opaque PostgreSQL-backed sessions, rotation/grace handling, CSRF, `/me`,
  logout, device listing, per-session revoke, and logout-everywhere.
- Request/body bounds, trusted-proxy client-IP extraction, rate limiting,
  security/cache headers, JSON envelopes, and structured logging.
- Declarative SQL → sqlc, Atlas-authored/Goose-applied migrations, migration
  locking, drift checks, and live-database test helpers.

Resume storage is active Phase 2A work on an isolated branch and is not on
`main` yet. Resume HTTP, publish, realtime, render, and media packages remain
future phases.

## Configuration

The server requires `DATABASE_URL`, `ENV`, and `PUBLIC_ORIGIN`. `PORT` defaults
to `8080`, `LISTEN_HOST` to `127.0.0.1`, and `LOG_LEVEL` to `info`.
`TRUSTED_PROXY_CIDRS` and all three provider credential pairs are required in
staging/production and optional in development. See [`.env.example`](../../.env.example)
and [`internal/config/config.go`](internal/config/config.go) for the complete,
validated contract.

## Run locally

Run Go commands from this directory so the root `go.work` supplies the schema
module:

```sh
go build ./...
go vet ./...
go test ./...

PORT=8080 LISTEN_HOST=127.0.0.1 \
  DATABASE_URL=postgres://user:pass@localhost:5432/aboutme \
  PUBLIC_ORIGIN=http://localhost:8080 LOG_LEVEL=info ENV=dev \
  go run ./cmd/server
```

For the full one-origin stack, use `make dev` from the repository root.
