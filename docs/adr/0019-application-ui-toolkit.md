# 0019: Tailwind and shadcn-vue as the application UI toolkit

Status: Accepted (2026-09-02, 2026-09-24).

## Context

The application chrome, every page and editor panel outside the pure resume
renderer, was hand-written CSS keyed on element selectors, six hand-rolled modal
dialogs with their own focus traps, and forms that exposed the editor's presence
intents as Set, Clear, and Remove buttons under every field. It was dense,
inconsistent between pages, and expensive to change.

The resume renderer has the opposite requirement. The same document must render
the same pixels in the editor preview, the public page, and the print browser,
so nothing the chrome loads may leak into it.

## Decision

The application chrome is built on Tailwind CSS v4 and shadcn-vue primitives
generated into `apps/web/app/components/ui`, with reka-ui underneath. Three
layers own every visual and interactive behavior:

1. `components/ui`: generated shadcn-vue primitives. A change is a regeneration
   plus a reviewed diff, except the guarded token edits below.
2. `components/app`: shared composites such as the shell, page header, form
   field, confirm dialog, status banner, and empty state. They own ids, ARIA
   wiring, focus behavior, and copy patterns.
3. Pages and editor panels compose those two layers and add no styling of their
   own beyond layout utilities.

**Guarded token edits.** A generated primitive may carry a local edit to a
variant's classes when all of these hold:

- The edit only swaps utilities for chrome tokens that `theme.css` defines. It
  changes no markup, behavior, prop, or accessibility attribute.
- A unit test asserts each edited class and fails when a regeneration drops it.
- After `apps/web/scripts/ui-add.sh` regenerates the primitive, the author
  re-applies the edits and the guard test passes before the change lands.

The button primitive (`apps/web/app/components/ui/button/index.ts`) carries
guarded edits for the `default`, `link`, `seal`, and `destructive` variants,
asserted by `apps/web/test/ui/button-variants.test.ts`. A primitive that needs
new markup or behavior gets a composite in `components/app` instead.

**Renderer isolation.**

- Tailwind loads without Preflight. The stylesheet imports only the theme and
  utilities layers. A small chrome reset lives in the base layer, and its
  selectors exclude `.resume-document`, `.paged-resume`, and their descendants.
  The reset and the chrome tokens apply only when the root element carries
  `data-ui="app"`, which the render harness and print path never set.
- The renderer import boundary admits only the schema, the icon package, Vue,
  and renderer-local modules, so renderer code cannot import a UI primitive or
  the class helper.

Design tokens live on the document root, because dialogs and menus teleport to
`<body>`. The dark variant keys off the `data-theme` attribute and cookie. ADR
0020 sets the token values.

Text fields commit on blur or Enter. A non-empty value is set, an empty value
removes the field, and Escape reverts to the last committed value. The editor
core keeps its explicit presence intents; only the field UI stops exposing them.

The editor preview never blocks on the owner photo read. While the read is
loading or unavailable, the preview renders a projection without photo metadata
and shows the photo state inline, so the renderer contract that pairs photo
metadata with an authorized URL still holds.

## Rejected alternatives

- **Hand-built shared components on the existing CSS.** The team would own every
  accessibility detail of dialogs, menus, tabs, and selects.
- **A full component framework such as Nuxt UI, PrimeVue, or Vuetify.** Larger
  runtime, its own theming layer, and less control over markup and test hooks.
- **Tailwind classes on the existing markup.** Keeps the hand-rolled dialogs and
  dense field controls, which are the problems.
- **Tailwind with Preflight.** A global reset changes how the preview renders
  headings, lists, images, and borders while the public page and print browser
  stay unreset.
- **An app composite that wraps `Button` for token edits.** Every surface and
  `buttonVariants` call site must switch to it, and a missed call site silently
  falls back to the generated colors.
- **Changing `--primary` so one token serves fills and text.** It also changes
  every fill, ring, and chip that the palette tunes separately.

## Consequences

- `apps/web` depends on Tailwind, the Tailwind Vite plugin, the shadcn Nuxt
  module, reka-ui, class-variance-authority, clsx, and tailwind-merge, pinned.
  The shadcn-vue CLI runs through `npx` at a pinned version and is not a
  dependency.
- Generated components import icons from `@lucide/vue`; a wrapper script
  rewrites the generator's default import so no second icon package enters the
  bundle.
- The renderer golden HTML and screenshot suites must pass unchanged when chrome
  changes. A diff there is a leak, not a baseline update.
- Unit tests query by role, label, and stable data attributes.
- Legacy stylesheets load in a cascade layer below the utilities so migrated
  components win, and are deleted when their last consumer migrates.
- Regenerating a primitive with local edits is a two-step change; the guard test
  fails in CI if the second step is skipped.

## History

- Former ADR 0029 (2026-09-02): the toolkit and renderer isolation. Its "keep
  the zinc and emerald values" clause was replaced by the visual identity
  records (now ADR 0020).
- Former ADR 0052 (2026-09-24): guarded token edits to generated primitives,
  replacing 0029's "never hand-styled" rule. Former ADR 0065 added the
  `destructive` variant to the guarded edits.
