# 0029 — Tailwind and shadcn-vue as the application UI toolkit

Status: Accepted (2026-09-02)

Amends the web and rendering design (Approved v4, section 5) for the application
chrome: every page and editor panel outside the pure resume renderer.

## Context

The application chrome is hand-written CSS keyed on element selectors, six
hand-rolled modal dialogs with their own focus traps, and forms that expose the
editor's presence intents as Set, Clear, and Remove buttons under every field.
The result is dense, inconsistent between pages, and expensive to change: a
button looks different in the list, the settings page, and the inspector, and
every dialog re-implements Escape, Tab wrapping, and return focus.

The resume renderer has the opposite requirement. The same document must render
the same pixels in the editor preview, the public page, and the print browser,
so nothing the chrome loads may leak into it.

## Decision

The application chrome is built on Tailwind CSS v4 and shadcn-vue primitives
generated into `apps/web/app/components/ui`, with reka-ui underneath. Three
layers own every visual and interactive behavior:

1. `components/ui`: generated shadcn-vue primitives. They are never hand-styled;
   a change is a regeneration plus a reviewed diff.
2. `components/app`: shared composites such as the shell, page header, form
   field, confirm dialog, status banner, and empty state. They own ids, ARIA
   wiring, focus behavior, and copy patterns.
3. Pages and editor panels compose those two layers and add no styling of their
   own beyond layout utilities.

Two rules protect the renderer:

- Tailwind loads without Preflight. The stylesheet imports only the theme and
  utilities layers. A small chrome reset lives in the base layer and its
  selectors exclude `.resume-document`, `.paged-resume`, and their descendants.
  The reset applies only when the root element carries `data-ui="app"`, which
  the render harness never sets.
- The renderer import boundary already admits only the schema, the icon package,
  Vue, and renderer-local modules, so renderer code cannot import a UI primitive
  or the class helper.

Design tokens keep the existing zinc and emerald values and move from the app
wrapper to the document root, because dialogs and menus teleport to `<body>`.
The dark variant keys off the existing `data-theme` attribute and cookie.

Text fields commit on blur or Enter. A non-empty value is set, an empty value
removes the field, and Escape reverts to the last committed value. The editor
core keeps its explicit presence intents; only the field UI stops exposing them.

The editor preview never blocks on the owner photo read. While the read is
loading or unavailable the preview renders a projection without photo metadata
and shows the photo state inline, so the renderer contract that pairs photo
metadata with an authorized URL still holds.

## Rejected alternatives

- **Hand-built shared components on the existing CSS.** No new dependency, but
  the team owns every accessibility detail of dialogs, menus, tabs, and selects,
  and the result would not match the shadcn conventions the owner asked for.
- **A full component framework such as Nuxt UI, PrimeVue, or Vuetify.** Larger
  runtime, its own theming layer, and less control over markup and test hooks.
- **Tailwind classes on the existing markup.** Cheapest, but keeps the
  hand-rolled dialogs and the dense field controls, which are the problems.
- **Tailwind with Preflight.** Simplest setup, but a global reset changes how
  the preview renders headings, lists, images, and borders while the public page
  and print browser stay unreset. Preview fidelity is a design invariant.

## Consequences

- `apps/web` gains Tailwind, the Tailwind Vite plugin, the shadcn Nuxt module,
  reka-ui, class-variance-authority, clsx, and tailwind-merge as pinned
  dependencies. The shadcn-vue CLI runs through `npx` at a pinned version and is
  not a dependency.
- Generated components import icons from the existing `@lucide/vue` package; a
  wrapper script rewrites the generator's default import so no second icon
  package enters the bundle.
- The renderer golden HTML and screenshot suites must pass unchanged after the
  toolkit lands. A diff there is a leak, not a baseline update.
- Unit tests query by role, label, and stable data attributes. Tests that
  reached into native selects, class names, or in-place dialogs are rewritten as
  part of the migration.
- The legacy stylesheets are deleted when the last consumer migrates. Until then
  they load in a cascade layer below the utilities so migrated components win.
