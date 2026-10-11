# Community showcase filters

The [page layout](showcase.md) owns composition and shared interaction rules.
The [showcase contract](../showcase-contract.md) owns filter values; the
[approved copy](../showcase-copy.md) owns interface text quoted here.

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
