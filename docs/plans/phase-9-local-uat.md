# Phase 9 — Local manual UAT (execution plan)

> **Adopted Rev 1 (2026-08-04), owner decision.** UAT is performed by the
> main-session UAT executor (GPT-5.6 Sol), not by the human owner. This plan is
> prospective: its expected results are derived only from the design spec,
> OpenAPI contract, accepted ADRs, and traceability acceptance IDs immediately
> before the first run, then remain immutable for that run. The implementation
> informs only the user actions and fixtures used to exercise those contracts.

## Goal and role boundary

Prove that a release candidate works as a user experiences it before any AWS
resource is created. The main-session UAT executor (GPT-5.6 Sol) autonomously
operates the complete local application on this laptop through the
project-scoped Playwright MCP server. The human owner does not execute UAT.

A fresh independent reviewer verifies the report and artifacts without editing
product code, tests, snapshots, seeds, or UAT criteria. The integration owner
asks the human owner for AWS resource-creation authorization only after both
verdicts are `PASS`.

This plan distinguishes three gates:

1. Automated unit, integration, contract, E2E, accessibility, security, and
   phase-acceptance checks run while features are developed.
2. P9 local manual UAT uses real user-like browser actions against the complete
   Podman deployment.
3. P9A later reruns applicable scenarios and operations drills on AWS staging,
   after the human owner authorizes AWS resource creation.

## Hard preconditions

- [ ] The candidate is a clean, exact commit containing every P0–P8 web-v1
      feature, its reviewed migrations, and the completed PI code-only/local-IaC
      work; no working-tree product changes are present.
- [ ] Every affected build, unit, integration, contract, migration, security,
      scripted Playwright E2E, accessibility, and automated phase-acceptance
      gate is green at that commit.
- [ ] The local project MCP configuration is trusted and its Playwright server
      is available at the exact pinned version. Missing MCP tools make the run
      `BLOCKED`; the human owner is not asked to replace them by running UAT.
- [ ] Podman, `podman compose`, the resolved Chrome executable and SHA-256,
      Chrome and MCP versions, and the candidate's image IDs are recorded. The
      executor and evidence reviewer use that same browser binary; browser drift
      makes the run `BLOCKED`. `CADDY_HTTP_PORT=8080` is configured locally,
      without recording any `.env` value.
- [ ] The local UAT catalog is mapped to current acceptance IDs and frozen
      before deployment. It includes expected outcomes and required evidence.
- [ ] The UAT-only identity-provider service, endpoint-override contract, and
      production fail-closed tests described below are implemented and green.
- [ ] No AWS or Cloudflare mutation is in scope. AWS credentials are not needed
      and are not passed to the UAT stack or browser.

## Disposable full-stack deployment

Before this phase is executable, its implementation task adds safe integration-
owner targets for an isolated `aboutme-uat` Podman Compose project:

- `make uat-up` combines `deploy/compose.yml` with a reviewed
  `deploy/compose.uat.yml` overlay, builds the candidate images, and starts
  Postgres, migrations, Go API, Nuxt SSR, Caddy, and the UAT-only mock OAuth
  service. The only browser origin is `http://localhost:8080` through Caddy.
- `make uat-reset` removes and recreates only the explicitly resolved
  `aboutme-uat` containers and data volume, then loads deterministic fake users,
  mock OAuth identities, and resume fixtures. It never targets development or
  personal data.
- `make uat-down` tears down only that resolved project and its disposable
  volume, including after a failed run. Evidence is preserved outside the
  volume.

The implementation of those targets is reviewed separately. UAT does not use a
host database, a partially started service, a development server, or a direct
API as a substitute for the deployed browser surface. Readiness must prove the
whole Caddy → web/API → Postgres path before the first scenario.

## UAT-only mock OAuth contract

P9 adds a deterministic provider rather than treating the existing in-process
auth test server as deployable product code:

- A dedicated `mock-oauth` service exists only in `deploy/compose.uat.yml`. It
  builds a separate test-support binary/image, publishes no host port, and joins
  a UAT-only `uat-oauth` network with Caddy. It never joins the server's
  trusted-proxy `edge` network and is absent from the production server image.
- The UAT Caddy configuration adds `/__uat/oauth/*` before the Nuxt fallback and
  proxies it to `mock-oauth`. Browser authorization pages and application
  callbacks therefore stay on `http://localhost:8080`; existing callbacks at
  `/api/v1/auth/{provider}/callback` and every other product route are
  unchanged.
- `mock-oauth` exposes real browser authorization pages; Google/LinkedIn OIDC
  discovery, JWKS, and token endpoints; and GitHub OAuth2 authorization, token,
  `/user`, and `/user/emails` endpoints. It exact-matches callback URLs,
  preserves `state`, validates OIDC `nonce` and PKCE S256, and makes codes
  opaque and one-use. UAT selects a named fake account/outcome in the accessible
  authorization page; it never fabricates a callback URL or bypasses the
  product's provider client.
- A dedicated reviewed configuration-contract change adds `uat` to the closed
  `ENV` vocabulary and defines these variables, required only for `ENV=uat`:

  ```text
  GOOGLE_OIDC_ISSUER_URL=http://caddy/__uat/oauth/google
  LINKEDIN_OIDC_ISSUER_URL=http://caddy/__uat/oauth/linkedin
  GITHUB_OAUTH_AUTHORIZE_URL=http://localhost:8080/__uat/oauth/github/authorize
  GITHUB_OAUTH_TOKEN_URL=http://caddy/__uat/oauth/github/token
  GITHUB_API_BASE_URL=http://caddy/__uat/oauth/github
  ```

  Google/LinkedIn discovery advertises the exact configured internal issuer, the
  public Caddy authorization URL, and internal Caddy token/JWKS URLs. GitHub
  keeps separate browser authorization, backchannel token, and API URLs.

- Server configuration fails at startup if a UAT endpoint is missing in
  `ENV=uat` or present in `dev`, `staging`, or `prod`. In `ENV=uat`, the three
  existing fake client-ID/secret pairs are required, `PUBLIC_ORIGIN` must equal
  `http://localhost:8080`, the public authorization/callback URLs must remain
  under that origin, and backchannel URLs must use the internal Caddy authority.
  Staging and production continue to use only built-in real-provider endpoints.
  The UAT overlay passes the same required configuration to `server` and the
  one-shot `migrate` service because both currently use the shared config
  loader, even though migrations never call a provider.
- `mock-oauth` itself refuses startup unless `ENV=uat`, the public origin is the
  exact loopback UAT origin, and every callback is explicitly allowlisted. The
  normal/self-hosting and production-rendered configurations are tested to
  contain no mock service, image, network, route, or endpoint variable. No mock
  path is added to OpenAPI because it is deployment-only test infrastructure.
- `make uat-reset` recreates the provider's in-memory state and loads named fake
  accounts/outcomes for verified, unverified, missing-email,
  duplicate-email/linking, already-linked identity, expiry, replay, denial, and
  provider-error paths. It exposes no management endpoint. Fixed UAT client
  credentials and signing material are visibly non-production fixtures and
  contain no real secret or personal data.

These variables and guards are implemented with config parsing/tests and
`.env.example` names before P9, then receive independent security review. No
worker may substitute a generic or unreviewed endpoint override.

## Browser and identity contract

- The local project MCP configuration starts the exact project dependency in
  headless, isolated Chrome mode. A local-origin allowlist is a guardrail, not a
  security boundary. The server stores artifacts under the ignored
  `.playwright-mcp/` directory without size-based eviction, enables devtools
  tracing/video, and disables arbitrary Playwright server code execution.
- The UAT executor uses accessibility snapshots and role/name locators for
  interactions; screenshots support visual evidence but do not replace semantic
  inspection.
- Each scenario begins from its declared clean fixture state. Browser storage is
  isolated, and no personal browser profile, persisted login, `.env` file, real
  credential, or personal data is supplied to MCP.
- The UAT-only mock OAuth service exercises deterministic login, linking, recent
  reauthentication, denial, failure, and callback flows. Real-provider smoke is
  P9A staging work.
- Direct API calls and database queries may verify a browser action's effect,
  but cannot replace the user action being accepted.

## Frozen scenario catalog

At dispatch, expand each row below into exact numbered scenarios with acceptance
IDs, initial state, user actions, expected UI/HTTP effects, and required
evidence. Criteria cannot change during a run.

1. Authentication and sessions: every mock provider, logout, provider linking,
   rejected email merge, device revoke, logout-everywhere, expired/replayed
   callback behavior.
2. Resume lifecycle: empty state, create up to the limit, reject the fourth,
   edit every section and customization, validation and size boundaries, delete
   and recovery behavior defined by the product contract.
3. Autosave and conflicts: coalescing, visible save state, offline queue and
   reconnect, two-tab conflict and `412` recovery, idempotent replay.
4. Rendering and accessibility: every template and pagination mode, Vietnamese
   fonts, responsive desktop/mobile layouts, keyboard-only editing/publishing,
   focus behavior, landmarks, names, and contrast/axe checks.
5. Publishing and discovery: slug create/rename/tombstone, every disclosure and
   visibility toggle combination, public SSR, canonical/JSON-LD/robots/sitemap/
   `llms.txt`/Markdown behavior, cache invalidation, unpublish `404`.
6. Realtime: second-tab refresh, heartbeat/reconnect, polling fallback, and
   immediate stream closure on unpublish.
7. Render artifacts: preview/PDF agreement, public-download gating, valid
   owner/public PDF, OG image, and template thumbnails.
8. Privacy: complete export, recent-reauth account deletion, public
   disappearance, and verified purge of owned data and media.
9. Security: cross-origin CSRF rejection, hostile rich-text neutralization,
   CSP/security headers, spoofed forwarding headers, route-specific rate limits,
   and absence of secrets in UI, console, network evidence, or logs.
10. Recovery and usability: clear error states for unavailable dependencies,
    refresh/back-forward behavior, no unexplained console or server errors, and
    essential workflows at the configured desktop and mobile viewports.

## Evidence and report contract

Evidence is stored only in ignored local paths. A run directory under
`.superpowers/uat/p9/<commit>/<run-id>/` contains:

- `report.md` and a machine-readable manifest with the exact commit, image IDs,
  migration head, configuration variable names only, Podman/Compose, Chrome, and
  Playwright MCP versions;
- one immutable row per scenario: initial state, actions, expected behavior,
  observed behavior, `PASS|FAIL|BLOCKED`, timestamps, retries, state changes,
  and evidence paths with hashes;
- accessibility snapshots, screenshots, trace/video, console and network logs,
  request IDs, relevant Caddy/server logs, and database verification where the
  criterion requires it;
- deployment readiness and cleanup results.

After every scenario, the executor inventories files newly written under
`.playwright-mcp/`, copies them into that run's evidence directory, and records
source/destination SHA-256 values in the manifest. The MCP output directory is
not cleared until independent review passes. A run cannot pass if a source and
archived hash differ, an output is unassigned, or archival occurs after a later
scenario could overwrite its evidence.

`BLOCKED` counts as failure. Missing evidence, an undisclosed retry or state
change, an unexplained browser/server error, or evidence from another commit
fails the run. A later product-code commit makes every affected row stale.
Secrets and `.env` values are never captured; if an artifact contains one, the
run fails and the artifact is handled as a credential incident rather than
committed or casually copied.

## Failure, fix, and rerun loop

1. The UAT executor stops the affected scenario and records `FAIL` or `BLOCKED`
   without changing its criterion.
2. The integration owner preserves the failed deployment logs and browser
   evidence.
3. A fresh diagnostic worker investigates the evidence and contract. The
   integration owner does not guess at the cause.
4. If product code is defective, a separate implementation worker writes the
   failing automated regression first, implements the smallest fix, and passes
   independent defect review.
5. The UAT executor rebuilds and resets the isolated deployment, then reruns
   every scenario whose product path or prerequisite changed. The previous
   failed run remains immutable.

## Independent verification and exit

A fresh reviewer receives the frozen catalog, exact commit, report, manifest,
and read-only evidence. The reviewer:

- verifies hashes, version pins, candidate identity, deployment completeness,
  cleanup, and `PASS` observations;
- samples every evidence type and reruns a deterministic subset through MCP;
- returns `PASS`, `FAIL`, or `BLOCKED` without editing evidence or product code.

P9 exits only when the main-session local UAT verdict and the independent
evidence verdict are both `PASS` at the same unchanged commit. The integration
owner then reports the result and asks the human owner whether AWS resource
creation is authorized. Without that recorded authorization, no bootstrap apply,
ECR push, AWS or Cloudflare mutation, DNS mutation, staging apply, or
deployment-workflow dispatch may occur. Production launch remains a separate
human decision after P9A.
