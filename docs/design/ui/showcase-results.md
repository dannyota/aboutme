# Community showcase results

The [page layout](showcase.md) owns composition and shared interaction rules.
The [showcase contract](../showcase-contract.md) owns listing fields; the
[approved copy](../showcase-copy.md) owns interface text quoted here.

## Grid

The results area (`data-testid="showcase-results"`) is a one-column grid: the
active-filter row, the state element, then the pager, with a 16 px row gap (20
px from 1024 px). It carries `container-type: inline-size` and
`align-content: start`, so the active-filter row stays at the top while the grid
is shorter than the rail. Below 1024 px it starts 16 px under the bar.

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
