# 0002 — Go API + Nuxt SSR with one shared renderer

Status: Accepted (2026-08-01)

## Context

The resume layout must render identically in three places: the editor's live
preview, the SEO/GEO public page, and the PDF. Maintaining two renderers (e.g.
Go templates + Vue components) guarantees drift.

## Decision

Write the renderer **once** as pure Vue components. Nuxt runs it client-side
(editor preview) and server-side (public pages). The Go server owns auth, the
JSON API, SSE, and produces PDFs/og-images by printing an internal Nuxt route
with headless Chromium.

## Consequences

- Node runs in production beside Go (two app services + Caddy).
- Preview = public page = PDF by construction; golden snapshots + screenshot
  diffs protect this.
- Validated by an equivalent architecture in a production resume-builder
  application (client React store + Astro SSR public pages + server PDF from the
  same JSON).
