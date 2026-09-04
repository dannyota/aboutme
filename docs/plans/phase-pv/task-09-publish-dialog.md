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

### The stamp in the shell

After an accepted publish, `EditorShell` already receives the canonical
metadata; the T07 `public-mark` appears. Add the press: the mark's root gets
`data-stamp="landing"` for 180 ms with the transition
`transform: scale(1.12) → 1; opacity: 0 → 1`, driven by a CSS class toggled from
a `watch` on `live && slug`. Unpublish toggles `data-stamp="lifting"` for 120 ms
(`opacity: 1 → 0`). Under
`matchMedia('(prefers-reduced-motion: reduce)').matches` no class is applied and
the mark appears or disappears at once. The `watch` and the media query live in
`apps/web/app/composables/useStamp.ts`:

```ts
export function useStamp(isPublic: Ref<boolean>): {
  stampState: Ref<"idle" | "landing" | "lifting">;
};
```

## TDD cases

Update `test/editor/publish-dialog.test.ts` first for: the prefix adornment and
`aria-describedby`; switches by role with `aria-checked`; the seal-variant
Publish button and no other seal control in the dialog; success shows the stamp
`aria-label` and the canonical anchor; Copy link writes the canonical absolute
URL and shows "Copied"; clipboard rejection shows the fixed text; every P5B case
still passes. Add `test/editor/use-stamp.test.ts`: a false→true change yields
`landing` then `idle` after 180 ms with fake timers; true→false yields `lifting`
then `idle` after 120 ms; with reduced motion mocked true the state stays `idle`
throughout.

## Ownership and checks

Owned paths:

- `apps/web/app/components/editor/PublishDialog.vue`
- `apps/web/app/composables/useStamp.ts`
- `apps/web/app/components/app/StateMark.vue` (stamp classes only)
- `apps/web/test/editor/publish-dialog.test.ts`, `test/editor/use-stamp.test.ts`

Acceptance: `AC-UI-012`, `AC-PUB-006` through `AC-PUB-010` re-proof.

Run:

```sh
cd apps/web
npx vitest run test/editor/publish-dialog.test.ts test/editor/use-stamp.test.ts
npx eslint app/components/editor/PublishDialog.vue app/composables/useStamp.ts app/components/app/StateMark.vue test/editor
npx vue-tsc --noEmit
```

Do not edit `app/editor/publishController.ts`, `publishApi.ts`, the shell beyond
mounting `useStamp` on the public mark, or Git state. Report the first failing
test, exact commands, and the clipboard behavior observed in jsdom.
