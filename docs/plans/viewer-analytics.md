# Viewer analytics (0.6.4 to 0.6.6)

Status: planned; waiting for the owner approvals V1 to V8 in [the design](../design/viewer-analytics/README.md#owner-approval). Design: [viewer analytics](../design/viewer-analytics/README.md), [ADR 0060](../adr/0060-viewer-data-controller-and-consent.md), [ADR 0061](../adr/0061-layered-human-view-counting.md), [ADR 0062](../adr/0062-sign-in-to-view-without-an-account.md). Three releases, one feature each, numbered in shipping order as V1 recommends. If the owner keeps the original numbers (0.6.4 tracking, 0.6.5 sign-in, 0.6.6 counts), only the labels change; the order stays, because tracking and sign-in record into the counting path.

Starts after [LinkedIn sign-in and import](linkedin.md) (0.6.2 and 0.6.3); the gate lists LinkedIn only once it is enabled. Migration numbers: the manager assigns them after the queued ones.

|Release|Outcome|Risk|
|-|-|-|
|0.6.4 View counts|Start and collect beacon, seven filter layers, WAF labels, daily aggregates, share signals, owner exclusion, `/app/views` pages, `viewer_mode` column (only `count` accepted), privacy notice counts item|Medium: public write endpoint, WAF change, migration|
|0.6.5 Viewer tracking|Mode `ask`, owner terms, consent popup, viewer cookies, view events and durations, consent records, `/privacy/viewer`, notice and terms|High: sensitive data, consent, public cookies|
|0.6.6 Sign in to view|Mode `sign_in`, gate, `view` OAuth purpose, viewer identities and passes, gated routes, join invite, release fence|High: OAuth, public access control, revocation, rollback|

## Before any code

- The owner answers V1 to V8. V7 (DPIA and cross-border dossiers) gates the 0.6.5 deploy, not its build.
- The manager adds `docs/plans/traceability/ac-view.md` with `AC-VIEW-*` rows from the design's rules, layers, consent table, and gated routes.

## 0.6.4 View counts

|Role|Files|Work|
|-|-|-|
|designer|`DESIGN.md` (new "Views page" section)|Spec for `/app/views` and `/app/views/{id}`: counts table, 90-day bars without a chart library, M breakdown, share signals, definition line, empty states, phone and desktop|
|backend|new migration (`viewer_mode`, `view_ref`, `resume_view_days`, `resume_share_signal_days`); `apps/server/internal/store/` queries and sqlc output; new `apps/server/internal/viewcount/` (token, ALTCHA verify, crawler list, edge headers, dedupe, owner check, hourly cap, buffer and flush); new `apps/server/internal/viewapi/` (start, collect, `GET /views`, `GET /resumes/{id}/views`, `PUT /resumes/{id}/viewer-mode` accepting `count` only); one call in `apps/server/internal/publicapi/html.go` to a new `html_crawler.go`; `apps/server/internal/privacyretention/` (400-day rows); `apps/server/cmd/server/main.go`; `apps/server/go.mod` and `go.sum` (`github.com/altcha-org/altcha-lib-go/v2`, report every line); `docs/api/openapi.yaml`|Layers 1 and 4 to 7 and the edge-header reads of 2 and 3 as the counting page states; rate policies and memory bounds from budgets; tests from the design's Go counting list|
|frontend|new `apps/web/app/public/viewBeacon.ts` and tests; one mount call in `apps/web/app/public/public-resume.client.ts`; `apps/web/package.json` and lockfile (`altcha-lib`, report every line); new `apps/web/app/pages/app/views/index.vue`, `[id].vue`; new `apps/web/app/components/views/`; new `apps/web/app/i18n/views.ts`; settings or app navigation link; regenerated `apps/web/app/api/generated/openapi.ts`; web source manifest; `apps/web/app/i18n/legal.ts` (counts item, "what we do not do" line) and its test|Beacon with visible time, trusted events, main-thread proof of work, no cookie or storage; Views pages to the designer's spec; notice text as approved|
|devops|`deploy/aws/modules/edge/main.tf` (Bot Control and Anonymous IP groups in Count with the collect scope-down, two label rules with inserted headers, origin request policy adds `CloudFront-Viewer-Country` and `CloudFront-Viewer-City`) and its tests; `deploy/caddy/production/edges/` (pass the four headers on the CloudFront listener, strip other `x-amzn-waf-*`, strip all four elsewhere); `deploy/caddy/Caddyfile` (strip); `deploy/caddy/production/test.sh`|Adversarial review before merge; plan must show no block action and no destroy|
|qa|`deploy/dev-https-browser/public.spec.ts`, `verify-evidence.mjs`|Count after 8 s and a scroll; none without interaction; replay rejected; owner not counted; live checks 1 to 6 after deploy|

## 0.6.5 Viewer tracking

|Role|Files|Work|
|-|-|-|
|designer|`DESIGN.md` (consent popup section)|Card and bottom sheet at phone and desktop widths, equal buttons, shadow-root tokens, reserved padding|
|backend|new migration (`viewer_terms_at`, `resume_view_events`, `resume_view_durations`, `view_consents`); store queries; `apps/server/internal/viewcount/` (device, source, place classifiers; event append; duration); new `apps/server/internal/viewerdata/` (consent route and cookies, viewer data `GET` and `DELETE`); `apps/server/internal/viewapi/` (accept `ask` with terms, events in the resume read, `DELETE /resumes/{id}/viewer-data`); `apps/server/internal/privacyretention/` (90 and 180 days); `docs/api/openapi.yaml`|Consent before events; decline stores no key; view key vector; withdrawal in one transaction; tests from the design's Go tracking list|
|frontend|new `apps/web/app/public/overlay/` (shadow-root host, `ConsentPopup.vue`); `apps/web/app/public/viewBeacon.ts` (consent state, `sendBeacon` duration); new `apps/web/app/components/editor/PublishViewers.vue` with a one-line mount in `PublishDialog.vue` (664 lines); move `apps/web/app/pages/privacy.vue` to `apps/web/app/pages/privacy/index.vue` and add `privacy/viewer.vue`; `apps/web/app/components/views/` (event list, delete all); `apps/web/app/i18n/views.ts`, `publish.ts`, `legal.ts` (viewer item, purpose, rights, cookies, terms for owners); generated client; source manifest|Popup text and notice as approved; equal buttons; no stacking; route move keeps `/privacy` output identical|
|qa|`deploy/dev-https-browser/public.spec.ts`, `privacy.spec.ts`, `publish.spec.ts`; public page baselines with the popup|Agree, decline, ignore, withdraw, delete all; baselines at phone and desktop; live check 7|

The reviewer does an adversarial pass on 0.6.5 and confirms by name: no event without a stored agree record, decline and ignore store no key, cookies set only by the server with `__Host-` attributes, withdrawal deletes at once, no viewer data in logs or MCP, equal-choice UI.

## 0.6.6 Sign in to view

|Role|Files|Work|
|-|-|-|
|designer|`DESIGN.md` (gate and join invite sections)|Gate layout; invite card and bar geometry; close control|
|backend|new migration (`resume_viewers`, `viewer_passes`, `oauth_transactions` purpose `view` and `resume_id`); store queries; `apps/server/internal/auth/` (`transaction.go` purpose, start and callback branch in new `view_purpose.go`); new `apps/server/internal/viewerpass/`; `apps/server/internal/publicapi/` (gate in `html.go` through a new `gate.go`, pass checks in `json.go`, `photo.go`, `artifact.go`); `apps/server/internal/realtimeapi/service.go` (live stream check); `apps/server/internal/publicstate/` (discovery off for `sign_in`); `apps/server/internal/directrender/` (gate envelope); `apps/server/internal/viewapi/` (accept `sign_in`, fence wait, pass revocation, named views); `apps/server/internal/viewerdata/` (pass access); `apps/server/internal/privacyretention/`; `docs/api/openapi.yaml`|Everything in the sign-in page; tests from the design's Go sign-in list, including a subject that belongs to an account|
|frontend|`apps/web/server/utils/public-render/envelope.ts` and `apps/web/server/workers/public-render/render.ts` (gate envelope); new `apps/web/app/components/public/PublicGate.vue`; `apps/web/app/public/overlay/JoinInvite.vue`; `PublishViewers.vue` (third mode); `apps/web/app/components/views/` (people list); `apps/web/app/i18n/` gate, invite, and notice text; generated client; source manifest|Gate and invite as approved; invite timing and placement; close stored for 90 days|
|devops|`docs/runbooks/production.md` (raise the fence to v0.6.6 at deploy; switch `sign_in` resumes to `count` before any lower rollback)|No infrastructure change|
|qa|`deploy/dev-https-browser/public.spec.ts` (gate with the mock provider), `verify-evidence.mjs`; baselines for gate and invite|Every gated route with and without a pass; mode switch revokes; live checks 8 and 9|

The reviewer does an adversarial pass on 0.6.6 and confirms by name: the `view` callback never creates a session or touches an account, the pass check runs before the public cache on every gated route, `sign_in` waits for the fence, turning it off revokes passes, the fence is raised, and the preview card exposes no contact data.

## Writing rules for briefs

Code, comments, tests, and living docs cite the design, ADRs 0060 to 0062, or `AC-VIEW-*` IDs, never this plan, its releases, or task names. No em dashes. Human-read Markdown keeps Prettier's 80-column wrap. Run `make pre-push` before every push; GitHub CI is the gate. Commit messages never mention AI or agents.
