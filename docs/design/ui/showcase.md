# aboutme community showcase page

The layout of `/showcase`: results first, a count line, filters in a bottom
sheet below 1024 px and in a left rail from 1024 px, tiles with a footer row,
and an invite card at the end of the grid. The [showcase design](../showcase.md)
owns product scope and approvals. The
[showcase contract](../showcase-contract.md) sets what a tile holds, the order,
the filters, and the API; copy here quotes the
[approved tables](../showcase-copy.md). Part of the
[visual design](../../../DESIGN.md).

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
- From 1024 px the wrapper is a grid with columns
  `256px minmax(0, 1fr) minmax(18rem, auto)`, a 32 px column gap, a 28 px row
  gap, and the areas `"header header count"` and `"rail results results"`. The
  filter bar takes `display: contents`, so its count line lands in the `count`
  area, and its Filters button is `display: none`.

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
From 1024 px it fills the count column, at least 18 rem wide, with right-aligned
text at the bottom of the header row, so its box does not move when the text
replaces the skeleton.

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

See [Filters below 1024 px](showcase-filters.md#filters-below-1024-px).

### Sticky bar

See [Sticky bar](showcase-filters.md#sticky-bar).

### Filters button

See [Filters button](showcase-filters.md#filters-button).

### Bottom sheet

See [Bottom sheet](showcase-filters.md#bottom-sheet).

## Active filters

See [Active filters](showcase-filters.md#active-filters).

## Rail from 1024 px

See [Rail from 1024 px](showcase-filters.md#rail-from-1024-px).

## Grid

See [Grid](showcase-results.md#grid).

## Tile

See [Tile](showcase-results.md#tile).

## Invite card

See [Invite card](showcase-results.md#invite-card).

## Pager

See [Pager](showcase-results.md#pager).

## States

See [States](showcase-results.md#states).

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
