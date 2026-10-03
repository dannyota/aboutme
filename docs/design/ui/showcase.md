# aboutme community showcase page

The layout of `/showcase`: results first, a count line, filters in a bottom
sheet below 1024 px and in a left rail from 1024 px, tiles with a footer row,
and an invite card at the end of the grid. The [showcase design](../showcase.md)
sets what a tile holds, the order, the filters, the API, and every string; copy
here quotes its tables. Part of the [visual design](../../../DESIGN.md).

Every value is a semantic token, so the page holds in both themes; the card
images stay light in both. Both filter layouts render on the server and switch
by CSS media query only, so hydration never changes the layout.

## Page frame

The page uses the Library's frame: `main` at `max-w-7xl`, 16 px side padding, 32
px from 640 px. Its top padding is 20 px below 641 px, 32 px from 641 px, and 40
px from 1024 px; its bottom padding is 40 px below 641 px, 56 px from 641 px,
and 64 px from 1024 px. It sends the noindex headers the showcase design names
and renders in the interface language.

No ancestor of the sticky bar sets `overflow`, so the bar sticks in iOS Safari
too. Inside `main`, the page wrapper holds, in DOM order: the header, the filter
bar, the rail, and the results area.

- Below 1024 px the wrapper is plain block flow and the rail is `display: none`.
- From 1024 px the wrapper is a grid with columns `256px minmax(0, 1fr) auto`, a
  32 px column gap, a 28 px row gap, and the areas `"header header count"` and
  `"rail results results"`. The filter bar takes `display: contents`, so its
  count line lands in the `count` area, and its Filters button is
  `display: none`.

## Header

The `header` (`data-testid="showcase-header"`) has no band, fill, border, or
padding. It holds the h1, “CV từ cộng đồng” or “Community resumes”, bold with
-0.02em tracking, and under it the lead, “CV thật do người dùng aboutme.vn xuất
bản và chọn hiện ở đây.” or “Real resumes that aboutme.vn users published and
chose to show here.”, in `--muted-foreground`, at most 42 rem wide, 8 px under
the h1. It holds no focusable element.

| Width          | h1          | Lead        | Space under the header |
| -------------- | ----------- | ----------- | ---------------------- |
| Below 641 px   | 28 px, 1.3  | 15 px, 1.5  | 8 px to the bar        |
| 641 to 1023 px | 32 px, 1.3  | 15 px, 1.5  | 8 px to the bar        |
| From 1024 px   | 40 px, 1.25 | 16 px, 1.55 | 28 px grid row gap     |

## Count line

One `p` (`data-testid="showcase-count"`), 14 px with a 20 px line height, on one
line. It reads “{N} CV · sớm nhất trước” or “{N} resumes · earliest added
first”, with “1 resume” in English. “{N} CV” or “{N} resumes” is `--foreground`
at weight 600; the rest is `--muted-foreground`. N is always the listing
`total`, formatted with `Intl.NumberFormat` in the interface language (1.234 in
Vietnamese, 1,234 in English). It truncates with an ellipsis rather than wrap.
It is the page's polite live region (`role="status"`, `aria-atomic="true"`), so
a rail or chip change announces the new line once.

Below 1024 px it takes the sticky bar's free width, left of the Filters button.
From 1024 px it sits at the right end of the header row (`justify-self: end`,
`align-self: end`).

| State                    | Count line                                      |
| ------------------------ | ----------------------------------------------- |
| First load               | A `Skeleton` 6 rem wide and 20 px high          |
| A later load             | The last text, unchanged                        |
| `list`, `empty`          | “{N} CV · sớm nhất trước”, N the `total`        |
| `no-match`, past the end | The same; “0 CV · sớm nhất trước” when 0        |
| `failed`, after a load   | The last text, unchanged                        |
| `failed`, no load yet    | “Sớm nhất trước” or “Earliest added first” only |

Keeping the last text means a load never interrupts the pager's focus move with
an announcement, and the line never repeats the error banner.

## Filters below 1024 px

### Sticky bar

The bar (`data-testid="showcase-filter-bar"`) is a flex row, `position: sticky`,
`top: 0`, `z-index: 10`, 60 px tall including its 1 px `--border` bottom edge,
with an 8 px gap, content centered. The site header is not sticky, so the bar
sticks at the viewport top. It runs edge to edge: -16 px inline margins with 16
px padding, -32 px and 32 px from 640 px.

Its background is opaque `--background`, with no `backdrop-filter`, so its text
sits on a measured pair and tiles never show through. Only this row sticks; the
active-filter row scrolls in normal flow.

Below 1024 px the root scroller has `scroll-padding-top: 68px`, the bar height
plus room for the focus ring, so keyboard focus and the pager's scroll never
leave a tile link, Report, the invite, a pager link, Retry, or the empty action
under the bar (WCAG 2.4.11).

### Filters button

A 44 px outline button (`data-action="showcase-filters-open"`): 1 px `--input`
border, transparent fill, 10 px radius, 14 px side padding, an 8 px gap, the
lucide `ListFilter` icon at 18 px (`aria-hidden`), then “Bộ lọc” or “Filters” in
14 px weight 500 `--foreground`, with `aria-haspopup="dialog"` and
`aria-expanded`.

With any filter on, the border turns `--primary`, the right padding drops to 10
px, and a badge (`data-testid="showcase-filters-badge"`, `aria-hidden`) follows
the label: the number of active filters, 1 to 3, in a pill at least 22 px wide
and 22 px high with 6 px side padding, `--primary` fill, and 12 px weight 700
`--primary-foreground`. The button's accessible name becomes “Bộ lọc, {n} đang
bật” or “Filters, {n} active”, which starts with the visible label. The server
HTML holds the badge for a filtered URL. The button stays visible and enabled in
every state.

### Bottom sheet

The button opens a bottom sheet (`data-testid="showcase-filter-sheet"`) on
`components/ui/sheet`, `side="bottom"`: a reka-ui modal Dialog named by its
title, with a focus trap, Escape to close, and focus back on the Filters button.
Its content mounts only while open. It passes `showClose: false` and draws its
own close button.

**Box.** `--popover` fill, a 1 px `--border` top edge, the top corners at
`--radius-feature` (20 px), the bottom corners square. Below 641 px it spans the
width; from 641 px it is centered, at most 40 rem wide. It is a flex column
capped at `85dvh`, with an `85vh` fallback. The overlay is the primitive's own.
From top to bottom:

1. **Grip.** A 40 by 4 px `--input` pill, centered, 8 px from the top,
   `aria-hidden`; decoration only, with no drag gesture.
2. **Title row.** 12 px under the grip, 16 px side padding, the title on the
   left and the close button on the right. The title is an h2, “Bộ lọc” or
   “Filters”, 20 px/1.3 bold. The close button is a 44 px ghost icon button:
   lucide `X` at 20 px (`aria-hidden`), 10 px radius, accessible name “Đóng” or
   “Close”. It is the first focus target when the sheet opens.
3. **Fields.** The scroll area: `overflow-y: auto`, `min-height: 0`, 16 px side
   padding, 20 px above, and 20 px between groups. It holds the shared filter
   fields (`data-testid="showcase-filters"`), described below.
4. **Actions.** Pinned under the fields: a 1 px `--border` top edge, 12 px top
   padding, 16 px side padding, and `calc(16px + env(safe-area-inset-bottom))`
   bottom padding. Two equal columns, 12 px apart, each a 48 px button with 12
   px side padding and 15 px text.

**Role.** A visible heading “Vị trí” or “Role”, 14 px weight 600
`--muted-foreground`, 8 px above the group. The group is one `ToggleGroup`,
`type="single"`, `data-testid="showcase-roles"`, named “Lọc theo vị trí” or
“Filter by role”: All roles (“Mọi vị trí”), the nine Library roles in Library
chip order with the Library labels, then Other (“Khác”). The 11 options wrap, 8
px apart: 40 px pills, 14 px side padding, 14 px weight 500 `--foreground` on
`--card` with a 1 px `--border`, `--surface-indigo` on hover. A pressed option
takes a `--primary` fill and border, weight 600 `--primary-foreground`, and
`aria-pressed="true"`. One tab stop; arrow keys move in DOM order; Enter or
Space selects; a pressed option stays pressed.

**Resume language.** A visible heading “Ngôn ngữ của CV” or “Resume language” in
the same style, which also names the group. One `ToggleGroup`, `type="single"`,
`data-testid="showcase-languages"`: “Mọi ngôn ngữ”, “Tiếng Việt”, “Tiếng Anh”,
or All languages, Vietnamese, English. It is a three-column grid with an 8 px
gap; each option is 44 px high, 6 px side padding, 10 px radius, centered text
that may wrap to two lines at line height 1.2, with the role options' fills,
border, and keys.

**Template.** The `SelectField`, stacked: the label “Mẫu” or “Template” in the
heading style, then 8 px under it a full-width 44 px `NativeSelect`
(`name="template"`). Options: All templates (“Mọi mẫu”), the 20 presets by
preset name in alphabetical order, then Custom design (“Thiết kế riêng”).

**Clear filters** (`data-action="showcase-filters-clear"`). An outline button (1
px `--input` border, weight 500), “Xóa bộ lọc” or “Clear filters”. It sets every
filter to All and keeps the sheet open and focus on itself. It is never
disabled.

**Show results** (`data-action="showcase-filters-apply"`). The default primary
button, weight 600: “Xem {N} CV” or “Show {N} resumes”, with “Show 1 resume”, N
the live `total`. While a new total loads, it reads “Xem kết quả” or “Show
results”. It closes the sheet.

**Applying.** Every change in the sheet applies at once: it replaces the URL
query, drops `page`, and loads page 1 behind the sheet. A query value the page
does not know counts as All and is not sent to the API, as the Library reads
`?role=`; a `page` outside 1 to 100 counts as 1. Show results, Escape, the close
button, and the overlay all close the sheet and keep the applied filters.

**Sheet status.** reka hides the page's count line while the modal sheet is
open, so the sheet holds its own visually hidden `role="status"`
(`aria-atomic="true"`), mounted only while open, that mirrors the count line's
text.

**Width change.** When the viewport crosses 1024 px with the sheet open, the
sheet closes and focus moves to the rail's pressed Role option. A `@vueuse/core`
`useMediaQuery` watcher only closes; it never decides what renders.

## Active filters

One element (`data-testid="showcase-active-filters"`), the first row of the
results area, so it sits under the sticky bar below 1024 px and above the grid
from 1024 px. It renders only while a filter is on, in the server HTML too, and
leaves no gap when absent. It is a `role="group"` named “Bộ lọc đang bật” or
“Active filters”, with no visible prefix: a flex row that wraps, 8 px apart,
holding one chip per active filter (role, language, template) and then Clear
all.

**Chip** (`data-action="showcase-filter-remove"`, `data-filter` set to `role`,
`lang`, or `template`). The whole chip is one button: 36 px high, 12 px left and
10 px right padding, full radius, `--secondary` fill, 1 px `--border`, 14 px
weight 500 `--foreground`, `--accent` on hover. The label comes first: the role
label, the language label, or the template name or “Thiết kế riêng” / “Custom
design”, truncated with an ellipsis past 14 rem. A lucide `X` at 14 px follows
it, 4 px away, `aria-hidden`. The accessible name is “{label}, gỡ bộ lọc” or
“{label}, remove filter”, so it starts with the visible text. Activating it sets
that one filter to All.

**Clear all** (`data-action="showcase-filters-clear-all"`). A 36 px text button,
8 px side padding, 14 px weight 500 `--link`, underlined: “Xóa hết” or “Clear
all”. It sets every filter to All.

## Rail from 1024 px

An `aside` (`data-testid="showcase-filter-rail"`) named “Bộ lọc” or “Filters”,
in the `rail` grid area, 256 px wide: `--card` fill, 1 px `--border`,
`--radius-dialog` (14 px), 20 px padding, 24 px between groups. It is not
sticky. It holds the same shared filter fields as the sheet
(`data-testid="showcase-filters"`), laid out for the rail. Element ids come from
`useId()`, so the rail and an open sheet never share one.

**Role.** The heading “Vị trí” or “Role”, 14 px weight 600 `--muted-foreground`,
8 px above the list. The same `ToggleGroup` and name as in the sheet, with
`orientation="vertical"`: a column with 2 px between rows. Each row is 36 px
high, 12 px side padding, 10 px radius, left-aligned 14 px `--foreground` at
weight 400, transparent, `--muted` on hover. The pressed row takes an `--accent`
fill, weight 600, and an 8 px `--primary` dot at its right end (`aria-hidden`),
and carries `aria-pressed="true"`. Arrow Up and Down move focus.

**Resume language.** The heading “Ngôn ngữ của CV” or “Resume language”, then
the same three options as a three-row list styled exactly like the Role list,
with the full labels “Mọi ngôn ngữ”, “Tiếng Việt”, “Tiếng Anh” or All languages,
Vietnamese, English, never VI and EN (WCAG 2.5.3); three full labels do not fit
one segmented row in the rail's 216 px.

**Template.** The `SelectField`, stacked, with the 40 px `NativeSelect` filling
the rail width.

A rail change applies as a sheet change does; focus stays on the control.

## Grid

The results area (`data-testid="showcase-results"`) is a one-column grid: the
active-filter row, the state element, then the pager, with a 16 px row gap (20
px from 1024 px). It carries `container-type: inline-size`. Below 1024 px it
starts 16 px under the bar.

Tiles sit in a `ul` grid that stretches each tile to its row's height:

| Width and results width        | Columns | Column gap | Row gap |
| ------------------------------ | ------- | ---------- | ------- |
| Below 641 px                   | 1       | 16 px      | 16 px   |
| 641 to 1023 px                 | 2       | 24 px      | 24 px   |
| From 1024 px, results < 860 px | 2       | 28 px      | 28 px   |
| From 1024 px, results ≥ 860 px | 3       | 28 px      | 28 px   |

The three-column rule is a container query nested in a `(width >= 1024px)` media
query, so tablets keep two columns; results reach 860 px at about a 1216 px
viewport. A tile is 358 px wide at 390 px, 340 at 768, 322 at 1024, and 290
at 1440.

## Tile

Each `li` (`data-showcase-slug`) is a flex column on `--card` with a 1 px
`--border`, `--radius-dialog` (14 px), and `--shadow-product`. It holds two
focus targets, in order: the tile link and the Report link. Report is never
inside the tile link.

**Tile link.** A block `NuxtLink` (`data-showcase-tile`) to `/{slug}`, same tab,
`rel="nofollow"`, covering the card image and the meta block, not the footer
row. Its focus ring is a 2 px `--ring` outline at 2 px offset with the 14 px
radius. When the link is hovered, the whole tile lifts 4 px over 200 ms
ease-out, as Library cards do.

**Card image.** A box with `aspect-ratio: 1200 / 630`, a `--muted` fill, 13 px
top corners, a 1 px `--border` bottom edge, and `overflow: hidden`; it holds its
height before the image arrives. Inside it, an `img` fills the box
(`object-fit: cover`) with `src` set to the versioned card URL, `width="1200"`,
`height="630"`, `loading="lazy"`, `decoding="async"`, and `alt` set to the
item's `imageText`. Every tile loads lazily, the first included. When the image
fails, as after a color change in an open tab, the `img` turns transparent over
the `--muted` box.

**Meta block.** 12 px top, 16 px side, and 4 px bottom padding, 8 px between its
two rows:

1. The meta row, a flex row that wraps with an 8 px gap, items centered:
   - The role chip, only when the item has a role (`data-showcase-role`): a 24
     px pill, 10 px side padding, full radius, `--surface-blue` fill, 12 px
     weight 600 `--foreground`, with the Library role label or “Khác” / Other.
     It is a label, not a control.
   - The language, 14 px `--muted-foreground`: “Tiếng Việt”, “Tiếng Anh”, or
     “Ngôn ngữ khác” / Vietnamese, English, or Other language.
2. The template name or “Thiết kế riêng” / “Custom design”, 14 px weight 500
   `--foreground`, wrapping when long.

**Footer row.** Outside the tile link, pinned to the tile bottom
(`margin-top: auto`): a flex row, items centered, space between, 4 px top, 8 px
right and bottom, and 16 px left padding.

- The slug text, “aboutme.vn/{slug}”, 13 px `--muted-foreground`, one line,
  `min-width: 0`, truncated with an ellipsis (**Owner approval** S14). It is
  plain text, not a link. Report's accessible name carries the full slug when
  the text truncates.
- The Report link (`data-action="showcase-report"`): a 40 by 40 px icon link, 10
  px radius, the lucide `Flag` icon at 18 px (`aria-hidden`) in
  `--muted-foreground`, turning `--foreground` on an `--accent` fill on hover.
  Its focus ring matches the tile link's; its name is “Báo cáo CV {slug} qua
  email” or “Report resume {slug} by email”. It opens `mailto:danny@aboutme.vn`
  with the interface-language subject “Báo cáo trang Cộng đồng: {slug}” or
  “Report showcase: {slug}”, URL-encoded. The existing `components/ui/tooltip`
  shows “Báo cáo” or “Report” above it on hover and on focus; a screen reader
  then also hears that word as the description, which is accepted.

## Invite card

In the `list` state, on every page, the grid's last `li` is the invite card
(`data-testid="showcase-invite"`); no other state shows it. It is not a tile: no
`data-showcase-tile`, and the pager's focus move skips it.

The card is a flex column, content centered vertically, on `--surface-sand` with
a 1 px dashed `--input` border and `--radius-dialog` (14 px), no shadow. Padding
is 20 px by 16 px below 641 px and 24 px from 641 px, with 12 px between items:

1. The title, an h2, “Muốn CV của bạn ở đây?” or “Want your resume here?”, 20
   px/1.3 bold `--foreground`.
2. The body, “Khi xuất bản CV, bật Hiện trong trang Cộng đồng. Bạn tắt lúc nào
   cũng được.” or “When you publish, turn on Show in the community showcase. You
   can turn it off any time.”, 15 px/1.5 `--muted-foreground`. The switch name
   matches the publish dialog's label exactly.
3. The button (`data-action="showcase-invite-create"`), the default primary
   `Button` rendered as a `NuxtLink`, 44 px high, 16 px side padding, 15 px
   weight 600, left-aligned: “Tạo CV miễn phí” or “Create a free resume” in
   every case. Its target is the empty action's: `/app/resumes` signed in,
   `/register` signed out with password registration on, otherwise `/login`.

## Pager

The pager (`data-testid="showcase-pager"`) shows only when `pageCount` is above
1, 40 px under the grid. It is a `nav` named by the page status through
`aria-labelledby`, holding Previous, the page status, and Next.

- Previous and Next are `NuxtLink`s styled as the 36 px outline button, with
  `ChevronLeft` before “Trang trước” / Previous and `ChevronRight` after “Trang
  sau” / Next, icons `aria-hidden`. They keep the filters and set `?page=`.
- On the first page Previous, and on the last page Next, render as a `span` with
  the same classes, `aria-disabled="true"`, 50 percent opacity, and no tab stop.
- The status reads “Trang {page}/{pageCount}” or “Page {page} of {pageCount}”,
  14 px `--muted-foreground`, tabular figures.
- From 641 px the three sit in one centered row, 12 px apart. Below 641 px the
  status takes its own line and the buttons share the next row at equal width.

After Previous or Next, the page scrolls the results area to the top (instantly
under reduced motion; below the bar thanks to the scroll padding) and moves
focus to the first tile link once the page loads, or to Retry when it fails.

## States

The grid area shows exactly one state, its element carrying `data-state`. The
bar, the Filters button, and the rail stay visible in every state.

| State      | When                            | Shows                                   |
| ---------- | ------------------------------- | --------------------------------------- |
| `loading`  | First render and every new load | Six skeleton tiles; the `ul` is busy    |
| `list`     | Items arrived                   | The tiles, the invite card, the pager   |
| `empty`    | `total` is 0 and no filter set  | `EmptyState` with the empty action      |
| `no-match` | `total` is 0 with any filter    | The no-match line                       |
| `failed`   | The request failed or timed out | `StatusBanner` with the error and Retry |

**Loading.** The server HTML holds this state: six `aria-hidden` skeleton tiles
with the tile box, a `Skeleton` in the image box, and one 16 px `Skeleton` line
two thirds wide in the meta block; the `ul` carries `aria-busy="true"` until
items render.

**Empty.** `EmptyState`, full grid width, titled with the Empty text and no
description. Its action (`data-action="showcase-empty-action"`) is a default 36
px `Button` rendered as a `NuxtLink`: “Mở CV của bạn” / “Open your resumes” to
`/app/resumes` signed in; signed out, “Tạo CV” / “Create your resume” to
`/register` with password registration on, otherwise to `/login`.

**No match.** The No match line, 15 px `--muted-foreground`, as the Library's.
It carries no `role="status"`: the count line already announces “0 CV · sớm nhất
trước”. No pager, except past the end.

**Past the end.** A `page` above `pageCount` gets `200` with empty items and the
real `pageCount`. The page shows the no-match line and the pager, where only
Previous is a link.

**Load failed.** `StatusBanner` with `kind="error"`, full grid width, holding
the Load failed text and, 8 px under it, a small outline Retry `Button`
(`data-action="showcase-retry"`). Retry keeps the banner, disables itself, and
reloads the same query, so focus stays on it; success replaces the banner with
the tiles. A rate-limited or `400` response shows this state too.

## Focus

Focus order: the shell; below 1024 px the Filters button, from 1024 px the
rail's Role group, language group (one stop each), and template select; each
active-filter chip and Clear all; per tile the tile link and Report; the invite
button; Previous and Next; the footer. Retry or the empty action takes the
grid's place. The open sheet runs: close, Role, language, template, Clear
filters, Show results.

| Action                     | Focus moves to                                 |
| -------------------------- | ---------------------------------------------- |
| Remove a chip              | The next chip, else the previous chip          |
| Remove the last chip       | Filters button below 1024 px, else pressed row |
| Clear all                  | Filters button below 1024 px, else pressed row |
| Clear filters in the sheet | Stays on Clear filters                         |
| Close the sheet            | The Filters button                             |
| Viewport crosses 1024 px   | The rail's pressed Role row                    |
| Previous or Next           | The first tile link, or Retry on failure       |

“Pressed row” is the rail's pressed Role option: All roles after Clear all.

## Accessibility and fit

Text uses only pairs the theme tests check in both themes: `--foreground`,
`--muted-foreground`, and `--link` on every ground the page uses, and
`--primary-foreground` on `--primary`. The 13 px slug on `--card` is about 6.1:1
light and 7.5:1 dark. The tests check `--input` on `--background` and `--card`;
it measures about 3.8:1 and 3.4:1 on `--popover` and 3.3:1 and 3.6:1 on
`--surface-sand` (light, dark). Focus rings use `--ring`, 3:1 on every ground.

Every control's accessible name starts with its visible text (WCAG 2.5.3); the
close button and Report are icon-only. Targets: Filters, the close button,
language options, and the sheet select 44 px; sheet actions 48 px; role options
and Report 40 px; active-filter chips, Clear all, and rail rows 36 px.

Vietnamese strings are the longest; each fits 360 px with no sideways scroll:

- Bar: “1.234 CV · sớm nhất trước” and the Filters button with its badge need
  about 320 px of the 328 px row; the count line truncates first.
- Sheet language options are about 104 px each at 360 px; “Mọi ngôn ngữ” wraps
  to two lines inside its 44 px when a font renders wider.
- Sheet actions are about 158 px each; “Show 1,234 resumes” fits.
- Active-filter chips wrap to a second line at 390 px with three filters on;
  long template names truncate at 14 rem.
- The pager moves the status to its own line below 641 px, because “Trang
  trước”, “Trang 100/100”, and “Trang sau” need about 375 px in one row.

The finish review checks 360, 390, 768, 1024, 1216, and 1440 px in both
languages and themes, with the sheet open and three filters on.

## Motion

The global reduced-motion rule under `html[data-ui="app"]` stops the sheet's
slide, the overlay's fade, the tooltip's fade, the tile lift, and the skeleton
pulse; the pager's scroll is instant. Nothing else on the page animates.
