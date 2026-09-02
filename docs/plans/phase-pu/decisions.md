# Phase PU decisions

These values are frozen for the phase. Changing one is a reviewed authority
change with evidence, never test tuning.

## U1 — Dependencies

Exact pins in `apps/web/package.json`, added by T01 and never changed by a
worker:

| Package                    | Version | Kind |
| -------------------------- | ------- | ---- |
| `tailwindcss`              | 4.3.3   | dev  |
| `@tailwindcss/vite`        | 4.3.3   | dev  |
| `shadcn-nuxt`              | 2.8.2   | dev  |
| `reka-ui`                  | 2.10.4  | prod |
| `@vueuse/core`             | 14.4.0  | prod |
| `class-variance-authority` | 0.7.1   | prod |
| `clsx`                     | 2.1.1   | prod |
| `tailwind-merge`           | 3.6.0   | prod |

- The shadcn-vue CLI is not a dependency. Every generator call runs
  `npx --yes shadcn-vue@2.8.2` through `apps/web/scripts/ui-add.sh`.
- `@lucide/vue` 1.31.0 stays the only icon package. The wrapper script rewrites
  `lucide-vue-next` imports to `@lucide/vue`; a generated file that still
  imports `lucide-vue-next` fails T01.
- If a generated primitive imports `@vueuse/core` directly, the owner adds it as
  a prod dependency pinned to the exact version already resolved under `reka-ui`
  and records the version here. No other transitive package is promoted.

## U2 — Stylesheet contract

`apps/web/nuxt.config.ts` lists exactly `~/assets/css/tailwind.css` in `css`.
`fonts.css` is imported by `tailwind.css`, not by Nuxt, so every rule sits in a
named cascade layer.

`app/assets/css/tailwind.css` is exactly:

```css
@layer theme, base, legacy, components, utilities;

@import "tailwindcss/theme.css" layer(theme);
@import "./fonts.css" layer(theme);
@import "./theme.css" layer(theme);
@import "./base.css" layer(base);
@import "./app.css" layer(legacy);
@import "./editor.css" layer(legacy);
@import "./auth.css" layer(legacy);
@import "./landing.css" layer(legacy);
@import "tailwindcss/utilities.css" layer(utilities) source("../../");

@custom-variant dark (&:where([data-theme='dark'], [data-theme='dark'] *));
```

Rules:

- `tailwindcss/preflight.css` is never imported. The T01 stylesheet test reads
  `tailwind.css` and fails on the substring `preflight`.
- `theme.css` defines the tokens on `:root` and `html[data-theme='dark']` with
  the exact values in [U6](#u6--visual-tokens) and maps them through
  `@theme inline`.
- `base.css` contains the chrome reset. Every selector in it starts with
  `html[data-ui='app'] body` and every descendant selector excludes the renderer
  through
  `:not(:where(.resume-document, .paged-resume, .resume-document *, .paged-resume *))`.
  The T01 stylesheet test parses `base.css` and fails on any rule that lacks
  either guard.
- `app.vue` sets `data-ui="app"` on `<html>` for every route except
  `/_harness/**` through `useHead({ htmlAttrs })`, and applies the `aboutme-app`
  class to its wrapper on the same routes.
- The four legacy stylesheets load in the `legacy` layer, below `utilities`, so
  a migrated component's classes win over element rules. Each surface task
  deletes the legacy rules it replaced; T13 deletes the four files and their
  import lines.
- Migrated components carry no `<style>` block. Layout is Tailwind utilities;
  state is `data-*` attributes styled with variants.

## U3 — Three layers

| Layer      | Path                          | Ownership                                 |
| ---------- | ----------------------------- | ----------------------------------------- |
| Primitives | `app/components/ui/<name>/`   | Generated; T01 owner; never hand-edited   |
| Composites | `app/components/app/*.vue`    | T03 author; contracts frozen in this plan |
| Surfaces   | `app/pages/**`, editor panels | One surface task each                     |

- Primitives generated at T01: `button`, `input`, `label`, `textarea`,
  `native-select`, `select`, `checkbox`, `radio-group`, `switch`, `dialog`,
  `alert-dialog`, `sheet`, `tabs`, `card`, `badge`, `alert`, `separator`,
  `tooltip`, `dropdown-menu`, `scroll-area`, `skeleton`, `toggle`,
  `toggle-group`, `slider`, `collapsible`, `table`, `avatar`.
- Components import primitives explicitly from `@/components/ui/<name>`; the
  shadcn-nuxt module registers them with the `Ui` prefix only so an accidental
  auto-import is visible in review.
- `app/lib/utils.ts` exports `cn(...inputs: ClassValue[]): string` built from
  `clsx` and `twMerge`. It is the only shared helper the layers add.
- The existing `components/ui/{AppChrome,AccountControl,ThemeToggle}.vue` move
  to `components/app/` at T01 unchanged; T03 rebuilds them there.

## U4 — Field commit rule

`TextField` and every text-bearing composite implement exactly this:

| Event                                 | Draft state               | Emitted intent              |
| ------------------------------------- | ------------------------- | --------------------------- |
| blur, or Enter in a single-line field | non-empty and changed     | `{ kind: 'set', value }`    |
| blur or Enter                         | empty and model defined   | `{ kind: 'unset' }`         |
| blur or Enter                         | empty and model undefined | nothing                     |
| blur or Enter                         | unchanged                 | nothing                     |
| Escape                                | any                       | nothing; draft reverts      |
| external model change while clean     | —                         | draft follows the model     |
| external model change while dirty     | —                         | draft keeps the user's text |

- Values are sent as typed; no trimming. The server owns bounds.
- Rich text commits through the same table on blur of the ProseMirror root; an
  empty document maps to `unset`.
- Enumerations, checkboxes, switches, colors, and numbers commit on change.
- Date fields keep their own validation and commit on blur; clearing every part
  is `unset`.
- `fieldIntent.ts` becomes `{ kind: 'set'; value: T } | { kind: 'unset' }`. The
  mapping to `Presence` in `SectionPanel.vue` and `PersonalDetailsPanel.vue` is
  unchanged.
- `ContactList` keeps emitting the whole array on every change.

## U5 — Test-hook policy

- Every `data-testid`, `data-action`, `data-*`, `aria-label`, visible label, and
  button text listed under [retained hooks](file-structure.md#retained-hooks)
  survives. A task file that changes one lists the old and new value under "Hook
  changes".
- New hooks: `data-testid` on pages and settings, `data-action` in the editor,
  `data-field` on editor field wrappers.
- Unit tests query with `getByRole`-style selectors (`[role="..."]`,
  `[aria-label="..."]`), label text, `data-testid`, or `data-action`. They do
  not use tag selectors, class selectors, or element index.
- Dialogs teleport to `<body>`. A test that opens one mounts with
  `attachTo: document.body`, queries `document.body`, and unmounts at the end.
- Enumerations use `NativeSelect`, so `setValue()` on the `select` inside a
  `[data-field]` wrapper stays valid.
- Checkboxes use the reka `Checkbox` (`button[role="checkbox"]`); tests click
  the role and assert `aria-checked`.

## U6 — Visual tokens

- Palette: the zinc values already in `app.css`, verbatim, on `:root` and
  `html[data-theme='dark']`; `--positive` and `--positive-foreground` kept as
  the only accent; `--radius: 0.625rem`.
- Font: `--font-sans: 'Inter', system-ui, sans-serif` (Inter is bundled by
  `fonts.css`). No display face.
- Type scale for the chrome: `text-xs` 0.75rem, `text-sm` 0.875rem (body),
  `text-base` 1rem, `text-lg` 1.125rem (panel titles), `text-2xl` 1.5rem (page
  titles), `text-4xl` 2.25rem (landing headline only).
- Density: default control height `h-9`; editor inspector controls `h-8` and
  `text-sm`; inspector section gap `gap-4`.
- Editor grid unchanged: `4rem 16.5rem minmax(32rem, 1fr) 22rem`, narrow
  breakpoint 72rem, view switch at 42rem.
- Preview canvas `bg-muted`; the resume page `shadow-md`; zoom values 0.84 wide
  and 0.72 narrow as today.
- Motion: only `transition-colors` on interactive states and the primitives' own
  open/close animations; both disabled under `prefers-reduced-motion`.

## U7 — Copy rules

- Sentence case everywhere. Buttons name the action: "Create resume", "Save
  changes", "Delete section", "Add entry".
- An empty state has a title, one sentence, and the primary action.
- An error states what happened and what to do; it never echoes server text or a
  document path.
- Strings a browser proof asserts stay verbatim. The current set is listed per
  task under "Strings held".

## U8 — Verification and evidence

- Per task: `cd apps/web && npx vitest run <owned test files>` RED then GREEN,
  then `make web-lint web-typecheck`. Workers do not run `make web-test`,
  `make web-build`, or any browser proof.
- Per wave: the owner runs `make web-test`, then reviews every migrated screen
  in light and dark at 1440×1000 and 1024×768 with the Playwright MCP server
  against the native stack, and keeps screenshots under `.dev/design-qa/pu/`
  (ignored). A finding returns to the task author before the next wave.
- T01 and T13: `npx vitest run test/renderer` and `make web-e2e` prove the
  renderer unchanged.
- T13: `make web-build`, `make web-e2e`, `make dev-https-auth-check`,
  `make dev-https-editor-check`, `make dev-https-mcp-check`,
  `make dev-https-entry-check`, `make dev-https-public-check`, then the exit
  checklist, `make ci`, and connected `make scan`.
