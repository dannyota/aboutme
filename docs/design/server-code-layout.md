# Server code layout

Status: proposed. Change source organization and dependencies only. Preserve
HTTP/MCP bytes, database operations, cookies, limits, logs and lifecycle. No
migration, OpenAPI, schema, dependency upgrade, runtime flag or UI copy changes.

Shorter files and reusable protocols cost more files and adapter types.
[Security](security.md), [API](api.md), [data](data.md) and their accepted auth,
provider, second-factor and MCP ADRs retain authority. Keep packages stable for
mechanical splits; new packages serve only the dependency boundaries below.

## Measurements and file splits

Measured 2026-11-08 using `git ls-files` and `wc -l`, including blanks/comments.
Keep existing test/generated-path exclusions; sqlc exemption requires the
recognized generated header in the first five lines, never a `.sql.go` suffix.
`internal/store/queries.sql.go` (5,542), `querier.go` (791) and
`second_factor_totp.sql.go` (728) are generated, including `querier.go` despite
its ordinary filename. The authored server inventory above 400 follows.

Paths use `apps/server/`; unqualified targets are siblings. Estimates include
imports/comments. Aim for 200 to 350 lines, ceiling 400; shorter cohesive files
are allowed. Preserve statement/defer order and whole transactions.

| Source                                    | Lines | First code ceiling that catches it | Target files and concern                                                                                                                                                                                                                                                      |
| ----------------------------------------- | ----: | ---------------------------------: | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cmd/server/main.go`                      |   674 |                                600 | Keep `main.go` for dispatch, graph, listeners and shutdown (~200); move construction to the focused `internal/app/` files named below (each below 400), keeping the graph and existing effect order in main.                                                                  |
| `internal/auth/handlers.go`               |   666 |                                600 | `service.go`: Service, constants and NewService (~210); `routes.go`: registration and session middleware (~160); `google_callback.go`: Google start/callback (~200); `provider_response.go`: shared completion, redirects and closed logging (~140).                          |
| `internal/secondfactor/totp_service.go`   |   652 |                                600 | `totp_enrollment.go`: start, complete and policy handle (~260); `totp_verification.go`: pending completion, budget and lazy re-encryption (~220); keep decoding, state, removal and row helpers in `totp_service.go` (~230).                                                  |
| `internal/secondfactor/webauthn.go`       |   646 |                                600 | `webauthn.go`: RP and option construction (~220); `webauthn_decode.go`: strict envelopes, base64 and transport decoding (~280); `webauthn_verify.go`: library user adapter, verification and counter projection (~200).                                                       |
| `internal/resumeapi/photo.go`             |   638 |                                600 | `photo_upload.go`: intake, candidate and replacement preparation (~200); `photo.go`: routes, owner read, crop/delete handlers and validators (~270); `photo_decode.go`: multipart observer, crop decoder and normalization errors (~220).                                     |
| `internal/secondfactor/service.go`        |   630 |                                600 | `service.go`: options, construction and state reads (~250); `passkey_enrollment.go`: start, completion and persistence (~200); `passkey_verification.go`: assertion and counter events (~150); `passkey_removal.go`: removal and factor-change commit (~100).                 |
| `internal/auth/second_factor_handlers.go` |   612 |                                600 | `second_factor_handlers.go`: options, routes, common admission, error and response helpers (~290); `second_factor_pending_http.go`: pending status/options/completion (~170); `second_factor_account_http.go`: state, registration, removal and recovery regeneration (~200). |
| `internal/printrender/browser.go`         |   593 |                                500 | `browser_process.go`: environment, allocator, supervisor and join proof (~230); `browser.go`: ready/render lifecycle, attempt state and capture (~210); `browser_navigation.go`: CDP navigation, paused requests and callbacks (~210).                                        |
| `cmd/mcp-workflow/workflow_runner.go`     |   589 |                                500 | `workflow_runner.go`: run/create/reconcile and grant capture (~320); `workflow_photo.go`: copy/upload/crop and write reconciliation (~150); `workflow_completion.go`: final verification, revocation and journal (~150).                                                      |
| `internal/oauthsrv/token_endpoint.go`     |   583 |                                500 | `token_endpoint.go`: handler, admission identity and response mapping (~210); `token_form.go`: bounded decoding and exact field sets (~140); `code_exchange.go`: locked code exchange (~160); `refresh_exchange.go`: locked refresh and client-state errors (~180).           |
| `internal/auth/session.go`                |   560 |                                500 | `session.go`: manager, issue and authentication (~230); `session_rotation.go`: winner, successor and predecessor grace (~160); `session_revocation.go`: revoke, epoch replacement, proof checks and token helpers (~220).                                                     |
| `internal/authmail/worker.go`             |   512 |                                500 | `worker.go`: constructor, run loop and claim (~220); `worker_delivery.go`: send, terminal/requeue decisions and scope lock (~230); `worker_cleanup.go`: expired leases, cleanup transactions and backoff (~120).                                                              |
| `internal/accountapi/delete.go`           |   506 |                                500 | `delete.go`: HTTP, plan preparation and public coordinator (~260); `delete_transaction.go`: locked mutation, self-proof and commit classification (~280).                                                                                                                     |
| `internal/privacyretention/worker.go`     |   504 |                                500 | `worker.go`: config, construction, result logging (~180); `idempotency_cleanup.go`: bounded expiry and advisory lock cleanup (~160); `retention_cleanup.go`: retention pages, OAuth cleanup and backlog (~240).                                                               |
| `internal/accountapi/export.go`           |   499 |                                400 | `export.go`: routes, request admission, bounded snapshot and envelope (~300); `export_document.go`: document/photo projection, portable providers and validation (~230).                                                                                                      |
| `internal/config/config.go`               |   494 |                                400 | `config.go`: Config and defaults (~190); `load.go`: Load/LoadEnv and construction order (~200); `origin.go`: port, environment, log level, origin and listen parsing (~150).                                                                                                  |
| `cmd/dev-seed/seed.go`                    |   470 |                                400 | `seed.go`: config, dispatch and database seed/cleanup (~250); `seed_document.go`: document split, photo removal and strict JSON scanning (~250).                                                                                                                              |
| `internal/oauthsrv/clients.go`            |   465 |                                400 | `service.go`: dependencies, instance and constructor (~140); `clients.go`: registration handler and response (~170); `registration_decode.go`: strict JSON, ignored-member policy and redirect checks (~200).                                                                 |
| `internal/resumeapi/writesafety.go`       |   445 |                                400 | `writesafety.go`: mutation types, binding and HTTP admission (~250); `write_transition.go`: executeTransition and its existing commit/recovery ordering (~230).                                                                                                               |
| `internal/resumeapi/structure.go`         |   441 |                                400 | `structure.go`: routes, command decoding, targets and preparation (~210); `structure_layout.go`: raw layout, ordered mutations and array helpers (~260).                                                                                                                      |
| `internal/viewcount/counter.go`           |   428 |                                400 | `counter.go`: types, construction and Start (~220); `collect.go`: Collect, judge, network windows and ObserveHTML (~240).                                                                                                                                                     |
| `internal/printrender/pdf.go`             |   427 |                                400 | `pdf_metadata.go`: date updates, xref and info dictionary (~220); `pdf_tokens.go`: tokenization, references, date and decimal grammar (~240).                                                                                                                                 |
| `internal/auth/password_rate.go`          |   420 |                                400 | `password_rate.go`: policies, public admission and HMAC keys (~240); `password_failure_budget.go`: failure map, overflow and monotonic clock (~210).                                                                                                                          |
| `internal/publicformat/markdown.go`       |   418 |                                400 | `markdown.go`: public projection, plain text, links and dates (~260); `markdown_richtext.go`: rich HTML tree conversion and lists (~190).                                                                                                                                     |
| `internal/mailcapture/server.go`          |   404 |                                400 | `server.go`: capture/messages/viewer handlers and loopback authentication (~250); `client.go`: sender, endpoint validation and strict message decoding (~190).                                                                                                                |
| `internal/auth/sessions_handlers.go`      |   401 |                                400 | `sessions_handlers.go`: collection/list and logout (~210); `sessions_revoke_http.go`: per-session/all-session revocation, locked checks and error mapping (~220).                                                                                                             |

## Thin command and construction

Add `internal/app` for aboutme construction. Domains never import it.
Constructors accept explicit dependencies and return concrete components; typed
dependency structs keep longer signatures clear. They read no environment, open
no HTTP listeners and start no additional worker loops. Preserve existing
constructor I/O, acquisitions and failure order; main owns cleanup immediately
on acquisition. Do not merge the separate provider, password, factor and route
session managers.

Move `newPasswordAuth`, `newSecondFactorRoutes`, `newAgentAccessRoutes` and
`newMediaBackend` into focused `app/password_auth.go`, `second_factor.go`,
`agent.go` and `media.go`. Move public runtime parsing, readiness, logging,
print construction, cards, view counts and cache observation into `runtime.go`,
`readiness.go`, `logging.go`, `print.go`, `preview_cards.go`, `view_counts.go`
and `discovery_evict.go`. Add focused reader/account/resume/router constructors
in `public.go`, `account.go`, `resumes.go`, `router.go`. Move construction tests
with their subjects; preserve operator helpers' callers and distinct exit codes.

Main's ordered outline below abbreviates arguments only. Each call checks its
error before the next step; each resource keeps its current defer/join owner.

```text
main: dispatch(args); preserve existing exit-code mapping
run:
  cfg = config.LoadEnv(); logger = app.NewLogger(cfg)
  ctx, stop = signal.NotifyContext(...); defer stop()
  pool = store.NewPool(ctx, cfg.DatabaseURL); defer pool.Close(...)
  queries = store.New(pool); state = app.NewPublicState(ctx, queries)
  runtime = app.ParsePublicRuntime(cfg)
  providers = auth.NewService(logger, cfg, pool)
  passwords = app.NewPasswordAuth(ctx, logger, cfg, pool, queries)
  media = app.NewMedia(ctx, cfg); projector = docmigrate.NewIdentityProjector()
  reader = app.NewReader(queries, projector, state, media, runtime)
  showcase = app.NewShowcase(pool, reader, projector, logger)
  showcase.RecomputeAll(ctx)
  cache = publiccache.New(...); renderer = directrender.New(...)
  print = app.NewPrint(...); defer closePrintQueue(); private = print.Handler
  sessions = auth.NewSessionManagerWithPool(pool)
  account = app.NewAccount(..., sessions, ...); providers.SetAccountDeleteHandler(...)
  cards = app.NewPreviewCards(...); hub = realtime.NewHub(...); defer hub.Close()
  viewPassKey = app.DecodeViewPassKey(cfg)
  streams = app.NewStreams(..., sessions, ...); views = app.NewViewCounts(...)
  public = app.NewPublic(...); readiness = app.NewReadiness(...)
  resumes = app.NewResumes(..., sessions, ...)
  agents = app.NewAgents(ctx, ..., sessions, resumes)
  factors = app.NewSecondFactor(...)
  router = app.NewRouter(providers, passwords, account, public, resumes, agents, ...)
  open public and private-print listeners; defer their existing cleanup
  start realtime/mail; check TOTP health; start cards, views and health ticker
  servePair(...); cancel and join workers; unwind existing defers
```

Keep view flushing alive through request drain; preserve print teardown,
readiness probes, contexts and timeouts. Command sibling `serve.go` holds
serving helpers; main retains lifecycle and the visible graph, never one
`app.Run(cfg)`. Startup coverage follows the accepted
[tooling transition](local-tooling-simplification.md#production-runtime-coverage).

## Reuse boundaries

Keep internal packages, with no public module or compatibility commitment. A
receiving repository copies them, adjusts module imports, preserves license
notices, and supplies persistence, host authorization and resource operations.

| Boundary     | Imports and coupling present today                                                                                                                                                                                        | Proposed direction                                                                                                                                                                                                                             |
| ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `oauthsrv`   | `store`/pgx in transactions; `auth` and `api` in consent/authorize; `api` client IP and limiter; `config.CIMDConfig`; standard `log/slog` in rollback diagnostics. No `resumeapi` import.                                 | Core owns protocol and transactions through its own ports. `internal/oauthstore` maps sqlc/pgx and locked auth proof. `internal/oauthhttp` owns cookie middleware, API envelopes and browser redirects. Config maps to local options in `app`. |
| `mcpapi`     | `store.OAuthQueries`/pgx in bearer; `oauthsrv` token/scope types; `resumeapi` in server and every tool group; `api.BoundedRateLimiter`. No direct `config` or logger import.                                              | Core keeps bearer admission and stateless SDK transport. `internal/resumemcp` owns tools, descriptions, branding, closed resume error mapping and the resume executor adapter.                                                                 |
| Google login | `google.go` itself imports only context/fmt, go-oidc and oauth2. Its Service, start/callback and shared helpers couple it to `store`, `api`, `config`, `accountemail`, `slog`, sessions, pending factors and view passes. | `internal/googleoidc` owns discovery/exchange/verification. `auth` retains transactions, cookies, account/link/view decisions, logging and session/factor issue. GitHub/LinkedIn stay in `auth`.                                               |

### OAuth persistence and host ports

Define ports in `oauthsrv`, beside the consuming concern. Use existing method
names and argument/result shapes below, replacing every `store` type with an
owned value type in `oauthsrv`; no aliases to generated types, pgx, auth or api.
Copy required fields without changing nullability, UTC times, UUID values,
digests, epochs, row counts or missing-row semantics. Parameter structs retain
existing names with the OAuth prefix; adapters translate field by field.

| Consumer interface | Methods (all database methods take `context.Context` first)                                                                                                                  |
| ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `ClientReader`     | `GetDCRClient`, `GetOAuthClientByMetadataURL`, `GetOAuthClient`                                                                                                              |
| `ClientWriter`     | `CreateOAuthClient`, `GetOAuthClientForUpdate`, `UpsertOAuthMetadataClient`, `TouchOAuthClientLastUsed`                                                                      |
| `ClientCollector`  | `ListIdleOAuthClientCandidates`, `DeleteOAuthClients`                                                                                                                        |
| `CodeReader`       | `GetOAuthAuthorizationCodeByDigest`                                                                                                                                          |
| `CodeWriter`       | `GetOAuthAuthorizationCodeByDigestForUpdate`, `CreateOAuthAuthorizationCode`, `ConsumeOAuthAuthorizationCode`                                                                |
| `GrantReader`      | `GetLiveOAuthGrant`, `ListLiveOAuthGrantsForUser`, `CountLiveOAuthGrantsForUser`                                                                                             |
| `GrantWriter`      | `GetOAuthGrantForUpdate`, `UpsertOAuthGrant`, `RevokeOAuthGrant`, `RevokeOAuthGrantForUser`                                                                                  |
| `TokenReader`      | `GetOAuthTokenAuthorityByDigest`                                                                                                                                             |
| `TokenWriter`      | `CreateOAuthToken`, `InsertRotatedOAuthToken`, `SupersedeOAuthToken`, `RevokeOAuthTokenFamily`, `RevokeOAuthTokensForGrant`                                                  |
| `SubjectLocker`    | `LockUser(ctx, userID uuid.UUID) (Subject, error)`; Subject carries ID and AuthEpoch only.                                                                                   |
| `ConsentAuthority` | `RequireRecent(ctx, userID, sessionID uuid.UUID, now time.Time) error`; transaction-bound adapter locks concrete session then factor policy and invokes existing proof rule. |
| `Transactions`     | `Begin(ctx) (Transaction, error)`; Transaction exposes the facets above plus `Commit(ctx) error`, `Rollback(ctx) error`. Functions accept only their required facets.        |
| `AddressSource`    | `ClientAddress(*http.Request) (netip.Addr, bool)`; adapter uses canonical proxy policy.                                                                                      |
| `Bucket`           | `Admit(time.Time, string) (bool, int)`, `Check(time.Time, string) (bool, int)`; existing bounded api limiter satisfies it.                                                   |

Read-only facets outside a transaction use pool-backed adapters; transaction
facets share exactly one pgx transaction and must never open another. Expose
`oauthstore.WrapTx` for caller-owned factor transactions; it cannot commit or
rollback. `RevokeGrantsForEpochChangeTx` accepts grant-reader/writer and token
revocation facets from that wrapper, retaining atomic epoch/session/mail work.
Cleanup of expired codes and terminal tokens stays in `privacyretention`.

Map `pgx.ErrNoRows` to `oauthsrv.ErrNotFound`, closed transactions to
`ErrTransactionClosed`, and auth proof errors to `ErrSessionInvalid` or
`ErrReauthRequired`; preserve `errors.Is` classifications. All other errors fail
closed and retain internal context, never raw HTTP exposure. Core retains PKCE,
epochs, expiry, replay and rotation decisions and lock order; adapters only
execute queries and the host-specific session proof. Commit replay revocation
even when returning invalid_grant, exactly as today. Refused grant admission
uses locked read-only validation and performs no business writes.

`oauthhttp.ConsentService` consumes OAuth-owned values through
`ConsentContext(ctx, userID, query)`,
`ConsentDecision(ctx, SessionRef, decision)`, `ListGrants(ctx, userID)` and
`RevokeGrant(ctx, userID, grantID)`, preserving result/error shapes.
`SessionRef` holds user/session IDs and epoch, never proof. Add
`Authorizer.Authorize(ctx, AuthorizeRequest, *SessionRef) (Authorization, error)`.
`oauthsrv` owns both values. Request contains raw query and canonical address;
core parses it, resolves one metadata snapshot, validates scope/resource/PKCE
and redirect, checks live grant coverage, and performs locked silent code issue.
The optional session comes only from existing optional-session middleware.

Authorization is a closed result with read-only accessors for action, redirect,
status, protocol error and retry delay. Only core constructs it. Actions are
login, consent, issued-code redirect, trusted-error redirect or direct
rejection. No adapter builds a destination from request parameters. Preserve
existing HTTP mapping: untrusted invalid input 400; trusted protocol failure 302
to the validated redirect; no session 302 to login; interactive consent and
issued code 302; stale proof falls back to consent; rate refusal 429 with
Retry-After; unexpected failure 500. Preserve each branch's headers and fixed
body bytes. `oauthhttp` keeps method dispatch and cookie/CSRF middleware; it
writes those core outcomes. Raw registration/token/revoke handlers remain in
core.

Keep admission reservation state in OAuth instances. Make its existing
`RegisterAdmission.AdmitRegister`, `TokenAdmission.AdmitToken`, `AdmitGrant` and
`FinishGrant` ports usable with exported opaque `GrantAttempt` and a closed
`GrantResult` enum; preserve arguments and release/failure/success semantics.
Inject separate Bucket instances through constructors for each existing policy,
including CIMD address/URL/global buckets; do not share debt accidentally. CIMD
owns its bounded cache, DNS/address policy and pinned-TLS fetcher unchanged; map
`config.CIMDConfig` to local `CIMDOptions`. Keep standard `*slog.Logger` where
needed, with fixed diagnostics. Do not invent a general logging interface.

### MCP ports

`mcpapi.BearerStore` has `Authority(ctx, digest [32]byte) (Authority, error)`
and `TouchLastUsed(ctx, tokenID uuid.UUID, now, before time.Time) error`.
Authority owns copies of the token/grant/user IDs, kind, scopes, epochs,
revocation/supersession and expiry fields checked in `bearer.go`. The adapter
maps sqlc absence to a local sentinel; lookup or touch failures remain 500.
Inject `ParseAccessToken func(string) ([32]byte, error)` backed by OAuth's exact
parser, and local scope values, so MCP need not import OAuth or pgx.

`mcpapi.ToolRegistrar.RegisterTools(*mcp.Server)` installs application tools.
Core owns private authority context and exposes read-only
`AuthorityFromContext(ctx) (Principal, error)`. Principal carries user, grant,
token IDs, digest and scopes. Only bearer middleware installs it; request JSON
never does.
`resumemcp.AgentExecutor.ExecuteAgent(ctx, Principal, Call) Response` uses
consumer-owned Call/Response shapes matching `resumeapi/validate.go`. Its
adapter reconstructs `resumeapi.NewAgentPrincipal` and calls the same kernel; it
retains transaction-time token/grant/epoch revalidation, never trusts an
admission snapshot to authorize a write. Move the existing fifteen tool schemas
and instructions unchanged. Pass SDK implementation metadata from `app`, with
aboutme's exact name, version, icons and title. Supply closed tool-error mapping
through `mcpapi.ToolErrors.Close(*mcp.CallToolResult)`, implemented by
`resumemcp`; transport framing and pre-tool HTTP errors stay in core. Inject
token/user Buckets; concurrency slots remain instance-owned in MCP. Another
application supplies its own registrar, resource executor, error mapper, bearer
store, parser, metadata and limits.

### Google ports

`auth.GoogleProvider` has `Discover(ctx) (*googleoidc.Authorization, error)` and
`VerifyCode(ctx, googleoidc.Exchange) (googleoidc.VerifiedIdentity, error)`. For
account and view starts, auth calls Discover before `begin`/`beginView`
generates secrets and inserts the transaction. Discovery failure creates no
transaction or cookie. Success returns an immutable, privately constructed
Authorization holding validated endpoints; its `AuthorizeURL(Start) string` only
formats the URL after begin succeeds, with no I/O, rediscovery or failure. The
copied package must preserve this ordering boundary and endpoint snapshot; tests
cover discovery failure before either begin path. `googleoidc` owns these types,
Start, Exchange, VerifiedIdentity, Profile, scope mode and closed errors, with
no host imports. Client implements the interface without host type aliases.
Start contains state, nonce, PKCE verifier, exact redirect and closed ScopeMode:
View selects only `openid`; Account selects `openid`, `profile`, `email` in the
existing order. Auth derives the mode from its trusted purpose, never arbitrary
requested scopes. Exchange carries code, expected nonce, verifier and the stored
redirect. VerifiedIdentity exposes subject and `Profile() (Profile, error)`;
Profile holds email, verified-email flag and name. Auth calls Profile only after
the view branch, preserving the viewer's no-profile-decode/no-account behavior.

`googleoidc.HTTPClient.Do(*http.Request) (*http.Response, error)` is the
transport port. Adapt it to oauth2/go-oidc's concrete client while retaining
cancellation and the ten-second timeout. Cache discovery per Client and retry
failures. Constructor receives credentials and client, not host config. Google
remains the production issuer; local mocks retain validated endpoints and
loopback-only, no-proxy transport. No request may override issuer or endpoints.
Auth consumes the one-use transaction and checks state before exchange, clears
the cookie on every exit, and maps closed failures to existing reasons.
New-account creation alone requires canonical verified email; preserve link and
existing-login rules.

## Go rules proposed for instructions/go.md

These proposed local rules use newer first-party references where Effective Go
has no guidance. Style edits cover moved/touched authored code only; generated
files remain generator-owned.

| Rule                                                                                                                                      | First-party basis                                                                                                                                                                                                                                                                                           | Current evidence and application                                                                                                                                                                                                             |
| ----------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Doc comment on every exported symbol, including fields, methods and each constant. Explain contract and units.                            | [Google doc comments](https://google.github.io/styleguide/go/decisions#doc-comments); [Effective Go commentary](https://go.dev/doc/effective_go#commentary). Per-field comments are a stricter local rule.                                                                                                  | `mcpapi.Principal` fields, `oauthsrv.CIMDRateError.Error`, and individual `TokenKindAccess`/`TokenKindRefresh` constants lack their own comments.                                                                                            |
| Add operation context with `%w` when callers should inspect the cause; use `errors.Is/As`. Preserve closed boundary errors and redaction. | [Google error wrapping](https://google.github.io/styleguide/go/best-practices#error-wrapping); [Effective Go errors](https://go.dev/doc/effective_go#errors); [Go wrapping guidance](https://go.dev/blog/go1.13-errors#whether-to-wrap).                                                                    | `oauthsrv.readSecretBody` returns a bare reader error; `auth.oidcProviderCache.discover` returns a bare discovery error. Wrap once at the useful boundary, never expose token/provider/SQL text.                                             |
| Pass context first for work that accepts it; propagate request cancellation. Framework handlers use request context.                      | [Google contexts](https://google.github.io/styleguide/go/decisions#contexts); [context docs](https://pkg.go.dev/context). Effective Go has no context rule.                                                                                                                                                 | Sampled service methods already comply. No first-position violation found in inspected paths; lifecycle detachment must retain its documented reason.                                                                                        |
| No package-level mutable runtime state. Inject clients, clocks, entropy, caches and locks.                                                | [Google global state](https://google.github.io/styleguide/go/best-practices#global-state); [Effective Go initialization](https://go.dev/doc/effective_go#initialization) describes initialization, not a ban.                                                                                               | `auth.providerHTTPClient`, `googleScopes`, `oauthsrv.oauthEntropyMu`, registration/redirect maps. Use instance state, fresh slices or switches; allow only the immutable exceptions in [owner decisions](#alternatives-and-owner-decisions). |
| Define small interfaces at the consumer, only for an actual dependency. Return concrete implementations normally.                         | [Google interfaces](https://google.github.io/styleguide/go/decisions#interfaces); [Effective Go interfaces](https://go.dev/doc/effective_go#interfaces).                                                                                                                                                    | `store.OAuthQueries` is producer-owned and broad; `mcpapi.AgentExecutor` leaks producer types. Transaction facets are an explicit encapsulation exception to concrete returns.                                                               |
| Prefer named table-driven subtests for repeated cases; keep distinct scenarios and concurrency tests separate.                            | [Google table-driven tests](https://google.github.io/styleguide/go/decisions#table-driven-tests); [Go table tests](https://go.dev/wiki/TableDrivenTests). Effective Go has no testing section.                                                                                                              | `auth/session_test.go` has separate issue-shape tests suitable for a table; its race/fence tests should remain separate. No wholesale test rewrite.                                                                                          |
| `cmd/*/main.go` parses, wires and owns lifecycle only; constructors live under `internal`.                                                | [Google least mechanism](https://google.github.io/styleguide/go/guide#least-mechanism); [Effective Go package names](https://go.dev/doc/effective_go#package-names) does not mandate command layout; [Go server organization](https://go.dev/doc/modules/layout#server-project) supports internal packages. | This is a local architecture rule. `cmd/server/main.go` contains media selection, runtime validation, agent route assembly and readiness fixtures.                                                                                           |

## Stepwise length limits

Keep existing test/generated-path exemptions. Exempt sqlc outputs only when the
first five lines have `Code generated .* DO NOT EDIT`, including `querier.go`.
Add no `.sql.go` filename pattern; SQL sources stay handwritten. Fixtures prove
oversized header-bearing sqlc passes and handwritten `custom.sql.go` without
that header fails, plus ordinary Go/Markdown and exact/plus-one limit cases.

Each server-ceiling PR changes SERVER_CODE_MAX and splits its exact Go cohort
under `apps/server` plus callers and tests: 600 catches >600; 500 catches
501..600; 400 catches 401..500. Server files appear above; other paths below are
deferred. Produce splits <=400 and re-measure before each PR, including new
files. Update the engineering standard in that PR. No unfinished-code
exemptions.

| CODE_MAX | Additional repository paths caught (lines)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
| -------: | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
|      600 | `apps/web/e2e/pdf-paint.ts` (700); `apps/web/app/i18n/editor-controls.ts` (690); `apps/web/e2e/pdf-display.ts` (688); `apps/web/app/components/editor/EditorShell.vue` (680); `apps/web/app/stores/resumes.ts` (660); `apps/web/app/components/resume/ResumeDocument.vue` (657); `apps/web/app/components/settings/SecondFactorSettings.vue` (656); `apps/web/app/editor/templateGroup.ts` (637); `apps/web/app/pages/app/settings/sessions.vue` (625)                                                                                                                                                                                                                                                                                                                                                                                                                                                             |
|      600 | `deploy/caddy/production/test.sh` (698); `deploy/dev-https-browser/second-factor-lib.ts` (696); `deploy/aws/scripts/deploy.sh` (691); `deploy/dev-https-browser/run.sh` (689); `deploy/dev-https-browser/editor-fixtures.ts` (669); `deploy/vn/scripts/deploy-host.sh` (631); `deploy/vn/scripts/restore-drill.sh` (618)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
|      600 | `scripts/mcp-owner-workflow.sh` (700); `scripts/dev-native.sh` (697); `scripts/server-runtime-smoke.sh` (644)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
|      500 | `apps/web/scripts/linkedin-pdf-shape.mjs` (591); `apps/web/app/editor/publishController.ts` (586); `apps/web/app/components/resume/PagedResume.vue` (580); `apps/web/app/components/editor/structure/StructurePanel.vue` (580); `apps/web/app/pages/_harness/render.vue` (579); `apps/web/app/import/linkedin/build.ts` (577); `apps/web/app/components/editor/customization/CustomizationPanel.vue` (570); `apps/web/app/pages/index.vue` (559); `apps/web/app/templates/catalog.ts` (540); `apps/web/app/utils/deploymentDocument.ts` (531); `apps/web/app/i18n/verify.ts` (531); `apps/web/app/pages/login/second-factor.vue` (530); `apps/web/app/import/linkedin/sections.ts` (508); `apps/web/app/pages/showcase.vue` (507); `apps/web/app/components/editor/EditorPreview.vue` (504)                                                                                                                        |
|      500 | `deploy/dev-https-browser/second-factor-pages.ts` (593); `deploy/vn/host/install.sh` (586); `deploy/vn/host/crowdsec-retention.sh` (531); `deploy/caddy/production/build/crowdsec_feed.go` (511)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                   |
|      500 | `scripts/dev-https-check.sh` (528)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
|      400 | `apps/web/app/i18n/guide.ts` (495); `apps/web/e2e/display-assertions.ts` (492); `apps/web/app/pages/app/import/linkedin.vue` (483); `apps/web/nuxt.config.ts` (482); `apps/web/app/public/public-resume.client.ts` (472); `apps/web/app/components/settings/TotpSettings.vue` (471); `apps/web/app/components/editor/PublishDialog.vue` (463); `apps/web/eslint.config.mjs` (455); `apps/web/app/pages/templates/index.vue` (452); `apps/web/app/utils/previewText.ts` (433); `apps/web/app/components/editor/forms/ContactList.vue` (429); `apps/web/app/composables/secondFactorSettings.ts` (425); `apps/web/app/i18n/editor-fields.ts` (415); `apps/web/app/editor/pdfDownload.ts` (414); `apps/web/server/utils/public-render/envelope.ts` (412); `apps/web/app/components/editor/list/CreateResumeDialog.vue` (412); `apps/web/app/editor/coordinatorSettle.ts` (409); `apps/web/app/i18n/legal-vi.ts` (407) |
|      400 | `deploy/vn/scripts/fence.sh` (463); `deploy/vn/host/crowdsec-privacy-notice-host.sh` (429)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                         |
|      400 | `packages/schema/validation/store.ts` (437)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
|      400 | `scripts/pre-push.sh` (479); `scripts/generate-public-roots.mjs` (468)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                             |

Lower MD_MAX 450 to 400, then 350, then 300 in separate PRs. Split these exact
cohorts by headings into indexed sibling pages <=300; retain inbound anchors as
short links. Keep accepted decisions in their topic ADR and move supporting
procedure detail only with owner approval and a History entry. Preserve research
evidence verbatim. Include link/engineering-standard updates and re-measure.

| MD_MAX | Markdown paths caught (lines)                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                          |
| -----: | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
|    400 | `docs/runbooks/production.md` (450); `docs/design/vietnam-tech-resumes.md` (450); `docs/design/mcp-cimd-authorization.md` (450); `docs/design/crowdsec-privacy-notification.md` (450); `docs/design/vmonitor-quota-renewal.md` (449); `docs/design/crowdsec-http-privacy.md` (449); `docs/design/vietnam-host-operations-delivery.md` (442); `docs/design/vietnam-host-operations.md` (439); `docs/design/local-tooling-simplification.md` (432); `docs/design/public-page-theme.md` (413); `docs/design/security.md` (404)                                                                            |
|    350 | `docs/design/ui/library-filters.md` (395); `docs/design/templates/contract.md` (391); `docs/design/server-code-layout.md` (391); `deploy/vn/README.md` (387); `docs/research/aws-cost/comparison.md` (384); `docs/design/deployment-transparency/README.md` (384); `docs/design/passkey-second-factor-contract.md` (380); `docs/research/aws-cost/workload.md` (375); `docs/design/deployment-transparency/visual.md` (374); `docs/design/link-previews.md` (372); `docs/design/linkedin-import-ui.md` (364); `docs/design/single-host-production.md` (361); `docs/design/vietnam-production.md` (360) |
|    300 | `docs/research/flowcv/api-surface.md` (331); `docs/design/web.md` (317); `docs/adr/0026-vietnam-hosted-production.md` (314); `docs/design/display-assertions.md` (312); `docs/design/budgets.md` (302)                                                                                                                                                                                                                                                                                                                                                                                                 |

Global cohorts touch frontend and devops, conflicting with small server-only
PRs. Keep global CODE_MAX at 700 during the server ratchet. The global tables
record deferred work; separate lane work must precede global limits.

## Ordering, security and verification

Split >600 and thin main; split 501..600; split 401..500; extract Google, OAuth,
then MCP in separate PRs. Apply the decided ceilings with their cohorts and run
Markdown ratchets separately. Keep each PR buildable, with its real
adapters/tests and no parallel auth path.

| Attacker / failure               | Invariant preserved across the move and adapter                                                                                                                                                                                                                              |
| -------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Anonymous visitor                | Full decode and bounds before effects; cookie-only consent versus bearer-only token/MCP routes; state, one-use transaction, PKCE and strict Google nonce before identity. CIMD retains pinned DNS/TLS, forbidden addresses, bounded cache and no fetch under database locks. |
| Signed-in cross-account attacker | Adapter carries concrete caller IDs; locked user/session/policy check before consent; every resume/photo operation rechecks owner and live authority in the shared kernel.                                                                                                   |
| Hostile resume content           | No second mutation or sanitizer path; renderer sandbox, media normalization, private print capability and public revocation fences retain exact calls and ordering.                                                                                                          |
| Stolen session or MCP token      | Epoch checks, code replay and refresh-family revocation survive adapters; resource mutation rechecks under locks. Grant revoke and factor change remain atomic. No new recovery or publish scope.                                                                            |
| Network attacker                 | Exact origin, Secure/HttpOnly host cookies, CSRF, strict provider/CIMD TLS, trusted-proxy address parsing and redacted logs remain at their existing edges.                                                                                                                  |
| Compromised container            | Interfaces are not isolation. Existing process credentials still grant existing authority; no new secret copies, mounts, telemetry or network listeners.                                                                                                                     |
| Database writer without keys     | TOTP associated-data binding and failure isolation remain. This refactor adds no integrity protection for arbitrary database forgery; opaque token digests alone do not defeat a writer who inserts a known token digest.                                                    |
| Leaked backup                    | No additional persisted data. Token/code digests and sealed mail/TOTP retain current protection; backup confidentiality still follows existing operations policy.                                                                                                            |

Host-managed keys keep their current consumers: provider credentials in Google's
Client, TOTP keys in factors, mail keys in mail services. Copies supply their
own credentials. Preserve recovery-code-only lost-factor recovery, row-local
TOTP key failure, pending-auth refusal and startup failure behavior.

No migration, data loss, token invalidation or older-client fallback is needed.
Wire contracts and existing rollback floors remain unchanged. Preserve budgets,
payload/worker bounds and cache counts; DTOs must not retain request secrets.

| Change                                        | Evidence required from primary/hosted CI                                                                                                                                                                                                            |
| --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Every code split / constructor move           | Existing package tests unchanged; `make server-build server-vet server-test`. `server-test` already runs `go test ./...` from `apps/server`. Run the configured golangci-lint gate; do not repeat tests for reassurance.                            |
| Persistence / OAuth ports                     | `make sqlc-check server-test-db server-test-integration server-migration-test`; existing rollback, duplicate exchange, refresh reuse, revocation, CIMD, grant-admission and epoch races against real adapters. No query or migration diff expected. |
| Google / auth / factors / command graph / MCP | `make dev-https-auth-check dev-https-editor-check dev-https-mcp-check dev-https-entry-check`; keep Google mock, linked-account, second-factor and both DCR/CIMD coverage. Existing Linux CI remains authoritative.                                  |
| Print / public / photo split                  | Existing renderer, PDF, write-safety and revocation tests; `make dev-https-public-check` and affected existing HTTPS proof shards. Preserve print supervisor process/join tests.                                                                    |
| Checker and Markdown ratchets                 | Primary runs `make pre-push`; checker fixtures plus `make operational-test-fast`; `node scripts/check-doc-links.mjs`; pinned Prettier and markdownlint on touched human docs.                                                                       |
| Reuse acceptance                              | Hosted scratch-module compile/test of copied core packages with fake ports and no aboutme imports; adapter integration tests preserve real database constraints. Existing protocol tests move with core, host proofs stay with adapters.            |

Extraction PRs must update explicit package selections in `Makefile` and any
workflow callers so moved DB tests in `app`, `oauthstore`, `oauthhttp` and other
new packages still run with required DB/race settings. The primary must assign
those shared files in the brief; ordinary `go test ./...` can skip DB tests.

Independent design review uses Sol xhigh. Under
[workflow](../../instructions/workflow.md), every security split or wiring move
uses Sol xhigh implementation and Astra high adversarial review, even when
mechanical. Standard work uses Terra xhigh implementation and Sol xhigh review.
The original reviewer confirms fixes. Classes below include each source's split
targets and tests; paths are relative to `apps/server/`.

| Route    | Files and boundaries                                                                                                                                                                                                                                                                                                                                                                 |
| -------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Complex  | `internal/auth/{handlers,second_factor_handlers,session,password_rate,sessions_handlers,google,view_purpose}.go`; `internal/secondfactor/{totp_service,webauthn,service}.go`; `internal/oauthsrv/{token_endpoint,clients}.go`; all Google/OAuth/MCP extraction sources and adapters                                                                                                  |
| Complex  | `cmd/server/{main,serve}.go` and all proposed `internal/app/` wiring; `cmd/mcp-workflow/workflow_runner.go`; `internal/resumeapi/{photo,writesafety,structure}.go`; `internal/accountapi/{delete,export}.go`; `internal/authmail/worker.go`; `internal/privacyretention/worker.go`; `internal/printrender/browser.go`; `internal/config/config.go`; `internal/mailcapture/server.go` |
| Standard | `cmd/dev-seed/seed.go`; `internal/viewcount/counter.go`; `internal/printrender/pdf.go`; `internal/publicformat/markdown.go`; documentation and length-checker changes                                                                                                                                                                                                                |

Reclassify standard briefs that touch security/contracts or span components.
Primary runs static checks; this sandbox runs no local tests. Every merge
follows existing landing gates; each deploy needs green main CI on that exact
commit. Release each completed refactor separately; no batch deployment gate.

## Alternatives and owner decisions

- Same-package splits are low risk but retain host coupling: use them first.
- Reject one application constructor: main must show dependencies and cleanup.

Decisions recorded 2026-11-09. Source: owner.

- Apply code limits to `apps/server` Go files first: 700 to 600, 500, then 400.
  Web and scripts keep 700 until a later step.
- Lower Markdown limits from 450 to 400, 350, then 300. Each step splits exactly
  the documents it catches.
- Keep adapter and core packages under `apps/server/internal`, with interfaces
  owned by consumers. Defer public Go modules until a second consumer exists.
- Use one root Go module, `danny.vn/aboutme`, as in the owner's other Go
  projects, with Go code in its current directories. The Caddy build recipe
  stays a nested module with Caddy's own dependency graph.
- Allow only immutable sentinel values (sentinel errors and compiled regular
  expressions) and embedded assets as exceptions to the package-level
  mutable-state ban. Never mutate or reassign them; preserve error identity.
