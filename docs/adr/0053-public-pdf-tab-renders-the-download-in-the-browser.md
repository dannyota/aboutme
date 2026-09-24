# 0053: The public PDF tab renders the download in the browser

Status: Proposed (2026-09-25). Waits for the owner's approval of the choices
marked **Owner** in the [public PDF tab design](../design/public-pdf-tab.md).

## Context

When a resume's owner allows PDF download, readers of the public page should see
every page of the file that "Download PDF" returns before they download it. With
download off there is no preview.

iOS Safari shows only the first page of a PDF in an `<iframe>`, `<embed>`, or
`<object>`, and the public Content Security Policy (CSP) blocks both objects and
frames. Three options were compared: server-rendered page images per published
revision, pdf.js drawing the PDF in the browser, and a paginated HTML print view
in an iframe.

Production is one burstable 2 vCPU host with a 512 MiB cap shared by Go and
Chromium, one active render, and a 60-second generation-keyed public cache of
128 entries. Every public artifact must pass the live-state gate and revocation
drain of [ADR 0022](0022-public-artifact-revocation.md).

## Decision

1. The public page shows a "PDF" tab only while `downloadEnabled` is true. The
   tab bar renders after hydration, so server-rendered HTML does not change and
   the HTML resume stays the indexed content and the default view.
2. The tab fetches the existing `GET /api/v1/public/resumes/{slug}/pdf` route
   and draws those bytes with pdf.js into canvases. No route, server rasterizer,
   cache entry, or OpenAPI change is added.
3. pdf.js loads as a separate content-hashed module on first selection and runs
   on the main thread, so the public CSP, including `worker-src 'none'`, stays
   unchanged. It makes no request of its own and draws canvases only: no text,
   annotation, or form layer and no scripting.
4. Drawing is lazy and bounded: at most four canvases hold pixels, each at most
   1,600 device pixels wide and 4,194,304 pixels, and only the first 20 pages
   draw.
5. The HTML resume is the text alternative; each canvas carries a page label.

## Consequences

- Revocation needs no new code: the tab reads the same gated route as the
  download, and a `404` removes the tab. Pixels already drawn stay with the
  reader, as ADR 0022 accepts for any admitted bytes.
- The tab shows the literal bytes of the download, with the same entity tag. The
  gallery sample page images also come from pdf.js, so the two views match.
- The host gains no raster CPU, memory, or cache pressure. Readers who view but
  do not download still cause PDF renders, bounded by the existing public
  artifact limits and queue.
- Readers download about 415 KB of pdf.js (Brotli) on first use, and parsing
  runs on the main thread. Resume PDFs are small, so the pause is short, but
  low-end phones may stutter.
- Readers without JavaScript get no tab; the download link still works.
- iOS Safari's canvas memory limit is the main device risk. Playwright's WebKit
  is not iOS Safari, so a real-iPhone check is part of acceptance.
- If the browser approach fails on real devices, server page images per revision
  are the fallback; the design records their route shape. Adopting them needs a
  new ADR.
