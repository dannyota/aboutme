# Link previews (0.6.0 and 0.6.1)

Status: 0.6.0 is tagged `v0.6.0`; 0.6.1 is planned. The owner approved every choice in [the design](../design/link-previews.md#owner-decisions), and ADR 0055 is accepted. Design: [link previews](../design/link-previews.md), [ADR 0055](../adr/0055-stored-link-preview-card.md). The owner moved the publish-panel preview and the Verify page ([deployment transparency](deployment-transparency.md)) into 0.6.1 with the card. The next releases are 0.6.2 LinkedIn sign-in and 0.6.3 LinkedIn import ([linkedin.md](linkedin.md)).

|Release|Outcome|Risk|
|-|-|-|
|0.6.0 Preview text|Title, description from the summary, locale, URL, site name, image type and alt on every live resume page; privacy notice paragraph on third-party copies|Medium: HTML validator and render contract|
|0.6.1 Preview card, publish-panel preview, and Verify page|Stored, versioned 1200 by 630 card with name, headline, photo, branding; no contact data; built at publish and on change. The publish dialog shows the card and a chat-card mock; no new setting. The Verify page as in [deployment transparency](deployment-transparency.md#verify-page)|High: migration, publish revocation, render queue; the Verify page adds a public route and a reserved slug|

## Owner review

- The owner reviews the Vietnamese privacy notice text in each release that changes it (0.6.0 and 0.6.1) before that release ships.

## 0.6.0 Preview text

Deploy order: the web image starts before the app, so the Nuxt decoder accepts a missing `preview` object and then emits today's head. Go always sends it and validates the new head.

|Role|Files|Work|
|-|-|-|
|backend|new `apps/server/internal/previewmeta/` (code, tests, `testdata/` cases); `apps/server/internal/directrender/` (request type and tests); `apps/server/internal/publicapi/html.go`; new `apps/server/internal/publicapi/html_meta.go` and `html_meta_test.go`; `html_test.go`, `html_diagnostics_test.go`|Text rules from the design (normalize, scrub, title, description with fallbacks and cut, locale, image text); pass `preview` in the render request; move the meta checks out of `html.go` (658 lines) into `html_meta.go` and accept exactly the new tags; raise `htmlFormatVersion`; test the largest document still fits 532,480 bytes|
|frontend|`apps/web/server/utils/public-render/envelope.ts`; `apps/web/server/workers/public-render/render.ts`; `apps/web/test/public-render/render.test.ts`; `apps/web/test/seo.test.ts` if it asserts resume tags; `apps/web/app/i18n/legal.ts` and its test|Decode the closed `preview` object; emit the tags in design order with attribute escaping; head snapshots for a Vietnamese and an English resume; privacy notice paragraph on third-party copies in both languages, as approved|
|qa|`deploy/dev-https-browser/privacy.spec.ts`, `exports.spec.ts`, `verify-evidence.mjs` (their resume head assertions)|Update head assertions; after deploy, run the live check list in the design on a fictional resume and store evidence under `.dev/personas/`|

## 0.6.1 Preview card

|Role|Files|Work|
|-|-|-|
|designer|`DESIGN.md` (new "Link preview card" section); new `apps/web/app/components/preview/preview-card.css`|Final card spec inside the design's layout rules; card CSS and tokens; review the built card at 1200, 600, and 300 px wide and in a square crop|
|backend|new `apps/server/migrations/00007_resume_preview_cards.sql` (manager confirms the number); store queries and sqlc output under `apps/server/internal/store/`; new `apps/server/internal/previewcard/` (envelope inputs, version, scheduler, store transaction); `apps/server/internal/printsnapshot/` (card envelope); `apps/server/internal/renderjob/` (low-priority admission); `apps/server/internal/printrender/` (card byte limit); `apps/server/internal/publicresume/` (photo key digest accessor); `apps/server/internal/publicapi/` (new `card.go` route, `routes.go`, `/og.png` alias in `artifact.go`); `apps/server/internal/previewmeta/` (versioned image URL); `apps/server/internal/publicformat/` (robots rule and testdata); unpublish, rename, and delete transactions in `apps/server/internal/resumeapi/` or the store; `apps/server/cmd/server/main.go`; `docs/api/openapi.yaml`|Everything in "Build, storage, and serving" and "Privacy" of the design; tests for version mismatch 404, store refused after unpublish, stale row, alias, scheduler debounce and sweep, queue priority, and contact sentinels absent from the envelope|
|frontend|new `apps/web/app/components/preview/PreviewCard.vue` and its app wrapper; `apps/web/server/utils/print/envelope.ts` (card kind); `apps/web/server/workers/print/render.ts`; new harness page for the card under `apps/web/app/pages/_harness/`; web tests including the layout pin hash; regenerated `apps/web/app/api/generated/openapi.ts`; web source manifest; `apps/web/app/i18n/legal.ts` (stored-card sentence)|Render the closed card envelope with the designer's CSS; reject unknown keys; no script; pin the component and CSS hash to the layout version|
|qa|new `apps/web/e2e/card.spec.ts` and its baselines; `deploy/dev-https-browser/` public check updates|Pixel baselines for the design's cases, safe-square geometry, byte limit with the noisiest photo, contact sentinels absent from the card text; live check list after deploy, then again after unpublish|
|devops|none in the repository|Read the web ACL's sampled requests during the live checks for blocked crawler user agents|

The reviewer does one adversarial pass on 0.6.1, after the publish-panel part, and confirms by name: gate before every card read, row deleted in the unpublish, rename, and delete transactions, no store after revocation, old versions return 404, no contact field in the card envelope, and owner PDF exports keep their queue places.

## 0.6.1 Publish-panel preview part

Built after the card work in the same release; the frontend starts it once `PreviewCard.vue` exists.

|Role|Files|Work|
|-|-|-|
|designer|`DESIGN.md` (publish dialog section)|Spec for the card and chat-card mock in the dialog at phone and desktop widths|
|frontend|new `apps/web/app/components/editor/PublishPreview.vue` (`PublishDialog.vue` is 664 lines, so only a one-line mount there); new `apps/web/app/utils/previewText.ts` and tests reading the Go cases in `apps/server/internal/previewmeta/testdata/`|Render `PreviewCard.vue` and the text from the TypeScript copy of the rules; no new request or setting|
|qa|`deploy/dev-https-browser/` editor check; baselines for the dialog|Proof at phone and desktop widths in both languages|

## Writing rules for briefs

Code, comments, tests, and living docs cite the design doc, ADR 0055, or `AC-*` IDs, never this plan, its releases, or task names. No em dashes. Human-read Markdown keeps Prettier's 80-column wrap. Run `make pre-push` before every push; GitHub CI is the gate.
