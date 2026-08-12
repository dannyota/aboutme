# aboutme

aboutme is an AGPL-3.0 resume builder and hosted display service. A person can
build resumes in one account and publish each resume at its own URL. Accounts do
not have public profile pages.

Status: Phase 2A Tasks 1–11 are on `main`. Task 12 and both phase gates remain.
Authentication and session management are implemented, but P1.1 still has a
known settings-page mismatch. Resume HTTP, editing, rendering, publishing, and
production deployment remain planned. See the
[delivery index](docs/plans/implementation-plan.md#delivery-index).

The intended product and architecture live in the
[Draft v4 design](docs/design/README.md). It is not yet approved. The
[current-state architecture](docs/architecture.md) records what exists now.

## Implemented now

- Google, GitHub, and LinkedIn sign-in; explicit provider linking; opaque
  sessions; CSRF protection; current-user and session-management APIs.
- Health and readiness probes, request bounds, security and cache headers,
  trusted-proxy client-IP handling, and rate limiting.
- A committed TypeScript API client generated from OpenAPI, with contract and
  drift checks.
- Goose-only relational migrations, sqlc data access, immutable resume schema
  v1, bounded validation, owner-scoped resume store operations, revision
  compare-and-swap, transactional idempotency, projection, and backfill.
- Twenty draft template preset JSON files.

## Planned v1

- A section-based editor with live preview and debounced autosave.
- Per-resume publishing, PDF control, search indexing control, and clean slug
  URLs.
- One Vue renderer for editor preview, public HTML, PDF, and generated images.
- Server-rendered public pages, structured data, sitemap, markdown resumes, and
  `llms.txt`.
- Private, authorization-gated resume photos.

Flutter is deferred until after the web service launches.

## Repository

| Path              | Responsibility                                                                 |
| ----------------- | ------------------------------------------------------------------------------ |
| `apps/server`     | Go API, authentication, resume domain/store, and future public and render work |
| `apps/web`        | Nuxt SSR, authenticated UI, generated API client, and future editor/renderer   |
| `apps/mobile`     | Deferred Flutter client                                                        |
| `packages/schema` | Resume JSON Schema, immutable releases, generated types, fixtures, and presets |
| `deploy`          | Compose deployment and Caddy route table                                       |
| `docs`            | Design, ADRs, API contract, plans, guides, standards, and runbooks             |

See the [documentation map](docs/README.md) for authority and lifecycle rules.

## Development

Use the pinned Node version in [`apps/web/.nvmrc`](apps/web/.nvmrc) and the Go
version in [`apps/server/go.mod`](apps/server/go.mod).

```sh
npm ci
(cd apps/web && npm ci)
make dev-native
```

Open `http://localhost:20080`. The command starts the one shared PostgreSQL
container and native Go, Nuxt, and Caddy processes. Stop only the native
processes with `make dev-native-down`.

See the [native development runbook](docs/runbooks/native-development.md) for
status, logs, ports, and database rules. Run `make check` for the fast local
gate and `make ci` before handoff.

## Self-hosting

The current Compose artifact supports local deployment evaluation. It does not
yet provide the HTTPS/443 path required for authentication UAT or safe public
exposure. See the [self-hosting guide](docs/guides/self-hosting.md) for its
exact scope and limits.

## License

[AGPL-3.0](LICENSE). A modified version offered as a network service must make
its corresponding source available to that service's users.
