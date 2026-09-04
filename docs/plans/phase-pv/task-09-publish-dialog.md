# PV T09 — publish dialog and the stamp

## Contract

Restyle `PublishDialog.vue` (already on PU's `FormDialog` after T01) and add the
stamp. Every P5B behavior stays; the T01 test file remains the proof.

### Dialog

- Title "Publish resume"; description "Choose how this resume is shared
  publicly." (held).
- Slug: `TextField` with a leading adornment `aboutme.vn/` rendered inside the
  field (`aria-hidden`, the input's `aria-describedby` names it); the input
  keeps `data-action="publish-slug"`, `name`, `minlength`, `maxlength`,
  `pattern`, and the local grammar error text (held).
- The three choices are `SwitchField`s with `data-action="publish-live"`,
  `"publish-download"`, `"publish-seo-geo"` (held) and their existing
  explanation sentences directly beneath each switch as the field hint.
  Dependent switches disable when Public resume is off (held behavior).
- Actions: "Publish" as `Button variant="seal"`; "Cancel" as `ghost`. Reauth and
  retry buttons stay `default` or `secondary`; none are seal.
- Success state: replace the plain link with
  `AppSeal size="stamp" :link="canonicalLink"` above the link text
  `aboutme.vn{canonicalLink}` as an anchor (held `href`), and a "Copy link"
  `Button variant="secondary" data-action="copy-link"` that writes
  `` `${location.origin}${canonicalLink}` `` with
  `navigator.clipboard.writeText`, shows "Copied" for 2 s, and on rejection
  shows the fixed text "Copy failed. Select the link to copy it." Never
  constructs the link from the input.

### The stamp in the shell and preview

After an accepted publish, `EditorShell` receives the canonical metadata. The
T07 title mark appears, and `EditorPreview` overlays `AppSeal size="stamp"` at
the preview sheet's lower-right foot without changing the renderer. Both roots
get `data-stamp="landing"` for 180 ms with the transition
`transform: scale(1.12) → 1; opacity: 0 → 1`. On unpublish they remain rendered
with the last accepted canonical link while `data-stamp="lifting"` runs for 120
ms (`opacity: 1 → 0`), then disappear. Under
`matchMedia('(prefers-reduced-motion: reduce)').matches` no animation attribute
is applied and both marks appear or disappear at once. The watch, retained link,
timers, and media query live in `apps/web/app/composables/useStamp.ts`:

```ts
export function useStamp(publicLink: Ref<string | null>): {
  stampState: Ref<"idle" | "landing" | "lifting">;
  displayLink: Ref<string | null>;
};
```

A non-null canonical link changing to a different non-null link uses the landing
transition for the new link. The title `StateMark` and preview seal use
`displayLink`, never dialog input. Shared unscoped `[data-stamp]` animation
styles live with `StateMark.vue` and apply to both roots.

## TDD cases

Update `test/editor/publish-dialog.test.ts` first for: the prefix adornment and
`aria-describedby`; switches by role with `aria-checked`; the seal-variant
Publish button and no other seal control in the dialog; success shows the stamp
`aria-label` and the canonical anchor; Copy link writes the canonical absolute
URL and shows "Copied"; clipboard rejection shows the fixed text; every P5B case
still passes. Add `test/editor/use-stamp.test.ts`: a null→link change yields
`landing` then `idle` after 180 ms with fake timers; link→null retains
`displayLink` through `lifting`, then clears it after 120 ms; a changed link
lands again; with reduced motion mocked true the state stays `idle` and
`displayLink` changes immediately. Update the shell and preview tests to prove
the accepted canonical link drives both marks, the stamp overlays the preview
sheet rather than renderer output, and lifting keeps both marks present until
its timer ends.

## Ownership and checks

Owned paths:

- `apps/web/app/components/editor/PublishDialog.vue`
- `apps/web/app/components/editor/EditorShell.vue`, `EditorPreview.vue` (stamp
  mounting only)
- `apps/web/app/composables/useStamp.ts`
- `apps/web/app/components/app/StateMark.vue` (stamp classes only)
- `apps/web/test/editor/publish-dialog.test.ts`,
  `test/editor/use-stamp.test.ts`, `test/editor/editor-shell.test.ts`,
  `test/editor/editor-preview.test.ts`

Acceptance: `AC-UI-012`, `AC-PUB-006` through `AC-PUB-010` re-proof.

Run:

```sh
cd apps/web
npx vitest run test/editor/publish-dialog.test.ts test/editor/use-stamp.test.ts test/editor/editor-shell.test.ts test/editor/editor-preview.test.ts
npx eslint app/components/editor/PublishDialog.vue app/components/editor/EditorShell.vue app/components/editor/EditorPreview.vue app/composables/useStamp.ts app/components/app/StateMark.vue test/editor
npx vue-tsc --noEmit
```

Do not edit `app/editor/publishController.ts`, `publishApi.ts`, the shell beyond
mounting `useStamp` on the two marks, renderer components, or Git state. Report
the first failing test, exact commands, and the clipboard behavior observed in
jsdom.
