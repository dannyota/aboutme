# Sign in to view (next release after the MCP guide)

Status: approved, ready to build. The owner settled V9 to V13 on 2026-09-27 in [the design](../design/viewer-analytics/README.md#owner-approval). Design: [sign in to view](../design/viewer-analytics/sign-in-to-view.md), [delivery](../design/viewer-analytics/delivery.md), [legal](../design/viewer-analytics/legal.md), [ADR 0022](../adr/0022-viewer-privacy-and-counting.md). View counts shipped in v0.6.4. Consented viewer tracking is dropped for good (ADR 0022): no viewer tables, no consent migrations, no viewer list, no overlay. This is the one release left in this plan.

Ships as the next release after the MCP guide page, one feature, its own version number. Migration: one file, numbered next after the latest in `apps/server/migrations` (`00008_resume_view_counts.sql` today); the manager assigns the number when the backend brief starts and serializes it with any other queued migration.

|Release|Outcome|Risk|
|-|-|-|
|Sign in to view|Switch behind `SIGN_IN_TO_VIEW_ENABLED`, gate, `view` OAuth purpose for Google and LinkedIn, pass cookie, gated routes before the public cache, join invite, release fence floor|High: OAuth, public access control, revocation, production infrastructure, rollback|

## Before any code

1. Done 2026-09-27: the owner settled V9 to V13 and reviewed the Vietnamese of V4 (gate text, invite, notice rows, publish switch line).
2. Done: `docs/plans/traceability/ac-view.md` holds the `AC-VIEW-*` rows from the design, state `OPEN`. Briefs cite those IDs.
3. Done: [ADR 0016](../adr/0016-sign-in-providers.md) (start methods: `GET` also accepts `purpose=view`; an unknown purpose still means login) and the OAuth start list in `docs/design/security.md` are amended; `docs/design/budgets.md` already carries the pass cookie row and needs no change.
4. Still open, before the backend brief: verify LinkedIn accepts an authorize request with scope `openid` alone (V10). If it does not, the `view` start keeps the account scopes and still discards every claim; the design line changes, not the gate text.

## Order and roles

1. designer, then backend and devops in parallel (disjoint files), then frontend once OpenAPI and the generated client land, then qa, then one reviewer pass over the whole release.
2. backend-lead owns the Go contract; frontend-lead starts from the merged OpenAPI; devops-lead owns flag, key, fence floor, runbook. Leads settle cross-lane details directly and report every cross-lane file to the manager.

## File sets

|Role|Files|Work|
|-|-|-|
|designer|`DESIGN.md` (gate and join invite parts)|Gate layout at phone and desktop; invite card and bar geometry; close control; disabled discovery switch note (V13)|
|backend|new `apps/server/migrations/000NN_sign_in_to_view.sql`; `apps/server/sql/queries.sql` (publish upsert, public snapshot and realtime selects gain switch and epoch, epoch raise, sitemap and `llms.txt` filters, view transaction insert and consume); `apps/server/sql/account_export.sql`; sqlc output in `apps/server/internal/store/`; `apps/server/internal/config/` (flag, `VIEW_PASS_KEY`); `apps/server/internal/api/capabilities.go`; `apps/server/internal/auth/` (`transaction.go` purpose `view` and resume ID; `start.go` `GET` branch; new `view_purpose.go` for start and callback; one-line dispatch in `handlers.go` and `linkedin.go`, both near the length limit); new `apps/server/internal/viewpass/` (seal, open, cookie merge and prune); `apps/server/internal/publicresume/reader.go` (snapshot switch, epoch, effective discovery); `apps/server/internal/publicapi/` (new `gate.go` with the pass check and gate render and its HTML rules; `html.go` variant for the invite marker; `json.go`, `photo.go`, `artifact.go` PDF branch only; `routes.go`); `apps/server/internal/directrender/types.go` (gate request, invite marker); `apps/server/internal/realtimeapi/service.go` (pass check at admission); `apps/server/internal/viewapi/public.go` (start gated, `signedIn`); `apps/server/internal/resumeapi/` (`publish.go` optional field; `resumes_publish.go` flag issue, revoking class when turned on while live, epoch raise on turn-on and re-publish, recovery proof field; `resumes_fields.go` response); `apps/server/cmd/server/main.go`; `docs/api/openapi.yaml`|Everything in the design's Setting, Gate, Gated routes, Sign-in flow, and Pass cookie sections; the Go and store tests in [delivery](../design/viewer-analytics/delivery.md#tests)|
|frontend|`apps/web/server/utils/public-render/envelope.ts` (gate envelope, invite marker); `apps/web/server/workers/public-render/render.ts` (gate root); new `apps/web/app/components/public/PublicGate.vue`; new `apps/web/app/components/public/JoinInvite.vue`; new `apps/web/app/public/joinInvite.ts` (timing, placement, `localStorage`); `apps/web/app/public/public-resume.client.ts` (mount invite); `apps/web/app/public/viewBeacon.ts` (pass `owner` and `signedIn` on); `apps/web/app/components/editor/PublishDialog.vue` (675 lines: the switch goes in a new `apps/web/app/components/editor/PublishAccess.vue`); `apps/web/app/i18n/publish.ts`; `apps/web/app/i18n/legal.ts` (the two sign-in notice rows); generated `apps/web/app/api/generated/openapi.ts`; source manifest|Gate with no script; invite as approved; switch shown only with the capability; discovery switch disabled while on; web unit tests in both languages|
|devops|`deploy/aws/modules/tasks/main.tf` and `variables.tf` (flag env, `VIEW_PASS_KEY` from SSM); `deploy/aws/prod/variables.tf`; `deploy/aws/scripts/secrets.sh` (`view-pass-key`, 32 random bytes); `deploy/aws/scripts/fence.sh` and `deploy.sh` (floor for the flag: refuse an app revision with the flag on while the fence is below this release's number) and their tests and `testdata/respond`; dev defaults in `.env.example`, `scripts/dev-native.sh`, `scripts/lib/dev-https-lifecycle.sh` (flag on, a dev key); new `docs/runbooks/sign-in-to-view.md` (flag-off deploy, `--activate`, flag on, key rotation, what rollback means) with a one-line pointer in `docs/runbooks/production.md` (at 450 lines: trim a line in the same edit)|No new AWS service; adversarial review before merge|
|qa|`deploy/dev-https-browser/public.spec.ts` (gate and sign-in through the mock Google and mock LinkedIn, no nonce on LinkedIn), `verify-evidence.mjs`; pixel baselines for the gate and the invite card and bar in both languages|Every gated route with and without a pass; switch on revokes an open page and stream; switch off serves publicly; invite timing and close; live check 7 after the flag is on|

`.env.example`, `scripts/`, and the root `Makefile` are shared: the devops brief names each line; the manager commits them. The flag floor number is this release's own number, known when the manager picks the version; devops writes it once the version is fixed and before the tag.

## Release

1. Merge after green CI; tag; deploy with `SIGN_IN_TO_VIEW_ENABLED=false` and `VIEW_PASS_KEY` present. Rollback is still safe here.
2. Production proof with the flag off: public pages, login, and publish unchanged; the switch is hidden.
3. `deploy.sh --activate <tag>` raises the fence to this release.
4. Reviewed `tofu apply` with the flag on; redeploy the same tag; run live check 7 on a fictional resume.
5. After step 3, `--rollback` below this release is refused; a defect is fixed forward (V12). Turning the flag off again only stops new turn-ons; gated resumes stay gated.

## Reviewer checklist

The reviewer does one adversarial pass over the whole release and confirms by name:

- The `view` start and callback never read, create, link, or sign in an account, never create a session or a CSRF token, and write and log nothing about the viewer, for Google and LinkedIn, including a subject that belongs to an existing account.
- The callback re-reads the resume by ID, sets a pass only when it is live with the switch on, binds the current epoch, and redirects only to the current slug, `/`, or the gate with a closed message; no open redirect through `slug`.
- The pass check runs after admission and before the public cache on `/{slug}`, JSON, photo, PDF, live stream, and view start; preview card and `og.png` stay public and carry no contact data; `.md`, sitemap, and `llms.txt` treat the resume as not discoverable.
- Gated responses carry `private`; the gate is `no-store` and `noindex`, has no script and no resume content, and reads only the two closed `signin` values.
- Turning the switch on for a live resume is revoking, waits for the fence, ends open live streams, and raises the epoch; re-publishing raises it too; turning it off needs no fence.
- The pass MAC covers every field under a domain label with constant-time compare; expiry is checked; a pass for another resume or an older epoch fails; the cookie is `__Host-`, `HttpOnly`, `Secure`, `SameSite=Lax`, bounded to 10 passes; `VIEW_PASS_KEY` never reaches logs, the database, or the public repo.
- The flag refuses turn-on but never lifts a gate; an older publish body keeps the stored value; the migration's down step deletes `view` transactions first; grants follow ADR 0005.
- The deployer refuses the flag on below the floor; the fence is raised before the flag goes on; the runbook matches the scripts.
- The invite shows only on a pass holder's `sign_in` resume, never to the owner or a signed-in account, never covers resume text, and its link has no tracking parameter.

## Writing rules for briefs

Code, comments, tests, and living docs cite the design, ADR 0022, ADR 0016, or `AC-VIEW-*` IDs, never this plan, its release, or task names, and never a version number. No em dashes. Human-read Markdown keeps Prettier's 80-column wrap. Review the diff and run `make pre-push` before every push; GitHub CI is the gate. Commit messages never mention AI or agents.
