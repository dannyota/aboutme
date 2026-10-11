# Go server

`apps/server` is the Go API module. Intended component boundaries live in the
[system design](../../docs/design/system.md) and
[repository design](../../docs/design/repository.md). The
[current-state architecture](../../docs/architecture.md) describes the running
system.

## Implemented surface

See [authentication and agent access](../../docs/architecture/auth-and-agent-access.md), [resume data](../../docs/architecture/resume-data.md), and [rendering and editor](../../docs/architecture/rendering-and-editor.md).

## Source map

| Path | Purpose |
| --- | --- |
| `cmd/server/` | Runtime wiring and one-shot command entry points |
| `cmd/db-setup/`, `cmd/migrate/` | Database roles and migrations |
| `internal/config/`, `internal/api/` | Validated configuration and HTTP policy |
| `internal/auth/`, `internal/authmail/`, `internal/oauthsrv/`, `internal/mcpapi/` | User authentication, mail, and agent access |
| `internal/resume/`, `internal/resumeapi/`, `internal/store/` | Resume domain, HTTP handlers, and generated data access |
| `internal/media/`, `internal/mediacleanup/`, `internal/privacyretention/` | Private media and lifecycle workers |
| `internal/directrender/`, `internal/renderjob/`, `internal/renderprocess/` | Public HTML transport and browser rendering |
| `migrations/`, `sql/queries.sql`, `sqlc.yaml` | Relational schema and sqlc sources |

## Data sources

See [resume data](../../docs/architecture/resume-data.md#implemented-resume-data-layer).

The root `go.work` connects this module to the generated schema module at
`packages/schema/gen/go`. Run Go commands from `apps/server` so the workspace is
found upward.

## Configuration

The server requires `DATABASE_URL`, `ENV`, and `PUBLIC_ORIGIN`. `PORT` defaults
to `8080`, `LISTEN_HOST` to `127.0.0.1`, and `LOG_LEVEL` to `info`.
`TRUSTED_PROXY_CIDRS` is required in staging and production. Provider
credentials are required there only for providers enabled by
`PROVIDER_LOGIN_ENABLED`. See [`.env.example`](../../.env.example) and
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

Run these commands from this directory. Database-backed gates use the one shared
container started by `make test-db-up` from the repository root.

Run one-shot jobs from this directory. The
[privacy runbook](../../docs/runbooks/privacy.md#commands-and-schedules) defines
the required environment, command results, and failure handling. The
[scheduled-job architecture](../../docs/architecture/resume-data.md#scheduled-jobs)
distinguishes production timers from the retained AWS command.

```sh
go run ./cmd/server idempotency-expiry-sweep
go run ./cmd/server media-deletion-sweep
go run ./cmd/server media-orphan-sweep --dry-run
go run ./cmd/server privacy-retention-sweep
go run ./cmd/server release-snapshot-sweep
```
