# Color and accessibility tokens

Status: **Draft v2** (2026-08-12). Not approved.

Defines derived color roles, multi-surface behavior, and the accessibility floor
for the [template token vocabulary](tokens.md).

## 4. Color roles

Templates and components address roles, never the raw hex values. Every role is
derived by `useResumeStyles`; the raw values in `customization.colors` are never
mutated.

| Role                      | Source                                                                         | Contrast target              |
| ------------------------- | ------------------------------------------------------------------------------ | ---------------------------- |
| `--color-surface`         | `colors.background`                                                            | — (it is the surface)        |
| `--color-surface-header`  | `colors.surface` if the effective target is `header`, else `--color-surface`   | — (it is a surface)          |
| `--color-surface-sidebar` | `colors.surface` if the effective target is `sidebar`, else `--color-surface`  | — (it is a surface)          |
| `--color-heading`         | `colors.primary`, clamped                                                      | 4.5:1 on its surface         |
| `--color-body`            | `colors.text`, clamped                                                         | 4.5:1 on its surface         |
| `--color-meta`            | `colors.text` mixed 25% toward the surface, then clamped                       | 4.5:1                        |
| `--color-accent`          | `colors.accent` if present, else `colors.primary`                              | raw, not for direct use      |
| `--color-accent-text`     | `--color-accent`, clamped                                                      | 4.5:1 on its surface         |
| `--color-accent-solid`    | `--color-accent`, clamped against its surface and actual track                 | 3:1 against both             |
| `--color-on-accent`       | `#000000` or `#ffffff`, whichever scores higher against `--color-accent-solid` | 4.5:1                        |
| `--color-link`            | alias of `--color-accent-text`                                                 | 4.5:1                        |
| `--color-rule`            | `--color-accent` mixed 60% toward the surface, then clamped                    | 1.5:1, decorative only       |
| `--color-track`           | `--color-accent` mixed 80% toward the surface; may fall back to the surface    | — none, by design (see note) |

Notes:

- **`--color-track` carries no contrast floor, and cannot.** Its derivation
  (accent mixed 80% toward the surface) caps it at 1.61:1 on a white surface
  even for a pure-black accent — verified independently by two preset designs.
  This is acceptable because the track is not the meaning-carrier: in a level
  widget only the FILLED portion asserts anything, and it meets 3:1 against both
  track and surface. The §5 "meaningful non-text" floor therefore applies to the
  filled bar, filled dots, and chip fill — never to the track. The renderer
  first clamps the fill against both the derived track and the surface. If no
  black-or-white search direction can satisfy both, the track becomes the
  surface and the fill uses the ordinary single-surface clamp. This fallback may
  make the track invisible, which is valid because it carries no meaning.
  Renderer consequence: a level widget must remain correct when the track is
  invisible. An absent `level` renders no widget. A present level, including
  zero, emits the exact accessible widget name from contract §5.6; levels 1–5
  additionally use the visible fill. An empty track is never the sole difference
  between "rated 0" and "unrated".
- `--color-link` is the link's **color** role only. Its underline is
  renderer-fixed ([Geometry](geometry.md)) and no preset can remove it, so a
  link is separable from body text even where the two colors are close.
- `colors.accent` and `colors.surface` are the only optional colors and neither
  can be cleared to `""` — `hexColor`'s pattern forbids it. Absent is the only
  unset state; the accent falls back to `colors.primary` and the surface to
  `colors.background`, never to a hard-coded brand color or tint.
- `--color-surface-sidebar` was reserved as a role in the first draft so that
  giving the sidebar an independent tint would be a token change rather than a
  markup change. `colors.surface` plus `layout.surfaceTarget` is that change;
  the role is no longer an unconditional alias, and `--color-surface-header`
  joins it on the same footing. Anything below "its surface" in the table above
  means whichever of the three the element actually paints on.
- `--color-on-accent` always exists: for any accent, the contrast ratios of
  black and white against it multiply to exactly 21, so the larger of the two is
  at least √21 ≈ 4.58 and clears the 4.5:1 floor. (The first draft specified an
  outline-chip fallback for the case where neither reached 4.5:1; that case is
  arithmetically unreachable, so the fallback was removed — a `tag` chip always
  fills.)
- **Mix space (normative):** every "mixed N% toward the surface" step in the
  table above is a component-wise linear interpolation of the **gamma-encoded
  sRGB channels** (the CSS `color-mix(in srgb, …)` behavior), not a mix in
  linear light or OKLab. The spaces disagree enough to change conformance:
  mixing text 25% toward white in linear light darkens the result's computed
  luminance so far that no text color could reach `--color-meta`'s 4.5:1 target
  without clamping, while the sRGB mix leaves typical dark text at 6–7:1
  unclamped. Contrast checks in preset rationale docs and golden tests must use
  this definition.

### 4.1 The effective surface target

`layout.surfaceTarget` says which region `colors.surface` fills. The renderer
resolves it to an **effective** target first, as a total function over the
stored values — it never rejects a document and never writes one:

```text
effectiveSurfaceTarget(customization):
  t = layout.surfaceTarget, or "none" when the key is absent
  if t == "none":                       return "none"
  if colors.surface is absent:          return "none"   # nothing to fill with
  if t == "sidebar" and layout.columns == 1: return "none"   # no sidebar region
  return t
```

The two degradations are the point, not an edge case. `contract.md` §7 already
requires that in one-column mode "any sidebar-specific treatment (tint, narrower
measure) degrades to the main treatment", and a user reaches exactly that state
with one click of the columns toggle. Making it a validation error would make a
document unsaveable that the editor itself produced, so both combinations stay
stored verbatim: toggling back to two columns, or re-picking a color, restores
the tint with no rewrite of either field.

A tinted header band spans the full content measure, inside `--page-margin-x`. A
tinted sidebar fills its own column, `--sidebar-ratio` wide, and continues
across page breaks with the sidebar flow (`print.md` §5).

### 4.2 Text over `colors.surface`

`colors.surface` introduces a second text-on-surface pair, and the guarantee for
it is the same one §5 gives for an arbitrary accent, applied with a different
second argument: **every clamped role is evaluated once per surface, against the
surface the element actually paints on.** Concretely, an element inside the
tinted region resolves `--color-heading`, `--color-body`, `--color-meta`,
`--color-accent-text`, `--color-accent-solid`, `--color-link`, `--color-rule`,
and `--color-track` against that region. Roles with a floor use
`clampAgainst(color, [--color-surface-header-or-sidebar], target)` instead of
the page surface. Mix-toward-the-surface steps and the track/solid pair use that
same region surface. `--color-on-accent` follows because it is chosen against
`--color-accent-solid`, which is itself per-surface.

Three consequences worth stating outright:

- The floor holds for any of the 16.7 million surfaces a user can pick,
  including a near-black band under `colors.text: #1a1a1a`. The clamp keeps the
  hue and chroma of the text color and moves only its lightness toward the black
  or white endpoint that has the higher minimum contrast, until it passes —
  4.5:1 for body, headings, meta, and links, 3:1 for `--fs-name` and for
  non-text marks that carry meaning. For one surface, either black or white is
  always at least √21 ≈ 4.58:1. A neutral endpoint is therefore the
  deterministic last resort when no hue-preserving step passes.
- It is non-destructive. `customization.colors` keeps the user's hexes; only the
  derived roles differ between the tinted region and the rest of the page.
- The same text color can therefore resolve to two different values on one page,
  which is correct and is why §5's "per surface" bullet is a hard rule rather
  than an optimization note. Code must not hoist a clamped role to the document
  root, and golden snapshots must cover a document with a tinted region so the
  second resolution is pinned too — `fixtures/full.json` carries one
  (`surfaceTarget: "sidebar"` over two columns).

**Rendering regression coverage.** The two artifacts have different scopes
because their execution costs differ:

- **SSR string goldens: every preset, both pagination modes.** They are cheap,
  deterministic, and byte-diffable, and they are what pins the per-surface
  resolution above for the presets that tint.
- **Screenshot baselines: a named representative subset, roughly six presets,
  plus the continuous-mode case.** The subset is named explicitly in the Phase 3
  golden and Playwright tasks, not derived at run time, and is chosen against
  `contract.md` §8 so that it covers at minimum one one-column preset, one
  two-column preset, one tinted sidebar (`fixtures/full.json`,
  `surfaceTarget: "sidebar"`), one tinted header (`surfaceTarget: "header"`),
  one `pageFormat: "letter"`, and one dense/small-type preset. A preset outside
  the subset is still covered by string goldens; what it loses is pixel-level
  regression detection.
- P9's browser UAT exercises **the same named subset**, not all twenty presets,
  so the three coverage surfaces cannot drift apart.

## 5. Accessibility floor

Two hard floors, guaranteed by the renderer for every template and every user
color choice.

**Size.** Body text renders at exactly `font.baseSizePx`, whose schema floor is
10 px (7.5 pt at Chromium's 96 CSS px per inch). The smallest text the renderer
can produce is `--fs-meta` at `max(0.9 × base, 9px)`. A base below 13 px (≈ 9.75
pt) is below normal print-legibility practice; the renderer must not silently
override the user's explicit choice, so the containment is an editor warning,
and the residual risk is recorded in `limitations.md` §9.7. The same reasoning
applies to `spacing.*` at `0`: legal, explicit, and not overridden. That
includes `spacing.pageMargin` at `0`, which puts content inside the unprintable
edge of most desktop printers — again an editor warning, never a schema
rejection, since a screen-only or professionally trimmed resume is a legitimate
use.

**Contrast.** Measured as the WCAG 2.x relative-luminance ratio.

| Usage                                                        | Minimum |
| ------------------------------------------------------------ | ------- |
| text below 24 px, or below 18.66 px bold                     | 4.5:1   |
| text at or above 24 px, or 18.66 px bold (`--fs-name`)       | 3:1     |
| non-text that carries meaning: level bars, dots, tag borders | 3:1     |
| decorative section rule                                      | 1.5:1   |

The rule is decorative because the heading's size and weight already signal the
section boundary; a rule may therefore be faint, but it may never be the only
boundary signal.

### How the guarantee holds for an arbitrary accent

The user may pick any of 16.7 million colors, including yellow on white. The
renderer clamps at the point of use:

```text
clampAgainst(color, surfaces, target):
  if contrast(color, every surface) >= target: return color
  blackScore = minimum contrast(#000000, each surface)
  whiteScore = minimum contrast(#ffffff, each surface)
  endpoint = #000000 if blackScore >= whiteScore, else #ffffff
  if contrast(endpoint, every surface) < target: return failure
  keep the hue and chroma of color in OKLCH
  for L from color.L toward endpoint.L, stepping 0.005:
      candidate = gamut-clipped sRGB of oklch(L, C, H)
      if contrast(candidate, every surface) >= target: return candidate
  return endpoint

deriveLevelColors(accent, surface):
  track = mixInGammaEncodedSRGB(accent, surface, 80%)
  solid = clampAgainst(accent, [surface, track], 3)
  if solid succeeded: return [solid, track]
  track = surface
  solid = clampAgainst(accent, [surface], 3)
  return [solid, track]
```

Properties this relies on:

- **Pure and deterministic.** Same inputs, same output, in preview, SSR, and
  print. Golden snapshots pin the computed values, so a change to the clamp is a
  visible diff rather than a silent drift.
- **Hue-preserving when possible.** The search changes only lightness and stops
  at the first passing value. A black or white endpoint is used only when the
  original hue and chroma cannot reach the floor after gamut clipping.
- **Non-destructive.** `customization.colors` keeps the user's hex. The clamp
  produces a different derived role, never a write.
- **Per surface.** Text in the sidebar clamps against `--color-surface-sidebar`,
  text in the header band against `--color-surface-header`, text elsewhere
  against `--color-surface`. They are equal only while `colors.surface` is
  absent or the effective target is `none`; `colors.surface` is exactly what
  makes them diverge, so the code must never assume they are equal (§4.2).
- The user's own `colors.text` on `colors.background` is clamped by the same
  rule. Light grey on white becomes dark enough to read. This is the one place
  the renderer overrides a direct user choice, and it is deliberate: an
  unreadable published resume is worse than an approximated color.
- **Multi-surface fill.** A meaningful filled bar or dot is checked against its
  actual track and its page or tinted-region surface. A derived track that makes
  the pair unsatisfiable falls back to that surface; no authored color is
  changed.
