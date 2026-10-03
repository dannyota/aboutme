# Community showcase redesign

Status: approved 2026-10-03 (manager, by owner delegation). Ships as v0.6.37, the last 0.6.x release. The manager approved every recommendation in Open questions as written. Traceability: no new AC rows and no AC state moves; only the AC-SHOW-002 statement changes so it no longer forbids the slug footer.

Code, comments, tests, and living docs cite `docs/design/ui/showcase.md` (the page layout, new in step 1), `docs/design/showcase.md`, ADR 0029, or `AC-SHOW-*` IDs, never this plan or its step or question numbers.

## Goal

Rebuild `/showcase` to the owner-approved Claude Design mockups of 2026-10-03 on phone and desktop: results first, filters in a bottom sheet on phone and a left rail on desktop, a count line from the listing `total`, a tile with a footer row and an icon Report control, and an invite card at the end of the grid. Filter behavior, the API, privacy rules, and state copy stay as they are.

Visual source (local mockups, read in full before any work): `Main.dc.html` (phone, dark, Vietnamese, `activeFilters` prop for the filtered state), `PhoneFilters.dc.html` (sheet open), `Desktop.dc.html` (desktop, light, English) under `/tmp/claude-1000/-home-danny-src-aboutme/84083e7c-a36b-45e2-8e63-46e3b8dddf18/scratchpad/design-community/project/`. Bracketed text is placeholder data; tiles keep the stored card image; colors are the existing semantic tokens, never hexes.

## Non-goals

- No API, schema, migration, or server change. The page reads `total`, which the listing already returns.
- No change to the filter rules: URL query, `page` reset on change, unknown values count as All and are not sent, the same API parameters.
- No change to order, page size, pager behavior, noindex rules, the Report mailto, or the privacy rules (no cookie, no storage, no counting script).
- No change to the Library (`/templates`), the homepage, the publish dialog, or the site header.
- No new copy for the empty, no-match, and failed states (they keep today's text).
- No page thumbnail on the tile (S2 keeps that as a later decision).

## Current state

- `apps/web/app/pages/showcase.vue` (257 lines): boxed header band with the order note, `ShowcaseFilters` (horizontal role chip row, language chips, inline template select), the grid, states, pager.
- `components/showcase/ShowcaseFilters.vue` (182), `ShowcaseTile.vue` (207), `ShowcasePager.vue` (118); `i18n/showcase.ts` (102); `lib/showcaseQuery.ts`; `composables/useShowcase.ts`.
- Tests: `test/showcase/showcase-page.test.ts` (634 lines), `showcase-i18n.test.ts`, `showcase-query.test.ts`, `use-showcase.test.ts`, `legal-showcase.test.ts`; e2e `e2e/showcase.spec.ts`, `e2e/showcase-baselines.spec.ts` (16 baselines: filled and empty, vi and en, light and dark, 390 and 1440).
- `components/ui/sheet` exists (reka-ui Dialog). Its `SheetContent` always appends a 16 px close icon whose screen reader text is the English word "Close"; the editor uses it.
- The site header (`AppShell.vue`) is not sticky, so a sticky bar can stick at `top: 0`.
- The reduced-motion rule in `assets/css/base.css` covers every element under `html[data-ui="app"]`; the showcase page sets it.

## Design decisions

These follow the mockups and the brief; the designer writes them into the spec in step 1.

1. Header: no band. h1 at 28 px below 641 px and 40 px from 1024 px (the designer sets 641 to 1023 px), bold, -0.02em, then the existing lead. The order note moves into the count line.
2. Count line: one element, placed by CSS grid at the right of the header row from 1024 px and in the sticky bar below it. It reads "{N} CV · sớm nhất trước" / "{N} resumes · earliest added first", English singular "1 resume". N is always the listing `total`. `{N} CV` / `{N} resumes` in `--foreground` weight 600, the rest `--muted-foreground`, 14 px. The old order note string goes away.
3. Phone and tablet (below 1024 px, see Q1): no chip row. A sticky bar under the header holds the count line and a 44 px "Bộ lọc" / "Filters" outline button. With any filter on, the button shows a `--primary` badge with the number of active filters and its accessible name becomes "Bộ lọc, {n} đang bật" / "Filters, {n} active".
4. The button opens a bottom sheet built on `components/ui/sheet` (reka-ui Dialog: focus trap, Escape closes, focus returns to the trigger, `aria-modal`, labelled by its "Bộ lọc" / "Filters" title). The sheet holds Role (all 11 options as wrapped 40 px chips, a single `ToggleGroup`), Resume language (3 options, a 3-column segmented `ToggleGroup`, 44 px), Template (the existing `SelectField`, 44 px), and two 48 px actions: "Xóa bộ lọc" / "Clear filters" and "Xem {N} CV" / "Show {N} resumes" ("Show 1 resume"). The sheet title is "Bộ lọc" / "Filters"; the group headings are "Vị trí" / "Role", "Ngôn ngữ của CV" / "Resume language", and "Mẫu" / "Template"; the role group keeps its accessible name "Lọc theo vị trí" / "Filter by role".
5. Active-filter chips: one element, placed by CSS grid. With any filter on, a row of removable chips (role, language, template, in that order) plus "Xóa hết" / "Clear all" sits under the count line on phone and above the grid on desktop. The row is a `group` named "Bộ lọc đang bật" / "Active filters". Each chip is one button, label visible, accessible name "{label}, gỡ bộ lọc" / "{label}, remove filter", so the name starts with the visible text (see Q6). Desktop drops the mockup's "Showing" prefix: the group name already says what the row is.
6. Desktop (from 1024 px): a left rail (`aside` named "Bộ lọc" / "Filters", 240 to 260 px, `--card` with `--border`) holds the Role list (a vertical single `ToggleGroup`, 36 px rows, pressed row marked with a `--primary` dot and weight 600), the language control (see Q8), and the Template select. The results sit to its right: 3 columns when the grid is at least 860 px wide (a container query, about a 1216 px viewport), else 2. The count line sits at the right end of the header row, as in the mockup.
7. Both patterns render on the server and switch by CSS media query only. No JavaScript breakpoint decides what renders, so there is no hydration mismatch and no layout shift. Below 1024 px the rail is `display: none`; from 1024 px the bar's Filters button is `display: none`. The sheet content mounts only while open. If the viewport crosses 1024 px while the sheet is open, the sheet closes (a `@vueuse/core` `useMediaQuery` watcher that only closes, never renders) and focus moves to the rail's pressed Role row, since the Filters button is now hidden.
8. Tile: keeps the stored card image and the tile link. The meta block shows the role chip (when present) and the language on one row, then the template name. A footer row outside the link shows "aboutme.vn/{slug}" in 13 px `--muted-foreground` (see Q2) and the Report control: a 40 px icon link with the lucide `Flag` icon (`@lucide/vue`, already a dependency), accessible name unchanged ("Báo cáo CV {slug} qua email" / "Report resume {slug} by email"), the same mailto, and a visible tooltip "Báo cáo" / "Report" on hover and focus (the existing `components/ui/tooltip`). Report stays its own focus target after the tile link. The tooltip adds the description "Báo cáo" / "Report", so a screen reader hears the word twice; the plan accepts that duplicate as harmless.
9. Invite card: the last cell of the grid in the `list` state (see Q5), a 1 px dashed border in the token the designer names, title "Muốn CV của bạn ở đây?" / "Want your resume here?", body "Khi xuất bản CV, bật Hiện trong trang Cộng đồng. Bạn tắt lúc nào cũng được." / "When you publish, turn on Show in the community showcase. You can turn it off any time.", button "Tạo CV miễn phí" / "Create a free resume". The switch name in the body matches the publish dialog label in `i18n/publish.ts` exactly ("Hiện trong trang Cộng đồng" / "Show in the community showcase"), so the body copy needs no change. The button reuses the empty action's target rule in `showcase.vue`: `/register` signed out with password registration on, else `/login`, and `/app/resumes` signed in, where the visitor creates a resume from the list. The label stays "Tạo CV miễn phí" / "Create a free resume" in every case.
10. Vietnamese spelling: the repository writes "Xóa" (for example "Xóa CV"), so the new strings use "Xóa bộ lọc" and "Xóa hết", not the mockup's "Xoá".
11. The sheet gets its own localized 44 px close button in the header row. The close button reads "Đóng" / "Close". `SheetContent` gains an optional `showClose` prop (default `true`, added to its `reactiveOmit` list so it never reaches the DOM), so the editor sheet is unchanged and the showcase sheet passes `false`. The editor's English "Close" text and 16 px target go to the backlog, not this release.
12. The sheet is capped at `85dvh` (fallback `85vh`), its fields scroll inside it, and the two actions stay pinned at its bottom with `env(safe-area-inset-bottom)` padding. Below 641 px it spans the width; from 641 px it is centered at most 40 rem wide.
13. Focus after removing a chip: to the next chip, else the previous one, else "Clear all" is gone too, so focus goes to the Filters button below 1024 px or to the rail's pressed Role row from 1024 px. After "Clear all": the same final target. After "Clear filters" in the sheet: focus stays on that button.
14. Screen readers: the count line is the page's polite live region (`role="status"`, `aria-atomic="true"`), so a rail or chip change announces "{N} CV · sớm nhất trước". The open sheet is modal and reka hides everything outside it with `aria-hidden`, so the sheet holds its own visually hidden `role="status"` that mirrors the count text and mounts only while the sheet is open. The no-match line drops its own `role="status"` to avoid a double announcement; the count line then reads "0 CV · sớm nhất trước". The pager's focus move after Previous and Next stays.
15. The count line keeps one line height in every state: a 6 rem `Skeleton` on the first load only; during later loads it keeps the last text, so the pager's focus move is not interrupted; "{N} …" in `list`, `empty`, and `no-match` with N the `total`; and when the load failed it keeps the last text, or shows only "Sớm nhất trước" / "Earliest added first" if no load has succeeded, so it never repeats the error banner. The bar and the Filters button stay visible in every state, as the filter rows do today.
16. Below 1024 px the page sets `scroll-padding-top` on the root scroller to the sticky bar height while it is mounted, so the pager's scroll and keyboard focus on any element (tile link, Report, invite, pager, Retry, empty action) never land under the bar (WCAG 2.4.11).
17. Sticky bar background: the mockup uses the page background at 94 percent over the Aurora body gradient. The designer picks either that (`color-mix` of `--background`) or an opaque `--background` band, and names it in the spec; no `backdrop-filter` either way.

## Open questions

Answered 2026-10-03: the manager approved each recommendation as written.

- Q1. Pattern from 641 to 1023 px. Recommendation: the phone pattern (sticky bar and sheet) with a 2-column grid. Reason: a 260 px rail plus a 32 px gap leaves about 410 px for the grid at 768 px, so tiles would be 1 column or 2 columns of about 195 px, too small to read the card; the phone pattern keeps 2 columns of about 340 px.
- Q2. The tile footer prints "aboutme.vn/{slug}" as text. `docs/design/showcase.md` (S2) and `AC-SHOW-002` say the tile's own text never shows the slug. Recommendation: approve it as S14, since the card image and the Report label already expose the same slug; the designer amends S2 text and `AC-SHOW-002`.
- Q3. The count line shows the listing total. ADR 0029 says a listing holds no count, and S2 rules out a view count on a tile. Recommendation: approve it as part of S14 and add one design sentence that the page total is not a per-listing field and not a view count; no ADR change.
- Q4. What "Xem {N} CV" applies. Recommendation: every sheet change applies at once (URL, `page` reset, reload behind the sheet), and the button shows the live `total` and closes the sheet; Escape and the close button keep the applied filters. While a new total loads, the button reads "Xem kết quả" / "Show results". The other reading, a draft that applies only on the button, needs a count request per change against a rate-limited route, and Escape would silently drop changes.
- Q5. Where the invite card shows. Recommendation: as the last grid cell on every page in the `list` state; never in `loading`, `empty` (it already has its own create action), `no-match`, or `failed`.
- Q6. Chip target size. The mockup puts a 28 px (phone) or 24 px (desktop) close icon in each chip. Recommendation: make the whole 36 px chip the button, with "×" as an `aria-hidden` icon; the visible label stays inside the accessible name.
- Q7. Phone sticky bar height. With three filters the chip row wraps to two lines at 390 px, making the bar about 140 px tall. Recommendation: only the count and Filters row sticks (60 px); the chip row sits under it in normal flow.
- Q8. Desktop language control labels. The mockup shows "All / VI / EN" in a narrow rail. Short labels with full accessible names fail WCAG 2.5.3 (a speech user saying "click VI" gets no match). Recommendation: show the full labels "Mọi ngôn ngữ", "Tiếng Việt", "Tiếng Anh" / "All languages", "Vietnamese", "English" as a 3-row list styled like the Role list, since three full labels do not fit one 220 px segmented row.
- Q9. Library page. Recommendation: no change now; after this release the designer judges whether `/templates` should get the same sheet and rail, as a 0.7.x proposal.

## File sets

| Role | Files |
|-|-|
| designer | new `docs/design/ui/showcase.md` (the page layout: "Page frame" through "States" of the "Community showcase" section, plus its parts of "Accessibility and fit", moved out of `docs/design/ui/landing-and-library.md`, which is at 440 lines; "Navigation" and "Publish dialog block" stay there with a one-paragraph pointer), `docs/design/ui/landing-and-library.md`, `docs/design/ui/shell-and-editor.md` (its `#community-showcase` link), `docs/design/README.md` (index row), `docs/design/showcase.md` (Copy table for the new strings, S2 and S14 rows, trimmed elsewhere to stay at or under 450 lines), `docs/plans/traceability/ac-showcase.md` (the AC-SHOW-002 statement only). `docs/design/showcase.md` is an architect path and `docs/plans/traceability/` a manager path; the manager's brief to this lead assigns both to the designer for this change. The Q2 amendment covers S2, "What a listing shows" (the sentence "The whole tile except Report links to `/{slug}`" now excludes the footer row too), the UI spec's Report row, and AC-SHOW-002 |
| frontend | `apps/web/app/pages/showcase.vue`; `apps/web/app/components/showcase/ShowcaseFilters.vue` (shared fields with a `layout: 'sheet' \| 'rail'` prop); new `ShowcaseFilterBar.vue`, `ShowcaseFilterSheet.vue`, `ShowcaseActiveFilters.vue`, `ShowcaseInvite.vue` in the same directory; `ShowcaseTile.vue` (including its header comment that says the tile never shows the slug); `ShowcasePager.vue` (its design-citation comment only); `apps/web/app/components/ui/sheet/SheetContent.vue` (`showClose` prop only); `apps/web/app/i18n/showcase.ts`; `apps/web/app/lib/showcaseQuery.ts` (active-filter count and list helpers); tests `apps/web/test/showcase/showcase-page.test.ts` (including the "showcase tiles" test that asserts no slug text), `showcase-i18n.test.ts`, `showcase-query.test.ts`, new `showcase-filters.test.ts`; `scripts/web-e2e-source.manifest` regenerated by `make web-source-manifest-update` (shared generated file: report every changed line) |
| qa | `apps/web/e2e/showcase.spec.ts`, `apps/web/e2e/showcase-baselines.spec.ts`, the showcase PNGs in `apps/web/e2e/baselines/`, the root `Makefile` `web-e2e-update` expected-file list (shared file: report every changed line); `deploy/dev-https-browser/publish.spec.ts` only if it asserts layout (read shows it reads the listing request, so no change expected) |
| reviewer | none (read-only) |
| frontend-lead | this plan; deletes it at release close |

Every new web file is under `apps/web/app/components/showcase/`, and `deploy/web.Dockerfile` copies `apps/web/app/` whole, so the Dockerfile needs no change.

## DOM hooks

Frontend and qa work from these names in parallel; neither changes them without the lead.

- Kept: `data-testid="showcase-page"`, `showcase-results`, `showcase-roles`, `showcase-languages`, `showcase-pager`; `data-role`, `data-lang`, `select[name="template"]`; `data-showcase-slug`, `data-showcase-tile`, `data-showcase-role`; `data-action="showcase-report"`, `showcase-retry`, `showcase-empty-action`; every `data-state`.
- Moved: `data-testid="showcase-header"` stays on the header element though the band goes; `showcase-filters` moves to the shared fields component, so it appears in the rail and in the open sheet.
- New: `data-testid="showcase-count"`, `showcase-filter-bar`, `showcase-filter-rail`, `showcase-filter-sheet`, `showcase-filters-badge`, `showcase-active-filters`, `showcase-invite`; `data-action="showcase-filters-open"`, `showcase-filters-clear`, `showcase-filters-apply`, `showcase-filter-remove` (with `data-filter="role|lang|template"`), `showcase-filters-clear-all`, `showcase-invite-create`.
- The rail and the sheet both hold `showcase-roles` and `showcase-languages`; specs scope them (`getByTestId('showcase-filter-sheet').getByTestId('showcase-roles')`). Element ids come from `useId()`, so the two copies never share an id.

## Steps

The lead's brief allows commits and pushes on `feat/showcase-redesign` only; the lead never pushes `main`, tags, or deploys. At most three workers at once. Every worker verifies its worktree base first (`git -C /home/danny/src/aboutme-redesign log --oneline -1` matches the SHA in its brief). Workers do not run Git; the lead commits each worker's exact file set on `feat/showcase-redesign`.

1. Designer (Opus) writes the four design files to describe decisions 1 to 17 and the owner's answers exactly. Ends when: the lead reads the diff against the mockups and this plan, `make pre-push` exits 0 (Prettier, markdownlint, lengths), every design file is at or under 450 lines, and the lead commits.
2. Frontend (Sonnet) builds the page from the committed spec and writes unit tests first: count line and pluralization, badge count and accessible name, sheet open, close by Escape and button, focus return, apply and clear, chip removal and focus, Report accessible name and tooltip, invite target signed in, signed out, and with registration closed, the count and sheet live-region text, the `showClose` prop. Ends when: the lead reads the diff line by line, `make pre-push` exits 0, and the lead commits. In parallel, from the DOM hooks above:
3. QA (Sonnet) updates `e2e/showcase.spec.ts` (sheet keyboard flow, Escape, focus return, chip removal, rail filters at 1440, the 768 px pattern, Tab and Shift+Tab focus never under the bar, the sheet closing at a resize past 1024 px, query handling unchanged) and `e2e/showcase-baselines.spec.ts` (new states), and the Makefile list. Ends when: the lead reads the diff, `make pre-push` exits 0, and the lead commits after step 2's commit.
4. The lead merges `origin/main` into the branch first if it moved, then pushes the branch, dispatches the `Baselines` workflow on it, and qa copies the candidate showcase images, committing only images this change moves plus the new ones. Ends when: the lead has checked each committed image against the mockups and the SHA256SUMS file of that run.
5. If `origin/main` moved after step 4 and the merge touches `apps/web/`, `packages/schema/`, or any baseline image, the lead reruns step 4 first. Then it pushes and dispatches `gh workflow run ci.yml --ref feat/showcase-redesign -f base_sha=$(git merge-base origin/main HEAD)` once. Ends when: the run is green at the exact head.
6. Designer finish review on the step 4 images (390 and 1440 px, both languages, light and dark, plus sheet, filtered, and 768 px) against the mockups. Ends when: a verdict with findings; fixes loop through steps 2 to 5.
7. Reviewer (Opus) reviews the whole branch diff: accessibility, focus management, no layout shift, query handling, privacy rules unchanged, writing rules. Ends when: verdict; findings fixed by their author and re-checked by a Sonnet worker; green CI at the new exact head.
8. The lead reports to the manager: head SHA, green run ID, file sets by role, verdicts, baselines and their run.

No local stack or local browser run is needed: Playwright in CI covers focus and keyboard behavior, and the Baselines workflow produces the review images. If a failure only a local session can show appears, the lead asks the manager for a bounded exception first.

## Acceptance criteria

Testable statements for this plan only; they add no traceability rows. The designer rewrites the AC-SHOW-002 statement for Q2. Tests cite AC-SHOW-002 and the design sections.

1. The page has no header band; the h1 measures 28 px at 390 px and 40 px at 1440 px.
2. The count line reads "{total} CV · sớm nhất trước" in Vietnamese and "{total} resumes · earliest added first" in English, with "1 resume" for a total of 1, and holds one line height while loading and after a failure.
3. Below 1024 px no role chip row renders outside the sheet; the Filters button is at least 44 by 44 px.
4. With k active filters (k from 1 to 3), the button shows the badge k and its accessible name is "Bộ lọc, k đang bật" / "Filters, k active"; with none, no badge and the name is "Bộ lọc" / "Filters".
5. The button opens a modal dialog named "Bộ lọc" / "Filters" holding 11 role options, 3 language options, the template select (All, 20 presets by name, Custom design), "Xóa bộ lọc", and "Xem {N} CV"; Tab stays inside it; Escape closes it; focus returns to the button.
6. A sheet change writes the URL query as today (drops `page`, unknown values count as All and are not sent); "Xem {N} CV" closes the sheet; "Xóa bộ lọc" clears every filter and keeps the sheet open.
7. With filters on, the chip row lists one chip per active filter with the name "{label}, gỡ bộ lọc" / "{label}, remove filter"; activating one removes only that filter and moves focus as decision 13 says; "Xóa hết" / "Clear all" removes all.
8. From 1024 px the rail renders the same three filters and no Filters button shows; the grid has 3 columns at 1440 px and 2 at 1024 px.
9. Each tile shows the role chip (when present) and the language on one row, the template name, and a footer with "aboutme.vn/{slug}" and a 40 px Report link whose accessible name is unchanged, whose mailto is unchanged, which shows "Báo cáo" / "Report" on hover and focus, and which is a separate tab stop after the tile link.
10. In the `list` state the grid ends with the invite card; its button links to `/register` signed out with password registration on, else `/login`, and to `/app/resumes` signed in; no other state shows it.
11. A filter change announces the new count line once: from the rail or a chip through the count line, and inside the open sheet through the sheet's own status region. A pager move or reload does not re-announce a skeleton.
12. The header, the sticky bar, the chip row, and the rail do not move after hydration (the server HTML holds the bar, the badge for a filtered URL, and the chip row); a Playwright layout-shift observer reads 0 for those elements.
13. Empty, no-match, failed, and loading keep today's copy; the privacy, noindex, and request tests pass unchanged.
14. The sheet's slide animation does not run under reduced motion.
15. The desktop language options show their full names, and every control's accessible name starts with its visible text (WCAG 2.5.3).
16. When the viewport crosses 1024 px with the sheet open, the sheet closes and focus lands on the rail's pressed Role row.
17. Below 1024 px, Tab and Shift+Tab onto any tile link, Report link, or pager link leave it fully below the sticky bar.
18. At 390 px no text overflows its box in Vietnamese with three filters on (checked in the filtered baseline).

## Baselines

The 16 existing showcase baselines move. New ones (names follow `showcase--{state}--{locale}--{theme}--{width}.png`): `sheet--vi--dark--390`, `sheet--en--light--390`, `filtered--vi--dark--390`, `filtered--en--light--1440`, `filled--vi--light--768`. Total 21. The Makefile expected list gains the five names. The lead commits only images whose bytes changed plus the new five, all from one Baselines run on the exact commit that CI then tests.

## Risks

| Risk | Handling |
|-|-|
| Baseline churn: 16 moved images plus 5 new hide a real regression | One Baselines run on the exact candidate; the lead and the designer inspect every changed image against the mockups; no other spec's baselines may change (the run's SHA256SUMS match the committed non-showcase images) |
| Vietnamese text overflow at 390 px (chips, "Xem 12 CV", count line, footer slug) | Chips wrap; the footer slug truncates with an ellipsis and keeps its full text in the link name; the filtered vi baseline at 390 checks it; long template names in chips truncate at 14 rem |
| Sticky bar covers content or focus | Only the 60 px row sticks (Q7); `scroll-padding-top` on the root (decision 16); AC 17 in e2e |
| iOS Safari sticky and sheet quirks | No `overflow` on any ancestor of the bar (checked in the spec); no `backdrop-filter` on the bar (decision 17); sheet height in `dvh` with a `vh` fallback; safe-area padding; reka's scroll lock is the same one the editor sheet already uses on phones. Not verified on a real iPhone in this release; the owner checks it in production |
| Touch targets | Filters 44 px, sheet chips 40 px, segments and select 44 px, actions 48 px, Report 40 px, active chips 36 px (Q6); rail rows 36 px on desktop |
| Reduced motion | The global `data-ui="app"` rule already stops the sheet animation and tile lift; AC 14 tests it |
| Screen readers announce too much or nothing | One live region outside the sheet plus one inside it (decision 14); unit test for its text; e2e checks the no-match line lost `role="status"` |
| Duplicate controls in the DOM (rail hidden by CSS plus the sheet) | `display: none` removes the rail from the accessibility tree; `useId()` keeps ids unique; specs scope their locators |
| Hydration mismatch or layout shift from breakpoints | CSS-only switch (decision 7); AC 12 |
| Red CI runs | The first CI dispatch waits until specs and baselines are committed, so no known-red run happens; any red run gets a named cause in the report |
| `docs/design/showcase.md` is at 450 lines and `docs/design/ui/landing-and-library.md` at 440 | The designer trims repeated text in `showcase.md` and moves the page layout to the new `docs/design/ui/showcase.md` |
| Baselines from an older base | `origin/main` merges before the Baselines run; a later merge that touches web code or baselines reruns it (steps 4 and 5) |

## Release steps

1. The manager merges `feat/showcase-redesign` into `main` locally once step 8 is green (if `main` moved and the merge touches web code or baselines, the lead reruns steps 4 and 5 first), as one feature commit set, and pushes `main` alone.
2. Wait for the green `main` push run on that exact commit; tag `v0.6.37` on that SHA; wait for the release-images workflow.
3. `tofu plan` from the release worktree (no unexpected change), then `AWS_PROFILE=aboutme deploy/aws/scripts/deploy.sh v0.6.37`.
4. QA checks production at 390 and 1440 px in both languages and both themes: sheet open, filter, chip removal, Report mailto, invite link; the owner checks on his iPhone.
5. Delete the merged branch locally and on the remote, and remove the `aboutme-redesign` worktree.

## Done, and closing 0.6.x

v0.6.37 is done when it runs in production, the production check passes, and the designer and reviewer verdicts have no open finding above minor. Closing 0.6.x then means:

- This plan is deleted in the closing docs commit; any open item (editor sheet close label and size, Q9 Library follow-up, an iPhone finding) moves to `docs/plans/backlog.md`.
- No other 0.6.x tag follows. Every other 0.6.x branch is already merged.
