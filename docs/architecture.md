# Current-state architecture

This document describes the repository at `main` commit `8ccf0d2`, verified on
2026-08-12. The [design](design/README.md) owns intended behavior. The
[roadmap](plans/implementation-plan.md) owns delivery state and gates.

## Running system

```mermaid
graph LR
    B[Browser] --> C[Caddy]
    C --> W[Nuxt SSR]
    C --> G[Go API]
    G --> P[(PostgreSQL)]
    G --> O[Google, GitHub, and LinkedIn]
```

| Component  | Implemented responsibility                                                                   |
| ---------- | -------------------------------------------------------------------------------------------- |
| Caddy      | One-origin routing, forwarding-header removal, and canonical client-IP delivery to Go        |
| Go server  | Health, authentication, sessions, users, resume domain/store primitives, and request policy  |
| Nuxt       | Landing and login pages, authenticated user state, session settings, and typed API transport |
| PostgreSQL | Auth records, sessions, users, resume aggregates, slug tombstones, and idempotency records   |

Daily development runs Go, Nuxt, and Caddy as native processes at
`http://localhost:20080`. They use the `aboutme_dev` database in the one shared
`aboutme-test-db` container. See the
[native development runbook](runbooks/native-development.md).

The Compose deployment has four long-lived containers plus a one-shot migration
container. PostgreSQL is not published to the host. Caddy is the only published
service. The current Compose Caddyfile serves HTTP; this is suitable for
deployment smoke checks but does not yet satisfy the P9 HTTPS-on-443 UAT
contract.

## Implemented HTTP surface

The [OpenAPI document](api/openapi.yaml) is the exact HTTP authority. The
implemented surface includes:

- `GET` and `HEAD` health and readiness probes;
- Google and LinkedIn OpenID Connect plus GitHub OAuth login;
- authenticated, CSRF-protected provider link and reauthentication starts;
- current-user lookup, logout, session listing, per-session revoke, and
  logout-everywhere;
- JSON envelopes, request IDs, body bounds, security and cache headers,
  trusted-proxy client-IP handling, and rate limiting.

The committed TypeScript API types are generated from OpenAPI. The web client
uses those types through `openapi-fetch`, and `make api-check` detects drift.

Resume HTTP routes are not implemented.

## Implemented resume data layer

Phase 2A Tasks 1–12 are present, but both phase gates remain. The landed slice
provides:

- immutable resume schema v1, retained v1 Go and TypeScript types, and released,
  accepted, and emitted version registries that currently contain only version
  1;
- hand-written, append-only goose migrations as the sole relational schema
  source;
- sqlc-generated data access from those migrations and `sql/queries.sql`;
- schema-derived bounds, aggregate validation, and a bounded codec;
- owner-scoped CRUD primitives, a three-resume cap, and revision
  compare-and-swap (CAS) that persists the complete document aggregate;
- a transactional idempotency primitive keyed by user, caller-supplied route,
  UUID key, and caller-supplied body hash; it stores a JSON status/body result
  and reaps the active user's expired records on their next execution;
- pure document projection, explicit adjacent-converter machinery, a one-row CAS
  backfill primitive, independent write, migration, and bounds suites, and a
  bounds completeness guard.

These are data-layer primitives, not resume HTTP behavior. Production has no
adjacent version pair while v1 is the only released version. No process invokes
the backfill candidate query, and no scheduled idempotency-expiry sweep exists.
The Draft v4 operation tuple and request fingerprint, stored response headers,
bounded cleanup and capacity accounting, HTTP retry behavior, and fixed
customization-delta allowlist remain P2B work. P8 owns the authoritative hourly
global expiry sweep.

Twenty template preset JSON files are committed. Their design contract remains
draft, and the renderer, sanitizer, and licensed font assets have not landed.

## Known delivery gaps

- P1.1's settings-page POST flow, provider-bound navigation, CSRF retry, and
  deterministic identity order are implemented. The bounded browser proof and
  both phase gates remain before P1.1 closes.
- P2A Task 12 traceability and downstream handoffs are landed. Its phase defect
  review and exact-candidate acceptance remain before P2A closes.
- The current Compose route serves HTTP. P9 requires the complete image-based
  deployment at an HTTPS origin on port 443. The UAT harness must close this gap
  before acceptance can run.

## Not implemented

Resume HTTP and media, sanitizing and rendering, the editor, public publishing,
Server-Sent Events, PDF and image rendering, privacy jobs, production
infrastructure, staging, production deployment, and Flutter remain planned.
