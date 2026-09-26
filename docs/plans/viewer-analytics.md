# Sign in to view (0.6.6)

Status: planned; owner approvals are in [the design](../design/viewer-analytics/README.md#owner-approval). Design: [viewer analytics](../design/viewer-analytics/README.md), [ADR 0060](../adr/0060-viewer-data-controller-and-consent.md), [ADR 0061](../adr/0061-layered-human-view-counting.md), [ADR 0062](../adr/0062-sign-in-to-view-without-an-account.md). View counts shipped in v0.6.4. Consented viewer tracking is dropped for good (ADR 0060), so sign in to view, with its join popup, is the one release left.

Starts after the [LinkedIn PDF import](linkedin.md) (0.6.5). Migration numbers: the manager assigns them after the queued ones.

|Release|Outcome|Risk|
|-|-|-|
|0.6.6 Sign in to view|Mode `sign_in`, gate, `view` OAuth purpose, viewer identities and passes, gated routes, join invite, release fence|High: OAuth, public access control, revocation, rollback|

## Before any code

- The owner answers the open items of V1 to V8.
- The manager adds `docs/plans/traceability/ac-view.md` with `AC-VIEW-*` rows from the design's rules, layers, consent table, and gated routes.
- The 0.6.6 file list below names parts the dropped tracking release would have created (`apps/server/internal/viewerdata/`, `apps/web/app/public/overlay/`, `PublishViewers.vue`, consent migrations). The architect rechecks it against the [sign in to view](../design/viewer-analytics/sign-in-to-view.md) design before code starts.

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
