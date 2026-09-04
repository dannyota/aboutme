# PV adversarial coverage

Each case belongs to its named task author. The phase reviewer checks the
integrated result; there is no separate adversarial-test worker.

| Risk                                          | Required proof                                                                                                                                           | Owner              |
| --------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------ |
| Rebase drops a `main` fix                     | Every `main`-only commit's test file still exists and passes after T01; the publish proof passes on the rebased tree                                     | T01                |
| Chrome CSS leaks into the renderer            | Renderer unit snapshots and `make web-e2e` baselines byte-identical; the base stylesheet test refuses an unguarded rule                                  | T01, T02           |
| Dark theme leaves the sheet dark              | A test renders the preview under `data-theme="dark"` and asserts the document root keeps the document's own background                                   | T02, T07           |
| Seal red used for anything but public state   | A stylesheet test greps the built CSS: `--seal` consumers are exactly `AppSeal`, `StateMark`, and the Publish variant                                    | T02                |
| Landing fetches data or trips CSP             | SSR test asserts no `$fetch`/`useFetch` in `index.vue`'s render, the response carries the base CSP, and the fixture validates against the current schema | T03                |
| Landing fixture drifts from the schema        | `landing-sample.test.ts` validates the compiled-in document with the generated validator on every run                                                    | T03                |
| Hostile text in titles, slugs, client names   | List, editor title, publish success, consent page render `<img onerror>` and `<script>` payloads as text; DOM contains no element from them              | T04, T05, T07, T09 |
| Publish state shown from unaccepted input     | The list and the title mark read `live` and `slug` only from the canonical `ResumeSummary`/accepted metadata, never from dialog state                    | T05, T09           |
| Overflow menu steals row activation           | Opening Rename or Delete from the menu does not navigate; keyboard: Enter on the sheet opens, Escape closes the menu and returns focus                   | T05                |
| User-agent parsing throws on garbage          | `describeUserAgent` returns "Unknown browser" for empty, non-ASCII, and 4 KB strings; never throws                                                       | T06                |
| Relative time depends on the wall clock       | `formatRelativeTime(iso, now)` takes `now`; tests inject it; invalid ISO returns the raw string                                                          | T05, T06           |
| Theme choice lost across navigation           | Cookie-backed theme is read at SSR for every `/app/**` page; test covers settings and list                                                               | T06, T07           |
| Phone layout clips or overlaps                | Shell test at 390 px asserts the inspector width equals the viewport and the preview sheet's scaled width is below the viewport                          | T07                |
| Bottom switch hides content behind it         | Edit view gets bottom padding equal to the switch height plus safe-area inset; asserted in the shell test                                                | T07                |
| Label map misses a customization field        | `field-labels.test.ts` iterates every scalar field in `fields.ts` and fails on a missing human label or enum display name                                | T08                |
| Stamp motion ignores reduced motion           | Dialog test with `matchMedia('(prefers-reduced-motion: reduce)')` mocked true asserts no transition class and immediate mark presence                    | T09                |
| Copy link writes an unaccepted URL            | "Copy link" copies only the canonical accepted link; clipboard failure shows fixed copy and never throws                                                 | T09                |
| Agent gains publish authority through UI work | No MCP change; `AC-MCP-007` evidence rerun in T10                                                                                                        | T10                |
| Evidence disclosure                           | Screenshots and proofs contain the seeded fixture only; no cookie, token, password, or personal data                                                     | T10                |
| Scope creep                                   | No renderer change, API change, new route, analytics, thumbnails in the list, or new dependency beyond PU's pins                                         | T01–T10            |
