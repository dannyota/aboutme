# LinkedIn (0.6.2 and 0.6.3)

Status: planned; waiting for owner approvals L1 to L4 in [LinkedIn sign-in](../design/linkedin-sign-in.md#owner-approval) and I1 to I3 in [LinkedIn import](../design/linkedin-import.md#owner-approval), and acceptance of [ADR 0058](../adr/0058-linkedin-sign-in-in-production.md) and [ADR 0059](../adr/0059-linkedin-import-in-the-browser.md). Two small releases, one feature each, after 0.6.1 (preview card, publish-panel preview, and Verify page; see [link previews](link-previews.md)).

|Release|Outcome|Risk|
|-|-|-|
|0.6.2 LinkedIn sign-in|LinkedIn's documented flow, production wiring, privacy notice, mock and proofs; the flag turns on as a separate step after the release is live|High: authentication, IAM, production secrets|
|0.6.3 LinkedIn import|`/app/import/linkedin` reads the LinkedIn data download in the browser, shows a review, and creates a new resume|Medium: hostile file parsing in the browser, privacy claim|

## Before any code

1. Owner approves L1 to L4 and I1 to I3; the manager sets ADR 0058 and 0059 to Accepted and updates `docs/design/decisions.md`.
2. 0.6.2 only: the owner does the [LinkedIn app setup](../design/linkedin-sign-in.md#linkedin-app-setup) steps 1 to 6 before the enable step; the release itself does not need them.
3. 0.6.3 only: the owner sends the header rows and date shapes described in [facts to confirm](../design/linkedin-import.md#facts-to-confirm-before-the-build); the architect updates the mapping if they differ; frontend starts after that.

## 0.6.2 LinkedIn sign-in

|Role|Files|Work|
|-|-|-|
|backend|`apps/server/internal/auth/linkedin.go`, `linkedin_test.go`, `linkedin_adversarial_test.go`, new `apps/server/internal/auth/testdata/linkedin-discovery.json`; `apps/server/internal/config/config_test.go`|Tests first. No `code_challenge` or `code_verifier` for LinkedIn; `oauth2.AuthStyleInParams`; cancel on `user_cancelled_login`, `user_cancelled_authorize`, `access_denied`; discovery snapshot test; prod config test for `google,linkedin`. Google keeps PKCE.|
|backend|`apps/server/internal/uatmock/` (LinkedIn mode, fixtures, tests); `apps/server/cmd/mock-oauth/main.go`|Serve `/linkedin` beside `/google` with LinkedIn's rules and five accounts from the design; 401 `invalid_client` on `code_verifier` or Basic credentials; cancel buttons.|
|qa|`scripts/dev-https.sh`, `scripts/lib/dev-https-lifecycle.sh`, `scripts/lib/dev-https-caddy.sh`, `scripts/dev-https-test.sh`; new `deploy/dev-https-browser/linkedin.spec.ts` and its shard registration; `deploy/dev-https-browser/entry.spec.ts`|Harness: LinkedIn client values, issuer URL, Caddy authorize route, secret redaction, `PROVIDER_LOGIN_ENABLED=google,linkedin`. Proof cases from the design's Tests list. Manager confirms script ownership.|
|frontend|`apps/web/app/i18n/legal.ts` and its test; `apps/web/app/i18n/auth.ts`; the registration and verify-email notice components that offer Google|Privacy notice text in both languages as approved; collision copy with `{provider}`; notices offer every listed provider.|
|devops|`deploy/aws/modules/tasks/main.tf`, `variables.tf`; `deploy/aws/modules/identity/main.tf`; `deploy/aws/prod/variables.tf`, `prod.tfvars.example`; `deploy/aws/scripts/testdata/respond` if its fixtures need LinkedIn; `docs/runbooks/production.md` ("LinkedIn sign-in" section from the design's app setup and enable steps)|Accept `""`, `"google"`, `"google,linkedin"`; LinkedIn secrets only when listed; role reads the two new parameters only. Plan must show no destroy.|
|architect|`docs/design/security.md`, `docs/design/web.md`, `docs/architecture.md` if it names providers, `docs/plans/traceability/ac-auth.md` LinkedIn rows|Living docs match the built state: OAuth sentence, production may enable Google and LinkedIn, notices list providers.|
|reviewer|read-only|Adversarial pass: nonce bound and checked before any token use, no `code_verifier` or Basic credentials to LinkedIn, Google PKCE unchanged, collision writes nothing, cancel after state check, IAM limited to two parameters, secret redaction in harness logs.|

Enable after 0.6.2 is live and healthy, as its own step: the design's "Release and enable" steps 3 to 5. The owner does step 5.

## 0.6.3 LinkedIn import

|Step|Role|Files|Work|
|-|-|-|-|
|1|designer|`DESIGN.md` (new "LinkedIn import" section)|Spec for the instructions, file picker, review screen, notices, size meter, and cap state at 390 and 1280 px, both themes. Owner reviews the Vietnamese copy.|
|2|frontend|new `apps/web/app/import/linkedin/` (`zip.ts`, `csv.ts`, `dates.ts`, `text.ts`, `map.ts`, `build.ts`); new `apps/web/app/pages/app/import/linkedin.vue`; new `apps/web/app/components/import/`; new `apps/web/app/i18n/import.ts`; `apps/web/app/pages/app/new.vue` (entry link only); `apps/web/app/i18n/resume-create.ts`; `apps/web/app/i18n/legal.ts` and its test; new `apps/web/test/import/linkedin/` with the fixtures the design names; web source manifest|Tests first against the fixtures and the in-test hostile archives. No new package. No `v-html`.|
|3|qa|new `deploy/dev-https-browser/linkedin-import.spec.ts` and its shard registration|Proof cases from the design, with the request log assertions.|
|4|architect|`docs/design/web.md` (surfaces table: `/app/import/linkedin`), `docs/design/product.md` (journey)|Living docs match the built state.|
|5|designer|read-only|Finish review against the spec.|
|6|reviewer|read-only|Adversarial pass: every limit enforced on output bytes, not declared sizes; only allowlisted entries inflated; no request carries file bytes or leaves the origin; values render as text; request stays under 256 KiB.|

## Writing rules for briefs

Code, comments, tests, and living docs cite the design pages, ADR 0058, ADR 0059, or `AC-*` IDs, never this plan, its releases, or task names. No em dashes. Human-read Markdown keeps Prettier's 80-column wrap. Run `make pre-push` before every push; GitHub CI is the gate. Fixtures use synthetic people only.
