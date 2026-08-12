# 7. Repository boundaries

The repository is a monorepo with explicit authorities and one-way dependency
rules. This page describes ownership; [`../architecture.md`](../architecture.md)
describes what is implemented now.

| Path                          | Responsibility and authority                                                     |
| ----------------------------- | -------------------------------------------------------------------------------- |
| `apps/server/`                | Go HTTP, auth, domain stores, publishing, realtime, media, and bounded rendering |
| `apps/web/`                   | Nuxt SSR, authenticated UI, editor, and the shared Vue renderer                  |
| `apps/mobile/`                | Deferred Flutter client                                                          |
| `packages/schema/`            | Resume JSON Schema, immutable releases, generators, fixtures, and preset JSON    |
| `apps/server/migrations/`     | Sole relational schema source; append-only goose SQL                             |
| `apps/server/sql/queries.sql` | sqlc query source                                                                |
| `docs/api/openapi.yaml`       | Implemented HTTP contract                                                        |
| `docs/design/`                | Intended product and architecture                                                |
| `docs/adr/`                   | Proposed and accepted decision rationale                                         |
| `docs/plans/`                 | Execution order, task ownership, gates, and acceptance traceability              |
| `deploy/`                     | UAT/self-host deployment artifacts and future production infrastructure          |

Generated files are committed but never hand-edited. Document types derive from
JSON Schema. Store types derive from migrations and sqlc queries. Web API types
derive from OpenAPI. A generator drift check is paired with a conformance test;
byte-identical generation alone does not prove fidelity.

Dependency direction is deliberate:

- Editor code may depend on renderer code. Renderer code never depends on the
  editor, store, or API client.
- HTTP handlers depend on domain boundaries. Domain code never depends on HTTP.
- PostgreSQL and object storage are reachable only through Go.
- Nuxt never receives direct database credentials.
- Caddy is the only viewer-facing origin and client-IP boundary.

Repository trees change during implementation, so design pages do not carry a
speculative full folder tree. New packages must preserve these boundaries and
update the current-state architecture when they land.
