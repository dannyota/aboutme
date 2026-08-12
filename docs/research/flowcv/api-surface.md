# FlowCV live API surface — read-only probe

Purpose: find resume/CV capabilities in the live FlowCV product that the local
notes at `/home/danny/src/flowcv/docs/API.md` and `PULLPUSH.md` do not document.
Record the real shape of a FlowCV resume document.

Every statement below is tagged **OBSERVED** (seen in a response) or
**INFERRED** (reasoned from what was seen). External behaviour is evidence, not
authority for this project's design.

## Method and redaction

Probe was read-only: no create, update, delete, publish, unpublish, or push. The
account holds one real resume, so **all values are redacted** — only structure
is recorded (field names, types, nesting, enum tokens, bounds). Raw responses
were captured to a scratchpad outside the repo and reduced with a masking
script; no credential, cookie, token, share URL, or personal value appears here.

### Commands run

| Command                                  | Result                                           |
| ---------------------------------------- | ------------------------------------------------ |
| `flowcv --version`                       | `flowcv 0.7.0` (pip `flowcvcli`)                 |
| `flowcv --help`, `flowcv <cmd> --help`   | local help only, no API call                     |
| `flowcv doctor`                          | auth valid via cached session; 1 resume resolves |
| `flowcv export --format flowcv -o <tmp>` | 26 KB raw resume JSON (one `GET /resumes/{id}`)  |
| `flowcv templates --json`                | 105 template records                             |
| `flowcv share --json`                    | `{live: bool, url: string}`                      |
| `flowcv icons --json`                    | 20 icon keys (client-side list)                  |
| `flowcv resumes --json`                  | 1 record: `{id, title, webToken, live}`          |

OBSERVED: the CLI authenticates from `.env` (email/password) via a cached
session file. Every command above returned successfully. Mutating subcommands
(`new`, `add`, `rm`, `field`, `pd`, `customize <path> <value>`, `publish`,
`unpublish`, `push`, `import`, `apply-template`, `download`) were **not run**.
`download` is nominally a GET but appends to the server-side `downloads`
history, so it was treated as a mutation and skipped.

## Resume document skeleton (redacted)

OBSERVED top level of `GET /resumes/{id}`:

```text
id                      <uuid>
userId                  <uuid>
mongoId                 ""            # legacy, empty
title                   <string>
order                   int           # 0 for the only resume
feedbackToken           <string:10>
webToken                <string:12>
uuid                    <uuid>        # distinct from id
feedbackEnabled         bool
webResumeLive           bool
webResumeDownloadBtn    bool
webResumeSearchIndex    bool
webResumeCached         bool
personalDetails         object
content                 object        # sectionId -> section
customization           object        # ~130 leaf paths, see below
feedback                object        # {} when unused
businessDetails         object        # {} on a personal account
downloads               array         # download history, see below
usingBusinessTemplateId null
schemaVersion           "21"          # string, not int
lastChangeAt            <iso8601>
createdAt / updatedAt   <iso8601>
lng                     "en"
tags                    []
```

### `content` — sections

OBSERVED: `content` is a map
`sectionId -> {entries[], displayName, iconKey, sectionType}`. Canonical
sections key on their type name (`profile`, `work`, `education`, `skill`);
**custom sections key on a generated 21-character token** (two distinct custom
sections observed, both `sectionType: "custom"`, each with its own `displayName`
and `iconKey`). `displayName` and `iconKey` were observed **empty strings** on
the canonical sections — INFERRED: empty means "use the built-in default
heading/icon", not "no heading".

### Entry fields per section type

OBSERVED field sets. A field absent from a section below was absent from the
response, not empty — INFERRED: FlowCV omits never-set keys rather than storing
a sentinel.

| sectionType | Domain fields observed                                                                                                                           |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `profile`   | `text` (HTML, `<p>`)                                                                                                                             |
| `work`      | `jobTitle`, `employer`, `employerLink`, `city`, `country`, `startDate`, `endDate`, `startDateNew`, `endDateNew`, `description` (HTML `<ul><li>`) |
| `education` | `degree`, `school`, `schoolLink`, `city`, `country`, `startDate`, `endDate`, `startDateNew`, `endDateNew`                                        |
| `skill`     | `skill` (the label — not `name`), `isHidden`                                                                                                     |
| `custom`    | `title`, `titleLink`, `description` (HTML `<p>`, `<strong>`), `isHidden`                                                                         |

Shared on every entry (OBSERVED): `id`, `createdAt`, `updatedAt`, plus
server-side bookkeeping `created_at`, `updated_at` (snake_case duplicates),
`isNewEntry` (bool), `showPlaceholder` (bool). `isHidden` appeared only on
`skill` and `custom` entries — INFERRED: written on first toggle, absent
otherwise, so consumers must default it to `false`.

Entry `id` format is **not uniform**: 21-character tokens on older entries and
36-character UUIDs on `skill` entries (both OBSERVED in the same document).

### Dates — two parallel representations

OBSERVED, per entry, for `startDate` / `endDate`:

```text
{
  year:              "YYYY" | null
  month:             "MM"   | null
  hide:              bool          # suppress this date in output
  ongoing:           bool          # "Present"-style open end
  onlyYear:          bool          # render year without month
  customOngoingWord: <string>      # word rendered when ongoing
}
```

Alongside them, `startDateNew` / `endDateNew` are flat display strings —
OBSERVED as `"YYYY"` and as a 7-character non-numeric token. INFERRED: the flat
pair is a denormalized render cache holding either `MM/YYYY`, `YYYY`, or the
ongoing word. Both representations must be written together to stay consistent.

### `personalDetails`

OBSERVED keys: `fullName`, `jobTitle`, `displayEmail`, `phone`, `address`,
`birthday` (object, `{}` when unset), `social` (object, `{}` when unset),
`detailsOrder` (array of detail keys — observed values were field names such as
`address`, `phone`, `displayEmail`), `photo`, `imageIdToSave`, `imageIdToDelete`
(both `""`), `showPlaceholder` (bool).

`photo` with no picture set contained **only** `{shape: "round"}` — INFERRED:
the crop fields (`imageId`, `xPct`, `yPct`, `widthPct`, `heightPct`,
`originalWidth`, `originalHeight`) documented in `API.md` appear only once an
image exists. `imageIdToSave` / `imageIdToDelete` are INFERRED to be a
staged-upload protocol: the client parks the new/old image id there and the
server reconciles on save.

### `downloads`

OBSERVED: array of 39 records, each
`{date: <iso8601-ish string>, type: string}`. Only enum value seen for `type`:
`"direct"`. INFERRED: other values exist for the email-delivery and public-link
paths. Every PDF render appends a record.

## Customization tree

OBSERVED: ~130 leaf paths under `customization`. Grouped, with the value domain
seen (values shown only where they are enum tokens, never user content):

- **Page/print** — `pageFormat` (`"A4"`), `fullDateFormat` (`"MM/DD/YYYY"`),
  `monthYearFormat` (`"MM/YYYY"`), `spacing.marginHorizontal`,
  `spacing.marginVertical`, `expert.footer.pages|email|name` (bools — the page
  number/footer toggles).
- **Layout** — `layout.detailsPosition` (`"top"`, and per-position sub-trees for
  `left` / `right`), `layout.colsFromDetails.{top,left,right}`
  (`"one" | "two" | "mix"`),
  `layout.colWidthsFromDetails.{top,left,right}.{leftWidth,rightWidth}` (integer
  percentages summing to 100).
- **Section order** — `sectionOrder.one.sectionsSorted`,
  `sectionOrder.two.leftSectionsSorted` + `rightSectionsSorted`, and
  `sectionOrder.mix` which is a **flat array**, not an object. All hold section
  ids; every content section id appeared exactly once per layout (OBSERVED).
- **Type** — `font.fontFamily`, `font.selected` (`"serif"`), `creativeNameFont`
  (`"bodyFont"`), `spacing.fontSize` / `lineHeight` / `spacingFactor` as
  **string-encoded steps** (`"1".."6"` seen) alongside absolute point sizes as
  numbers: `nameFontSizePt` 21, `jobTitleFontSizePt` 13.5,
  `sectionHeadingFontSizePt` 12, `titleAndSubtitleFontSizePt` 10.
- **Colors** — `colors.mode` (`"basic"`), then per mode: `selected`
  (`"single"`), `single` / `singleCustom` hex, and
  `multi.{accentColor, backgroundColor, textColor}` with a `multiCustom` twin —
  repeated under `colors.advanced` with `light` / `strong` variants. Page
  border: `colors.border.{top,right,bottom,left}` bools plus `width` (`"m"`),
  `selected`, `single`, `singleCustom`.
- **Accent application** —
  `applyAccentColor.{name, jobTitle, headings, headingLine, dates, icons, linkIcons, dotsBarsBubbles}`
  (8 bools).
- **Headings** — `heading.style` (**`"zigZagLine"` OBSERVED** — wider than the
  `"line" | "box"` enum in the local notes), `heading.capitalization`
  (`"uppercase"`), `heading.icons` (`"filled"`).
- **Header** — `header.accentuateName`, `detailsArrangement` (`"wrap"`),
  `detailsDisplayCenter` / `detailsDisplayLeftRight` (`"icon"`), `detailsGrid`,
  `iconFrame` (`"none"`), `iconFrameStyle` (`"filled"`), `jobTitlePosition`
  (`"below"`), `jobTitleStyle` (`"normal"`),
  `header.photo.{show, size ("m"), grayscale}`,
  `header.photoPositionFromHeaderPosition.{top,left,right}` (`"right"`).
- **Entry layout** — `entryLayout.displayMode` (`"dateLocationRight"`),
  `dateLocationOrder` (`"dateLocation"`), `dateStyle` / `locationStyle`
  (`"normal"`), `subtitleStyle` (`"italic"`), `dateLocationOpacity`,
  `bodyIndentation` (`"0"`), `colMode` (`"auto"`), three column-width triples
  (`dateContentLocation` 20/63/17, `dateLocationLeft` 22/78, `dateLocationRight`
  76/24), and five `…Placement` knobs (`"trySameLine"` / `"below"` / `"right"`).
- **Per-section display** — `skillDisplay`, `languageDisplay`,
  `certificateDisplay`, `interestDisplay` each with `selected`
  (`"grid" | "text"`), `grid.columns` (`"two" | "three"`), `text` (`"bullet"`),
  `subinfoSeparator` (`"dash"`), and for skill/language a `level.selected`
  (`"text"`); plus `educationDisplay.degreeBeforeSchool`,
  `workDisplay.jobTitleBeforeEmployer`,
  `declarationDisplay.{line, position, showHeading}`, `customSkillSections`.
- **Misc** —
  `advanced.{linkIcon ("boxArrow"), listStyle ("bullet"), groupPromotions}`,
  `expert.showProfileHeading`, `expert.subTitlePlacement`, `lastUsedTemplateId`
  (uuid), `unsplashImageHistory` (see below).

### Templates and icons

OBSERVED: 105 templates, projected by the CLI to
`{id: uuid, title: string, premium: bool}` — **103 free, 2 premium**. INFERRED:
the catalog endpoint carries each template's full `customization` payload (the
local notes say so) and the CLI trims it.

OBSERVED icon keys, the CLI's verified set (20): `address-card`, `book`,
`briefcase`, `certificate`, `chart-line`, `code`, `flask`, `globe`,
`graduation-cap`, `head-side-brain`, `heart`, `house-user`, `language`,
`lightbulb`, `newspaper`, `shield-check`, `star`, `trophy`, `users`, `wrench`.
INFERRED (from CLI help): any FontAwesome-style key may be accepted.

### Publishing / export

OBSERVED: `flowcv share --json` returns `{live: bool, url: string}` — a single
public URL derived from `webToken`. The resume object carries four independent
publish flags: `webResumeLive`, `webResumeDownloadBtn`, `webResumeSearchIndex`,
`webResumeCached`. OBSERVED (CLI help): `download` takes `--pages` (default 10)
and `--token` for fetching a public resume's PDF without auth; `export` supports
`--format flowcv | jsonresume`.

Out of scope, noted only for completeness: cover letters, job tracker, email
signatures, personal websites, and AI tools exist on the same account.

## Not in the local notes

Fifteen capabilities or shape details that `API.md` / `PULLPUSH.md` do not cover
(or state differently):

1. **Structured date objects** — `startDate` / `endDate` with `hide`, `ongoing`,
   `onlyYear`, `customOngoingWord`. The notes mention only the flat
   `startDateNew` / `endDateNew` strings; both representations coexist.
2. **Custom section ids are generated tokens**, and multiple `custom` sections
   coexist. The notes name a single `custom1`.
3. **Non-uniform entry ids** — 21-character tokens and UUIDs in one document.
4. **Per-entry bookkeeping** — `created_at` / `updated_at` snake_case
   duplicates, `isNewEntry`, `showPlaceholder`.
5. **`downloads[]` history** (`{date, type}`, enum `"direct"`) — a server-side
   audit trail of every render.
6. **`webResumeSearchIndex` and `webResumeCached`** — indexing and cache flags
   on the public web resume, separate from `webResumeLive`.
7. **Resume feedback feature** — `feedbackEnabled` + `feedback` object; the
   notes list `feedbackToken` as a key but not the capability.
8. **Business/branded templates** — `businessDetails`,
   `usingBusinessTemplateId`.
9. **Unsplash background images** — `customization.unsplashImageHistory[]` with
   `photoId`, `urlFull` / `urlRegular` / `urlThumb`, `width`, `height`,
   `brightness`, `dominantColors[]`, and photographer attribution
   (`photographerId`, `photographerFullName`, first/last, `userHtmlLink`).
10. **`sectionOrder.mix` is a flat array**, unlike `one` / `two` which nest
    under `sectionsSorted` / `left|rightSectionsSorted`.
11. **`heading.style` enum is larger** than `"line" | "box"` — `"zigZagLine"`
    observed.
12. **Per-section display customization** for skill, language, certificate,
    interest, declaration, education, work — grid vs text, column count, level
    rendering, separators.
13. **Entry-layout geometry** — three column-width triples and five placement
    knobs.
14. **Mixed spacing units** — string-encoded steps and absolute point sizes in
    one `spacing` object.
15. **Accent-colour application matrix** — eight independent toggles.

Minor: `schemaVersion` is the string `"21"`; `photo` collapses to `{shape}` when
no image is set; `imageIdToSave` / `imageIdToDelete` form a staged-upload
protocol.

## Relevant to aboutme

Mapping against the
[resume aggregate's entry-field table](../../design/data.md#resume-aggregate).
"Richer" means FlowCV carries more than aboutme's contract; "absent" means
FlowCV has no equivalent.

| aboutme sectionType | FlowCV (OBSERVED unless noted)                                                | Verdict                                                        |
| ------------------- | ----------------------------------------------------------------------------- | -------------------------------------------------------------- |
| `profile`           | `text` (HTML)                                                                 | same                                                           |
| `work`              | same field names, plus dual date representation                               | richer (dates)                                                 |
| `education`         | same field names; `description` absent because unset                          | same                                                           |
| `skill`             | label field is `skill`, not `name`; `level` / `infoHtml` absent because unset | naming differs; level rendering lives in customization instead |
| `language`          | no entries in this account; `languageDisplay.level.selected` exists           | INFERRED same shape                                            |
| `certificate`       | no entries; `certificateDisplay.*` exists                                     | INFERRED same shape                                            |
| `project`           | no entries and no `projectDisplay` path observed                              | unverified                                                     |
| `custom`            | `title`, `titleLink`, `description`; `subtitle` / `city` absent because unset | same                                                           |
| every entry         | `id` + `isHidden` + four bookkeeping fields                                   | richer (bookkeeping)                                           |

Contract-level comparisons:

- **Date range.** aboutme's `{start:{y,m?}, end:{y,m?}|null, present:bool}` is a
  strict subset of FlowCV's object, which adds `hide`, `onlyYear`, a
  user-editable `customOngoingWord`, and a denormalized display string.
  aboutme's invariants (`present ⇒ end=null`) have no FlowCV equivalent —
  INFERRED that FlowCV lets its two representations drift.
- **Absence is meaningful.** aboutme's "a missing key means never entered"
  matches FlowCV's OBSERVED behaviour: unset domain fields and `isHidden` are
  absent from the response.
- **Entry ids.** aboutme requires UUIDs unique across the document; FlowCV mixes
  UUIDs and 21-character tokens. aboutme is stricter — no change needed, but an
  importer must accept non-UUID ids.
- **Customization allowlist.** aboutme plans a fixed allowlist of delta paths;
  FlowCV's live tree is ~130 leaf paths across 12 groups, a useful
  order-of-magnitude for sizing it.
- **Aggregate invariant.** aboutme requires every `content` section key to
  appear exactly once across `customization.layout.sections`. FlowCV holds the
  same relationship in `sectionOrder` and it held in the observed document — but
  FlowCV keeps **three** parallel orderings (`one`, `two`, `mix`) that must each
  stay in sync, which aboutme's single-array design avoids.
- **Publishing.** aboutme has `live` + `slug` + `seo_geo_enabled` + tombstones;
  FlowCV has an opaque `webToken` and four flags (live, download button, search
  index, cache). Richer on indexing/download gates, absent on slugs, reserved
  names, and tombstones.
- **Section metadata.** Both use `displayName` + `iconKey`. FlowCV's empty
  string meaning "built-in default" fits aboutme's draft-permissive rule.
- **Limits.** FlowCV free tier is one resume; aboutme allows three with a DB
  trigger. No FlowCV size bounds were observable via the read-only probe.

## Limitations

One account and one resume were read, so field sets reflect only what that
document populates: section types with no entries here (`language`,
`certificate`, `project`, `course`, `award`, `reference`, `interest`,
`publication`, `organisation`) are known only through their customization paths.
Enum domains are lower bounds — each path shows the single value currently set,
and discovering the rest would need writes this probe did not perform. No schema
or validation-error probing was attempted, so every type and bound claim comes
from one observed instance.
