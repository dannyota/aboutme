# MCP guide page (0.6.11)

Status: planned, waiting for owner approvals. Design: [MCP guide](../design/mcp-guide.md), [copy](../design/mcp-guide-copy.md), and [MCP client compatibility](../design/mcp-client-compatibility.md). No code starts before the owner answers both approval lists.

Two releases, in order. Claude's clients fail against today's OAuth server, so the compatibility release ships first and the owner proves Claude in production before the guide goes live. The manager assigns the version numbers; the guide keeps 0.6.11 only if compatibility takes an earlier free number.

|Release|Outcome|Risk|
|-|-|-|
|Compatibility|Claude on the web and Claude Code connect, refresh, and revoke; consent names where approval returns|High: OAuth, rate limits, consent; adversarial review|
|Guide|Public `/guide/mcp` page in both languages, header, footer, landing, and settings links, sitemap and `llms.txt`|Low: static page, one new reserved root|

## Release 1: MCP client compatibility

Order: backend and frontend in parallel on disjoint paths, then qa, then review, then the owner's production proof after deploy.

|Role|Files|Work|
|-|-|-|
|backend|`apps/server/internal/oauthsrv/metadata.go`, `clients.go`, `authorize.go`, `token_endpoint.go`, `consent.go`, `session_http.go`, `rate.go` and their tests; `apps/server/cmd/server/main.go` and config for the egress-range list; `apps/server/cmd/mcp-workflow/` only if it asserts the old metadata `resource`; `docs/api/openapi.yaml` (`agent_limit_reached` on the consent operation) and the generated web client|Rules 1 to 5, 7, and 8 of the compatibility design, each with its rejected neighbors as failing tests first|
|frontend|`apps/web/app/pages/authorize.vue`, `apps/web/app/composables/useOAuthConsent.ts`, `apps/web/app/i18n/consent.ts`, their tests|Rule 6 host line from the validated `redirect_uri`, loopback wording, and the rule 7 message with the Settings link|
|qa|new `deploy/dev-https-browser/mcp-ts-sdk.spec.ts`; `deploy/dev-https-browser/package.json` and lockfile (pinned official TypeScript SDK); `deploy/dev-https-browser/proof-shards.mjs` and shard coverage check; `Makefile` target `dev-https-mcp-ts-sdk-check` (shared file, report every line)|TypeScript SDK proof: discovery, registration with the SDK's default body, consent, token, `tools/list`, one private write, forced refresh with `client_id` and `resource`, revoke, `401`|
|devops|production app configuration for the egress-range list (value `160.79.104.0/21` from Anthropic's published page); `docs/runbooks/production.md` line naming the list|Add the setting, no other infrastructure change|
|architect|`docs/design/api.md`, `mcp-owner-workflow.md`, `security.md`, `budgets.md`|State the new rules in the living docs; mark the compatibility design implemented|
|reviewer|none|Adversarial review of the OAuth diff: PKCE S256, exact redirect match, refresh rotation and reuse revocation, cross-client refresh, resource set of exactly two values, ignored members never stored or logged, bucket ceiling, consent host as text|

After deploy, the owner runs the production proof in the compatibility design with the owner test account: Claude on the web and Claude Code, one private edit each, use again after more than one hour, revoke, refused. The guide release waits for that result.

## Release 2: guide page

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

- Owner approvals: eight in the guide design, nine in the compatibility design.
- Visual Studio Code joins Other apps only after its own proof; a qa brief can reuse the compatibility proof with its redirect `http://127.0.0.1:33418`.
- Client ID Metadata Documents need their own design before any build.
