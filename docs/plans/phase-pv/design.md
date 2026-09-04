# Application visual identity: the stamped document

Status: **Approved by the human owner** (2026-09-04). ADR 0030 and the design
amendments are recorded by T00; this file is deleted when the phase exits.

## 1. What is wrong today

Evidence: `.dev/design-qa/current/` (light and dark, 1440 and 390 wide, captured
2026-09-04 against `main` at `27747a2`).

| Surface     | Finding                                                                                                                                                                                                                                          |
| ----------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Landing     | A centered headline, two buttons, three bullets. The product's artifact, a resume at a link, never appears. Signed-in visitors still see "Sign in" and "Create account" in the hero.                                                             |
| Chrome      | The theme toggle is a solid black button, the heaviest element on every page. Brand is a small bold word with no mark.                                                                                                                           |
| Sign in     | A card floating high on an empty page; the "Show" button does not match the field height; title and first label touch.                                                                                                                           |
| Resume list | A card inside a card. Rename and Delete are black primary buttons on every row. No publish state, no link, no updated time; the empty state says only "No resumes yet."                                                                          |
| Settings    | Unstyled: raw user-agent strings, ISO timestamps, native buttons, no layout. The dark theme chosen in the editor did not carry over.                                                                                                             |
| Editor      | Sound four-region layout. Three black controls compete in the top bar. Every text field carries Set, Clear, and Remove. Customization labels are schema paths (`font.family`, `spacing.entryGap`) and raw enum values (`be-vietnam-pro`, `bar`). |
| Phone       | Rail, outline, and inspector overlap; the inspector is clipped ("Personal detail", "Remo"); the top bar wraps the account controls into a second row.                                                                                            |
| Identity    | Zinc, Inter, emerald: the shadcn default with no decision behind it. Nothing on any page is specific to a resume builder for Vietnamese job seekers.                                                                                             |

The `codex/phase-pu` branch fixes the field controls, dialogs, and the settings
markup but keeps the default identity by decision U6. PV keeps PU's structure
and replaces the identity.

## 2. The world

Direction chosen by the owner on the Impeccable decision page (seed `aac522e4`,
pick card). Contract: `.impeccable/surfaces/apps-web-app-pages-index-vue.md`.

**Thesis.** The resume is a document and publishing is stamping it. A round red
seal at the sheet's foot, pressed by a person, never by an agent, is the only
red on any page. The chrome is the desk; the resume is the paper; the person is
never on screen.

**Refused.** The template-carousel hero, profile cards, avatars as identity,
colored status chips, black primary buttons, uppercase labels outside the seal.

### Tokens

Tokens live on `:root` and `html[data-theme='dark']` in
`apps/web/app/assets/css/theme.css` (PU's file) and map through `@theme inline`
to Tailwind names. The shadcn semantic names stay so generated primitives keep
working; the values change.

| Token                         | Role                                                                                      | Light                                                          | Dark (lamp-lit desk)                                   |
| ----------------------------- | ----------------------------------------------------------------------------------------- | -------------------------------------------------------------- | ------------------------------------------------------ |
| `--background` (desk)         | Page ground, preview canvas                                                               | `#EDEFEB`                                                      | `#121614`                                              |
| `--card` / `--popover`        | Panels, dialogs, inputs                                                                   | `#FFFFFF`                                                      | `#1A1F1C`                                              |
| `--foreground` (ink)          | Text                                                                                      | `#171A18`                                                      | `#ECEFEC`                                              |
| `--muted-foreground` (pencil) | Secondary text, draft and saved marks                                                     | `#5F6763`                                                      | `#9AA39E`                                              |
| `--border` / `--input` (rule) | Hairlines, field borders                                                                  | `#D8DDD9`                                                      | `rgba(255,255,255,0.12)`                               |
| `--secondary` / `--accent`    | Hover and selected fills                                                                  | `#E3E6E1`                                                      | `#242A27`                                              |
| `--primary` (signature)       | The person's own actions                                                                  | `#1F2A44` on `#FFFFFF`                                         | `#D7DEEE` on `#171A18`                                 |
| `--seal`                      | The seal and the public state only                                                        | `#C8102E` on `#FFFFFF`                                         | same, unchanged: ink does not change under a lamp      |
| `--destructive`               | Delete, revoke, errors                                                                    | `#B42318`                                                      | `#F0736A`                                              |
| `--ring`                      | Focus                                                                                     | `#1F2A44`                                                      | `#D7DEEE`                                              |
| `--radius`                    | Controls 6 px; dialogs and list sheets 8 px; the preview sheet 2 px; the seal is a circle |                                                                |                                                        |
| `--shadow-paper`              | Under paper only (preview sheet, list sheets, dialogs)                                    | `0 1px 2px rgba(23,26,24,.06), 0 12px 32px rgba(23,26,24,.10)` | `0 1px 2px rgba(0,0,0,.4), 0 12px 32px rgba(0,0,0,.5)` |

Removed: `--positive`, `--positive-foreground`, `--chart-*`. Nothing green
remains; "saved" is a pencil tick.

The preview sheet is rendered by the renderer with the document's own
`colors.background`, so it stays white in the dark theme without any chrome
rule. The canvas around it is the desk token.

### Type

One family for the chrome: `'Be Vietnam Pro', 'Inter', system-ui, sans-serif`.
Be Vietnam Pro is catalog rank 1, already bundled as a 100–900 variable font in
`fonts.css`, designed for Vietnamese diacritics, and no product's default UI
face. Inter stays as the fallback.

| Step | Size (rem / px) | Weight  | Use                          |
| ---- | --------------- | ------- | ---------------------------- |
| xs   | 0.75 / 12       | 400     | Timestamps, hints            |
| sm   | 0.8125 / 13     | 400–500 | Editor body, labels, buttons |
| base | 0.875 / 14      | 400     | Page body in the app         |
| md   | 1 / 16          | 400     | Landing and auth body        |
| lg   | 1.25 / 20       | 600     | Panel and section titles     |
| xl   | 1.5 / 24        | 600     | Page titles                  |
| 2xl  | 2 / 32          | 700     | Landing headline at 390 px   |
| 3xl  | 2.75 / 44       | 700     | Landing headline at 1440 px  |

Line height 1.5 for body, 1.2 for titles; headline tracking `-0.02em`; the seal
ring text is the only uppercase, tracked `+0.08em`. Running text stays under 72
characters. Digits in device lists and timestamps use
`font-variant-numeric: tabular-nums`.

### Spacing, structure, motion

- One 8 px module: control heights 32 px (editor) and 36 px (pages); panel
  padding 16 px; page gutters 24 px; section gap 32 px on pages, 16 px in
  panels.
- Borders and shadows are spent by role. A hairline rule separates regions and
  sections. A shadow appears only under paper. A card appears only where the
  thing is a sheet (list items, dialogs).
- The sheet stays whole at every width: the preview scales to fit and is never
  cropped; chrome restacks around it.
- Motion answers actions only: 150 ms color transitions, the primitives' own
  open and close, and the stamp (below). Nothing animates on page load.
  `prefers-reduced-motion` disables all of it.

### The seal

`apps/web/app/components/app/AppSeal.vue`: an inline SVG circle with ring text
on a `<textPath>` and a center word, `role="img"` with a plain-language
`aria-label`. Sizes: `mark` (20 px, ring omitted, a solid seal with a check
glyph, used inline next to titles and in the list) and `stamp` (96 px, ring
text, rotated `-8deg`, used on the landing and on the publish success state).
Ring text is the link in uppercase, `PUBLIC RESUME · ABOUTME.VN/{SLUG} ·`;
center text is `aboutme`. The seal uses `--seal` only.

### State marks

`apps/web/app/components/app/StateMark.vue` replaces `SaveStatus.vue` and any
colored chip: `saved` (pencil tick glyph + "Saved"), `saving` ("Saving…"),
`failed` ("Save failed", destructive), `draft` ("Draft", pencil), `public` (seal
mark + `aboutme.vn/{slug}`). State is a mark, not a hue.

## 3. Surfaces

### Landing (`/`)

Two columns on the desk at 1440. Left five twelfths: headline "The resume is
public. You are not.", one lead sentence, "Create account" (signature) then
"Sign in" (text link). Right seven twelfths: the `full` fixture (Ada Lovelace)
rendered through `ResumeDocument` at server render, as a white sheet at 0.6
scale with the paper shadow and the 96 px seal at its lower right reading the
sample link. Below: the three existing facts in one ruled row, then "Publishing
is three choices" as three ruled columns (Public resume, PDF download, SEO and
GEO) with one sentence each in the product's own words, then the license line.
Signed-in visitors see "Open your resumes" instead of the two entry buttons. No
data fetch; the fixture is compiled in. Stacks to one column at 42 rem with the
sheet under the headline, still whole.

### Sign in, register, recovery, verification, consent

The form sits on the desk with no card: a left-aligned title (`xl`), fields on
the 8 px module, one signature primary button, secondary links below. The
password toggle is an icon button inside the field at the field's height. Errors
are a ruled banner above the form in destructive ink. The consent page lists the
client name and scopes as a plain ruled list with Approve (signature) and Deny
(text).

### Resume list (`/app/resumes`)

Title "Resumes" with "Create resume" (signature) on the right. Up to three
sheets on the desk in a row (one column at 42 rem): each is a white sheet (8 px
radius, paper shadow) whose whole face opens the editor, carrying the title
(`lg`), "Updated 2 hours ago" (pencil), and a state mark: `public` with the seal
mark and link, or `draft`. Rename and Delete live in an overflow menu on the
sheet. Missing slots up to three render as dashed outlines that read "Create
resume"; the empty state is the same three outlines with the first one carrying
the title "No resumes yet." and the primary action.

### Settings (`/app/settings/sessions`)

Title "Settings". Sections divided by rules, each with a `lg` title: Signed-in
devices, Password, Connected agents (only when `agentAccess`), and provider
linking (only when `providerLogin`). A device row reads "Chrome 152 on Linux",
"Last seen 2 hours ago" in tabular pencil, "This device" as a pencil mark on the
current row, and Log out or Revoke as a secondary button. "Log out everywhere"
is a destructive-outline button under the list.

### Editor shell (`/app/resumes/{id}`)

The four-region grid is unchanged at 1440: rail 64 px, outline 264 px, preview,
inspector 352 px. Top bar: brand, title with the `saved` mark and, when public,
the `public` mark; on the right "Publish" (the only `--seal` control on the
page) and an account menu (avatar button opening Settings, theme, Log out). The
theme toggle leaves the bar. Rail buttons carry tooltips and a signature tint
when active. Outline rows use the section's own icon from
`components/resume/icons.ts`. The preview canvas is the desk with the sheet on
it, "1 page" as a pencil mark, no header bar.

Narrow (below 72 rem): outline and inspector stack in the edit view, the rail
becomes a horizontal strip above the inspector. Phone (below 42 rem): a fixed
bottom switch "Edit | Preview" with safe-area padding; the outline opens as a
Sheet from a "Sections" button; the preview fits the sheet to width, whole.

### Inspector

PU's composites and commit-on-blur fields stay. Customization labels become
words: "Font", "Base size (px)", "Section gap", "Entry gap", "Line height",
"Heading style", "Heading rule", "Columns", "Skill display", "Language display",
"Page size", "Page margin"; grouped into Type, Spacing, Headings, Layout, Colors
fieldsets. Enum values render their display names (fonts from `catalog.json`,
"Bar", "Dots", "Uppercase", "Letter", "A4"). Contact details are one compact row
each (type, value) with an overflow for label, hide, move, and remove.

### Publish dialog

reka Dialog on the Dialog primitive. Title "Publish resume". The slug field
shows `aboutme.vn/` as a prefix inside the field. Three switches with their
existing explanations beneath each. "Publish" in `--seal`; every other button
neutral. On success the dialog shows the `stamp` seal with the canonical link
and a "Copy link" button; in the preview the seal `mark` appears beside the
title in one 180 ms press (scale 1.12 to 1, opacity 0 to 1). Unpublish removes
it in 120 ms. Reduced motion: no transition.

### Public page and renderer

No change. The renderer's output, tokens, fonts, golden HTML, and screenshots
are the proof that the chrome cannot touch the document.

## 4. Copy

Sentence case. Buttons name the action. Labels are the person's words, never a
schema path. Errors say what happened and what to do. Strings a browser proof
asserts stay verbatim unless the task file lists the change.

## 5. Accessibility

No serious or critical axe violation on any page in both themes. Dialogs, menus,
the sheet, and the rail are keyboard operable with a visible `--ring` focus.
Seal red on white is 5.9:1; signature on white 12.4:1; pencil on desk 5.2:1. The
seal has an `aria-label`; state marks have text. Reduced motion is honored
everywhere.
