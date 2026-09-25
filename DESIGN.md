# aboutme visual design

This is the living visual record of the implemented web application. The account
is private; the resume is the public document. The UI makes that distinction
visible through a colorful Aurora canvas, a calm white sheet of paper, and a
seal applied by the person.

## Product principle

The landing page leads with “CV của bạn. Chia sẻ theo cách của bạn.” by default
and “Your resume. Your link. Your control.” in English. It shows a compiled-in
resume rendered by the shared `ResumeDocument`, rather than a profile card or
template carousel. Publishing is a deliberate action with three named choices:
Public resume, PDF download, and SEO and GEO. The publish dialog also controls
the optional browser-tab title and emoji icon.

The editor, public page, and PDF use the same document renderer. Application
chrome may frame the renderer but does not change its output.

## Language coverage

Vietnamese is the default site language. The language choice persists in the
`aboutme-locale` cookie. Every application route renders in the chosen language:

- `/`, `/privacy`, and `/terms`.
- `/templates` and `/templates/{id}`.
- `/login`, `/login/second-factor`, `/register`, `/forgot-password`,
  `/reset-password`, and `/verify-email`.
- `/app/resumes`, `/app/new`, and the editor at `/app/resumes/{id}`.
- `/app/settings/sessions` and the agent-consent page `/authorize`.

The shell shows the language toggle on these routes. The editor top bar, the
editor's section sheet on phones, and the “Sign in to continue editing” dialog
carry their own toggle. A path outside this list renders in English.

Public resume chrome follows the resume language: Vietnamese resumes show “Tạo
bằng aboutme.vn” and “Tải PDF”; other languages show “Built with aboutme.vn” and
“Download PDF”. Resume content and renderer labels follow the resume's own
language, independent of the site language.

## Visual direction

Colorful product UI. Calm white resume.
[ADR 0050](docs/adr/0050-aurora-application-identity.md) records the Aurora
identity.

- The application canvas is a pale blue ground with large, soft radial glows:
  blue and indigo at the top corners, cyan and a faint pink further down. The
  glows are CSS gradients on the body background under `data-ui="app"`, scroll
  with the page, and never animate.
- The resume is a whole white sheet with a neutral paper shadow. It stays white
  in dark theme and never takes an Aurora token, gradient, radius, or shadow.
- Blue leads. Primary actions, links, focus, and the logo are blue. Indigo,
  purple, and cyan are supporting accents for templates, customization, and
  product capabilities. Pink and orange are small, rare accents. A section uses
  a few accents, never all of them.
- Red means public. Seal red marks the public state, the Publish action, and the
  seal, and nothing else. Destructive actions use the separate destructive
  token.
- State is communicated by a mark or plain text: a pencil tick for saved, text
  for saving and draft, destructive text for failure, and a seal plus link for
  public.
- Chrome alignment follows an 8 px module. The sheet is never cropped.
- Copy is sentence case. Controls name the action they perform. English copy
  uses “resume”; Vietnamese localized copy uses “CV”.

## Logo

The logo is lowercase `aboutme`. The mark is a rounded document with a folded
top-right corner whose body forms a lowercase single-story `a`, filled with a
cyan to blue to indigo gradient. The `a` is cut out of the body, so the
background shows through it. The wordmark follows the mark: `about` in the text
color and `me` in the blue-to-indigo brand gradient, drawn as round-capped
strokes so it needs no font.

`AppLogo` renders it inline at 24, 32, or 48 px high, or as the mark alone. It
is one image named “aboutme”. Under `forced-colors: active`, the mark and `me`
use the text color. `public/favicon.svg` is the mark alone with a plain fold.

## Typography and tokens

Application chrome uses `Be Vietnam Pro`, then `Inter`, `system-ui`, and
`sans-serif`. Uppercase text is used inside the seal. The renderer keeps its own
typography and tokens.

The semantic tokens below are defined on `:root` and switched by
`html[data-theme="dark"]`. The dark theme is midnight blue.

| Role                              | Light     | Dark                        |
| --------------------------------- | --------- | --------------------------- |
| Page background                   | `#F5F8FF` | `#071126`                   |
| Foreground ink                    | `#101B3F` | `#F4F7FF`                   |
| Card                              | `#FFFFFF` | `#0D1935`                   |
| Popover                           | `#FFFFFF` | `#132244`                   |
| Primary action and focus ring     | `#1A5CEB` | `#72A0FF`                   |
| Primary hover                     | `#1550D4` | `#8FB3FF`                   |
| Primary foreground                | `#FFFFFF` | `#071126`                   |
| Secondary and accent              | `#EAF2FF` | `#132244`, `#1A2B52`        |
| Muted                             | `#EDF1FA` | `#132244`                   |
| Secondary/muted/accent foreground | `#101B3F` | `#F4F7FF`                   |
| Muted foreground                  | `#56648C` | `#9EACCA`                   |
| Border                            | `#DCE5F5` | `rgba(180, 200, 255, 0.16)` |
| Input border                      | `#7886AE` | `#5A6A95`                   |
| Link                              | `#123EDB` | `#8FB3FF`                   |
| Seal                              | `#CC2649` | `#CC2649`                   |
| Editor canvas                     | `#EEF3FC` | `#071126`                   |
| Destructive                       | `#B42318` | `#F0736A`                   |

Brand and surface tokens have Tailwind color utilities such as `bg-surface-blue`
and `text-brand-indigo`:

| Token                                | Light                | Dark                 |
| ------------------------------------ | -------------------- | -------------------- |
| `--brand-blue`, `--brand-deep-blue`  | `#246BFD`, `#123EDB` | `#72A0FF`, `#4C7DFF` |
| `--brand-indigo`, `--brand-cyan`     | `#6254FF`, `#35C8F5` | `#8B80FF`, `#54D6FF` |
| `--brand-purple`                     | `#A855F7`            | `#C08BFF`            |
| `--brand-pink`, `--brand-orange`     | `#F55DB1`, `#FF9C47` | `#FF7CC4`, `#FFB067` |
| `--surface-blue`                     | `#EAF2FF`            | `#10224A`            |
| `--surface-indigo`, `--surface-pink` | `#F0EEFF`, `#FFF0FA` | `#1A1A4A`, `#2A1533` |

Text on the canvas, a card, or a tinted surface meets WCAG AA: 4.5:1 for normal
text and 3:1 for large text, input borders, and focus rings, measured over the
brightest canvas glow. Blue text uses `--link`, not `--primary` or the brand
colors. Brand colors are for fills, icons, and large text.

`--gradient-brand` runs from brand blue to brand indigo; it colors `me` and at
most one key phrase or hero action on a page. `--gradient-aurora` holds the
canvas glows, at 7 to 14 percent opacity in light theme and 10 to 24 percent in
dark theme.

The standard radius is 10 px, dialogs use 14 px, feature and marketing cards use
`--radius-feature` (20 px), and the sheet stays at 2 px. `rounded-md` and
`rounded-lg` both resolve to the 10 px `--radius`. Product surfaces use
`--shadow-product`, a soft blue-tinted shadow. The sheet uses `--shadow-paper`,
a neutral shadow with no blue. The theme preference is persisted in the
`aboutme-theme` cookie.

Chrome that stands for a resume sheet uses the paper tokens, defined once on
`:root` and never switched by the dark theme: `--paper` `#FFFFFF`, `--paper-ink`
`#171A18`, `--paper-muted` `#5F6763`, and `--paper-hover` `#F0F2F1`. The
`.paper-surface` class paints the paper ground and ink and rebinds foreground,
muted foreground, accent, ring (`#1A5CEB`), and link (`#123EDB`) to their light
values, so shadcn controls inside stay legible on white paper in dark theme. It
is never applied inside the renderer.

Buttons use the button primitive's variants. `default` fills with `--primary`,
darkens to `--primary-hover` on hover, and carries `--shadow-primary`, a soft
blue lift in light theme and a plain dark shadow in dark theme. `link` is
`--link` text with an underline on hover. `seal` fills with `--seal` for
Publish. `outline`, `secondary`, `ghost`, and `destructive` keep the generated
classes. These three edits to a generated primitive are the exception that
[ADR 0052](docs/adr/0052-guarded-token-edits-to-generated-primitives.md)
records.

Dialogs share one rhythm: 24 px between the header, the body, and the actions; 6
px from title to description; and 16 px between fields, with hints 6 px under
their control. Inputs and selects fill the dialog width. Actions sit
right-aligned from 640 px up and stack full-width below it, primary first.

## Seal and state marks

`AppSeal` has two implemented forms:

- The 96 px stamp is a rotated red SVG with inner and outer rings, text on a
  path reading `PUBLIC RESUME · ABOUTME.VN/<SLUG>`, and `aboutme` in the center.
  Its default rotation is -8 degrees.
- The 20 px mark is a red circle with a white check and carries the public link
  beside it.

`StateMark` exposes six states: Saved (pencil tick), Unsaved (an edit held or
queued but not yet sent), Saving…, Save failed, Draft, and Public (the small
seal with the link). Public state requires a link. The editor top bar shows the
small public mark beside the resume title when the resume is public, and a
successful publish response shows the large stamp on its own. The seal never
appears on or over a rendered resume: the homepage sample and the editor preview
show none, so neither can read as an already-public document.

## Landing

The landing page performs no data fetch. It stacks on phones and uses a
12-column grid from 1024 px, with 80 px between sections, 112 px from 1024 px.

The hero text spans five columns. The headline's last phrase, “theo cách của
bạn.” or “Your control.”, takes the brand gradient and falls back to the text
color under `forced-colors: active`. Signed-out visitors see Create your resume,
Browse the library, and a Sign in text link, in that order. Signed-in visitors
see Open your resumes and Browse the library. The first action is a 48 px
brand-gradient button with `--shadow-cta` that shifts to
`--gradient-brand-strong` on hover. Actions stack full width below 28 rem.

The sample spans seven columns. It is Danny's own compiled-in resume, two
columns with a sidebar and a photo, on A4 page metadata rather than a Library
preset. It sits on a whole white sheet, 210 mm by at least 297 mm with
`--shadow-paper`, in front of a translucent ghost sheet and a
`--gradient-hero-glow`. From 42 rem the ghost sheet is offset and rotated 2
degrees. The sheet carries no seal. The sheet zooms 0.39 on phones, 0.5 from 28
rem, 0.6 from 42 rem, and 0.64 from 80 rem. Three decorative chips, Private by
default, PDF, and the link, float at the sheet edges from 42 rem and wrap in a
centered row under it below that. Screen readers skip them.

Four sections follow the hero:

1. Three feature cards on blue, indigo, and pink surface tints with the 20 px
   feature radius: Private by default, One link per resume, and Bring your own
   AI. They sit in three columns from 1024 px.
2. Choose your style: filter chips for ATS-friendly, Technical, First job, and
   Management, each linking to `/templates?filter=…`, and one real template card
   per chip, in two columns and four from 1024 px. Cards lift 4 px on hover
   unless reduced motion is set. A text link opens the full Library.
3. Publishing is three choices: an example settings card with Public resume and
   PDF download on and SEO and GEO off, and the stamp on its top edge.
4. Free and open source: the AGPL-3.0 link and an outline button to the GitHub
   repository.

The footer shows `aboutme.vn` and links Terms and Privacy.

## Library

The template collection is labeled Library, or Thư viện in Vietnamese, in the
shell, the page heading, and the breadcrumb. Its URL stays `/templates`.

`/templates` opens with a header on the `--surface-blue` tint with a border and
the 20 px feature radius, holding the h1 and a one-line lead. The page lists all
20 templates: the nine tech role samples first, in role chip order, then the
other four templates with a sample, then the rest by English preset name. The
surrounding copy, purpose, and sample tag follow the site language.

A role chip row comes first: All roles, then Backend, Frontend, Mobile,
DevOps/SRE, Data/AI, QA/Tester, Fresher/Intern, BrSE, and Security (Bảo mật in
Vietnamese). One toggle group, one tab stop: arrow keys move focus, Enter or
Space selects, and the pressed chip gets `aria-pressed="true"` and the active
chip look. Each role shows the template whose sample is for that role. The
choice is kept in the URL (`?role=backend`) and combines with the filter chips.
Filtered-out cards stay in the HTML, hidden, so the server-rendered page always
lists every template. The row scrolls sideways like the filter row.

Filter chips are 36 px pills on the card color with a border. All comes first
with the template count. Format chips (With a sample, ATS-friendly, One page,
Suits a photo) carry a cyan dot; audience chips (First job, Technical,
Management) carry a purple dot and sit after a thin divider. The dot is
decorative; the chip text carries the meaning. A hovered chip takes the
`--surface-indigo` tint. The active chip fills with `--primary`, its dot takes
the text color, and it carries `aria-current="page"`. One chip is active at a
time and is kept in the URL (`?filter=ats`). The chip row scrolls sideways
without a scrollbar when needed.

Each card is a link to the template page. A template with a sample shows the
stored image of the sample's first PDF page; the rest show a live render of the
generic filler. Either way the card is a white 2 px sheet with `--shadow-paper`
that lifts 4 px on hover unless reduced motion is set. Under it sit the name, a
two-line purpose, and a tag: a `--surface-blue` pill naming the sample, or a
muted pill reading “Nội dung minh họa” or “Illustrative content”. The grid has
two columns below 641 px, three from 641 px, four from 900 px, and five from
1180 px.

At 900 px and wider, `/templates/{id}` shows the document beside a 360 px sticky
info column with a 24 px top offset. Below 900 px, the info comes first and the
primary action sits in a card-colored bar fixed to the bottom of the screen. The
info column holds the Library breadcrumb, the template name as the h1, its
purpose, the sample language toggle when both languages exist, and facts: sample
persona marked fictional, page count, layout, paper size, and photo fit.
Two-column templates add a `--surface-blue` note linking ATS Plain for online
portals. Templates with a sample show “Use this sample”, a note that it makes a
private copy, and a blank-resume link; templates without one show a filler note
and the blank-resume action.

A template with a sample shows three tabs over the document: Page (“Trang CV”),
PDF, and “What an ATS reads” (“ATS đọc được gì”). Page shows the document on a
white 2 px sheet with `--shadow-paper`. PDF shows a one-line hint, then every
stored page of the sample's PDF download in order, each on its own white sheet
with `--shadow-paper` and a caption naming its page number; each page's alt text
names the page, the total, and the template. The ATS tab shows a one-line hint
and the resume's text in reading order, in the chrome typeface on a
`.paper-surface` panel with the same radius and shadow. A template without a
sample shows the sheet with no tabs.

"Use this sample" opens `/app/new`, a confirm step that never creates on its
own: thumbnail, summary, editable title, and the resume count, or the limit
message at three resumes. The Create resume dialog offers Blank or From a
sample; a first resume opens on the samples.

## Authenticated chrome and editor

The shared application shell is a card-colored bar with a bottom border, its
content held to a 76 rem column. It has the 24 px `AppLogo`, linked home, and a
Library link for every visitor. Signed-out navigation adds an Open source link
to the GitHub repository from 56 rem, the theme toggle, a ghost Sign in button,
and a primary create button. The create button reads Create your resume on the
home page, the Library, template pages, Terms, and Privacy, and Create account
elsewhere. On localized routes other than sessions and agent consent, both
account buttons hide below 44 rem, where the page's own links take over.
Signed-in navigation also shows Resumes, Settings, and an account menu. The
account menu contains Settings, theme switching, and Log out. On signed-in
screens below 640 px, the direct Library and Settings links are hidden; Settings
remains in the account menu. On localized routes, the shell adds the Vietnamese
and English toggle. Below 640 px its visible labels shorten to VI and EN while
their accessible names remain complete.

The six account pages (sign in, second factor, create account, forgot password,
reset password, and verify email) share `AuthLayout`. Below 1024 px they are a
single 26 rem column with the form alone. From 1024 px they become a two-column
card up to 64 rem wide with a border, the 20 px feature radius, and
`--shadow-product`. The form sits in the right column, held to 26 rem, and a
`--surface-blue` brand panel fills the left. The form comes first in the DOM;
the brand panel follows it and holds no focusable element. The panel shows the
32 px logo, the statement “CV của bạn luôn riêng tư cho đến khi bạn đăng.” or
“Your resume stays private until you publish it.”, three points with blue,
indigo, and purple icons (Free and open source, One link per resume, PDF in A4
or Letter), and a decorative white sheet tilted 3 degrees over the hero glow.

The resume list is a desk of up to three paper cards: one column, three from 768
px, with 24 px gaps and 32 px from 768 px. Each card is a `.paper-surface` sheet
at least 160 px high with the 2 px sheet radius and `--shadow-paper`, so it
stays white with neutral ink in dark theme. It lifts 4 px over 200 ms on hover
unless reduced motion is set. The whole card opens the editor. It shows the
title, the relative updated time in paper-muted text, and a public seal and link
or the Draft mark at the bottom. An overflow menu at the top right offers Rename
and Delete. Create resume, a large primary button with a plus icon in the page
header, is the one create control; it is disabled at three resumes. Remaining
slots are 2 px dashed outlines with the sheet radius on a half-opaque card fill,
reading Empty slot. For an empty list, the first slot says “No resumes yet.” and
how to start, and that three are allowed.

The editor is a four-region workspace on the flat `--editor-canvas` fill, with
no canvas glows:

1. A 4 rem tool rail for Document, Structure, Design, Templates, and Photo.
2. A 16.5 rem resume outline with Personal details, indigo section icons, a
   collapsible list, and Add section actions.
3. A preview region, at least 32 rem wide, holding the rendered sheet.
4. A 22 rem inspector for personal details, sections, structure, customization,
   templates, and photo.

The top bar, rail, outline, and inspector sit on the card color with borders
between them. Selection is shown by shape as well as color. The selected rail
tool takes the `--surface-blue` fill, `--link` text, and a 3 px `--primary` bar
inset on its left edge, moved to the bottom edge when the rail is horizontal.
The current outline item takes the `--surface-blue` fill and semibold text.

The editor top bar is 64 px high. It holds the 24 px logo linking to the resume
list, a divider, the resume title as the page h1, save state, public mark, the
language toggle, Download PDF with the page size beside it, Publish, and the
account menu. Publish is the seal button.

The preview opens with a card-colored toolbar holding a PDF and Web toggle; the
active mode uses the default button variant and the choice is remembered. PDF
mode shows each page as its own white sheet with the 2 px radius and
`--shadow-paper`, then an estimated page count beside a pencil glyph. Web mode
shows the continuous document on one full-width sheet. The preview area has 24
px padding, 16 px on phones. It reports loading or unavailable photos without
rendering a placeholder image, and a render failure says that edits are still
safe.

The Design panel opens with a Page & PDF group: page size with its dimensions,
and margins as Narrow, Normal, Wide, or Custom presets, where Custom reveals the
two axes. Margins show millimetres on A4 and inches on Letter, and a note says
the settings apply to the PDF and printing, not the web page.

Each template card in the inspector shows the sample resume rendered with that
template at 0.18 scale. A thumbnail renders only while its card is near the
viewport, and applying a template names any sections it moved between columns.

The publish dialog is a scrollable modal with the `aboutme.vn/` slug prefix. It
presents optional fields for the browser-tab title and emoji icon, followed by
the three switches with explanations. PDF download and SEO and GEO are disabled
until Public resume is enabled. The primary action is Publish, Unpublish, or
Update publication according to the current state. Success shows the seal,
public link, and Copy link.

Settings is a narrow, left-aligned page divided by top rules. Signed-in devices
show the device description, relative last-seen time, This device, and Log out
or Revoke actions, with Log out everywhere below. Password settings are always
present. Account export and deletion appear in the Privacy section. Connected
agents and sign-in providers appear only when their capabilities are enabled.

## Responsive behavior

At widths up to 72 rem, the editor tool rail becomes a horizontal bar, the
outline and inspector share the lower workspace, and the preview spans the
available editor area. At widths up to 42 rem, the outline moves into a Sections
sheet and the bottom Edit/Preview tab bar switches between the inspector and
preview.

At widths up to 42 rem, the editor top bar grows to 96 px and splits into two
rows. The first holds the logo mark alone, save state, and the language toggle;
the divider, title, and public mark are hidden. The second holds Download PDF,
Publish, and the account menu. The bottom Edit and Preview tabs mark the active
tab with the secondary fill and semibold `--link` text. The preview uses a fit
zoom calculated from the available width minus 32 px on phone screens; larger
narrow layouts use 0.72 and wide layouts use 0.84 unless full zoom is requested.
The active A4 or Letter sheet remains intact and scrollable.

## Interaction and motion

Text fields commit on blur or Enter; rich text also commits after a 400 ms pause
in typing, so the preview follows as the person writes. Empty values remove a
field, unchanged values send nothing, and Escape restores the last committed
value. Selects, checkboxes, switches, colors, and numbers commit on change. An
edit that cannot save yet, such as a date range with a start and no end, is held
in memory, shows Unsaved, and survives the field remounting.

Publishing stamps the top bar's public mark in a single 180 ms press from scale
1.12 to 1 with the ink fading in. Unpublishing lifts it in 120 ms. Primitive
controls use short color, focus, and open/close transitions. Nothing animates on
page load. Reduced motion disables the stamp and reduces application animation
and transition durations to an instant effect.

## Accessibility

The implemented surfaces use landmark labels, heading relationships, visible
focus rings, semantic buttons and links, toolbar and tab roles, and
`aria-current`/`aria-selected` state where applicable. Dialogs, sheets, menus,
and tabs are keyboard-operable. Save, photo, preview, publish, copy, and failure
messages use status or alert announcements where needed.

The renderer names the person in an h1 by default. A renderer embed inside a
chrome page whose heading owns the h1 passes `nameHeading: 'p'` in its render
context, so the name is a paragraph: the homepage sample, the sheet thumbnails
on Library cards, the new-resume confirmation, and the Create resume dialog, and
the editor's template thumbnails. The full document on a template page and the
editor preview keep the default h1.

The theme choice persists across visits. The app reset and focus styles apply
under `data-ui="app"` only; `.resume-document`, `.paged-resume`, and their
descendants are excluded so application CSS cannot alter the renderer.

## Component guardrails

- Build chrome with Tailwind CSS v4 and shadcn-vue/reka-ui primitives.
- Keep primitives in `app/components/ui`, shared composites in
  `app/components/app`, and surface layout in pages or editor panels.
- Do not hand-style a generated primitive. A token-colored variant edit is the
  one exception, and only with a guard test; re-apply it after
  `apps/web/scripts/ui-add.sh` regenerates the primitive
  ([ADR 0052](docs/adr/0052-guarded-token-edits-to-generated-primitives.md)).
- Use the existing field, dialog, menu, sheet, button, and status components. Do
  not introduce raw controls or hand-written dialogs in a surface; the crop
  stage and ProseMirror content root are the custom-widget exceptions.
- Preserve visible labels, `aria-label` text, and stable `data-*` hooks when
  changing components. Tests query roles, labels, and those hooks.
- Keep the renderer pure and outside application chrome styling. Do not add
  page-specific values that bypass the semantic tokens.
- Keep the single-meaning color rules: seal red is for public state and Publish,
  blue is for actions, links, and focus, and draft/saved states remain pencil
  marks rather than colored chips.
