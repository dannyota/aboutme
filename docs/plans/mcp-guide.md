# MCP guide page

Status: implemented on `feat/mcp-guide`, awaiting current baselines, review, CI, and production prerequisites. Design: [MCP guide](../design/mcp-guide.md), [copy](../design/mcp-guide-copy.md), and [MCP client compatibility](../design/mcp-client-compatibility.md). The owner approved both approval lists on 2026-09-27. Compatibility shipped in v0.6.18; the page bar shipped in v0.6.24. The guide takes the next free patch version when it ships.

## Remaining release work

1. Regenerate guide and affected shell baselines in hosted CI after merging current `main`; inspect and commit the candidates.
2. Complete the designer finish review and fresh reviewer pass, resolve findings, and obtain green branch CI at the exact head.
3. Confirm the owner's production proof for Claude web and Claude Code: private edit, reuse after more than one hour, revoke, and refused access. Confirmation remains pending.
4. Confirm no production resume slug or tombstone is `guide` through a reviewed read-only check. No ad hoc production database query path exists; settle the supported check before deploy.
5. Merge to `main`, push, wait for green CI at the exact commit, then tag, build release images, deploy, and verify production. Do not release before both production prerequisites pass.

## Implemented scope and review criteria

Order: backend registry and discovery first, because the web root list and the web source manifest follow it; then frontend; then qa baselines in CI; then designer finish review; then review.

|Role|Files|Work|
|-|-|-|
|backend|`packages/publicroots/public-roots.v9.json` renamed to the next version with root `guide`, dispatch `nuxt`; `scripts/generate-public-roots.mjs`; regenerated `apps/server/internal/publicroots/generated.go`, `apps/web/app/public-roots.generated.ts`, `deploy/caddy/public-roots.generated.caddy`, `deploy/caddy/testdata/public-roots.generated.json`; `packages/publicroots/public-roots.test.mjs`, `renderer-build-sources.v1.json`; `apps/server/internal/publicroots/generated_test.go`, `apps/server/internal/routetable/route_table_test.go`, `apps/web/test/public-roots.generated.test.ts`; `scripts/dev-https.sh`, `scripts/dev-https-test.sh`, `scripts/dev-native.sh`, `deploy/dev-https-browser/static-test.sh`; `apps/server/internal/publicformat/discovery.go` site pages and its sitemap and `llms.txt` fixtures|Reserve `guide`; list `/guide/mcp` as “Connect an AI assistant with MCP”; bump a discovery format version only where its tests require it; report every generated line|
|frontend|new `apps/web/app/pages/guide/mcp.vue`; new `apps/web/app/components/guide/` (copy block, can and cannot, Claude steps, troubleshooting); new `apps/web/app/i18n/guide.ts`; `apps/web/app/i18n/locale.ts`, `meta.ts`, `shell.ts`, `legal.ts` (footer label), `agent-settings.ts`; `apps/web/app/composables/useSiteSeo.ts`; `apps/web/app/components/app/AppShell.vue`; `apps/web/app/pages/index.vue`; `apps/web/app/landing/copy.ts`; `apps/web/app/components/settings/ConnectedAgents.vue`; `packages/publicroots/app-build-sources.v1.json` (web source manifest); new `apps/web/test/guide/guide-page.test.ts`; `apps/web/test/app/app-shell.test.ts`, `seo.test.ts`, `legal.test.ts`, and the locale, theme, and settings tests that list routes or links|Page, copy, and links as designed; reuse the verify copy helper; the URL comes from the site origin constant; tests for both languages, heading order, exact URL and commands, header visibility by width and sign-in state, `aria-current`, canonical and robots, and no data fetch|
|qa|`apps/web/e2e/screenshot.spec.ts`; new baselines `guide--light--390.png`, `guide--light--1280.png`, `guide--dark--390.png`, `guide--dark--1280.png`; regenerated `chrome--*`, `template-*`, and `verify--*` baselines whose header or footer changed, listed from the CI diff; `deploy/dev-https-browser/public.spec.ts`|Header overflow check at 704, 768, and 1024 px in both languages, signed in and out; public proof: `/guide/mcp` renders through Caddy in both languages, `/guide` is not found, `POST /mcp` without a token still returns `401` with the metadata challenge|
|designer|`DESIGN.md` language coverage list|Add `/guide/mcp`; finish review at 390 and 1280 px, both themes and languages|
|architect|`docs/design/localization.md`, `ui/landing-and-library.md`, `ui/shell-and-editor.md`, `product.md` (registry note if it names the version), `mcp-guide.md`|Living docs name the route and links; mark the guide design implemented|
|devops|none in the repository|Before deploy, a read-only production query confirms no resume slug or tombstone is `guide`; review the generated Caddy line|

Unchanged: every renderer, print, card, and public resume baseline; OpenAPI; the database.

Reviewer focus: the registry adds exactly one `nuxt` root and `/mcp` still dispatches to Go; every page claim matches the tool list, scopes, and limits; the URL and commands are exact; no seal red; heading order, live region, and copy button names; no header overflow; canonical, indexable, sitemap, and `llms.txt` agree; no data fetch or CSP change; no plan or task IDs in code, tests, or living docs.

Checks: GitHub CI on the branch and on `main`. No local runs. A tag and deploy need green `main` CI on the exact commit.

## Open

- Visual Studio Code joins Other apps only after its own proof; a qa brief can reuse the compatibility proof with its redirect `http://127.0.0.1:33418`.
- Client ID Metadata Documents need their own design before any build.
