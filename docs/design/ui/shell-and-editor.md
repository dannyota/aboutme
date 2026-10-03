# aboutme shell and editor

The application shell, account pages, resume list, editor, and settings. Part of
the [visual design](../../../DESIGN.md).

## Authenticated chrome and editor

The shared application shell is a card-colored bar with a bottom border, its
content held to a 76 rem column. It has the 24 px `AppLogo`, linked home, and a
Library link for every visitor, then a Community link (“Cộng đồng”) to the
[community showcase](showcase.md), then a link to the
[MCP guide](../mcp-guide.md) (“Kết nối AI” or “Connect AI”) at `/guide/mcp`,
public like the Library link. Below 64 rem the logo shows the mark alone, signed
in or out. Below 64 rem the signed-out header is also compact, with VI and EN
and an icon-only theme button, so every link and both account buttons fit
([Navigation](landing-and-library.md#navigation)). Signed-out navigation adds an
Open source link to the GitHub repository from 72 rem, the theme toggle, a ghost
Sign in button, and a primary create button. The create button reads Create your
resume on the home page, the Library, template pages, the showcase, Terms,
Privacy, and the MCP guide, and Create account elsewhere. On localized routes,
both account buttons hide below 44 rem, where the page's own links take over.
Signed-in navigation also shows Resumes, Settings, and an account menu. The
account menu contains Settings, theme switching, and Log out. On signed-in
screens below 640 px, the direct Library, Community, and Settings links are
hidden; Settings remains in the account menu. The guide link hides below 44 rem
signed out and below 64 rem signed in, where the signed-in bar also carries
Resumes, Views, and Settings. On localized routes, the shell adds the Vietnamese
and English toggle. Below 640 px its visible labels shorten to VI and EN while
their accessible names remain complete.

The six account pages (sign in, second factor, create account, forgot password,
reset password, and verify email) share `AuthLayout`. Below 1024 px they are a
single 26 rem column with the form alone. From 1024 px they become a two-column
card up to 64 rem wide with a border, the 20 px feature radius, and
`--shadow-product`. The form sits in the right column, held to 26 rem, and a
`--surface-blue` brand panel fills the left. The form comes first in the DOM;
the brand panel follows it and holds no focusable element. The panel shows the
32 px logo, the statement “CV của bạn luôn riêng tư cho đến khi bạn xuất bản.”
or “Your resume stays private until you publish it.”, three points with blue,
indigo, and ochre icons (Free and open source, One link per resume, PDF in A4 or
Letter), and a decorative white sheet tilted 3 degrees over the hero glow.

The resume list is a desk of up to three paper cards: one column, three from 768
px, with 24 px gaps and 32 px from 768 px. Each card is a `.paper-surface` sheet
at least 160 px high, so it stays white with neutral ink in dark theme. It lifts
4 px over 200 ms on hover unless reduced motion is set. The whole card opens the
editor. It shows the title, the relative updated time in paper-muted text, and a
public seal and link or the Draft mark at the bottom. An overflow menu at the
top right offers Rename and Delete. Create resume, a large primary button with a
plus icon in the page header, is the one create control; it is disabled at three
resumes. Remaining slots are 2 px dashed outlines with the sheet radius on a
half-opaque card fill, reading Empty slot. For an empty list, the first slot
says “No resumes yet.” and how to start, and that three are allowed.

The editor is a four-region workspace on the flat `--editor-canvas` fill, with
no canvas glows:

1. A 4 rem tool rail for Document, Structure, Design, Templates, and Photo.
2. A 16.5 rem resume outline with Personal details, indigo section icons, a
   collapsible list, and Add section actions.
3. A preview region, at least 29.5 rem wide, holding the rendered sheet. The
   four columns fill exactly 72 rem at the default inspector width.
4. An inspector for details, sections, structure, customization, templates, and
   photo: 22 rem, or dragged to 48 rem above 72 rem width, kept per browser.

The top bar, rail, outline, and inspector sit on the card color with borders
between them. Selection is shown by shape as well as color. The selected rail
tool takes the `--surface-blue` fill, `--link` text, and a 3 px `--primary` bar
inset on its left edge, moved to the bottom edge when the rail is horizontal.
The current outline item takes the `--surface-blue` fill and semibold text.

The editor top bar is 64 px high. It holds the 24 px logo linking to the resume
list, a divider, the resume title as the page h1, save state, public mark, the
language toggle, Download PDF with the page size beside it, Publish, and the
account menu. Publish is the seal button. Below 56 rem the logo shows the mark
alone and the bar's gaps shrink from 16 to 8 px. When space runs short, the
title and the public link truncate with an ellipsis before any control moves;
the link's `title` attribute holds the whole address.

The preview opens with a card-colored toolbar holding a PDF and Web toggle; the
active mode uses the default button variant and the choice is remembered. PDF
mode shows each page as its own sheet, then an estimated page count beside a
pencil glyph. Web mode shows the continuous document on one full-width sheet.
The preview area has 24 px padding, 16 px on phones. It reports loading or
unavailable photos without rendering a placeholder image, and a render failure
says that edits are still safe. The active A4 or Letter sheet remains intact and
scrollable. Tablet and desktop PDF previews have a zoom card (out, percent to
reset, in, Default size; 50 to 200%; Ctrl/Cmd +/−/0 and Ctrl/Cmd+wheel; kept per
browser; hidden on phones and in Web mode).

The Design panel opens with a Page & PDF group: page size with its dimensions,
and margins as Narrow, Normal, Wide, or Custom presets, where Custom reveals the
two axes. Margins show millimetres on A4 and inches on Letter, and a note says
the settings apply to the PDF and printing, not the web page.

Each template card in the inspector shows the sample resume rendered with that
template at 0.18 scale. A thumbnail renders only while its card is near the
viewport, and applying a template names any sections it moved between columns. A
search box above the cards filters them as typed by name, purpose, sample tag,
role, and filter chip in both languages, ignoring case and diacritics; every
word must match, Escape or the clear button empties it, and a polite status line
gives the count or a no-match message.

The publish dialog is a scrollable modal with the `aboutme.vn/` slug prefix. It
presents optional fields for the browser-tab title and emoji icon, followed by
the Public resume, PDF download, SEO and GEO, and, when offered, Require sign-in
to view switches with explanations, then the
[showcase block](landing-and-library.md#publish-dialog-block). PDF download and
SEO and GEO are disabled until Public resume is enabled. The primary action is
Publish, Unpublish, or Update publication according to the current state.
Success shows the seal, public link, and Copy link.

Settings is a narrow, left-aligned page divided by top rules. Signed-in devices
show the device description, relative last-seen time, This device, and Log out
or Revoke actions, with Log out everywhere below. Password settings are always
present. Account export and deletion appear in the Privacy section. Connected
agents and sign-in providers appear only when their capabilities are enabled.
Connected agents links the [MCP guide](../mcp-guide.md) at `/guide/mcp` from its
empty state (“Xem cách kết nối trợ lý AI” or “See how to connect an AI
assistant”) and again under the grant list when it has entries.
