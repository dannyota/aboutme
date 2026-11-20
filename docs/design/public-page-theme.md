# Public page bar and color scheme

This page specifies two changes to the public resume page at `/{slug}`: a
compact page bar in the application identity, and an owner-chosen color scheme
for the desk and the bar around the resume's pages. The resume pages, the PDF,
print, and the link-preview card stay light in every case.

Status: approved (2026-09-27) and built. The owner approved the direction: light
by default, the owner chooses the scheme per resume, and viewers see the owner's
choice. The dark scheme changes the white-sheet rule of
[ADR 0020](../adr/0020-application-visual-identity.md) and adds document v5 to
[ADR 0004](../adr/0004-resume-document-contract.md). Both records now carry the
change as accepted (2026-09-27); the page bar needs no contract change. On
2026-11-19 the owner kept the pages light in Dark
([PDF pages](public-pdf-pages.md#owner-decisions)): the scheme sets only the
desk and the bar.

## Owner decisions

- Light is the default for every resume, new and existing.
- The owner picks the scheme for each resume in the editor: Light, Dark, or
  Match device. Match device follows each viewer's `prefers-color-scheme`.
- Viewers see what the owner chose. The page has no viewer toggle (see
  [No viewer toggle](#no-viewer-toggle)).
- The resume pages, the PDF, browser print, and the link-preview card are always
  light; the scheme sets the desk and the bar (2026-11-19).

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
   follows the page's scheme with its own palette, so a light card never sits on
   a dark page. Its card floats with `--shadow-float`, and its Create action is
   a main pill (`#594BAD` light, `#7267C0` dark, white label).
6. **Editor copy and preview.** Approved (2026-09-27): the field copy below. The
   Web preview was removed on 2026-11-19; the editor shows the PDF preview.

## Page bar

The [page bar](public-page-bar.md) design owns the bar's structure, copy,
tokens, size and placement, and behavior.

### Structure

See [Structure](public-page-bar.md#structure).

### Tokens

See [bar tokens](public-page-bar.md#tokens).

### Size and placement

See [Size and placement](public-page-bar.md#size-and-placement).

### Behavior

See [Behavior](public-page-bar.md#behavior).

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

| Item         | Vietnamese                                                                                                | English                                                                                                          |
| ------------ | --------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| Label        | `Giao diện trang web`                                                                                     | `Web page theme`                                                                                                 |
| Option light | `Sáng`                                                                                                    | `Light`                                                                                                          |
| Option dark  | `Tối`                                                                                                     | `Dark`                                                                                                           |
| Option match | `Theo thiết bị`                                                                                           | `Match device`                                                                                                   |
| Hint         | `Người xem thấy giao diện này ở nền và thanh của trang công khai. Các trang CV, PDF và bản in luôn sáng.` | `Readers see this behind the pages and in the bar of your public page. The pages, the PDF and print stay light.` |

The label says web page because the setting applies only there. Match device
says what readers get in plainer words than System.

### Editor preview

- The editor shows only the PDF preview, always light, because the public page
  shows the same pages. The scheme changes only the public page's desk and bar.
- Template thumbnails, the Library, template pages, the homepage sample, and the
  new-resume screens always render light.

### No viewer toggle

The public page has no viewer toggle. Match device already follows each reader's
own setting, and a toggle would override the owner's explicit Light or Dark. A
remembered choice would also need a script to run before first paint, which the
public page's strict script policy forbids, so the page would flash the wrong
scheme on every load.

## Desk and bar

The [PDF pages](public-pdf-pages.md#what-a-viewer-sees) design owns the desk
color per scheme, and the [page bar](public-page-bar.md) its scoped tokens. The
[dark palette rule](public-page-dark-palette.md) is superseded: no rule darkens
the resume.

## What stays light

| Output                                      | Why it stays light                                          |
| ------------------------------------------- | ----------------------------------------------------------- |
| PDF download and the owner's PDF            | Snapshot drops the field; the print app passes no scheme    |
| Resume pages on the public page             | Pages keep the PDF's colors in every scheme (PDF pages)     |
| Browser print of the public page            | Print shows the continuous article on its authored ground   |
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
colors. Hosted Linux CI gates these assertions and their mutation tests. Keep
golden HTML and inspect optional review images. Paged, print, template, card,
gate, and landing surfaces remain light; the assertion change does not alter
visual values or schema behavior.

Tests to add or change:

- The public page geometry test keeps its checks and adds: the bar spans the
  viewport, the download button is at most 32 px high on a fine pointer, and the
  bar is hidden in print.
- The Go validator tests pass the new bar markup in Vietnamese and English, with
  download on and off.
- A dark public page keeps light pages on screen and in print; the desk and bar
  follow the scheme.
- The render and hydration tests cover the desk scheme and the bar markup.
- Go tests cover the v4 and v5 converters in both directions, the declared v4
  loss, a v1 to v4 write keeping the stored scheme, the customization allowlist
  set and unset paths, the public projection copying it, and the print snapshot
  dropping it.
