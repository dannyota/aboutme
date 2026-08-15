# Phase PA file structure and ownership

Each implementation path has one task author. Shared/generated paths belong to
the integration owner and are serialized.

## T00 — Authorities, budgets, traceability, and roots

- `docs/design/{README,product,security,data,api,deployment,web,decisions}.md`
- `docs/adr/0025-password-authentication-and-identity-linking.md`
- `docs/plans/budgets.md`
- `docs/plans/traceability/{README,ac-auth,ac-sec,ac-ops}.md`
- `packages/publicroots/public-roots.v5.json` and removal of v4 after all
  consumers regenerate.
- `scripts/generate-public-roots.mjs` and generated Go/Nuxt/Caddy/testdata
  consumers named by the current generator.
- `packages/publicroots/{app-build-sources.v1,renderer-build-sources.v1}.json`.
- Runtime v5 references in `scripts/dev-native.sh`, `scripts/dev-https.sh`,
  `scripts/dev-https-test.sh`, and `deploy/dev-https-browser/static-test.sh`.
- Route-table and public-root contract tests affected by the four new roots.

## T01 — Password storage

- `apps/server/migrations/00008_add_password_auth.sql`
- `apps/server/migrations/password_auth_test.go`
- `apps/server/sql/queries.sql`
- Generated `apps/server/internal/store/{db,models,querier,queries.sql}.go`
- New `apps/server/internal/store/password_contract.go` and tests.

## T02 — OpenAPI and generated client

- `docs/api/openapi.yaml`
- Generated `apps/web/app/api/generated/openapi.ts`
- `docs/api/test/password-auth.test.ts` and affected contract tests.
- `apps/web/test/api-client.test.ts` and
  `apps/web/test/nuxt/api-contract.test.ts` only for generated-operation parity.

## T03 — Password primitives

- New `apps/server/internal/accountemail/email.go` and `email_test.go`.
- New
  `apps/server/internal/password/{policy,blocklist,hibp,hash,admission,token}.go`
  and matching tests.
- New `apps/server/internal/password/blocklist/manifest.json`, `generate.mjs`,
  `generate.test.mjs`, generated `digests.bin`, and pinned
  `source/100k-most-used-passwords-NCSC.txt` plus `source/LICENSE`.
- Dependency window in `apps/server/go.mod`, `apps/server/go.sum`, and root
  `go.work.sum` belongs to the integration owner during this task.

## T04 — Encrypted outbox

- New `apps/server/internal/authmail/{payload,keyring,crypto,outbox}.go` and
  matching tests.
- New `apps/server/internal/authmail/testdata/` only for non-secret
  deterministic ciphertext fixtures.

## T05 — Provider and session fencing

- `apps/server/internal/auth/{handlers,session,google,github,linkedin,link,me}.go`
  and their existing/adversarial tests.
- New `apps/server/internal/auth/provider_identity.go` and tests.
- `apps/server/internal/auth/export_test.go` only for deterministic fences.

## T06 — Mail delivery and capture core

- New `apps/server/internal/authmail/{worker,sender,ses}.go` and tests.
- New `apps/server/internal/mailcapture/{server,store}.go` and tests.
- New `apps/server/cmd/mail-capture/{main,main_test}.go`.
- SES dependency changes in `apps/server/go.mod`, `apps/server/go.sum`, and root
  `go.work.sum` are a serialized integration-owner subwindow.

## T07 — Password rate policies

- New `apps/server/internal/auth/password_rate.go` and tests.
- `apps/server/internal/api/ratelimit.go` and tests only if the exact bounded
  outcome-aware primitive cannot be composed without a generic addition.

## T08 — Password HTTP service

- New `apps/server/internal/auth/password_{types,service,handlers}.go` and
  tests.
- New `apps/server/internal/auth/password_adversarial_test.go` and
  `password_race_test.go`.
- `apps/server/internal/auth/{csrf,errors}.go` and tests only for the new closed
  password route chain/error vocabulary; existing route mappings stay intact.

## T09 — Config, composition, and local lifecycle

- `apps/server/internal/config/{config,config_test}.go`
- `apps/server/cmd/server/{main,main_test}.go`
- `scripts/dev-native.sh`, `scripts/dev-https.sh`, `scripts/dev-https-test.sh`
- `deploy/dev-https-browser/static-test.sh`
- `.env.example` and root `Makefile`.
- `docs/runbooks/native-development.md`.

## T10 — Unauthenticated password pages

- `apps/web/app/pages/{login,register,verify-email,forgot-password,reset-password}.vue`
- New `apps/web/app/components/auth/PasswordField.vue` and tests.
- New `apps/web/app/composables/usePasswordAuth.ts` and tests.
- `apps/web/app/assets/css/auth.css` and the smallest post-P4 import point.
- `apps/web/test/{login,register,verify-email,forgot-password,reset-password}.test.ts`
  using the exact existing/new filenames.

## T11 — Password settings

- `apps/web/app/pages/app/settings/sessions.vue` and its tests.
- New `apps/web/app/components/auth/PasswordSettings.vue` and tests.
- `apps/web/app/composables/useAuth.ts` and tests for `hasPassword`/PUT.
- The smallest integrated app navigation/account-control path if a security
  settings link is needed; no shell/theme redesign.

## T12 — Native HTTPS UAT

- `deploy/dev-https-browser/password-auth.spec.ts`
- `deploy/dev-https-browser/run.sh`
- `deploy/dev-https-browser/playwright.config.ts`
- `deploy/dev-https-browser/static-test.sh`
- Existing `auth.spec.ts` only when shared setup must preserve provider proof.
- New server fixture command/tests under
  `apps/server/cmd/password-auth-fixture/`.
- Root `Makefile` password-UAT target and bounded evidence schema.
- Phase evidence under `docs/plans/phase-pa/` after a real run.

## Root-owned records after T12

The integration owner updates `docs/plans/implementation-plan.md`,
`docs/plans/traceability/`, `docs/architecture.md`, the authentication/local
development runbook, Phase PA state, and final evidence before fresh review.
Approved inputs and completed phase records are not rewritten.

T00, T09, and T12 intentionally repeat integration paths under the same
integration owner. T00 performs the atomic v5 registry cutover. T09 later adds
mail-capture lifecycle behavior and its root Make targets from that integrated
base. T12 finally extends the browser runner/static test and root Makefile with
the password UAT mode. These windows are sequential; no two authors own them
concurrently. T05 owns `auth/handlers.go`; T08 registers its independent
`PasswordService` from T09 composition and does not reopen that file.

## Compile dependency direction

```text
accountemail -> password, auth provider creation, password auth
password -> auth password service
store -> authmail, auth provider/session, password auth
authmail -> auth password service, server composition
mailcapture -> cmd/mail-capture only
auth password service -> api router composition
OpenAPI generated TypeScript -> Nuxt password pages/settings
```

`accountemail`, `password`, and `authmail` do not import `auth`, `api`,
`cmd/server`, Nuxt, Caddy, or lifecycle scripts. `authmail` receives store and
sender capabilities through exact narrow interfaces and never calls auth HTTP
handlers.
