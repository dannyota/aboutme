# apps/web

Nuxt 4 / Vue 3 — editor SPA, SSR public pages, and (later) the single shared
resume renderer (`components/resume/`). Layout and responsibilities: spec
§5/§7 ([`docs/specs/aboutme-design.md`](../../docs/specs/aboutme-design.md)).

Self-contained npm package — not part of a workspaces setup. Run all commands
from this directory.

## Scripts

| Command              | Purpose                                    |
| -------------------- | ------------------------------------------ |
| `npm run dev`        | Dev server on port 3000                    |
| `npm run build`      | Production build                           |
| `npm run lint`       | ESLint (Google TypeScript style)           |
| `npm run typecheck`  | `vue-tsc --noEmit`                         |
| `npm run test`       | Vitest (component tests, Nuxt environment) |

## Runtime config

`NUXT_PUBLIC_API_BASE` — Go API base path, defaults to `/api/v1` (same-origin;
Caddy routes `/api/v1/*` to the server — see spec §2 route table).

## Status

C1 skeleton: SSR landing page (`/`) rendering a static placeholder component,
ESLint + typecheck + Vitest wired. Editor, public resume pages, and the
isolated `components/resume/` renderer arrive in later phases.
