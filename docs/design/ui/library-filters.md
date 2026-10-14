# aboutme Library filters

The template Library at `/templates` uses a filter sheet below 1024 px and
wrapping chip rows from 1024 px. The
[landing and Library spec](landing-and-library.md#library) owns the frame,
header, cards, grid, and catalog order. [Showcase filters](showcase-filters.md)
owns the shared bar, sheet, and removable-chip visuals and behavior. This page
defines the Library variant. Part of the [visual design](../../../DESIGN.md).

## Decision and trade-offs

**Proposed, owner approval required:** adopt the showcase's phone sheet and keep
desktop rows, without its rail. The roadmap names a desktop rail; approve this
substitution before implementation. If a rail is required, revise this spec
first.

Rows wrap instead of hiding choices in a sideways scroller. Below 1024 px the
sheet leaves more room for cards, while its badge and removable chips expose
filters selected through landing links. Desktop rows keep all choices visible
and the existing card widths. Their cost is vertical space when they wrap; do
not assume a line count from character counts or a particular font advance.

The [showcase frame](showcase.md#page-frame) reserves 256 px plus a 32 px gap
for its rail. The Library's content width is `min(viewport, 1280) - 64` at these
widths. Its current grid uses viewport breakpoints and 28 px column gaps:

| Viewport | Content | Current columns × width | Rail, same columns | Rail, one fewer column |
| -------- | ------- | ----------------------- | ------------------ | ---------------------- |
| 1024 px  | 960 px  | 4 × 219 px              | 4 × 147 px         | 3 × 205.3 px           |
| 1180 px  | 1116 px | 5 × 200.8 px            | 5 × 143.2 px       | 4 × 186 px             |
| 1280+ px | 1216 px | 5 × 220.8 px            | 5 × 163.2 px       | 4 × 211 px             |

Each width is `(available - (columns - 1) × 28) / columns`. A rail does not
automatically remove a column: keeping the current CSS shrinks cards instead.
Reducing columns from four to three makes 20 cards take seven rows rather than
five; five to four makes five rows rather than four. Copying the showcase's
[results grid](showcase-results.md#grid) instead gives two columns at 1024 and
1180 px, and three at 1280+ px, because its results threshold is 860 px. Those
tiles have different proportions and content from Library cards. The Library has
only two chip groups, so the rail's extra space for a select is unused.

## Filter and URL contract

Role stays a single-choice `ToggleGroup`, with `aria-pressed` and the existing
keyboard model. Style stays a named `nav` with a `ul` of native links rendered
by `NuxtLink`, in both row and sheet. Only its current link has
`aria-current="page"`; no `aria-pressed`, button role, or roving tab index. Each
style link is a Tab stop; Enter follows it. Space and arrow keys keep native
behavior. A modal can contain links: its focus trap contains all focusable
descendants, not only buttons. See the W3C
[link pattern](https://www.w3.org/WAI/ARIA/apg/patterns/link/),
[modal pattern](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/), and
[`aria-current`](https://www.w3.org/TR/wai-aria-1.2/#aria-current).

Keeping links preserves copy-link, context menus, middle-click, modified-click,
and navigation without scripts. Converting them to toggles would save seven Tab
stops but remove those capabilities. Canonical URLs do not replace them. The
canonical remains `/templates`; landing style URLs remain valid.

- Role uses `role`, Style uses `filter`. Their intersection selects visible
  cards. Values and order follow `ROLES` and `FILTERS` in
  `apps/web/app/templates/catalog.ts`; labels are specified under Copy.
- Every target preserves unrelated query keys and the other valid filter. All
  removes only its own key; Clear filters and Clear all remove both. Unknown,
  empty, or repeated filter values count as All. Leave the incoming URL alone
  until an action; then remove invalid `role` and `filter` values.
- Each style link has a real, encoded `/templates` href containing that target.
  With scripts, ordinary primary activation and Enter use `router.replace`
  without scrolling or remounting the page. The sheet stays open and focus stays
  on the link. Modified or middle clicks retain browser navigation and leave the
  source tab unchanged. Disable speculative link prefetch here.
- Role changes and removal/clear buttons use the same query builder and replace
  behavior. Selecting the current value is a no-op. Serialize rapid changes
  against the latest intended filter pair, so one change cannot drop another.
  Only a successful latest navigation commits cards, count, and selected state;
  cancelled or failed navigation keeps the last committed state; focus recovery
  follows the rule below.
- Back/Forward and incoming URLs derive both groups, cards, badge, and count
  from the route. No separate saved filters, cookie, or local storage.
- Server HTML contains all 20 card links, with nonmatches `hidden`, the selected
  row controls, count, and phone badge/chips. The closed sheet is not mounted.
  Hidden cards are absent from focus order and the accessibility tree. The first
  two visible cards load eagerly, the rest lazily.

### Without JavaScript

Desktop Style links still navigate and the server applies both URL filters.
Below 1024 px, a `noscript` fallback after the count bar shows the same Style
navigation, wrapping at 8 px gaps with the row chip dimensions. It is hidden
from 1024 px, where the normal Style row is visible. In `noscript` at every
width, a Clear all link follows the style choices when either filter is on; it
removes both filter keys. This also recovers a role-filtered landing URL.

Role selection still requires scripts. Role buttons, Filters, and removable chip
buttons remain disabled until hydration; the fallback links stay usable without
it. No script-only dialog is required to reach Style links. Existing card text
and links remain server-rendered; this change does not add a fallback renderer
for live thumbnails.

## Layout and controls

Both responsive layouts exist in server HTML and switch with CSS, not a
viewport-dependent render branch. The sheet content mounts only while open.
Inside `main`, DOM order is header, desktop rows, bar/count, active chips,
no-script fallback, grid, and no-match line.

| Width         | Visible controls                                       | Vertical spacing                                                                               |
| ------------- | ------------------------------------------------------ | ---------------------------------------------------------------------------------------------- |
| Below 1024 px | Bar with count and Filters, then active chips when set | Header to bar 16 px; bar to chips 16 px; chips to grid 16 px; without chips, bar to grid 16 px |
| From 1024 px  | Role row, Style row, count                             | Header to Role 32 px; Role to Style 12 px; Style to count 24 px; count to grid 16 px           |

The desktop rows are `display: none` below 1024 px. The Filters button and
active-chip row are `display: none` from 1024 px. Hidden layouts leave no gap.
The fallback Style row uses 16 px top spacing; its Clear all link has 12 px
above it. The existing grid breakpoints remain 641, 900, and 1180 px,
independent of the filter breakpoint. The bar bleed changes at 640 px; the sheet
width changes at 641 px, as [shared controls](showcase-filters.md) specify.

### Desktop rows

Each row wraps with 8 px gaps and no horizontal scroll. Chips are 36 px high, 16
px side padding, a full pill radius, 1 px `--border`, `--card` fill, 14 px
weight 500 `--foreground`; hover uses `--surface-indigo`. Focus uses a 2 px
`--ring` outline at 2 px offset. The selected chip has `--primary` fill and
border and `--primary-foreground` text, still weight 500.

Role (`data-testid="gallery-roles"`) uses `data-role` for every option,
including `all`, without dots. One Tab stop; arrow keys move focus in DOM order,
Home/End move to the first/last option, Enter or Space selects, and a pressed
option stays pressed. Wrap order follows DOM order.

Style (`data-testid="gallery-styles"`) uses `data-filter` for each option,
including `all`. All has no dot. The four format choices use
`data-group="format"` and an 8 px `--brand-jade` dot, 8 px before the text. The
three audience choices use `data-group="audience"` and `--brand-ochre`. Dots are
`aria-hidden` and use `currentColor` when selected. Before First job, a
decorative divider is 1 × 24 px, `--border`, with 4 px side margins and no Tab
stop. It stays with the First job chip when rows wrap.

### Phone bar and sheet

Use the [sticky bar](showcase-filters.md#sticky-bar) and
[Filters button](showcase-filters.md#filters-button) with the Library spacing
above. No ancestor sets overflow. Root scroll padding is 68 px only below 1024
px while a filter-bar page is mounted; route exit removes it. Focus rings on
cards must clear the bar. The active count is 0 to 2; zero omits the badge.

Use the [bottom sheet](showcase-filters.md#bottom-sheet) box, title, close,
scroll area, safe-area actions, modal keyboard behavior, and dismissal rules.
Library fields have two groups, 20 px apart. Each heading is 14 px weight 600
`--muted-foreground`, 8 px above its options:

1. Role uses the showcase sheet's 40 px pills, 14 px side padding, 8 px gaps,
   selected weight 600, and keyboard model, without Other.
2. Style uses the same 40 px pill dimensions and selected weight 600, retaining
   the link semantics above and desktop dots. The divider becomes a full-width
   zero-height break before audience choices; no line is drawn.

Clear filters keeps the sheet open and focus on itself. Show results closes it;
its label always uses the committed visible count. Library filtering has no data
fetch or loading label. Escape, overlay click, and close keep the applied URL
filters. IDs for headings and ARIA references use `useId()` so hidden rows and
an open sheet never share an element ID.

### Active chips

Use [active filters](showcase-filters.md#active-filters), in Role then Style
order, followed by Clear all. Render only when at least one filter is on. Chip
labels omit counts, including the sample count. Allow wrapping with 8 px gaps;
two chips plus Clear all need not fit one line. Full labels remain in accessible
names when visible labels truncate at the shared 14 rem limit.

### Test hooks

Use the shared suffixes with the prefix `gallery`: `filter-bar`, `count`,
`filters-badge`, `filter-sheet`, `sheet-status`, `active-filters` for
`data-testid`; `filters-open`, `filters-close`, `filters-clear`,
`filters-apply`, `filter-remove`, `filters-clear-all` for `data-action`. Fields
use `data-testid="gallery-filters"`, with the two group hooks above. Removal
buttons use `data-filter="role"` or `"style"`. Scope selectors to the visible
row or sheet because hidden desktop groups remain mounted.

## Announcements and focus

The count is one `p` with `role="status"` and `aria-atomic="true"`, 14 px/20 px,
weight 600 `--foreground`. It is server-rendered, with no skeleton. Below 1024
px it fills the bar space before Filters and truncates first with an ellipsis;
above it is left-aligned under Style. At zero, hidden cards leave the no-match
text, 15 px `--muted-foreground`, 16 px below the preceding row, with no second
status role. Collapse the empty grid's spacing.

The page owns the announcement key. After a successful filter change, replace
the inner text span once even when the count is unchanged. Compare the latest
committed normalized pair with the last announced pair, not a sticky pending
boolean. Seed the pair from server state; hydration, reopening the sheet,
reselecting a chip, clearing already-clear filters, or changing only unrelated
query keys must not increment the key. Locale changes update copy in place
without incrementing it. The sheet's hidden status mirrors text and key while
open; the modal hides the page region from assistive technology.

If A changes to B and back to A before B settles, do not announce A again or let
B's late completion update the text/key. If B committed and was announced, a
later change back to A is a real change and announces once. The same rule must
survive the shared extraction for the showcase's asynchronous loads: failed
loads keep the last text/key, retry success announces only a changed filter
tuple, and paging alone never increments the key. Keep that page's request
coordination outside the shared presentation components.

Focus order: shell; on phones Filters, active chips, Clear all; on desktop Role
(one stop), every Style link; visible card links; footer. In the sheet: close,
Role (one stop), every Style link, Clear filters, Show results. Tab and
Shift+Tab wrap within the modal; all controls remain reachable by scrolling the
fields. Enter/Space activate buttons; Style links follow their own keys.

| Action                                              | Focus destination                      |
| --------------------------------------------------- | -------------------------------------- |
| Select Role or follow Style in the same tab         | Same control                           |
| Open sheet                                          | Close button                           |
| Remove chip                                         | Next chip, else previous chip          |
| Remove last chip or Clear all                       | Filters button                         |
| Clear filters in sheet                              | Clear filters                          |
| Dismiss sheet                                       | Filters button                         |
| Grow to 1024 px with sheet open                     | Close sheet, then pressed desktop Role |
| Grow to 1024 px with focus in hidden phone controls | Pressed desktop Role                   |
| Shrink below 1024 px with focus in desktop controls | Filters button                         |

On resize, move focus only if its control becomes hidden; never steal focus from
a card or shell control. Width watchers repair focus and close the sheet; CSS
decides layout. Normal dismissal must not override the desktop focus move.

For removal or clear, capture the initiating control before the shared focus
handoff. If the latest navigation fails or is cancelled, restore that control
only while it is mounted, visible, and enabled, focus is still at the handoff
destination, and no later user focus action or navigation has occurred.
Otherwise leave focus alone. Superseded actions never restore focus. Other
failed filter actions keep focus on their initiating control without a new focus
call.

## Copy and motion

Shared copy follows [showcase filters](showcase-filters.md). All labels below
are visible and accessible text; group names are distinct from headings. Counts
use plain digits. All counts use catalog size; sample counts count
sample-bearing templates, not sample languages.

| Key or value     | Vietnamese                                                                               | English                                                                                   |
| ---------------- | ---------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| `rolesLabel`     | Lọc theo vị trí                                                                          | Filter by role                                                                            |
| `allRoles`       | Mọi vị trí                                                                               | All roles                                                                                 |
| `roleHeading`    | Vị trí                                                                                   | Role                                                                                      |
| Roles, in order  | Backend, Frontend, Mobile, DevOps/SRE, Data/AI, QA/Tester, Fresher/Intern, BrSE, Bảo mật | Backend, Frontend, Mobile, DevOps/SRE, Data/AI, QA/Tester, Fresher/Intern, BrSE, Security |
| `stylesLabel`    | Lọc theo phong cách                                                                      | Filter by style                                                                           |
| `styleHeading`   | Phong cách                                                                               | Style                                                                                     |
| `all(N)`         | Tất cả {N}                                                                               | All {N}                                                                                   |
| `sample`         | Có CV mẫu                                                                                | With a sample                                                                             |
| `ats`            | Chuẩn ATS                                                                                | ATS-friendly                                                                              |
| `one-page`       | Một trang                                                                                | One page                                                                                  |
| `photo`          | Hợp với ảnh                                                                              | Suits a photo                                                                             |
| `first-job`      | Mới tốt nghiệp                                                                           | First job                                                                                 |
| `technical`      | Kỹ thuật                                                                                 | Technical                                                                                 |
| `management`     | Quản lý                                                                                  | Management                                                                                |
| `count(N)`       | {N} mẫu                                                                                  | {N} templates; 1 template                                                                 |
| `showResults(N)` | Xem {N} mẫu                                                                              | Show {N} templates; Show 1 template                                                       |
| `noMatch`        | Không có mẫu nào khớp với bộ lọc này.                                                    | No template matches this filter.                                                          |

Append a space and sample count to the sample option: “Có CV mẫu 13” / “With a
sample 13”. `stylesLabel` replaces `galleryCopy.filtersLabel`. Shared strings,
including Filters, its active-count name, close, Clear filters, active-group
name, remove name, and Clear all, keep the exact bilingual text in the shared
spec. The no-script Clear all link uses the same copy.

Every accessible name starts with visible text. Text, borders, and focus rings
use the semantic pairs in
[showcase accessibility](showcase.md#accessibility-and-fit). Chip color
transitions are 150 ms. The global reduced-motion rule under
`html[data-ui="app"]` disables them, the sheet/overlay motion, and card lift; no
added animation or smooth filter scroll.

## Shared component boundary

Extract presentation into `apps/web/app/components/app/FilterBar.vue`,
`FilterSheet.vue`, and `ActiveFilters.vue`, following
[ADR 0019](../../adr/0019-application-ui-toolkit.md). Keep thin showcase
adapters at the existing component paths to map domain state/copy and preserve
their public props, events, exposed methods, and unit-test imports. Library
fields live in `components/templates/LibraryFilters.vue` with
`layout: 'row' | 'sheet'`. `ShowcaseFilters.vue` keeps its role/language toggles
and template select.

- All three composites accept a fixed `showcase | gallery` prefix for every
  test/action hook, including internal focus queries. Showcase suffixes and
  `data-filter` values stay byte-for-byte unchanged. Preserve its existing CSS
  classes where page selectors or proofs use them.
- FilterBar owns the Sheet root/trigger and default sheet slot, accepts open
  state, count text, optional head/tail, key, active count, and control copy;
  emits `update:open`, exposes `focusFilters()`. An explicit Library layout
  variant sets its spacing, bold whole count, and desktop alignment. Showcase
  retains its skeleton, separator only with a tail, grid area, and alignment.
- FilterSheet owns modal presentation, default fields slot, title/close/clear
  copy, results-label prop, status, and key. It emits `clear` and forwards
  `close-auto-focus`; the showcase adapter maps clear to `change({})` and
  supplies its loading label. Dismissal stays under the shared Sheet root.
- ActiveFilters receives ordered `{kind, label}` chips and copy; emits
  `remove(kind, last)` and `clear-all`. Preserve keyed chip nodes and focus
  handoff before removal. Callers decide whether it renders and where focus goes
  when empty. It imports no catalog or route state.
- Pages own route-scoped `useHead` root class `filter-bar-scroll-padding`;
  FilterBar supplies the shared global CSS scoped to that class and
  `html[data-ui="app"]`. Keep it through Library/showcase navigation; remove it
  on other pages. Never mutate the root class during SSR in a module global.

Put shared control copy in `apps/web/app/i18n/filters.ts`, imported by both
surface catalogs. Do not spread a `filters: 'Filters'` key into `galleryCopy`:
its `filters` key is the style-label map. Expose shared copy under a nested
`filterControls` key in Gallery; preserve Showcase's existing flat API through
explicit aliases. The shared file imports only locale types, never either
surface catalog, so `showcaseCopy` can keep reusing Gallery role labels without
a cycle. Show-results/count copy stays surface-specific. The Library must not
pull showcase listing, API, or unrelated copy into its client chunk.

## Compatibility, security, and delivery

No schema, API, database, or stored user state changes. Existing URLs and older
open clients continue to work. Rollback restores the earlier layout without data
loss. Ship extraction and both call-site adaptations together, after owner
approval and independent design review. Preserve the latest showcase
announcement regression before extracting its controls.

No package, image, font, or network request is added for filtering. Reuse the
installed Sheet and toggle primitives. Record client chunk deltas in hosted
build evidence; keep catalog data and the 20 server card nodes bounded as today.

Anonymous and signed-in attackers can supply query values: exact allowlists map
them to static catalog choices, links stay on `/templates`, and labels render as
text. Never interpolate raw query text into HTML, selectors, module paths, or
URLs. Filtering has no write, authorization, key custody, revocation, or
recovery operation. It does not expose private resumes or read tokens. Hostile
resume content stays outside these controls; stolen credentials, network/host
compromise, database writers, and leaked backups gain no new interface through
this change. Existing session, transport, and renderer boundaries remain in
force. Failed navigation retains committed state as above.

## Proofs and baselines

Run in hosted CI under [web rules](../../../instructions/web.md); no local
browser or test exception is required. Tests prove DOM updates and focus, not
that every screen reader speaks identical wording.

- `apps/web/test/templates/gallery-chrome.test.ts`: both semantics, encoded
  hrefs preserving the other group, invalid/repeated values, clear/no-op,
  successful/failed/superseded navigation, failed removal/clear focus recovery
  without stealing later focus, same-count announcement and reverted change,
  sheet status, unique IDs, bilingual count/zero copy. Use controlled navigation
  promises for races; do not invent a Library data load.
- `apps/web/test/templates/gallery.test.ts`: extend query-builder tests for both
  keys and invalid normalization; retain catalog and canonical assertions.
- `apps/web/test/showcase/showcase-page.test.ts`: retain same-total, paging,
  failure/retry, and sheet-status tests. Retain or add the reverted-before-load
  regression here if absent. `showcase-filters.test.ts` and
  `showcase-i18n.test.ts` in that directory retain showcase keys and copy.
- `apps/web/e2e/gallery.spec.ts`: replace horizontal-scroll assumptions with
  row/sheet checks. At 390 and 1440 px prove Role keyboard use, Style Tab/Enter,
  real href and new-tab navigation, intersection, no-op, clear, and zero
  results. At 390 px prove links keep the sheet open, badge/status/results
  labels, removal focus, dismissal return, and both resize directions at
  1023/1024 px. Prove no-script Style navigation/reset at 390 and 1440 px and
  filtered SSR retains 20 links with correct hidden/current/pressed state and
  phone chips.
- In that browser file, check overflow and layout at 360, 390, 640, 641, 768,
  900, 1023, 1024, 1179, 1180, and 1440 px, both languages and themes. Check
  sticky focus clearance at 390/768 px, root padding removal after route exit,
  and the longest active labels. A phone integration case traverses the Style
  links inside the focus trap; reuse showcase coverage for primitive Escape,
  generic trap, and reduced motion.
- `apps/web/e2e/showcase.spec.ts` and `showcase-baselines.spec.ts` keep their
  existing selectors and expectations. Their shared-control coverage must pass
  unchanged; add an overlay-dismissal assertion to `showcase.spec.ts` for the
  shared sheet, preserving applied filters and returning trigger focus. Do not
  copy its network/pager/error tests into Gallery. Keep the shared safe-area
  padding formula unchanged; existing snapshots cover ordinary viewports only.
  Record nonzero safe-area behavior as unverified unless hosted browser evidence
  supplies an actual nonzero inset; do not claim it from a zero-inset baseline.

Hosted Baselines regenerates only the four Library images in
`apps/web/e2e/chrome.spec.ts`:
`chrome--templates--{light,dark}--{390,1440}.png`. Add
`apps/web/e2e/gallery-baselines.spec.ts` with these five files under
`apps/web/e2e/baselines/`:

| File                                     | State                                    |
| ---------------------------------------- | ---------------------------------------- |
| `gallery--sheet--vi--dark--390.png`      | `/templates`, sheet open                 |
| `gallery--sheet--en--light--390.png`     | `/templates`, sheet open                 |
| `gallery--filtered--vi--dark--390.png`   | `?role=backend&filter=technical`, closed |
| `gallery--filtered--en--light--1440.png` | Same filtered URL, desktop rows          |
| `gallery--rows--vi--light--1024.png`     | `/templates`, rows and four columns      |

Use the existing Gallery font/image/thumbnail waits for closed full-page
captures; sheet captures use a fixed 900 px viewport height. Keep showcase,
landing, template detail, renderer, and print baselines unchanged. Regenerate
the web source manifest through the primary when files move or are added.
