# Public page bar and color scheme

This page specifies two changes to the public resume page at `/{slug}`: a
compact page bar in the application identity, and an owner-chosen color scheme
that can show the resume on a dark ground. The PDF, print, and the link-preview
card stay light in every case.

Status: approved (2026-09-27) and built. The owner approved the direction: light
by default, the owner chooses the scheme per resume, and viewers see the owner's
choice. The dark scheme changes the white-sheet rule of
[ADR 0020](../adr/0020-application-visual-identity.md) and adds document v5 to
[ADR 0004](../adr/0004-resume-document-contract.md). Both records now carry the
change as accepted (2026-09-27); the page bar needs no contract change.

## Owner decisions

- Light is the default for every resume, new and existing.
- The owner picks the scheme for each resume in the editor: Light, Dark, or
  Match device. Match device follows each viewer's `prefers-color-scheme`.
- Viewers see what the owner chose. The page has no viewer toggle (see
  [No viewer toggle](#no-viewer-toggle)).
- The PDF, browser print, and the link-preview card are always light.

## Open decisions

Each item names the recommendation this page is written to. The owner approved
every item as written on 2026-09-27.

1. **Where the setting lives.** Approved (2026-09-27): a document field,
   `customization.colorScheme`, in document v5. The dark rule derives from the
   document's own colors, the Design panel and its preview already edit document
   fields, and agents can set it like any other leaf. The alternative is a
   publication setting on the resumes row, as the page title and favicon are
   ([ADR 0014](../adr/0014-public-page-head-and-link-preview.md)): no document
   version and no loss rule, but a migration, a publish request field, an
   OpenAPI change, and no agent access. Both block a rollback across the
   release.
2. **Two releases.** Approved (2026-09-27): the page bar ships as its own
   release, then the color scheme, one feature per release.
3. **The bar's mark in seal red.** Approved (2026-09-27): yes. The mark is the
   logo, which ADR 0020 already allows in seal red, and the page it sits on is
   public.
4. **Sign-in gate.** Approved (2026-09-27): the gate page stays light in both
   schemes. It is aboutme chrome shown before the resume, and its closed
   envelope stays unchanged.
5. **Join invite.** Approved (2026-09-27): the invite on a sign-in resume
   follows the page's scheme with the bar's tokens, so a light card never sits
   on a dark page.
6. **Editor copy and preview.** Approved (2026-09-27): the field copy below, and
   a Web preview that follows the editing device for Match device.

## Page bar

The [page bar](public-page-bar.md) design owns the bar's structure, copy,
tokens, size and placement, and behavior.

### Tokens

See [bar tokens](public-page-bar.md#tokens).

## Color scheme setting

### Document field

The scheme is a presentation leaf of the resume document:
`customization.colorScheme`, an optional enum of `light`, `dark`, and `system`.
Absent means `light`, so every existing resume stays light without a data
migration.

- A preset never sets it. Applying a template keeps the owner's value, or its
  absence, as it keeps `font.textAlign` and `header.photoPosition`.
- The column toggle does not touch it.
- It changes no content, order, or visibility.
- Agents set and unset it through the same customization path as any other
  optional leaf.

The document then has 28 customization leaves: 26 author-controlled, of which 11
are optional.

### Versions and compatibility

The field needs document v5 ([ADR 0004](../adr/0004-resume-document-contract.md)
and [data](data.md#document-versions)).

- **Up from v4.** The converter adds nothing: a v4 document is a v5 document
  with the field absent. No stored row is rewritten; reads project in memory.
- **Down to v4 and older.** Emitting v4 drops `customization.colorScheme`. It is
  the one v5 loss, and it composes with the older declared losses.
- **Older-client writes.** A v1 to v4 write keeps the stored `colorScheme`
  unconditionally: the field sits directly under `customization`, which every
  version requires, so no parent can disappear.
- **Public JSON.** `PublicCustomization` in OpenAPI gains the optional enum, and
  the public projection copies it, so the hydrated page and its live refresh see
  the same value as the server render.
- **Print snapshot.** The frozen print document drops the field, so the print
  worker never receives a scheme. The print app, which the worker renders in
  continuous mode, never passes a scheme to the renderer.
- **Link-preview card.** The card envelope carries only the clamped accent, so
  the scheme cannot reach the card, and the card version does not change.
- **Rollback.** Once a write stores v5, a release before it fails closed on that
  resume, as for every document version; fix forward
  ([production runbook](../runbooks/production.md#rollback)).

### Security, privacy, and size

- The field is a closed enum; the server and the editor reject anything else. No
  value from it is written into CSS. The dark colors are computed from the
  already validated `#rrggbb` colors and written as `#rrggbb` custom property
  values in the inline style, which the public HTML validator already checks.
- The page adds no script and no request. Match device is a CSS media query
  evaluated in the viewer's browser; nothing about the viewer's preference
  reaches aboutme ([ADR 0022](../adr/0022-viewer-privacy-and-counting.md)).
- The field adds at most 23 bytes to a document. A dark or Match device page
  adds about 1.5 KB of inline custom properties: 13 roles at up to three scopes.

### Editor placement and copy

The field is the first item of the Design panel's Colors group, above the five
colors. It uses the same select control as the panel's other enum fields, and it
shows and clears an absent value as `font.textAlign` does.

| Item         | Vietnamese                                                                    | English                                                               |
| ------------ | ----------------------------------------------------------------------------- | --------------------------------------------------------------------- |
| Label        | `Giao diện trang web`                                                         | `Web page theme`                                                      |
| Option light | `Sáng`                                                                        | `Light`                                                               |
| Option dark  | `Tối`                                                                         | `Dark`                                                                |
| Option match | `Theo thiết bị`                                                               | `Match device`                                                        |
| Hint         | `Người xem thấy giao diện này trên trang công khai. PDF và bản in luôn sáng.` | `Readers see this on your public page. The PDF and print stay light.` |

The label says web page because the setting applies only there. Match device
says what readers get in plainer words than System.

### Editor preview

- Web mode shows the chosen scheme: the continuous sheet takes the dark ground
  and roles. The sheet keeps its 2 px radius and paper shadow.
- Match device follows the editing device's `prefers-color-scheme`, which is
  what a reader on the same device sees. The editor's own light or dark theme
  does not drive the preview.
- PDF mode is always light, because it shows the PDF.
- Template thumbnails, the Library, template pages, the homepage sample, and the
  new-resume screens always render light.

### No viewer toggle

The public page has no viewer toggle. Match device already follows each reader's
own setting, and a toggle would override the owner's explicit Light or Dark. A
remembered choice would also need a script to run before first paint, which the
public page's strict script policy forbids, so the page would flash the wrong
scheme on every load.

## Dark palette rule

One pure function maps the resume's five authored colors to five dark source
colors. The unchanged role derivation of
[Color roles](templates/colors.md#4-color-roles) then runs on those sources,
once per surface, exactly as for light. No template has hand-picked dark colors.

All steps use OKLCH. A color outside sRGB is clipped by lowering its chroma in
0.002 steps at the same lightness and hue. Outputs are `#rrggbb`.

1. **Already dark.** If `colors.background` has lightness below 0.5, the dark
   scheme equals the light one and the steps below do not run.
2. **Tone.** The tone is the hue and chroma of `colors.background` when its
   chroma is at least 0.01 (a tinted paper), otherwise of `colors.primary`.
3. **Ground.** The dark `background` is
   `oklch(0.20, min(0.25 × toneC, 0.015), toneH)`: a near-black carrying a trace
   of the template's own hue.
4. **Text.** The dark `text` is `oklch(0.90, min(textC, 0.015), textH)`.
5. **Primary and accent.** Each keeps its hue and chroma and takes lightness
   `min(max(L, 1.10 − L), 0.94)`. A dark navy becomes a light steel blue; a
   mid-tone orange keeps its lightness and the clamp lifts it.
6. **Tinted surface.** A light `colors.surface` (lightness 0.5 or more) becomes
   `oklch(0.27, min(2 × C, 0.035), H)`, one step above the ground. A dark
   surface keeps its hue and chroma and takes lightness `max(L, 0.30)`, so an
   authored dark band stays a band above the ground.

The role derivation then applies its floors against the dark surfaces: 4.5:1 for
body, headings, meta, and links; 3:1 for the name and for level fills and tag
fills. In the dark scheme every clamp is the hue-preserving search; the light
scheme's shortcut that sends failing text on a dark band straight to white does
not apply, so headings keep their hue.

Other rules:

- Photos, and any image, render unchanged: no filter, no dimming, no border.
- Contact icons and brand marks take the dark `--color-meta`, as in light.
- The link underline stays, as every inline link's does.
- `customization.colors` is never rewritten. The dark sources are derived
  values, like every other role.

### How it reaches the page

- `RenderContext` gains an optional `colorScheme` of `dark` or `system`. Only
  the public page and the editor's Web preview pass it; paged mode ignores it.
  `resolveRenderModel` then computes the dark role set and writes each dark role
  next to its light role at every scope that carries roles (the article, a
  tinted header band, a tinted sidebar), as `--dark-color-<role>`.
- `.public-resume-page`, and the editor's Web preview sheet, carry
  `data-color-scheme="dark"` or `"system"`; light carries no attribute.
- A screen-only rule in `resumeColorScheme.css`, which `ResumeDocument.vue`
  imports, points each `--color-<role>` at its `--dark-color-<role>` under
  `dark`, and under `system` inside `@media (prefers-color-scheme: dark)`. On
  the public page the rule also sets `color-scheme: dark` and
  `background: Canvas` on the root through
  `:root:has(.public-resume-page[data-color-scheme])`, so scrollbars and
  overscroll turn dark. A root rule cannot read the page element's inline
  colors, so the root takes the browser's dark Canvas, not the template's
  ground. The page fills the viewport with its own ground (`min-height: 100vh`),
  so Canvas shows only in overscroll. The rule needs no script, so the first
  paint is already correct.
- `light-dark()` is not used: Safari before 17.5 drops it, which would leave
  text black on the dark ground.
- The page adds no `theme-color` meta; the HTML validator rejects it.

### Template mapping

The rule gives these dark values for each preset's own colors, computed by a
reference model of the rule. The unit test pins the implementation's values, and
a user's own colors go through the same rule.

| Template           | Ground    | Body      | Heading   | Link      | Tinted region     |
| ------------------ | --------- | --------- | --------- | --------- | ----------------- |
| academic-dense     | `#12171c` | `#d9dfe4` | `#aec9e6` | `#aec9e6` | none              |
| ats-plain          | `#161616` | `#dedede` | `#ebebeb` | `#ebebeb` | none              |
| classic-serif      | `#151617` | `#dadee6` | `#dbe0e7` | `#92b0e5` | none              |
| consulting-formal  | `#11171d` | `#d8dfe5` | `#9fbfe6` | `#6a9dd5` | none              |
| creative-accent    | `#1c1411` | `#e4dcd8` | `#dc5330` | `#dc5330` | `#352019` header  |
| designer-tag       | `#151711` | `#e0dddb` | `#acba99` | `#778d4e` | none              |
| editorial-wide     | `#191512` | `#e1ddd7` | `#cbb9a7` | `#b4805a` | none              |
| elegant-serif-two  | `#1c1315` | `#e3ddd7` | `#db8b99` | `#db8b99` | `#2f2512` sidebar |
| engineer-compact   | `#10171c` | `#d8dfe6` | `#92bcd4` | `#5a9db9` | none              |
| executive-band     | `#13161b` | `#d8dfe8` | `#b4c9e6` | `#a6753b` | `#1d2f45` header  |
| government-formal  | `#161616` | `#dedede` | `#ebebeb` | `#ebebeb` | none              |
| graduate-friendly  | `#1c1410` | `#e3ddd7` | `#c88262` | `#bc6934` | `#322313` header  |
| high-contrast      | `#161616` | `#dedede` | `#ebebeb` | `#5085ff` | none              |
| international-lang | `#171615` | `#e1ddd9` | `#e0dcd9` | `#a79483` | `#2a261d` header  |
| minimal-air        | `#151617` | `#dadee5` | `#dbdee4` | `#dbdee4` | none              |
| modern-sidebar     | `#0f171b` | `#d5e0e6` | `#88b7ce` | `#4090a3` | `#1b292f` sidebar |
| mono-print         | `#161616` | `#dedede` | `#ebebeb` | `#ebebeb` | none              |
| nordic-muted       | `#12171a` | `#d7dfe8` | `#8fa8bd` | `#63859b` | `#24272a` sidebar |
| one-page-tight     | `#131619` | `#d8dfe6` | `#aec0cf` | `#8ba6b9` | none              |
| startup-bold       | `#151617` | `#d8dee8` | `#e8ebf0` | `#e8ebf0` | none              |

Across all twenty, on every surface: body at least 10.1:1, meta 6.4:1, headings
4.5:1, links 4.5:1, and level and tag fills 3.5:1. Link values are those on the
ground; a tinted region clamps its own.

No template needs a hand-made exception. Two rule branches cover the special
cases:

- **executive-band** is the one preset with a dark authored band (`#16273d`).
  Step 6 keeps it navy and lifts it to `#1d2f45`, so the band still reads above
  the near-black ground, and its heading turns light blue rather than white.
- **Already dark** (step 1) covers a user who picked a dark background in the
  light scheme. No preset does.

## What stays light

| Output                                      | Why it stays light                                          |
| ------------------------------------------- | ----------------------------------------------------------- |
| PDF download and the owner's PDF            | Snapshot drops the field; the print app passes no scheme    |
| Browser print of the public page            | The dark rule is screen-only; print uses the light roles    |
| Link-preview card                           | The card envelope carries no scheme                         |
| Sign-in gate page                           | aboutme chrome before the resume; its envelope is unchanged |
| Library, template pages, thumbnails, sample | Their render context carries no scheme                      |
| "What an ATS reads" tab                     | A sample's text on a `.paper-surface` panel, unchanged      |
| `/{slug}.md` and the PDF text layer         | Text only; the scheme changes no content, order, or wording |

The ATS text is the same in both schemes: the scheme changes only colors, never
the DOM order, text, or hidden state.

## Display checks and tests

[Display assertions](display-assertions.md) owns the accepted screen/PDF
inventory, owner-scheme/viewer-preference matrix, and independent expected
colors. It replaces public page PNG comparisons only after Linux/macOS
acceptance. Until cutover, keep the existing light, dark, and system comparisons
and golden HTML. Paged, print, template, card, gate, and landing surfaces remain
light; the assertion change does not alter visual values or schema behavior.

Tests to add or change:

- The public page geometry test keeps its checks and adds: the bar spans the
  viewport, the download button is at most 32 px high on a fine pointer, and the
  bar is hidden in print.
- The Go validator tests pass the new bar markup in Vietnamese and English, with
  download on and off.
- A unit test runs the dark rule over all twenty presets and both columns and
  asserts every floor above on every surface.
- A dark public page under print media computes the light surface color.
- The render and hydration tests cover `data-color-scheme` and the bar markup.
- Go tests cover the v4 and v5 converters in both directions, the declared v4
  loss, a v1 to v4 write keeping the stored scheme, the customization allowlist
  set and unset paths, the public projection copying it, and the print snapshot
  dropping it.
