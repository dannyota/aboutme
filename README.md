# aboutme

Open-source resume builder and hosted display service. Build a polished resume
in a live editor, then publish it at a clean, memorable URL —
`aboutme.vn/your-name` — with first-class SEO and AI-answer-engine (GEO)
support.

**Status: Phase 2A (resume domain/store) is in progress.** Phase 0 foundations
and Phase 1 authentication/session work are merged. The reviewed Phase 2A
checkpoint through Task 7 is now integrated into `main`; Tasks 2b, 6a/6b, 8–12,
adversarial suites, and phase gates still remain. Follow the
[numbered delivery index](docs/plans/implementation-plan.md#numbered-delivery-index)
for the current checkpoint and next work.

The current design authority is
[`docs/specs/aboutme-design.md`](docs/specs/aboutme-design.md). Its header still
marks it `DRAFT`; accepted ADRs record decisions that supersede individual
clauses.

## Implemented now

- Versioned resume-schema generation and drift checks, OpenAPI lint/contract
  tests, Go/Nuxt scaffolds, migration tooling, CI gates, and the podman compose
  development stack. Generated OpenAPI TypeScript client tooling is a known
  follow-up before P2B.
- Google, GitHub, and LinkedIn sign-in; opaque sessions, CSRF protection,
  device/session management, explicit account linking, and matching Nuxt pages.
- Health/readiness endpoints, trusted-proxy client-IP handling, rate limiting,
  security/cache headers, and the Caddy route boundary.
- Resume tables, schema-derived validation, bounded codec/store operations,
  revision CAS, and transactional idempotency primitives. No resume HTTP route
  is exposed yet.

Resume CRUD HTTP endpoints, the editor/renderer, publishing, realtime, print,
cloud infrastructure, and production deployment remain planned work.

## Planned v1 highlights

- **Editor** — section-based resumes (work, education, skills, custom …), rich
  text, deep design customization, templates; instant live preview with
  debounced autosave.
- **Publishing** — each resume gets its own URL (`/{slug}`); users stay
  invisible. Per-resume toggles: public on/off, PDF download, SEO+GEO indexing.
- **SEO/GEO** — server-side rendering, JSON-LD (`ProfilePage`/`Person`),
  og-image, sitemap, and a markdown variant (`/{slug}.md`) + `llms.txt` for AI
  engines.
- **Auth** — Google / GitHub / LinkedIn sign-in only (no passwords).
- **PDF** — pixel-identical server render of the same layout engine.
- **Mobile** — Flutter app planned against the same versioned API.

## Architecture (short version)

Go API + Nuxt (Vue 3) SSR sharing one resume renderer; PostgreSQL with jsonb
resume documents; SSE for live refresh; headless Chromium for PDF/og-image. See
[`docs/README.md`](docs/README.md) for the documentation map.

| Directory         | Contents                                                          |
| ----------------- | ----------------------------------------------------------------- |
| `apps/server`     | Go API (auth, resumes, publishing, SSE, PDF pipeline)             |
| `apps/web`        | Nuxt 4 / Vue 3 (editor + public pages + the shared renderer)      |
| `apps/mobile`     | Flutter app (placeholder)                                         |
| `packages/schema` | Resume-document JSON Schema → generated Go/TS types (Dart in P11) |
| `deploy`          | podman compose (dev/self-host); AWS deployment is planned         |
| `docs`            | Specs, ADRs, API contract, runbooks                               |

## Development

Use the pinned Node version in [`apps/web/.nvmrc`](apps/web/.nvmrc) for root
docs tooling and the Nuxt app. The server's pinned Go version is declared in
[`apps/server/go.mod`](apps/server/go.mod). Flutter tooling is deferred with
P11.

```sh
npm ci            # install doc tooling
make docs-lint    # lint markdown + formatting
make docs-fmt     # format + fix + re-lint
```

## Self-hosting

See the [podman compose self-hosting guide](docs/self-hosting.md). It describes
the currently runnable slice and its authentication/TLS limitations.

## License

[AGPL-3.0](LICENSE). If you run a modified version of this software as a
service, you must make your modified source available to its users.
