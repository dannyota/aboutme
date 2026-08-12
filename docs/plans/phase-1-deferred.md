# P1.1 — Authentication contract closure

Status: **Tasks 0–3 landed; browser and phase gates remain** (verified at
`fa53fd2`, 2026-08-12).

P1.1 closes the remaining differences among the authentication server, settings
UI, OpenAPI, and acceptance evidence. It is a hard predecessor of P2B and adds
no migration.

## Authority

- [Security design](../design/security.md)
- [API design](../design/api.md)
- [ADR 0014: privileged OAuth starts](../adr/0014-oauth-start-methods.md)
- [ADR 0015: session rotation delivery](../adr/0015-session-rotation-delivery.md)
- [Current OpenAPI](../api/openapi.yaml)

## Current baseline

| Concern                                                    | Current state                                                                  | Required closure                                      |
| ---------------------------------------------------------- | ------------------------------------------------------------------------------ | ----------------------------------------------------- |
| Auth route limits, transaction reaping, and rejection logs | Implemented and tested                                                         | Reverify in the final auth gate                       |
| Privileged OAuth start                                     | Settings uses bodiless CSRF POST and validates the returned provider URL       | Run the bounded browser proof and final phase gates   |
| Session rotation delivery                                  | Successor-first-use grace and the one-retry CSRF flow are implemented          | Reverify lineage and lost-delivery cases              |
| Identity order                                             | SQL, `/me`, and settings use deterministic `(created_at, id)` order            | Reverify the live-database and settings cases         |
| Contract evidence                                          | OpenAPI, generated client, server, UI, and static guards agree at the baseline | Run contract drift checks and frozen phase acceptance |

## Task state

| Task | State  | Landed evidence                                                                                                            |
| ---- | ------ | -------------------------------------------------------------------------------------------------------------------------- |
| 0    | LANDED | Frozen web privileged-start matrix and live-database equal-time `/me` ordering test                                        |
| 1    | LANDED | Settings privileged-start POST flow, provider-bound URL checks, CSRF retry coverage, and author component/composable tests |
| 2    | LANDED | `(created_at, id)` sqlc query, generated output, author `/me` test, and frozen live-database test                          |
| 3    | LANDED | OpenAPI contract assertions and the package-wide GitHub no-OIDC callback-path guard                                        |

The bounded Playwright proof, integrated phase defect review, full checks,
connected scan, and frozen acceptance run remain open. These gates must pass at
one unchanged candidate before P1.1 closes.

## Task 0 — Freeze independent adversarial tests

Before Tasks 1 and 2 authors inspect an implementation diff, one fresh test-only
worker derives its cases from the authority documents and P11-001 through
P11-005. It owns only these new files:

- `apps/web/test/sessions-privileged-start-adversarial.test.ts`
- `apps/server/internal/auth/me_order_adversarial_test.go`

The web test mounts the settings surface as a black box. It proves link and
reauthentication use bodiless CSRF-protected POST requests, never fall back to
GET, preserve the one-retry CSRF rule, validate `authorizeUrl`, and navigate
only after a valid response. The live-database test inserts equal-time
identities in reverse UUID order and proves `/me` returns `(created_at, id)`
order.

The test worker records that matrix before reading product code or author tests.
It may then inspect existing harness code only to make the new tests compile.
The integration owner starts or verifies the one shared test database before the
worker runs:

```sh
(cd apps/web && npm test -- test/sessions-privileged-start-adversarial.test.ts)
(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL="${TEST_DATABASE_URL:-postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable}" go test ./internal/auth -run '^TestMeIdentityOrderAdversarial$' -count=1 -v)
```

Both tests must fail for the missing correction before implementation starts.
They then freeze. Tasks 1 and 2 authors may run these files but never edit them.
A contract defect in a frozen test stops dispatch for a fresh test-only
correction; it is not handed to an implementation author.

## Task 1 — Settings-page POST flow

Own `apps/web/app/composables/useAuth.ts`,
`apps/web/app/pages/app/settings/sessions.vue`,
`apps/web/test/{useAuth,useAuth-csrf-rotation,sessions,sessions-csrf-gating}.test.ts`,
and the bounded browser evidence. Reuse `useAuth().mutate` so the call carries
the current CSRF token and performs the existing one-time token refresh. Change
the helper to add `Content-Type: application/json` only when `body` is present;
the privileged bodiless POST must omit that header. Existing JSON mutations must
retain it.

For link and reauthentication:

1. Call `POST /api/v1/auth/{provider}/start?purpose=link|reauth` with no body.
2. Read `{data:{authorizeUrl}}` from the successful response.
3. Parse an absolute URL, reject credentials, fragments, malformed values, and
   every scheme except HTTPS. Match the provider to its fixed production
   authorization host/path. The only HTTP exception is an exact same-origin
   loopback URL under `/__uat/oauth/` while the current origin is also loopback;
   it exists for the isolated local harness. Then perform a top-level browser
   navigation. `javascript:`, `data:`, foreign loopback targets, and a valid
   HTTPS URL for the wrong provider all fail closed.
4. Keep the existing error state when the request, response shape, or navigation
   fails. Never fall back to a privileged GET.

Tests cover every provider, both purposes, one CSRF refresh, a second CSRF
failure, malformed success data, request failure, and preserved session revoke
actions. They assert a bodiless start has no `Content-Type`, while JSON
mutations still use `application/json`.

The browser check uses the project Playwright MCP server pinned in `.mcp.json`.
Start `make dev-native`, then use the pinned server's `browser_run_code_unsafe`
tool in a fresh isolated context to register `page.route` handlers for
`/api/v1/me`, `/api/v1/sessions`, and `/api/v1/auth/*/start` before opening
`http://localhost:20080/app/settings/sessions`. Exercise one link control and
one reauthentication prompt. For each start request, record the method, complete
URL, headers, and body; fulfill
`{"data":{"authorizeUrl":"http://localhost:20080/__uat/oauth/p1-authorized"}}`;
and assert the page performs that top-level navigation. The `unsafe` suffix is
the exact tool name exposed by the repository-pinned `@playwright/mcp@0.0.78`;
it does not authorize code outside this bounded browser procedure. The request
must be POST, carry the query purpose and CSRF header, have no body, and omit
`Content-Type`. Save the MCP accessibility snapshot and network evidence under
`.superpowers/acceptance/p1.1/<commit>/<run-id>/`. Full provider round trips
remain part of P9 HTTPS UAT.

## Task 2 — Deterministic identity order

Own a serialized source window for `apps/server/sql/queries.sql` and the focused
store and `/me` tests. The integration owner runs sqlc once and applies the
reserved generated output.

Change `ListIdentitiesByUserID` to `ORDER BY created_at ASC, id ASC`. Add a
same-timestamp fixture whose insertion order differs from UUID order. Prove the
store, `/me`, and settings default provider all use the same result. Run
`make sqlc-gen` once and inspect the generated diff before `make sqlc-check`.

## Task 3 — Contract and guard confirmation

OpenAPI must state all of these together:

- GET starts login only and rejects a privileged purpose without creating a
  transaction.
- POST is authenticated, CSRF-protected, bodiless, and takes `purpose` in the
  query string.
- POST returns `{data:{authorizeUrl}}`; the caller navigates after success.
- Shared callback errors do not claim that privileged GET starts can create link
  or reauthentication transactions.
- `/me` states that identities are ordered by `(created_at, id)`, oldest first.

The integration owner regenerates the TypeScript client only if the source
changes. Extend the static GitHub no-OIDC guard to cover the complete shared
callback path while excluding the Google and LinkedIn OpenID Connect files.

## Execution and ownership

Task 0 freezes before Tasks 1 and 2 start. Tasks 1 and 2 may then run in
parallel because their files are disjoint. Task 3 runs after both against the
integrated contract. Generated client/sqlc output, root manifests, and
acceptance records remain integration-owner files; Tasks 2 and 3 own their named
source files.

| Task | Exclusive author paths                                                                                                                                       |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 0    | `apps/web/test/sessions-privileged-start-adversarial.test.ts`; `apps/server/internal/auth/me_order_adversarial_test.go`                                      |
| 1    | `apps/web/app/composables/useAuth.ts`; `apps/web/app/pages/app/settings/sessions.vue`; the four named web tests above; git-ignored browser evidence          |
| 2    | `apps/server/sql/queries.sql`; `apps/server/internal/auth/me_test.go`; generated sqlc output is integration-owner only                                       |
| 3    | `docs/api/openapi.yaml`; `docs/api/test/openapi.test.ts`; `apps/server/internal/auth/github_adversarial_test.go`; generated client is integration-owner only |

Workers report their diffs and exact check output without staging or committing.
The integration owner verifies and integrates only these paths.

Authentication, sessions, and CSRF are high risk. Task 0 supplies the fresh
spec-derived black-box tests and freezes them before the implementation authors
start. The implementation authors also write their own failing tests first. A
different fresh reviewer inspects the integrated diff; authors fix findings and
an independent reviewer rechecks them.

## Acceptance and checks

P1.1 closes only when one unchanged candidate satisfies all of these:

- No settings control starts link or reauthentication with GET.
- The browser POST flow and server route agree on method, query, CSRF, response,
  and navigation.
- Equal-time identities have one deterministic `(created_at, id)` order through
  SQL, `/me`, and the settings UI.
- Login GET, privileged POST, callbacks, session rotation, device revocation,
  and no-oracle errors retain their existing security properties.
- OpenAPI, generated client, code, and tests agree.
- The frozen [P1.1 acceptance catalog](phase-1-1-acceptance-catalog-r1.md)
  remains unchanged during the run.

Required checks:

```sh
make sqlc-check
make api-check
make server-build server-vet server-test
make web-lint web-typecheck web-test web-build
make ci
make scan
```

Run the bounded Playwright MCP procedure described in Task 1 and record the
configured MCP package, Chrome version, calls, and evidence hashes. The phase
defect review and frozen acceptance run must both pass at the same commit.

## Downstream handoffs

| Constraint or debt                                                                                      | Owner                               |
| ------------------------------------------------------------------------------------------------------- | ----------------------------------- |
| Authenticated Nuxt fetches stay browser-only so server rendering cannot lose a rotated successor cookie | P4 API composable and lint boundary |
| A CSRF refresh retry reuses the same idempotency key                                                    | P2B write client and P4 autosave    |
| Native mobile adds a separate bearer-authenticated start; it never weakens cookie CSRF                  | P11                                 |
| Dead-session retention, auth audit records, and scheduled cleanup                                       | P8 privacy                          |
| Session revocation closes active Server-Sent Event streams                                              | P6A                                 |
| Account deletion proves the users-to-sessions cascade and rotation lineage behavior                     | P8 privacy                          |
| Provider response-size limits and real-provider failure diagnostics                                     | P9A security and operations gate    |
| `internal/user` either gains a production owner or its drift checks move to `internal/store`            | P8 account lifecycle                |

The exact application origin remains one value. Session cookies have no
`Domain`, CSRF compares the serialized origin exactly, and provider redirect
URIs derive from that origin. Production redirects `www` to the apex before
authentication. Native HTTP development cannot prove authenticated browser flows
because the cookies are always `Secure`; P9 uses the isolated HTTPS origin.
