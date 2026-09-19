# aboutme visual design

This is the living visual record of the implemented web application. The account
is private; the resume is the public document. The UI makes that distinction
visible through a desk, a sheet of paper, and a seal applied by the person.

## Product principle

The landing page leads with “CV của bạn. Miễn phí. Không ai thấy nếu bạn không
muốn.” by default and “Your resume. Free. No one sees it unless you want them
to.” in English. It shows a compiled-in resume rendered by the shared
`ResumeDocument`, rather than a profile card or template carousel. Publishing is
a deliberate action with three named choices: Public resume, PDF download, and
SEO and GEO. The publish dialog also controls the optional browser-tab title and
emoji icon.

The editor, public page, and PDF use the same document renderer. Application
chrome may frame the renderer but does not change its output.

## Language coverage

Vietnamese is the default site language. The language choice persists in the
`aboutme-locale` cookie. The home page, template gallery and detail pages,
sign-in and account-creation flows, password recovery, email verification,
Terms, and Privacy are available in Vietnamese and English. The shell shows the
language toggle only on these localized routes.

The resume list, new-resume confirmation, editor, settings, and agent consent
stay in English. Public resume chrome follows the resume language: Vietnamese
resumes show “Tạo bằng aboutme.vn” and “Tải PDF”; other languages show “Built
with aboutme.vn” and “Download PDF”. Resume content and renderer labels follow
the resume's own language, independent of the site language.

## Visual direction

- The application chrome is a cool grey desk. The resume is a whole white sheet
  with a paper shadow and stays white in dark theme.
- Ink is near-black, secondary information is pencil grey, and rules are
  hairlines. Signature blue-black marks the person’s actions.
- Seal red marks the public state and the Publish action. Destructive actions
  use the separate destructive token.
- State is communicated by a mark or plain text: a pencil tick for saved, text
  for saving and draft, destructive text for failure, and a seal plus link for
  public.
- Chrome alignment follows an 8 px module. The sheet is never cropped.
- Copy is sentence case. Controls name the action they perform. English copy
  uses “resume”; Vietnamese localized copy uses “CV”.

## Typography and tokens

Application chrome uses `Be Vietnam Pro`, then `Inter`, `system-ui`, and
`sans-serif`. Uppercase text is used inside the seal. The renderer keeps its own
typography and tokens.

The semantic tokens below are defined on `:root` and switched by
`html[data-theme="dark"]`.

| Role                              | Light     | Dark                        |
| --------------------------------- | --------- | --------------------------- |
| Page background                   | `#EDEFEB` | `#121614`                   |
| Foreground ink                    | `#171A18` | `#ECEFEC`                   |
| Card and popover                  | `#FFFFFF` | `#1A1F1C`                   |
| Primary action                    | `#1F2A44` | `#D7DEEE`                   |
| Primary foreground                | `#FFFFFF` | `#171A18`                   |
| Secondary, muted, accent          | `#E3E6E1` | `#242A27`                   |
| Secondary/muted/accent foreground | `#171A18` | `#ECEFEC`                   |
| Muted foreground                  | `#5F6763` | `#9AA39E`                   |
| Border and input                  | `#D8DDD9` | `rgba(255, 255, 255, 0.12)` |
| Focus ring                        | `#1F2A44` | `#D7DEEE`                   |
| Seal                              | `#C8102E` | `#C8102E`                   |
| Destructive                       | `#B42318` | `#F0736A`                   |

The chrome radius is 6 px, the sheet radius is 2 px, and the dialog radius is 8
px. `--shadow-paper` supplies the soft paper shadow in light and dark themes.
The theme preference is persisted in the `aboutme-theme` cookie.

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
small public mark beside the resume title when the resume is public. The preview
and successful publish response show the large stamp.

## Landing

The landing page is a responsive two-column layout from 42 rem upward. Its left
column spans five of twelve columns and the sample spans seven. Signed-out
visitors see Create account, Browse templates, and Sign in, in that order.
Signed-in visitors see Open your resumes and Browse templates.

The right column contains a compiled-in two-column Ada Lovelace sample inside a
210 mm by at least 297 mm white sheet with a soft shadow. The sample uses its
own Letter page metadata and does not use a gallery preset. A 96 px red seal
names `aboutme.vn/ada-lovelace`. The sheet uses `zoom: 0.6` on wide screens,
`0.5` below 42 rem, and `0.44` at widths up to 390 px.

Below the sample are three ruled facts: Yours to keep, One link per resume, and
Bring your own agent. A second ruled section explains Public resume, PDF
download, and SEO and GEO. The footer links the AGPL-3.0 repository, Terms, and
Privacy. The page performs no data fetch.

## Template gallery

`/templates` lists all 20 templates: the five with a sample first, then the rest
by English preset name. The surrounding copy, purpose, and sample tag follow the
site language. Each card shows page one of the template's own sample, or of the
generic filler tagged “Nội dung minh họa” or “Illustrative content”. One filter
chip is active at a time and is kept in the URL (`?filter=ats`); the chip row
scrolls sideways when needed. The grid has two columns below 641 px, three from
641 px, four from 900 px, and five from 1180 px.

At 900 px and wider, `/templates/{id}` shows the full document beside a 360 px
sticky info column with a 24 px top offset. Below 900 px, the info comes first
and the primary action sits in a bar at the bottom. Templates with a sample show
the sample language toggle when both languages exist, the fictional persona,
page count, “Use this sample”, a blank-resume link, and an “ATS reads” tab with
text in reading order. Templates without a sample show localized illustrative
filler and the blank-resume action. Every detail page shows the layout, paper
size, photo fit, and a reading-order note for two-column templates.

"Use this sample" opens `/app/new`, a confirm step that never creates on its
own: thumbnail, summary, editable title, and the resume count, or the limit
message at three resumes. The Create resume dialog offers Blank or From a
sample; a first resume opens on the samples.

## Authenticated chrome and editor

The shared application shell has the lowercase `aboutme` brand and a Templates
link for every visitor. Signed-out navigation also shows Sign in, Create
account, and a theme toggle. Signed-in navigation also shows Resumes, Settings,
and an account menu. The account menu contains Settings, theme switching, and
Log out. On signed-in screens below 640 px, the direct Templates and Settings
links are hidden; Settings remains in the account menu. On localized routes, the
shell adds the Vietnamese and English toggle. Below 640 px its visible labels
shorten to VI and EN while their accessible names remain complete.

The resume list is a desk of up to three white paper cards in a three-column
grid at medium widths. Each card shows its title, relative updated time, and a
public seal/link or Draft mark. An overflow menu provides Rename and Delete.
Create resume in the page header is the one create control. Remaining slots are
quiet dashed placeholders; for an empty list, the first says how to start and
that three are allowed.

The editor is a four-region workspace:

1. A tool rail for Document, Structure, Design, Templates, and Photo.
2. A resume outline with Personal details, section icons, a collapsible list,
   and Add section actions.
3. A scrollable preview region containing the white rendered sheet and an
   estimated page count.
4. An inspector for personal details, sections, structure, customization,
   templates, and photo.

The editor top bar keeps the brand, resume title, save state, public mark,
Download PDF with the page size beside it, Publish, and account menu. Publish is
the seal button. The preview reports loading or unavailable photos without
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
available editor area. At widths up to 42 rem, the outline is hidden and the
bottom Edit/Preview tab bar switches between the inspector and preview.

The editor top bar hides the divider, title, and public mark at phone widths.
The preview uses a fit zoom calculated from the available width minus 32 px on
phone screens; larger narrow layouts use 0.72 and wide layouts use 0.84 unless
full zoom is requested. The active A4 or Letter sheet remains intact and
scrollable.

## Interaction and motion

Text fields commit on blur or Enter; rich text also commits after a 400 ms pause
in typing, so the preview follows as the person writes. Empty values remove a
field, unchanged values send nothing, and Escape restores the last committed
value. Selects, checkboxes, switches, colors, and numbers commit on change. An
edit that cannot save yet, such as a date range with a start and no end, is held
in memory, shows Unsaved, and survives the field remounting.

Publishing stamps the preview in a single 180 ms press from scale 1.12 to 1 with
the ink fading in. Unpublishing lifts the stamp in 120 ms. Primitive controls
use short color, focus, and open/close transitions. Nothing animates on page
load. Reduced motion disables the stamp and reduces application animation and
transition durations to an instant effect.

## Accessibility

The implemented surfaces use landmark labels, heading relationships, visible
focus rings, semantic buttons and links, toolbar and tab roles, and
`aria-current`/`aria-selected` state where applicable. Dialogs, sheets, menus,
and tabs are keyboard-operable. Save, photo, preview, publish, copy, and failure
messages use status or alert announcements where needed.

The theme choice persists across visits. The app reset and focus styles apply
under `data-ui="app"` only; `.resume-document`, `.paged-resume`, and their
descendants are excluded so application CSS cannot alter the renderer.

## Component guardrails

- Build chrome with Tailwind CSS v4 and shadcn-vue/reka-ui primitives.
- Keep primitives in `app/components/ui`, shared composites in
  `app/components/app`, and surface layout in pages or editor panels.
- Use the existing field, dialog, menu, sheet, button, and status components. Do
  not introduce raw controls or hand-written dialogs in a surface; the crop
  stage and ProseMirror content root are the custom-widget exceptions.
- Preserve visible labels, `aria-label` text, and stable `data-*` hooks when
  changing components. Tests query roles, labels, and those hooks.
- Keep the renderer pure and outside application chrome styling. Do not add
  page-specific values that bypass the semantic tokens.
- Keep the single-meaning color rules: seal red is for public state and Publish,
  signature ink is for personal actions and focus, and draft/saved states remain
  pencil marks rather than colored chips.
