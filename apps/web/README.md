# Nuxt web app

`apps/web` is a self-contained Nuxt 4 and Vue 3 package. It is not part of an
npm workspace. Intended UI and renderer boundaries live in the
[web design](../../docs/design/web.md).

## Implemented surface

- Server-rendered landing and login pages.
- Client-side authenticated user state.
- Session and device settings with CSRF-aware mutations.
- A committed TypeScript API client generated from
  [`docs/api/openapi.yaml`](../../docs/api/openapi.yaml).

The settings page still uses GET links to start provider linking and
reauthentication. The implemented server contract requires authenticated POST,
then navigation to the returned authorization URL. This P1.1 defect remains
open.

The editor, public resume pages, sanitizer, and shared resume renderer have not
landed.

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
