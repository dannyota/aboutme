# Nuxt web app

`apps/web` is a self-contained Nuxt 4 and Vue 3 package. It is not part of an
npm workspace. Intended UI and renderer boundaries live in the
[web design](../../docs/design/web.md).

## Implemented surface

See [rendering and editor](../../docs/architecture/rendering-and-editor.md).

## Source map

| Path | Purpose |
| --- | --- |
| `app/pages/` | Public and authenticated routes |
| `app/components/ui/` | Generated UI primitives |
| `app/components/app/` | Shared application composites |
| `app/components/editor/`, `app/editor/` | Editor surfaces, commands, and reconciliation |
| `app/components/resume/` | Pure resume rendering |
| `app/i18n/`, `app/composables/useLocale.ts` | Interface copy and language state |
| `app/api/` | Typed API transport and generated OpenAPI types |
| `server/routes/`, `server/utils/`, `server/workers/` | SSR, private print routes, and bounded render workers |
| `test/`, `e2e/` | Unit, renderer, and browser checks |

[Rendering and editor](../../docs/architecture/rendering-and-editor.md) describes
application chrome, the bilingual workspace and settings, preview, public SSR,
and exports. [Authentication and agent access](../../docs/architecture/auth-and-agent-access.md)
describes the session and consent boundaries. The product and visual direction
live in [`PRODUCT.md`](../../PRODUCT.md) and [`DESIGN.md`](../../DESIGN.md).

## UI conventions

The application UI has three layers:

- Generated primitives live under `app/components/ui/`. Add them through
  `scripts/ui-add.sh`; do not hand-edit generated primitive files except
  for the token-colored variant edits that
  [ADR 0019](../../docs/adr/0019-application-ui-toolkit.md)
  allows under a guard test. Re-apply those edits after a regeneration.
  Their `data-slot` attributes mark primitive boundaries.
- Shared application composites live under `app/components/app/`.
- Pages and editor panels compose those layers into product surfaces.

The [editor architecture](../../docs/architecture/rendering-and-editor.md#implemented-authenticated-editor)
defines field commit behavior.

Tests query accessible roles and labels first, then retained stable hooks. Do
not couple tests to tags, utility classes, or list positions. Dialog and menu
content teleports to `body`, so tests mount surfaces with a real body target and
clean it after each case.

## API client

`app/api/generated/openapi.ts` is generated and must not be hand-edited.
`app/api/client.ts` is the typed `openapi-fetch` transport. Regenerate and check
the client from the repository root:

```sh
make api-gen
make api-check
```

## Commands

Run package commands from this directory:

- `npm run dev`: start Nuxt on its default development port.
- `npm run build`: build the production application.
- `npm run lint`: run ESLint.
- `npm run typecheck`: run `vue-tsc --build --noEmit`.
- `npm run test`: run Vitest.
- `npm run api:gen`: regenerate the committed OpenAPI types.
- `npm run api:drift`: check generated types without changing the tree.

Daily full-stack development uses `make dev-native`, which runs Nuxt on port
`20030` behind Caddy at `http://localhost:20080`. See the
[native development runbook](../../docs/runbooks/native-development.md).

`NUXT_PUBLIC_API_BASE` defaults to `/api/v1`. Browser calls stay on the Caddy
origin.
