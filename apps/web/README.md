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

The landing page server-renders a compiled-in sample resume through the shared
renderer and places its public seal on the sheet without fetching data. The
authenticated shell, built from `AppShell` and the shared `components/app`
composites, switches between signed-out and signed-in navigation after the
client session read. `AccountMenu` and `ThemeToggle` provide account actions
and the persisted theme choice; `PageHeader`, fields, status/loading states,
empty states, menus, and dialogs keep page behavior and focus handling shared.

`EditorShell` provides the top bar, account menu, publish action, tool rail,
resume outline, preview, and inspector. `EditorPreview` keeps the rendered
resume on a whole white sheet, reports the settled estimated page count, and
shows the public seal or photo state without exposing a photo key. `ResumeList`
uses paper-like cards with title, update time, a public seal/link or Draft mark,
empty create slots, and Rename/Delete menus. The sessions settings page uses
the shared settings and status components for devices, password changes,
provider linking, and connected-agent revocation. `PublishDialog` exposes the
slug, Public resume, PDF download, and SEO and GEO choices, then shows the seal,
public link, and Copy link after success.

The chrome uses Tailwind CSS v4 without Preflight and generated shadcn-vue /
reka-ui primitives. Its desk and paper tokens are cool grey and white in light
mode, lamp-lit in dark mode; signature blue-black is reserved for the person's
actions, while seal red is reserved for public state and publishing. `Be
Vietnam Pro` is the chrome font. At narrow widths the editor collapses the
rail and outline, switches between Edit and Preview, and stacks the inspector;
the sheet scales to the viewport down to 390 px and is never cropped.

The product and built-surface records are [`PRODUCT.md`](../../PRODUCT.md),
[`DESIGN.md`](../../DESIGN.md), and the [Impeccable surface briefs](../../.impeccable/surfaces/).

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
