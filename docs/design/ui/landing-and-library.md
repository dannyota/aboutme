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
filters, the API, and every string, and the [showcase page layout](showcase.md)
sets the page itself. This section holds the shell's Community link and the
publish dialog block.

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
2. **Listed line**, when shown (below).
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
  aboutme.vn/showcase. Ai cũng xem được trang đó.” or “Shows this resume's
  preview image, template, language, and role at aboutme.vn/showcase. Anyone can
  see that page.”
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

**Listed line.** A 14 px paragraph in `--foreground`, shown only while the
stored opt-in exists, the switch is on and enabled, and no request is running. A
stored opt-in is always listed, so the line has one form: “Đang hiện trong trang
Cộng đồng.” or “Shown in the community showcase.”, a space, then the link “Xem
trang Cộng đồng” or “View the showcase” to `/showcase` in the same tab, `--link`
text, underlined, as the dialog's public link is. It is plain text with no mark,
since the seal stays reserved for the public state. A successful publish updates
it from the returned resource.

A switch the owner just turned on shows no Listed line until the publish
succeeds; one just turned off hides it. When the line is absent it leaves no
gap: the issue line or the role field takes its place.

**Issue line.** A publish issue on `showcaseEnabled` or `showcaseRole` shows at
the block, not in the dialog's issue list, the way tab-title issues show at
their fields: 12 px `--destructive` text with `role="alert"`, cleared when the
owner changes a showcase control. `requires_open_view` reads “Tùy chọn này cần
tắt Yêu cầu đăng nhập để xem.” or “This option needs Require sign-in to view
off.”; `requires_live` uses the dialog's existing text for that code.

The switch's `aria-describedby` lists the description, the Listed line, and the
issue line, in that order, whichever are present.

### Accessibility and fit

In the publish dialog, focus runs from the sign-in switch to the showcase
switch, the Listed link when present, then the role select. The indented role
select is about 270 px wide at 390 px, enough for “Fresher/Intern”. The dialog
text uses `--foreground`, `--muted-foreground`, `--link`, and `--destructive` on
`--card`, pairs the theme tests check in both themes.

The finish review checks the signed-out header at 360, 390, 704, 768, 896, 1024,
and 1152 px.
