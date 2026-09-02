# Nuxt web app

`apps/web` is a self-contained Nuxt 4 and Vue 3 package. It is not part of an
npm workspace. Intended UI and renderer boundaries live in the
[web design](../../docs/design/web.md).

## Implemented surface

- Server-rendered entry, password-authentication, and public resume pages.
- Client-side authenticated user state, resume list, and optimistic-save editor.
- Session, device, and connected-agent settings with CSRF-aware mutations.
- A pure resume renderer shared by the editor preview and public SSR.
- A committed TypeScript API client generated from
  [`docs/api/openapi.yaml`](../../docs/api/openapi.yaml).

## UI conventions

The application UI has three layers:

- Generated primitives live under `app/components/ui/`. Add them through
  `scripts/ui-add.sh`; never hand-edit generated primitive files. Their
  `data-slot` attributes mark primitive boundaries.
- Shared application composites live under `app/components/app/`.
- Pages and editor panels compose those layers into product surfaces.

Text fields commit on blur or Enter. Empty text unsets a defined value,
unchanged text emits nothing, and Escape restores the current model value.
Enumerations, checkboxes, switches, colors, and numbers commit on change.

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
