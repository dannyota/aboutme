# Public PDF tab

Status: waits for the owner to approve [ADR 0053](../adr/0053-public-pdf-tab-renders-the-download-in-the-browser.md) and the **Owner** choices in the [design](../design/public-pdf-tab.md). One feature release, numbered when it ships; the optional backend release is separate.

Current code: `apps/web/app/components/public/PublicResumeApp.vue` shows the "Download PDF" link when `downloadEnabled` is true; `apps/web/app/public/public-resume.client.ts` hydrates and refetches; `apps/web/nuxt.config.ts` builds `public-resume.mjs` with `inlineDynamicImports: true`; `apps/server/internal/publicapi/artifact.go` serves the gated PDF. `pdfjs-dist` 6.2.108 is a web devDependency used by `apps/web/e2e/sample-pages.spec.ts`.

## Slices

|#|Owner|Outcome|Acceptance|
|-|-|-|-|
|1|architect|Design accepted|Owner approves ADR 0053 and each **Owner** choice; ADR status set to Accepted; the tab rules fold into `docs/design/web.md` (public page list), `docs/design/security.md` (public CSP unchanged, pdf.js limits), and `docs/design/budgets.md` (limits table); `decisions.md` row set to Accepted.|
|2|designer|Tab bar spec|Placement in the toolbar row at 320, 375, 393, 768, and 1280 CSS px; tab, panel, page border, status line, and error states in Aurora chrome tokens; no layout shift on hydration; focus ring and 44 by 44 CSS px targets. Spec in `DESIGN.md` or the brief's named file.|
|3|frontend|PDF tab shipped|`PdfPages` viewer component and the public tab per the design; pdf.js built as one content-hashed module under `/_nuxt/assets/`, loaded on first selection, main-thread worker via `globalThis.pdfjsWorker`; `pdfjs-dist` moved to `dependencies` at the same pin (report the manifest and lockfile lines); public CSP and server HTML unchanged; unit tests from the design's list, including the bundle size check; `make web-source-manifest-update` after new files; `make web-no-eval-check` passes. Green CI.|
|4|qa|Proofs|Playwright specs in WebKit (iPhone SE, iPhone 15) and Chromium (360 CSS px Android, one desktop width): page count, labels, same ETag and SHA-256 as the download, no extra requests, no CSP violation, print prints the HTML; page 1 baselines generated in hosted CI; `make dev-https-public-check` extended with the five revocation proofs in the design. Green CI.|
|5|qa|Production check|After deploy: a fictional two-page resume on a real iPhone in Safari shows every page; download off removes the tab; evidence under `.dev/personas/`.|
|6|backend|Optional: coalesce render misses|Only if the owner schedules it after production shows duplicate misses. Concurrent public PDF misses for one cache key share one render under a generation-scoped context; revocation still cancels it within the five-second drain; ADR 0022 concurrency tests extended. Separate release.|

Slices 2 to 5 form one release. Slice 2 may run beside slice 3 if the frontend brief pins the designer's file set.

## Rules

- Code, tests, and comments cite the design file or ADR 0053, never this plan or its slice numbers.
- The server-rendered public HTML must stay byte-identical; a change to the Go public HTML validator means the design was misread. Stop and report.
- No new public route, OpenAPI change, CSP change, or server dependency without a new architect decision.
- Test resumes and fixtures are fictional.
- Reviewer confirms by name: tab absent with download off, `404` removes the tab, no bytes cached outside the browser HTTP cache, no request besides the PDF route and `/_nuxt/` assets, unchanged CSP and SSR bytes.
