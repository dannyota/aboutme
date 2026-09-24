# Public PDF tab

Status: proposed in
[ADR 0053](../adr/0053-public-pdf-tab-renders-the-download-in-the-browser.md).
Choices marked **Owner** need the owner's approval before code starts.

When a resume's owner allows PDF download, the public page at `/{slug}` offers a
"PDF" tab. The tab shows every page of the file that "Download PDF" returns,
before the reader downloads it. With download off there is no tab.

The browser draws the pages itself: it fetches the public PDF route that the
download link already uses and renders those exact bytes with pdf.js into
canvases. The server gains no route, no rasterizer, and no cached artifact. The
HTML resume stays the indexed content and the default view.

## Options

iOS Safari shows only the first page of a PDF in an `<iframe>`, `<embed>`, or
`<object>`, and Android Chrome shows no inline PDF at all. The public Content
Security Policy (CSP) also sets `object-src 'none'` and, through
`default-src 'none'`, `frame-src 'none'`. A plain embed is therefore ruled out.
Three options remain:

| Criterion                     | (a) Server page images                                                | (b) pdf.js in the browser                                            | (c) Paginated HTML in an iframe              |
| ----------------------------- | --------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------- |
| Fidelity to the download      | Raster of the same bytes, if raster reuses the cached PDF             | Raster of the same response: same URL, entity tag, and bytes         | Not the file; line breaks drift from the PDF |
| iPhone Safari                 | Works: plain `<img>`                                                  | Works: canvas, within Safari's canvas memory                         | Works, but shows the approximation           |
| Host CPU and memory           | New raster step per render on a burstable 2 vCPU host                 | None beyond the PDF render the download already needs                | A new public print-view render path          |
| New public artifact           | Manifest plus one image per page, each gated and revoked              | None                                                                 | A new framed route, gated and revoked        |
| Cache pressure                | N+1 entries per revision in the 128-entry, 32 MiB, 60 s cache         | None beyond the existing PDF entry                                   | One more HTML entry                          |
| CSP change                    | None (`img-src 'self'`)                                               | None, with pdf.js on the main thread                                 | `frame-src` and a framable route             |
| Reader download on first open | About 100 KB per WebP page                                            | About 415 KB of pdf.js (Brotli), cached, plus the PDF                | HTML and CSS                                 |
| Works without JavaScript      | Only with a page count in SSR, which needs a render at HTML time      | No; the download link remains                                        | Yes                                          |
| Build size                    | New server dependency (pdf.js with a native canvas in Nuxt, or MuPDF) | Frontend only; `pdfjs-dist` is already pinned for the gallery images | Frontend and a new route                     |
| Reuse                         | Template gallery could share the image format                         | The editor's "exact PDF" view can reuse the same viewer              | None                                         |

Option (b) is chosen. It is the only option that adds no public artifact, so the
publish revocation rules of
[ADR 0022](../adr/0022-public-artifact-revocation.md) hold with no new code: the
tab reads the same gated route as the download. It draws the literal bytes the
download returns, not a second rendering of them. It adds no CPU, memory, or
cache load to the single production host beyond PDF renders the download already
causes. The gallery sample page images already come from pdf.js
(`apps/web/e2e/sample-pages.spec.ts`), so the tab matches them.

Option (a) remains the fallback if real iPhones cannot hold the canvases or if
the pdf.js download proves too slow for readers. Its shape is sketched in
[Fallback shape](#fallback-shape-option-a) so the choice can be revisited
without new research.

Option (c) is rejected: the paginated HTML is the editor's approximate preview,
not the file, and framing needs a new public route and a looser CSP.

## Reader behavior

- The tab bar has two tabs: "Page" (the HTML resume) and "PDF". Vietnamese
  resumes label them "Trang web" and "PDF". Labels follow the resume language,
  like the download link. **Owner**: labels.
- The tab bar appears only when `downloadEnabled` is true in the current public
  JSON. When a refresh turns download off, the tab bar disappears, the PDF panel
  is destroyed, and the page shows the HTML resume.
- "Page" is selected on load. The URL fragment `#pdf` selects "PDF" on load and
  is written with `history.replaceState` when the reader selects the tab, so a
  link can open the PDF view. The fragment never reaches the server. **Owner**:
  the `#pdf` deep link.
- The tab bar renders only after hydration. Server-rendered HTML stays
  byte-identical to today, so the Go public HTML validator, the golden HTML,
  crawlers, and readers without JavaScript see no change. The designer places
  the bar in the existing toolbar row (credit left, download right) so it causes
  no layout shift, and it fits at 320 CSS px without horizontal scroll.
- The PDF panel shows the pages in a single column at the measure width, each
  with a thin border on the page background, and a count line such as "2 pages".
  The "Download PDF" link stays where it is.
- The tab bar and the PDF panel are hidden in print. Printing always prints the
  HTML resume article, whichever tab is selected.
- Canvases draw only; there is no pdf.js text layer, annotation layer, form
  layer, or scripting. Links in the PDF are not clickable in the tab; the "Page"
  tab has them. **Owner**: no text layer in the first release.

## Rendering

The PDF viewer is one component, `PdfPages`, with no knowledge of the public
page. The public page passes it the PDF URL; the editor's planned "exact PDF"
view can pass its owner route later.

1. On first selection, the component dynamically imports pdf.js and its worker
   module. The worker module is assigned to `globalThis.pdfjsWorker` so pdf.js
   runs its parser on the main thread and never calls `new Worker`. The public
   CSP keeps `worker-src 'none'`. **Owner**: keep the CSP unchanged
   (recommended) rather than add `worker-src 'self'`.
2. It fetches the public PDF route with `credentials: 'omit'` and
   `cache: 'no-cache'`, so the browser revalidates with the stored entity tag.
3. It checks the response before parsing: status `200`, `Content-Type`
   `application/pdf`, a strong `ETag`, a streamed body of at most 16,777,216
   bytes (the server's own cap), and the `%PDF-` signature.
4. It opens the bytes with `getDocument({ data })` and these options:
   `useWasm: false`, `useSystemFonts: false`, `isEvalSupported` absent (removed
   upstream), no `cMapUrl`, `standardFontDataUrl`, or `wasmUrl`. Resume PDFs
   embed their fonts and hold only JPEG or PNG photos, so pdf.js makes no
   request of its own.
5. It reads each page's viewport first and reserves a box of that aspect ratio,
   so pages never shift as they draw. A4 and Letter pages both work.
6. Pages draw lazily. A page draws when its box comes within one viewport height
   of the screen. A page more than two viewport heights away releases its canvas
   (width and height set to 0). At most four canvases hold pixels at once.
7. Each canvas is `min(ceil(boxWidth × devicePixelRatio), 1600)` device pixels
   wide, at most 4,194,304 pixels. A 390 CSS px wide page box at device pixel
   ratio 3 draws a 1170 by 1655 canvas, about 7.7 MB of pixels.
8. Only the first 20 pages draw. A longer document shows "Showing 20 of N pages.
   Download the PDF to see all." **Owner**: the 20-page cap.
9. Leaving the tab, a newer document, unmount, and `pagehide` abort the fetch,
   cancel draws, release canvases, and call `loadingTask.destroy()`.

### Bundle

The public hydration script is one file, `public-resume.mjs`, built with
`inlineDynamicImports: true`. A plain `import('pdfjs-dist')` would inline about
1.7 MB into every public page load. The frontend slice therefore builds pdf.js
and its worker as one separate content-hashed module under `/_nuxt/assets/`,
which the public script imports by URL only when the tab is first selected.
`script-src 'self'` already allows it. The existing public script must not grow
by more than 4 KiB compressed, and the new module stays at or under 512 KiB
Brotli.

`pdfjs-dist` moves from `devDependencies` to `dependencies` at the same pinned
version. It is Apache-2.0, compatible with AGPL-3.0; its notice ships with the
module. `make web-no-eval-check` must still pass; the pinned build contains no
`eval(` or `new Function(`.

## Accessibility

- The tab bar follows the WAI-ARIA tabs pattern: `role="tablist"`, `role="tab"`
  with `aria-selected` and `aria-controls`, arrow-key movement, and
  `role="tabpanel"` with `aria-labelledby`. Focus stays on the selected tab.
- Each canvas has `role="img"` and an `aria-label` such as "Page 1 of 2, PDF of
  Nguyễn Văn An's resume" in the resume language.
- The panel starts with a sentence stating that the "Page" tab holds the same
  text, and links to it. The HTML resume is the text alternative for every page
  image.
- Loading, errors, and the page count use one `aria-live="polite"` status line.
- Each tab shows the chrome's focus ring and has a touch target of at least 44
  by 44 CSS px.

## Search and discovery

The tab adds no URL, no link, no `<img>`, and no server-rendered markup. The
HTML resume stays the only indexed representation. The PDF route keeps its
existing `X-Robots-Tag` behavior, which follows the discovery flag. The fragment
`#pdf` is ignored by crawlers and by the canonical link.

## Privacy

No new data leaves the host. pdf.js is served from the application's own origin,
makes no request of its own, and uses no content delivery network. The PDF bytes
are those the reader could already download. Nothing depends on the hosting
provider, so the move to Vietnamese providers
([ADR 0051](../adr/0051-vietnam-hosted-production.md)) needs no change: the new
module is a hashed `/_nuxt/` asset, which is the only path the edge caches.

## Route and API

No route is added or changed. `GET /api/v1/public/resumes/{slug}/pdf` keeps its
contract in `docs/api/openapi.yaml`, including `Content-Disposition`, which
`fetch()` ignores. The OpenAPI file does not change. No schema, migration, or
stored data changes, so there are no migration or loss rules.

## Cache and revocation

Revocation stays with the existing route, and the tab adds no rule of its own:

- The PDF route passes the origin live-state gate before any cache reuse or
  `304`, holds a generation lease through the body, and returns the uniform
  public `404` after unpublish, delete, rename, or download off.
- The tab never caches bytes outside the browser's HTTP cache, which revalidates
  on every fetch (`no-cache, must-revalidate`).
- When the public page learns of a change through its existing SSE refetch:
  - download off or unpublished: the tab bar and panel are removed at once and
    their canvases released;
  - a newer revision while the PDF tab is selected and the page is visible: the
    panel refetches after a random delay of one to five seconds, so many readers
    of one resume do not start renders together;
  - a newer revision while the tab is hidden or unselected: the panel is marked
    stale and refetches on the next selection.
- A PDF fetch that returns `404` also removes the tab and asks the page's
  realtime controller for an immediate public refetch.

A reader keeps pixels already drawn before a revocation, just as a reader keeps
a downloaded file or an open HTML page. ADR 0022 already accepts that bytes
admitted before a revocation cannot be retracted.

## Limits

| Limit                             | Value                                            |
| --------------------------------- | ------------------------------------------------ |
| PDF bytes accepted by the viewer  | ≤ 16,777,216 (same as the server cap)            |
| Pages drawn                       | First 20                                         |
| Canvases holding pixels           | ≤ 4                                              |
| Canvas width / area               | ≤ 1,600 device px / ≤ 4,194,304 px               |
| Client fetch deadline             | 30 s (server deadline is 20 s from admission)    |
| Refetch delay after a new version | Random 1 to 5 s, only while selected and visible |
| pdf.js module                     | ≤ 512 KiB Brotli, loaded on first selection      |
| Public hydration script growth    | ≤ 4 KiB compressed                               |

Server limits do not change. The tab reuses the public artifact limits: 300
requests and 20 render misses per client IP per minute, one active render, eight
queued, and the 60-second, generation-keyed public cache. These values move into
[budgets](budgets.md) when ADR 0053 is accepted.

## Failure behavior

| Condition                                 | Reader sees                                                                   |
| ----------------------------------------- | ----------------------------------------------------------------------------- |
| Loading pdf.js or the PDF                 | "Preparing the PDF…" status; boxes reserved once the page count is known      |
| `404`                                     | Tab removed; the page refetches and shows the current public state            |
| `429`                                     | "Too many requests. Try again in N seconds." from `Retry-After`; Retry button |
| `503`, network error, or 30 s deadline    | "The PDF preview is not available right now." Retry button and download link  |
| Bad type, size, signature, or parse error | Same message as `503`; the error name goes to the console, never the bytes    |
| pdf.js module fails to load               | Same message; the tab stays so Retry can load it again                        |
| One page fails to draw                    | That page's box shows "This page could not be shown."; other pages still draw |
| Canvas allocation fails on a small phone  | Pages draw at half the width cap once, then show the per-page message         |

No failure hides the "Download PDF" link, and no failure retries on its own.

## Older clients and release

A page loaded before the release has no tab and keeps working. The release
touches only `apps/web/`, so there is no API, schema, or fence change, and no
deploy order constraint. Rolling back removes the tab and nothing else.

The work ships as one frontend release, with qa proofs and a designer spec in
the same release. An optional backend release may later coalesce concurrent
render misses for one cache key, since the tab raises PDF reads by readers who
do not download. **Owner**: defer that release until production shows duplicate
misses (recommended). The plan is
[public-pdf-tab.md](../plans/public-pdf-tab.md).

## Test plan

Unit tests (Vitest, `apps/web/test/`):

- The tab bar shows only when `downloadEnabled` is true, disappears when a
  refresh sets it false, and never appears in server-rendered HTML.
- `#pdf` selects the tab on load only when download is enabled.
- The loader rejects a wrong status, type, missing entity tag, oversize body
  (streamed cutoff), and a missing `%PDF-` signature, and aborts on tab change,
  unmount, `pagehide`, and a newer revision.
- `404` removes the tab and requests a refetch; `429` shows the `Retry-After`
  value; `503` shows Retry and never retries on its own.
- Canvas size math at device pixel ratios 1, 2, and 3, for A4 and Letter; the
  four-canvas and 20-page caps.
- A new revision while selected and visible refetches within the jittered window
  (fake timers); while hidden it only marks stale.
- ARIA roles, labels, and keyboard movement; Vietnamese and English strings.
- A build check fails if `public-resume.mjs` contains pdf.js code or grows past
  its limit, or if the pdf.js module exceeds its limit.

End-to-end tests (Playwright, WebKit and Chromium, phone widths):

- Devices: WebKit with the iPhone SE (375 CSS px) and iPhone 15 (393 CSS px)
  descriptors, and Chromium with a 360 CSS px Android descriptor, plus one
  desktop width.
- A fictional two-page resume shows two labelled canvases in reading order.
- The PDF bytes behind the tab and the bytes "Download PDF" returns have the
  same entity tag and SHA-256 digest.
- The page makes no request beyond same-origin `/_nuxt/` assets and the PDF
  route, and raises no `securitypolicyviolation` event.
- A pixel baseline of page 1 in each browser at each phone width, generated in
  hosted CI.
- Print emulation with the PDF tab selected prints the HTML article.

Revocation proofs (extend `make dev-https-public-check`):

1. Download off while the PDF tab is open: the tab bar and every canvas leave
   the DOM, and a direct PDF read returns `404`.
2. Unpublish while the PDF tab is open: the page shows the public `404` state
   with no canvas.
3. Republish after an edit: the tab draws the new revision; a conditional read
   with the old entity tag gets `200` with new bytes, never `304`.
4. Rename: the old slug's PDF returns `404`; the new slug's tab works.
5. A fresh load of a page with download off has no tab, even with `#pdf`.

Real-device check: after deploy, the owner opens a two-page public resume on an
iPhone in Safari and confirms every page shows. Playwright's WebKit is not iOS
Safari, so this check is part of acceptance.

## Fallback shape (option a)

Recorded only for a later decision. If the browser approach fails on real
devices, the server would add:

- `GET /api/v1/public/resumes/{slug}/pdf/pages`: JSON `{ pages, width, height }`
  for the current generation;
- `GET /api/v1/public/resumes/{slug}/pdf/pages/{n}.webp`: page `n`, 1-based,
  1,240 px wide (150 DPI on A4);
- both gated on `live` and `download_enabled`, cached with the PDF under one
  generation key, rasterized from the exact cached PDF bytes, and sent with
  `X-Robots-Tag: noindex`;
- OpenAPI paths, a new public representation, the ADR 0022 test list for each,
  and a raster budget measured on the production host.
