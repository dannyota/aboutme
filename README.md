# aboutme

Open-source resume builder and hosted display service. Build a polished resume
in a live editor, then publish it at a clean, memorable URL —
`aboutme.vn/your-name` — with first-class SEO and AI-answer-engine (GEO)
support.

**Status: design phase.** The approved design lives in
[`docs/specs/aboutme-design.md`](docs/specs/aboutme-design.md); implementation
is starting from that spec.

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

| Directory         | Contents                                                     |
| ----------------- | ------------------------------------------------------------ |
| `apps/server`     | Go API (auth, resumes, publishing, SSE, PDF pipeline)        |
| `apps/web`        | Nuxt 4 / Vue 3 (editor + public pages + the shared renderer) |
| `apps/mobile`     | Flutter app (placeholder)                                    |
| `packages/schema` | Resume-document JSON Schema → generated Go/TS/Dart types     |
| `deploy`          | podman compose (dev/self-host) + AWS deployment              |
| `docs`            | Specs, ADRs, API contract, runbooks                          |

## Development

Requires Node (via fnm) for docs tooling; Go/Nuxt/Flutter toolchains arrive with
their apps.

```sh
npm ci            # install doc tooling
make docs-lint    # lint markdown + formatting
make docs-fmt     # format + fix + re-lint
```

## Self-hosting

`docs/self-hosting.md` (podman compose) — expands as the apps land.

## License

[AGPL-3.0](LICENSE). If you run a modified version of this software as a
service, you must make your modified source available to its users.
