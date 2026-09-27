# LinkedIn import from Save to PDF (0.6.6)

Status: planned; the owner approved I1 to I10 in [LinkedIn import](../design/linkedin-import.md#owner-approval) and [ADR 0023](../adr/0023-linkedin-import.md) on 2026-09-27. One release, one feature: `/app/import/linkedin` reads an English LinkedIn Save to PDF in the browser, shows a review, and creates a new resume. Risk: medium to high (hostile PDF parsing in the browser, a new runtime dependency, an app page CSP change, the privacy claim).

LinkedIn sign-in is done: it shipped in v0.6.2 and is on in production from v0.6.3 ([ADR 0016](../adr/0016-sign-in-providers.md)). Git keeps its release plan.

## Before any code

1. Done 2026-09-27: the owner answered I1 to I10, and ADR 0023 is accepted.
2. Done 2026-09-27: the owner's shape report settled the design's [facts to confirm](../design/linkedin-import.md#open-facts); the design now carries the measured sizes and gaps, per-column heading sizes, the unlabeled email, unparenthesized group durations, and the 1.4 sidebar entry threshold. Facts one profile could not settle stay marked **Verify**, and the parser handles them defensively. Steps 3 onward may start.

## Steps

Order: 1, then 2 with 3, then 4, then 5 to 7. Steps 2 and 3 share no files.

|Step|Role|Files|Work|
|-|-|-|-|
|1|frontend|new `apps/web/scripts/linkedin-pdf-shape.mjs`|Shape report only, with the design's `getDocument` options; prints no profile value. Reviewed by the frontend lead before the owner runs it.|
|2|designer|new `docs/design/linkedin-import-ui.md`, one pointer line in `DESIGN.md`|Spec for the instructions, file picker, messages (not LinkedIn, not English, unreadable, too large, old browser), review screen, notices, size meter, and cap state at 390 and 1280 px. Owner reviews the Vietnamese copy.|
|3a|frontend|`apps/web/package.json` and `package-lock.json` (move `pdfjs-dist` 6.3.289 from devDependencies to dependencies; report every line); `apps/web/app/utils/csp.ts` (`APP_CSP` gets `worker-src 'self'`), `apps/web/test/csp.test.ts`, `apps/web/e2e/normal-csp.spec.ts` if it pins the string|Tests first. Public, print, and harness policies unchanged.|
|3b|frontend|new `apps/web/app/import/linkedin/` (`pdfWorker.ts`, `read.ts`, `lines.ts`, `layout.ts`, `sections.ts`, `dates.ts`, `richText.ts`, `build.ts`); new `apps/web/app/pages/app/import/linkedin.vue`; `apps/web/nuxt.config.ts` (`/app/import/**` gets `ssr: false`); new `apps/web/app/components/import/`; new `apps/web/app/i18n/import.ts`; `apps/web/app/components/editor/list/CreateResumeDialog.vue` (link only, full page load); `apps/web/app/i18n/resume-create.ts`; `apps/web/app/i18n/legal.ts` and its test; new `apps/web/test/import/linkedin/` with the `.fo` sources, generated PDFs, and `fixtures/README.md`; web source manifest|Tests first against the fixtures and the in-test hostile files. No `v-html`. Fixture PDFs are rendered once by Apache FOP 2.3 in a pinned container under a manager brief with the shared lock and memory cap; CI never regenerates them.|
|4|qa|new `deploy/dev-https-browser/linkedin-import.spec.ts` and its shard registration|Proof cases from the design, with the request log assertions, in Chromium and WebKit.|
|5|architect|`docs/design/security.md` (CSP section: `worker-src 'self'` on app pages), `docs/design/web.md` (surfaces table: `/app/import/linkedin`), `docs/design/product.md` (journey), `docs/design/README.md` (index row), `docs/design/budgets.md` if it lists client parse limits|Living docs match the built state.|
|6|designer|read-only|Finish review against the spec.|
|7|reviewer|read-only|Adversarial pass, below.|

The reviewer confirms by name: every limit is enforced on bytes read and items counted, not on `File.size` or declared values; the worker is terminated at the time limit and each pick uses a fresh one; `getDocument` gets no URL, `useWasm: false`, `enableXfa: false`, and `stopAtErrors: true`, and nothing renders or loads the viewer; no request carries file bytes, leaves the origin, or happens between the pick and Create; values render as text; rich text stays inside the allowlist; the request stays under 256 KiB; the public, print, and harness policies keep `worker-src 'none'`; `pdfjs-dist` is exactly 6.3.289, its optional `@napi-rs/canvas` stays out of the production image, and no pdf.js code reaches the Nitro server bundle.

## Writing rules for briefs

Code, comments, tests, and living docs cite the design page, ADR 0023, or `AC-*` IDs, never this plan, its release, or step names. No em dashes. Human-read Markdown keeps Prettier's 80-column wrap. Run `make pre-push` before every push; GitHub CI is the gate. Fixtures use synthetic people only; no real LinkedIn PDF or text from one is committed.
