# aboutme landing and Library

The landing page, the template Library, and the community showcase. Part of the
[visual design](../../../DESIGN.md).

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
degrees. The sheet zooms 0.39 on phones, 0.5 from 28 rem, 0.6 from 42 rem, and
0.64 from 80 rem. Three decorative chips, Private by default, PDF, and the link,
float at the sheet edges from 42 rem and wrap in a centered row under it below
that. Screen readers skip them.

Four sections follow the hero:

1. Three feature cards on blue, indigo, and sand surface tints with the 20 px
   feature radius: Private by default, One link per resume, and Bring your own
   AI. They sit in three columns from 1024 px. The Bring your own AI card ends
   with a text link, “Xem cách kết nối” or “See how to connect”, to the
   [MCP guide](../mcp-guide.md) at `/guide/mcp`.
2. Choose your style: filter chips for ATS-friendly, Technical, First job, and
   Management, each linking to `/templates?filter=…`, and one real template card
   per chip, in two columns and four from 1024 px. A text link opens the full
   Library.
3. Publishing is three choices: an example settings card with Public resume and
   PDF download on and SEO and GEO off, and the stamp on its top edge.
4. Free and open source: the AGPL-3.0 link and an outline button to the GitHub
   repository.

The footer shows `aboutme.vn` and links Terms and Privacy. The
[verify page spec](../deployment-transparency/visual.md) adds Verify, and the
[MCP guide](../mcp-guide.md) adds “Kết nối AI” or “Connect AI” right after it.

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
Suits a photo) carry a jade dot; audience chips (First job, Technical,
Management) carry an ochre dot and sit after a thin divider. The dot is
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

## Community showcase

`/showcase` lists opted-in resumes as tiles. The
[showcase design](../showcase.md) sets what a tile holds, the order, the
filters, the API, and every string; this section sets the layout. Copy below
quotes that design's tables and must not drift from them. Every value is a
semantic token, so the page holds in the light and dark themes with no extra
rule. The card images stay light in both, as DESIGN.md sets for the link-preview
card.

### Page frame

The page uses the Library's frame: `main` at `max-w-7xl`, 16 px side padding and
40 px top and bottom, 32 px and 56 px from 640 px. It sends the noindex headers
the showcase design names and renders in the interface language.

The header band copies the Library header: `--surface-blue` fill, 1 px
`--border`, `--radius-feature`, 20 px by 32 px padding, 40 px by 48 px from 640
px. It holds three lines:

1. The h1, “CV từ cộng đồng” or “Community resumes”, in the Library h1 classes
   (30 px, 48 px from 640 px, bold, -0.02em).
2. The lead, 16 px `--muted-foreground`, at most 42 rem wide, 16 px under the
   h1.
3. The order note, “CV mới được duyệt hiện trước.” or “Newest approved first.”,
   14 px `--muted-foreground`, 8 px under the lead.

The band holds no focusable element.

### Filters

Two rows sit under the band. The first is 32 px under it and the second 12 px
under the first. Both use the Library's `.gallery-chip` look: 36 px pills on
`--card` with a 1 px `--border`, `--surface-indigo` on hover, and a `--primary`
fill with `--primary-foreground` text when pressed. Role and language chips
carry no dot.

**Role row.** One `ToggleGroup`, `type="single"`, labeled “Lọc theo vị trí” or
“Filter by role”: All roles (“Mọi vị trí”), the nine Library roles in Library
chip order with the Library labels, then Other (“Khác”). It behaves as the
Library role row does: one tab stop, arrow keys move focus, Enter or Space
selects, a pressed chip stays pressed, and the row scrolls sideways without a
scrollbar.

**Second row.** A flex row that wraps, 12 px column gap and 12 px row gap:

- A language `ToggleGroup` labeled “Ngôn ngữ của CV” or “Resume language”: “Mọi
  ngôn ngữ”, “Tiếng Việt”, “Tiếng Anh”, or All languages, Vietnamese, English.
  Same chip look and keys as the role row. Its own wrapper scrolls sideways when
  the three chips do not fit.
- The template field: `SelectField` with the visible label “Mẫu” or “Template”,
  laid out inline (label left, 8 px gap, select right, both centered) instead of
  stacked. The `NativeSelect` is 36 px high, 12 rem minimum and 16 rem maximum
  wide. Options: All templates (“Mọi mẫu”), then the 20 presets by preset name
  in alphabetical order, then Custom design (“Thiết kế riêng”). Below 641 px the
  field takes the full row and the select fills the width beside its label.

A filter change replaces the URL query, as the Library role row does, drops
`page`, and loads page 1. Focus stays on the control. A query value the page
does not know counts as All and is not sent to the API, the way the Library
reads `?role=`. A `page` outside 1 to 100 counts as 1.

### Grid

The results area sits 32 px under the filters. Tiles sit in a `ul` grid that
stretches each tile to its row's height:

| Width          | Columns | Column gap | Row gap |
| -------------- | ------- | ---------- | ------- |
| Below 641 px   | 1       | 16 px      | 16 px   |
| 641 to 1023 px | 2       | 24 px      | 24 px   |
| From 1024 px   | 3       | 28 px      | 32 px   |

At 1280 px a tile is about 389 px wide; at 390 px it is 358 px.

### Tile

Each `li` is a flex column on `--card` with a 1 px `--border`, the 10 px
`--radius`, and `--shadow-product`. It holds two focus targets, in order: the
tile link and the Report link. Report is never inside the tile link.

**Tile link.** A block `NuxtLink` to `/{slug}`, same tab, `rel="nofollow"`,
covering the card image and the meta block. Its focus ring is a 2 px `--ring`
outline at 2 px offset with the 10 px radius. When the link is hovered, the
whole tile lifts 4 px over 200 ms ease-out, as Library cards do, and reduced
motion removes the lift.

**Card image.** A box with `aspect-ratio: 1200 / 630`, a `--muted` fill, the top
corners at 9 px, a 1 px `--border` bottom edge, and `overflow: hidden`. The box
holds its height before the image arrives, so nothing shifts. Inside it, an
`img` fills the box (`object-fit: cover`) with `src` set to the versioned card
URL, `width="1200"`, `height="630"`, `loading="lazy"`, `decoding="async"`, and
`alt` set to the item's `imageText`. Every tile loads lazily, the first
included. When the image fails, as after a color change in an open tab, the
`img` turns transparent and the `--muted` box stays; the alt text and meta line
still name the tile.

**Meta block.** 12 px top and 16 px side padding, an 8 px grid gap:

1. The meta line, 14 px, wrapping when long: the template name or “Thiết kế
   riêng” / “Custom design” in `--foreground` at weight 500, a `·` in
   `--muted-foreground` with 6 px on each side and `aria-hidden="true"`, then
   the language in `--muted-foreground`: “Tiếng Việt”, “Tiếng Anh”, or “Ngôn ngữ
   khác” / Vietnamese, English, or Other language.
2. The role chip, only when the item has a role: the Library sample tag pill
   (`--surface-blue` fill, 12 px weight 500 `--foreground`, 10 px by 2 px
   padding, full radius), left-aligned, with the Library role label or “Khác” /
   Other. It is a label, not a control.

**Report row.** Pinned to the tile bottom (`margin-top: auto`), right-aligned,
with 8 px side and bottom padding and 4 px top. The Report link is 32 px high
with 8 px side padding, the 10 px radius, and 14 px `--muted-foreground` text
that turns `--foreground` and underlines on hover; its focus ring matches the
tile link's. It reads “Báo cáo” or “Report”, carries `aria-label` “Báo cáo CV
{slug} qua email” or “Report resume {slug} by email”, and opens
`mailto:danny@aboutme.vn` with the interface-language subject “Báo cáo trang
Cộng đồng: {slug}” or “Report showcase: {slug}”, URL-encoded. The visible word
starts the accessible name, so speech input finds it.

Hooks: `data-showcase-slug` on each `li` and `data-action="showcase-report"` on
Report.

### Pager

The pager shows only when `pageCount` is above 1, 40 px under the grid. It is a
`nav` named by the page status through `aria-labelledby`, holding Previous, the
page status, and Next.

- Previous and Next are `NuxtLink`s styled as the outline button at the default
  36 px height, with `ChevronLeft` before “Trang trước” / Previous and
  `ChevronRight` after “Trang sau” / Next, both icons `aria-hidden`. They keep
  the active filters and set `?page=`.
- On the first page Previous, and on the last page Next, render as a `span` with
  the same classes, `aria-disabled="true"`, 50 percent opacity, and no tab stop.
- The page status reads “Trang {page}/{pageCount}” or “Page {page} of
  {pageCount}”, 14 px `--muted-foreground`, tabular figures.
- From 641 px the three sit in one centered row with a 12 px gap. Below 641 px
  the status takes its own centered line and the two buttons share the next row
  at equal width, 12 px apart.

After Previous or Next, the page scrolls the results area to the top, instantly
under reduced motion, and moves focus to the first tile link once the page
loads, or to Retry when it fails.

### States

The grid area shows exactly one state. Each state element carries `data-state`
with the name below.

| State      | When                            | Shows                                   |
| ---------- | ------------------------------- | --------------------------------------- |
| `loading`  | First render and every new load | Six skeleton tiles; the `ul` is busy    |
| `list`     | Items arrived                   | The tiles and the pager                 |
| `empty`    | `total` is 0 and no filter set  | `EmptyState` with the empty action      |
| `no-match` | `total` is 0 with any filter    | The no-match line                       |
| `failed`   | The request failed or timed out | `StatusBanner` with the error and Retry |

**Loading.** The server-rendered HTML holds this state. Six skeleton tiles fill
the grid with the tile box, border, and radius; a `Skeleton` fills the image box
and one `Skeleton` line, 16 px high and two thirds wide, fills the meta block.
The skeletons are `aria-hidden`, and the `ul` carries `aria-busy="true"` until
the items render. The global reduced-motion rule stops the pulse.

**Empty.** `EmptyState`, full grid width, with the title “Chưa có CV nào ở đây.
Xuất bản CV của bạn và bật Hiện trong trang Cộng đồng để là người đầu tiên.” or
“No resumes here yet. Publish your resume and turn on Show in the community
showcase to be the first.” and no description. Its action is a `Button` (default
variant, 36 px) rendered as a `NuxtLink`:

- Signed in: “Mở CV của bạn” / “Open your resumes”, to `/app/resumes`.
- Signed out with password registration on: “Tạo CV” / “Create your resume”, to
  `/register`.
- Signed out with password registration off: the same label, to `/login`.

The filter rows stay visible in the empty state.

**No match.** One line, “Không có CV nào khớp với bộ lọc này.” or “No resume
matches these filters.”, 15 px `--muted-foreground`, `role="status"`, the same
treatment as the Library's no-match line. No pager, except past the end.

**Past the end.** A `page` above `pageCount` gets `200` with empty items and the
real `pageCount`. The page shows the no-match line and the pager, where only
Previous is a link.

**Load failed.** `StatusBanner` with `kind="error"`, full grid width, holding
“Không tải được danh sách. Hãy thử lại.” or “Could not load the list. Try
again.” and, 8 px under it, an outline `Button` at the small size reading “Thử
lại” / “Try again”. Retry keeps the banner in place, disables itself, and
reloads the same query, so focus stays on it; success replaces the banner with
the tiles. A rate-limited or `400` response shows this state too.

### Navigation

The shell adds **Cộng đồng** / **Community**, a link to `/showcase`, right after
Library, with Library's class: shown to every visitor and hidden below 640 px
when signed in. It takes `aria-current="page"` on `/showcase`. On `/showcase`
the shell's create button reads Create your resume, as on the Library.

The signed-out header keeps Library, Community, Connect AI, Sign in, and the
create button at every width where the design shows them. The English header is
the widest: at 704 px its right group (locale names, theme button, Sign in, and
Create your resume) measures 438 px, 522 px from 48 rem where the theme label
appears. So below 64 rem the signed-out header is compact:

| Part           | Below 64 rem              | From 64 rem            |
| -------------- | ------------------------- | ---------------------- |
| Logo           | Mark alone, never shrinks | Full logo              |
| Locale toggle  | VI and EN                 | Tiếng Việt and English |
| Theme button   | Icon alone                | Icon and label         |
| Header gaps    | 8 px                      | 16 px                  |
| Nav link sides | 8 px padding              | 10 px padding          |

The Open source link shows from 72 rem, where the full header fits with it. The
compact header needs about 680 px at 704 px, and the full header about 970 px at
1024 px and 1065 px with Open source. Accessible names stay complete in the
compact forms.

The logo row alone also applies signed in: below 64 rem the signed-in header
shows the mark alone too, because its nav with Community overflows a full logo
by 68 px at 704 px. Every other compact row is signed-out only.

### Publish dialog block

The showcase controls sit in the publish dialog's options `fieldset`, directly
after the Require sign-in to view switch, so the control they depend on sits
above them. When the sign-in switch is hidden, they follow the SEO and GEO
switch. They keep the fieldset's 12 px gap. PublishDialog.vue is near the code
length limit, so the block is its own component beside PublishAccess.vue.

In order:

1. **Switch.** `SwitchField`, `name="showcaseEnabled"`,
   `data-action="publish-showcase"`, labeled “Hiện trong trang Cộng đồng” or
   “Show in the community showcase”.
2. **Status line**, when shown (below).
3. **Issue line**, when shown (below).
4. **Role field**, while the switch is on and enabled: `SelectField` labeled “Vị
   trí hiển thị” or “Role shown”, hint “Tùy chọn. Giúp người xem lọc theo vị
   trí.” or “Optional. Lets visitors filter by role.”, options “Không chọn” /
   None (empty value), the nine Library roles in chip order with their Library
   labels, then “Khác” / Other. It fills the width.

Lines 2 to 4 indent 40 px, the switch width plus its 8 px gap, so they read as
part of the switch.

The switch description, which SwitchField shows under the label, follows the
first matching row:

| Condition                  | Switch               | Description           |
| -------------------------- | -------------------- | --------------------- |
| Busy                       | Disabled, value kept | As before the request |
| Public resume off          | Disabled, shown off  | Needs public line     |
| Require sign-in to view on | Disabled, shown off  | Needs open view line  |
| Otherwise                  | Enabled              | Help text             |

- Help: “Hiện ảnh xem trước, mẫu, ngôn ngữ và vị trí của CV này tại
  aboutme.vn/showcase sau khi chúng tôi duyệt. Ai cũng xem được trang đó.” or
  “Shows this resume's preview image, template, language, and role at
  aboutme.vn/showcase after we review it. Anyone can see that page.”
- Needs public: “Bật CV công khai để hiện trong trang Cộng đồng.” or “Turn on
  Public resume to show it in the community showcase.”
- Needs open view: “Không dùng được khi bật Yêu cầu đăng nhập để xem.” or “Not
  available while Require sign-in to view is on.”

Disabled shows off, unlike the SEO and GEO switch, because unpublishing or
turning on sign in to view ends the opt-in; nothing comes back. If the owner
undoes the change before submitting, the switch shows the stored value again.
The description keeps full `--muted-foreground`, never dimmed with the disabled
switch, so the reason stays legible. The dialog never sends
`showcaseEnabled: true` while the switch is disabled.

**Status line.** A 14 px paragraph in `--foreground`, shown only while the
stored opt-in exists, the switch is on and enabled, and no request is running.
It follows the stored `showcase.state`, and a successful publish updates it from
the returned resource. It is plain text with no mark, since the seal stays
reserved for the public state:

- `pending`: “Đang chờ duyệt. CV sẽ hiện trong trang Cộng đồng sau khi được
  duyệt.” or “Waiting for review. The resume appears in the community showcase
  once approved.”
- `listed`: “Đang hiện trong trang Cộng đồng.” or “Shown in the community
  showcase.”, a space, then the link “Xem trang Cộng đồng” or “View the
  showcase” to `/showcase` in the same tab, `--link` text, underlined, as the
  dialog's public link is.
- `declined`: the Declined text from the showcase design, with
  `danny@aboutme.vn` as a `mailto:` link in the same `--link` style.

A switch the owner just turned on shows no status line until the publish
succeeds; one just turned off hides it.

**Issue line.** A publish issue on `showcaseEnabled` or `showcaseRole` shows at
the block, not in the dialog's issue list, the way tab-title issues show at
their fields: 12 px `--destructive` text with `role="alert"`, cleared when the
owner changes a showcase control. `requires_open_view` reads “Tùy chọn này cần
tắt Yêu cầu đăng nhập để xem.” or “This option needs Require sign-in to view
off.”; `requires_live` uses the dialog's existing text for that code.

The switch's `aria-describedby` lists the description, the status line, and the
issue line, in that order, whichever are present.

### Accessibility and fit

Focus order on `/showcase`: the shell, the role row (one stop), the language row
(one stop), the template select, then per tile the tile link and its Report
link, then Previous and Next, then the footer. Retry or the empty action takes
the grid's place when shown. In the dialog: the sign-in switch, the showcase
switch, the status link when present, then the role select.

Contrast uses only token pairs the theme tests already check in both themes:
`--foreground` and `--muted-foreground` on `--card` and `--surface-blue`,
`--primary-foreground` on `--primary`, `--link` on `--card`, and `--destructive`
on `--card`. Report at 14 px `--muted-foreground` on `--card` is about 6.1:1
light and 7.5:1 dark. Focus rings use `--ring`, 3:1 on every ground. The card
images are fixed light art and carry no chrome text.

Vietnamese strings are the longest, and each must fit 390 px with no horizontal
page scroll:

- Language chips need about 354 px of the 358 px row; their wrapper scrolls
  sideways if a font renders wider.
- The template row is label, gap, and a select filling the rest; the longest
  preset name fits.
- The meta line wraps instead of truncating; at three columns from 1024 px the
  longest pair, a long preset name with “Ngôn ngữ khác”, takes two lines.
- The pager moves the status to its own line below 641 px, because “Trang
  trước”, “Trang 100/100”, and “Trang sau” need about 375 px in one row.
- In the dialog the indented role select is about 270 px wide at 390 px, enough
  for “Fresher/Intern”; the Declined text wraps.

The finish review checks `/showcase` at 360, 390, 768, 1024, and 1440 px in both
languages and both themes, and the signed-out header at 360, 390, 704, 768, 896,
1024, and 1152 px.
