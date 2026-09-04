# PV T02 — tokens, chrome font, seal, and state marks

## Contract

Replace the token values, set the chrome typeface, and add the two components
every later task composes. Read
`.impeccable/surfaces/apps-web-app-pages-index-vue.md` first; its OWN-WORLD
block and [`design.md`](design.md) section 2 are the values.

### `apps/web/app/assets/css/theme.css`

Rewrite `:root` and `html[data-theme='dark']` with the design token table. Keep
every shadcn semantic name the primitives read (`--background`, `--foreground`,
`--card`, `--card-foreground`, `--popover`, `--popover-foreground`, `--primary`,
`--primary-foreground`, `--secondary`, `--secondary-foreground`, `--muted`,
`--muted-foreground`, `--accent`, `--accent-foreground`, `--destructive`,
`--border`, `--input`, `--ring`, `--radius`). Add `--seal`, `--seal-foreground`,
`--shadow-paper`, `--radius-sheet: 2px`, `--radius-dialog: 8px`. Delete
`--positive`, `--positive-foreground`, and `--chart-1` through `--chart-5`. In
`@theme inline` set
`--font-sans: 'Be Vietnam Pro', 'Inter', system-ui, sans-serif` and map
`--color-seal`, `--color-seal-foreground`, `--shadow-paper`, and the two radii.
`--muted` equals `--secondary`.

Add the type scale as `--text-xs` 0.75rem, `--text-sm` 0.8125rem, `--text-base`
0.875rem, `--text-md` 1rem, `--text-lg` 1.25rem, `--text-xl` 1.5rem,
`--text-2xl` 2rem, `--text-3xl` 2.75rem with the line heights 1.5 for body steps
and 1.2 for `lg` and above.

### `apps/web/app/components/ui/button/`

Regenerate nothing; add one variant in the variants file: `seal`
(`bg-seal text-seal-foreground hover:bg-seal/90`). The `default` variant now
reads `--primary` (signature). No other variant changes.

### `apps/web/app/components/app/AppSeal.vue`

```ts
defineProps<{
  link: string; // canonical public path, e.g. "/ada-lovelace"
  size?: "mark" | "stamp"; // default 'stamp'
  rotate?: number; // default -8, ignored for 'mark'
}>();
```

Renders an inline `<svg role="img">` with `aria-label`
`` `Public at aboutme.vn${link}` ``. `stamp`: 96 px viewBox, two concentric
strokes (2 px and 1 px) in `currentColor`, ring text on a `<textPath>` reading
`` `PUBLIC RESUME · ABOUTME.VN${link.toUpperCase()} · ` `` at 9 px with
`letter-spacing: 0.08em`, center text `aboutme` at 14 px weight 600, the whole
group rotated by `rotate`. `mark`: 20 px viewBox, one filled circle and a 2 px
check path in `--seal-foreground`, no text. Color comes from
`color: var(--seal)` on the root; the component sets nothing else. Fonts inherit
the chrome family.

### `apps/web/app/components/app/StateMark.vue`

```ts
defineProps<{
  state: "saved" | "saving" | "failed" | "draft" | "public";
  link?: string; // required when state === 'public'
}>();
```

Renders `<span data-state-mark={state}>` with, per state: `saved` a 14 px
pencil-tick glyph (inline SVG) and the text "Saved"; `saving` the text "Saving…"
with `aria-live="polite"`; `failed` "Save failed" in `text-destructive` with
`role="alert"`; `draft` "Draft"; `public` an `AppSeal size="mark"` followed by
`` `aboutme.vn${link}` `` as a link to `link`. Text color is
`--muted-foreground` except `failed`. Replace `SaveStatus.vue` with a re-export
of `StateMark` mapping the editor's `saveState` (`'idle' | 'saved'` to `saved`,
`'saving'` to `saving`, `'failed'` to `failed`) so existing shell tests keep
passing.

### Fonts

`fonts.css` already declares `Be Vietnam Pro` (100–900 variable). Add
`font-display: swap` is present; nothing else. The renderer keeps loading
catalog fonts on demand.

## TDD cases

Write `test/app/theme.test.ts` first: parse `theme.css` and assert every token
in the design table with its exact value in both blocks, the absence of
`--positive` and `--chart-`, the font-sans stack, and that the built CSS
(`npx vite build` is not run here; grep the source) uses `var(--seal)` only in
`AppSeal.vue`, `StateMark.vue`, and the button variants file. Write
`test/app/seal.test.ts`: `stamp` renders ring text with the uppercased link and
the `aria-label`; `mark` renders no text and has the label; `rotate` defaults to
`-8`; hostile link text renders as text. Write `test/app/state-mark.test.ts`:
each state's text, `aria-live` on `saving`, `role="alert"` on `failed`, the seal
mark and link on `public`, and that `SaveStatus` maps the three editor states.

Adversarial: the dark-sheet case from
[`adversarial-coverage.md`](adversarial-coverage.md) (render `ResumeDocument`
with the `full` fixture under `data-theme="dark"` and assert the root's inline
background equals the document's `colors.background`).

## Ownership and checks

Owned paths:

- `apps/web/app/assets/css/theme.css`
- `apps/web/app/components/ui/button/index.ts` (variants only)
- `apps/web/app/components/app/AppSeal.vue`
- `apps/web/app/components/app/StateMark.vue`
- `apps/web/app/components/editor/SaveStatus.vue`
- `apps/web/test/app/theme.test.ts`, `seal.test.ts`, `state-mark.test.ts`

Acceptance: `AC-UI-007`.

Run:

```sh
cd apps/web
npx vitest run test/app/theme.test.ts test/app/seal.test.ts test/app/state-mark.test.ts test/renderer
npx eslint app/assets app/components/app app/components/editor/SaveStatus.vue test/app
npx vue-tsc --noEmit
```

Do not edit pages, panels, generated primitives beyond the variants file, or Git
state. Report the first failing test, exact commands, and any token the
primitives needed that the table lacks.
