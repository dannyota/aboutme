# aboutme

aboutme is an AGPL-3.0 resume builder and hosted display service. A person can
build resumes in one account and publish each resume at its own URL. Accounts do
not have public profile pages.

Status: authentication, the resume API and private media, the authenticated
editor, and publishing with server-rendered public pages are on `main`.
User-authorized MCP agent access is in its phase review. Realtime updates, PDF
and image export, the privacy lifecycle workers, and production deployment
remain planned. The [current-state architecture](docs/architecture.md) records
exactly what exists.

The intended product and architecture live in the
[Approved v4 design](docs/design/README.md).

## Implemented now

- Google, GitHub, and LinkedIn sign-in; email-and-password registration,
  verification, login, reset, and reauthentication; explicit provider linking;
  opaque sessions; CSRF protection; session-management APIs.
- Health and readiness probes, request bounds, security and cache headers,
  trusted-proxy client-IP handling, and rate limiting.
- A committed TypeScript API client generated from OpenAPI, with contract and
  drift checks.
- Goose-only migrations, sqlc data access, immutable resume schemas v1 and v2,
  bounded validation, owner-scoped resume operations, revision compare-and-swap,
  and transactional idempotency.
- The private resume HTTP surface: resume, entry, section, structure,
  customization, and photo operations over private media.
- One pure Vue renderer with twenty template presets and a licensed self-hosted
  font catalog, shared by the editor preview and the public page.
- The authenticated editor with debounced autosave, conflict reconciliation,
  template apply, and private photo crop.
- Per-resume publishing with clean slug URLs, server-rendered public HTML,
  JSON-LD, Markdown, sitemap, robots, and `llms.txt`, and immediate revocation.
- User-authorized agent access: first-party OAuth 2.1 with PKCE and a
  bearer-authenticated MCP server with fifteen resume tools (in phase review).

## Planned v1

- Publish-flow UX and live public-page refresh over Server-Sent Events.
- PDF and image export.
- Privacy lifecycle workers: media deletion, orphan reconciliation, and
  retention.
- Production infrastructure and deployment.

Flutter is deferred until after the web service launches.

## Repository

| Path              | Responsibility                                                                 |
| ----------------- | ------------------------------------------------------------------------------ |
| `apps/server`     | Go API: authentication, resume domain, media, publish, public read, OAuth, MCP |
| `apps/web`        | Nuxt SSR, authenticated editor, renderer, public pages, generated API client   |
| `apps/mobile`     | Deferred Flutter client                                                        |
| `packages/schema` | Resume JSON Schema, immutable releases, generated types, fixtures, and presets |
| `deploy`          | Compose deployment, Caddy route table, and the trusted browser harness         |
| `docs`            | Design, ADRs, API contract, plans, guides, standards, and runbooks             |

See the [documentation map](docs/README.md) for authority and lifecycle rules.

## Development

Use the exact local and CI tool versions in [`.tool-versions`](.tool-versions).
The Node and Go declarations in component manifests mirror that file.

```sh
npm ci
(cd apps/web && npm ci)
make tools-check ARGS=dev
make dev-native
```

Open `http://localhost:20080`. The command starts the one shared PostgreSQL
container and native Go, Nuxt, and Caddy processes. Stop only the native
processes with `make dev-native-down`.

See the [native development runbook](docs/runbooks/native-development.md) for
status, logs, ports, and database rules. Run `make check` for the fast local
gate. Independent contributors run `make ci` before a pull request; in a
coordinated worker session, the integration owner runs it before integration.

## Self-hosting

The current Compose artifact supports local deployment evaluation. It does not
yet provide the HTTPS/443 path required for authentication UAT or safe public
exposure. See the [self-hosting guide](docs/guides/self-hosting.md) for its
exact scope and limits.

## License

[AGPL-3.0](LICENSE). A modified version offered as a network service must make
its corresponding source available to that service's users.
