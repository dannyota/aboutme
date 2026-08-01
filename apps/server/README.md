# apps/server

Go API server — auth (Google/GitHub/LinkedIn OAuth), resume documents,
publishing, SSE, PDF/og-image pipeline.

Module `github.com/dannyota/aboutme/apps/server`, Go 1.26. Full layout and
responsibilities: spec §7
([`docs/specs/aboutme-design.md`](../../docs/specs/aboutme-design.md)).

## Current scaffold (Phase 0B — Task B1)

```text
cmd/server/     main.go — wiring only: config -> store -> router -> HTTP
                server with graceful shutdown on SIGINT/SIGTERM
internal/config env parsing + validation (PORT, DATABASE_URL, LOG_LEVEL, ENV)
internal/store  pgx/v5 pool (capped at 20 conns), Ping, context-aware Close
internal/api    router, middleware (request ID, slog logging, 256 KB body
                limit), response envelope, /healthz + /readyz
```

Everything else in spec §7 (`internal/auth`, `internal/resume`,
`internal/publish`, `internal/realtime`, `internal/render`, `internal/media`,
`cmd/migrate`, `sql/`, `migrations/`) lands in later tasks.

## Config

| Var | Required | Default | Notes |
| --- | --- | --- | --- |
| `PORT` | no | `8080` | 1-65535 |
| `DATABASE_URL` | yes | — | Postgres connection string |
| `LOG_LEVEL` | no | `info` | `debug`, `info`, `warn`, `error` |
| `ENV` | yes | — | `dev`, `staging`, `prod` |

## Run locally

```sh
go build ./...
go vet ./...
go test ./...

PORT=8080 DATABASE_URL=postgres://user:pass@localhost:5432/aboutme \
  LOG_LEVEL=info ENV=dev go run ./cmd/server
```

`GET /healthz` is liveness only (always 200, never touches the database).
`GET /readyz` checks the database with a short timeout; 503 with the
standard error envelope when it is unreachable.
