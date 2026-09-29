# Community showcase

Status: approved, ready to build. The owner answered S1 to S13 on 2026-09-27 in [the design](../design/showcase.md). Design: [showcase](../design/showcase.md), [ADR 0029](../adr/0029-community-showcase.md) (Accepted). One release, the last of the four upcoming ones; the manager assigns its version and its migration number after the queued ones.

|Release|Outcome|Risk|
|-|-|-|
|Community showcase|Opt-in switch and role in the publish dialog; review key and out-of-band review command; `/showcase` page with filters; uncached listing route; new public root; privacy and terms text|High: public access control, publish revocation, a migration, a new public root, a production operator command|

## Before any code

1. Done 2026-09-27: the owner answered S1 to S13; the design and ADR 0029 carry the approved answers, and the ADR reads Accepted.
2. Still open: the architect updates the living design pages the contract touches: `docs/design/product.md` (publish controls, V1 scope row), `docs/design/data.md` (table row), `docs/design/api.md` (endpoint rows), `docs/design/budgets.md` (listing rate limit, page size), `docs/design/ui/landing-and-library.md` pointer for the designer. `docs/design/decisions.md` is done.
3. Done: `docs/plans/traceability/ac-showcase.md` holds the `AC-SHOW-*` rows, state `OPEN`.
4. Still open: the manager confirms no production resume holds the slug `showcase` (the migration also refuses).

## Steps

Order: 1 designer and 2 backend in parallel; 3 backend public roots before 4 frontend; 5 devops after 2; 6 qa after 4 and 5; 7 reviewer last.

|Step|Role|Files|Work|
|-|-|-|-|
|1|designer|`docs/design/ui/landing-and-library.md` (new Showcase section)|Page layout: header band like the Library, filter rows (role chips, language chips, template select), 1/2/3-column grid at 641 and 1024 px, tile with the card at 1200:630, meta line, role chip, Report link, pager, empty and error states; publish-dialog showcase block with status lines. Finish review of the built page at 390 and 1440 px in both languages|
|2|backend|new migration (`resume_showcase` table, partial index, grants, `DO` block that raises when a resume holds slug `showcase`); `apps/server/sql/showcase.sql` and sqlc output in `apps/server/internal/store/`; new `apps/server/internal/showcase/` (review key, template match, listing service, review commands); new generator `packages/schema/scripts/generate-showcase-presets.mjs` and its Go output `apps/server/internal/showcase/presets.generated.go`; `apps/server/internal/previewcard/` (export the card-input builder the review key reuses, no behavior change); the resume write boundary in `apps/server/internal/resume/` (recompute derived columns in the write transaction); `apps/server/internal/resumeapi/` (publish fields, issues, owner resource `showcase`); `apps/server/internal/publicapi/` (new `showcase.go`, `routes.go`, rate policy); `apps/server/internal/accountapi/` and `apps/server/sql/account_export.sql` (export); `apps/server/cmd/server/showcase_review.go` and `main.go` dispatch; `docs/api/openapi.yaml`|Everything in the design's Opt-in, Review, Delivery, and Data and contract sections. Tests below|
|3|backend|`packages/publicroots/public-roots.v11.json` (renamed from v10), `scripts/generate-public-roots.mjs`, `packages/publicroots/public-roots.test.mjs`, `packages/publicroots/app-build-sources.v1.json`, `renderer-build-sources.v1.json`, generated `apps/server/internal/publicroots/generated.go` and test, `apps/server/internal/routetable/route_table_test.go`, `apps/web/app/public-roots.generated.ts` and `apps/web/test/public-roots.generated.test.ts`, `deploy/caddy/public-roots.generated.caddy` and `deploy/caddy/testdata/public-roots.generated.json`, `deploy/dev-https-browser/static-test.sh`, `scripts/dev-https.sh`, `scripts/dev-https-test.sh`, `scripts/dev-native.sh`|Add `showcase` with Nuxt dispatch, the same file set as the `/verify` root; report every changed line in the web, deploy, and scripts files|
|4|frontend|new `apps/web/app/pages/showcase.vue`; new `apps/web/app/components/showcase/ShowcaseTile.vue`, `ShowcaseFilters.vue`, `ShowcasePager.vue`; new `apps/web/app/composables/useShowcase.ts`; new `apps/web/app/i18n/showcase.ts`; new `apps/web/app/components/editor/PublishShowcase.vue` (keeps `PublishDialog.vue` under 700 lines) and its mount in `PublishDialog.vue`; `apps/web/app/i18n/publish.ts`; `apps/web/app/i18n/shell.ts` and `apps/web/app/components/app/AppShell.vue` (nav link, marketing path); `apps/web/app/i18n/legal.ts`; generated `apps/web/app/api/generated/openapi.ts`; unit tests under `apps/web/test/`; new `apps/web/e2e/showcase.spec.ts` and its entry in `WEB_E2E_NORMAL_SPECS` in the root `Makefile` (report the line); web source manifest|Page, filters in the URL, lazy images, tile links with `rel="nofollow"`, Report `mailto:`, noindex and nofollow head, no cookie or storage use; publish block with every state and issue from the copy tables; legal text exactly as approved|
|5|devops|new `deploy/aws/scripts/showcase.sh` sourced by `deploy.sh` (`--showcase-review` with `pending`, `approve`, `decline`, `show`), `deploy/aws/scripts/deploy.sh` flag parsing, `deploy/aws/scripts/deploy_test.sh`; `docs/runbooks/production.md` (review procedure and the approval checklist from the design)|One-shot task on the jobs family at the live tag's server image with the command override, provenance check as in the TOTP re-encrypt script, output read back from the task log; no OpenTofu change. Needs the reviewer's adversarial pass|
|6|qa|`deploy/dev-https-browser/publish.spec.ts` (or a new `showcase.spec.ts` and its shard entry), `verify-evidence.mjs`; new chrome baselines for `/showcase` (empty and filled, 390 and 1440 px, both languages) and the publish block|Opt in, see pending, approve with the local command, see the tile, filter, open the resume; unpublish, sign in to view, opt-out, and decline each remove the tile on the next request; Report link target; no cookie set on `/showcase`|
|7|reviewer|read-only|Adversarial pass, then the release|

## Tests

- Go store and migration: migration test with a resume holding `showcase` (refuses) and without (adds table); constraint rejects for each column; cascade on resume and account deletion; grants.
- Go showcase: review key is stable across accent and layout version and changes with slug, language, name, headline, photo, and crop; template match for all 20 presets, each kept leaf varied, one changed token gives `custom`; preset table matches `packages/schema/templates/`.
- Go write boundary: document, photo, and publish writes recompute the three derived columns in the same transaction; a write to a resume without a row touches nothing.
- Go publish: `requires_live`, `requires_open_view`, `invalid_format` for role; omitted fields keep state; unpublish and sign in to view delete the row; republish starts off; opt-out then opt-in clears the review; idempotent replay.
- Go listing: each of the five listing conditions excludes; filters and order; `first_listed_at` unchanged by re-approval; page bounds; every bad query is 400; headers `no-store` and noindex; sentinel contact values in every contact type never appear in the response; rate limit.
- Go review command: stale key does nothing; approve and decline outputs hold no name or headline.
- Concurrency: an opt-out, unpublish, or decline that commits while a listing request is in flight; every request admitted after success omits the resume.
- Web: filters round-trip through the URL; tile markup has no contact text; empty, no-match, and error states; publish block states in both languages; legal text snapshot.

The reviewer confirms by name: nothing is listed without an approved current key, every ending path deletes the row in the same transaction, the listing is never cached, no cookie or storage is used on `/showcase`, no contact detail reaches the listing, the review command's output holds no names, the public root cannot shadow an existing resume, and the deploy script change runs only the review command.

## Release

- Deploy after green CI on the exact commit; no release fence (a rollback only hides the showcase).
- After deploy, the owner turns on the switch for `/danny` and the operator approves it (S10). qa checks the live page in both languages and confirms `/showcase` sends noindex and sets no cookie.

## Writing rules for briefs

Code, comments, tests, and living docs cite the showcase design, ADR 0029, or `AC-SHOW-*` IDs, never this plan, its steps, or task names. No em dashes. Human-read Markdown keeps Prettier's 80-column wrap. Run `make pre-push` before every push; GitHub CI is the gate. Commit messages never mention AI or agents.
