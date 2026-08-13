# Native HTTPS Authentication Harness Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` or `superpowers:executing-plans` to
> implement this plan task by task. Steps use checkbox (`- [ ]`) syntax for
> tracking.

**Goal:** Serve the native application at `https://localhost:20443` and prove a
real Google login, Secure cookies, authenticated `/me`, CSRF enforcement,
reauthentication, and logout in a trusted pinned browser.

**Architecture:** A separate native lifecycle starts Go, Nuxt, Caddy, and a
deterministic OAuth mock on loopback against the existing `aboutme_dev`
database. Caddy owns the sole HTTPS origin and a project-local CA. A derived
pinned Playwright image imports that CA into a disposable NSS store and runs the
browser proof without mounting the repository or disabling certificate checks.

**Tech stack:** Go 1.26, Nuxt 4/Vue 3, Caddy 2.11.2, Bash, Podman, Playwright
1.62.1, Chromium 151.0.7922.34, Ubuntu Noble `libnss3-tools=2:3.98-1ubuntu0.2`.

## Global Constraints

- The approved design is
  `docs/plans/phase-9/native-https-auth-harness-design.md`.
- Fixed ports are Nuxt `20440`, Go `20441`, OAuth mock `20442`, Caddy `20443`,
  and the existing PostgreSQL `20432`.
- Every listener binds IPv4 loopback. The public origin is exactly
  `https://localhost:20443`.
- The harness reuses `aboutme-test-db`; it starts no second database or object
  store and never stops a foreign process or container.
- All generated state stays under ignored `.dev/native-https/`. Never change
  host trust, pass a TLS bypass flag, mount the repository or home directory
  into the browser, print secrets, use `sudo`, or change a sysctl.
- Production and staging provider endpoints remain fixed. Runtime overrides are
  accepted only in `ENV=dev` and only at the closed loopback/same-origin URLs.
- Workers own only the paths named by their task and never use Git. The
  integration owner owns root `Makefile`, lockfiles, staging, commits, full
  gates, and push.
- Every behavior change follows RED → GREEN. Record the exact failure before
  changing production code; never retry a flaky command into a pass.
- This slice does not close Phase 9 U1-U5 and adds no editor, publishing, public
  SSR, AWS, Cloudflare, DNS, registry, or staging behavior.

## File Structure and Waves

| Task | Responsibility                  | Exclusive paths                                                                        |
| ---- | ------------------------------- | -------------------------------------------------------------------------------------- |
| 1    | Runtime provider endpoints      | `apps/server/internal/config/**`, provider files in `internal/auth/**`, `.env.example` |
| 2    | Deterministic Google mock       | `apps/server/internal/uatmock/**`, `apps/server/cmd/mock-oauth/**`                     |
| 3    | Browser authorization allowlist | `apps/web/app/pages/app/settings/sessions.vue`, its focused tests                      |
| 4    | Native TLS lifecycle            | `scripts/dev-https.sh`, `scripts/dev-https-test.sh`                                    |
| 5    | Trusted browser image and proof | `deploy/dev-https-browser/**`                                                          |
| 6    | Owner integration and records   | root `Makefile`, safety tests, deployment/runbook/Phase 9 records                      |

Tasks 1-3 are disjoint and may run together. Task 4 consumes Tasks 1-2. Task 5
consumes Tasks 2-4. Task 6 is a serialized integration-owner window.

---

### Task 1: Validated Runtime Provider Endpoints

**Files:**

- Modify: `apps/server/internal/config/config.go`
- Modify: `apps/server/internal/config/config_test.go`
- Modify: `apps/server/internal/auth/handlers.go`
- Modify: `apps/server/internal/auth/google.go`
- Modify: `apps/server/internal/auth/linkedin.go`
- Modify: `apps/server/internal/auth/github.go`
- Modify: `apps/server/internal/auth/provider_http.go`
- Modify: `apps/server/internal/auth/export_test.go`
- Test: `apps/server/internal/auth/uat_endpoints_test.go`
- Modify: `.env.example`

**Interfaces:**

- Produce five `config.Config` fields named `GoogleOIDCIssuerURL`,
  `LinkedInOIDCIssuerURL`, `GitHubOAuthAuthorizeURL`, `GitHubOAuthTokenURL`, and
  `GitHubAPIBaseURL`.
- `auth.NewService` copies those fields into immutable service endpoint fields.
- Empty fields preserve the current production constants.
- Local OIDC discovery uses a bounded HTTP client whose transport permits only
  loopback server requests. After discovery, Google/LinkedIn authorization URLs
  must equal `PublicOrigin + /__uat/oauth/<provider>/authorize`, and token URLs
  must remain loopback.

- [x] **Step 1: Write configuration RED tests**

Add literal table cases proving: dev accepts `http://127.0.0.1:20442/google`;
staging/prod reject any override; Google and LinkedIn reject non-loopback
issuers; GitHub rejects a partial set, a non-loopback backchannel, or an
authorize URL outside `https://localhost:20443/__uat/oauth/github/authorize`.

```go
func TestLoad_DevGoogleOIDCIssuerLoopback(t *testing.T) {
    vars := validDevEnv()
    vars["GOOGLE_OIDC_ISSUER_URL"] = "http://127.0.0.1:20442/google"
    got, err := config.Load(env(vars))
    if err != nil { t.Fatal(err) }
    if got.GoogleOIDCIssuerURL != vars["GOOGLE_OIDC_ISSUER_URL"] {
        t.Fatalf("issuer = %q", got.GoogleOIDCIssuerURL)
    }
}
```

- [x] **Step 2: Run RED**

Run:

```sh
cd apps/server && go test ./internal/config -run 'ProviderEndpoint|OIDCIssuer' -count=1
```

Expected: compile failure because the five config fields do not exist.

- [x] **Step 3: Implement closed endpoint validation**

Add one helper returning a value object. Parse with `url.Parse`, reject user
info, query, fragment, missing port, non-loopback host, and unexpected path.
Reject every override outside dev. Require all three GitHub values together.

```go
type providerEndpoints struct {
    googleIssuer, linkedinIssuer string
    githubAuthorize, githubToken, githubAPI string
}

func loadProviderEndpoints(
    getenv func(string) string,
    env string,
    publicOrigin string,
) (providerEndpoints, error)
```

- [x] **Step 4: Write and verify auth wiring RED/GREEN**

Add `uat_endpoints_test.go` cases that construct `auth.NewService`, drive Google
discovery through a local server, and prove GitHub uses its three separate URLs.
Include malicious discovery documents with an external authorization, token, or
JWKS URL and prove no external request occurs. First observe requests still
reach production constants, then wire the new fields through `NewService`,
`googleProvider`, `linkedinProvider`, `githubOAuth2Config`, and
`githubAPIBaseURLFor`. Use a local-only transport while an OIDC override is
active and validate the discovered `oauth2.Endpoint` before creating a
transaction.

```go
func localProviderHTTPClient() *http.Client

func validateLocalOIDCEndpoint(
    endpoint oauth2.Endpoint,
    publicOrigin string,
    provider Provider,
) error
```

Run:

```sh
cd apps/server && go test ./internal/config ./internal/auth -run 'Endpoint|Issuer' -count=1
cd apps/server && go test ./internal/config ./internal/auth -count=1
```

Expected: both commands pass.

- [x] **Step 5: Format and hand off**

Run `gofmt` on owned Go files and report RED/GREEN output, changed paths, and
the exact endpoint grammar. The integration owner inspects, stages only these
paths, runs staged gitleaks, and commits
`feat(auth): configure local provider endpoints`.

### Task 2: Deterministic Google OIDC Mock

**Files:**

- Create: `apps/server/internal/uatmock/config.go`
- Create: `apps/server/internal/uatmock/service.go`
- Create: `apps/server/internal/uatmock/protocol.go`
- Create: `apps/server/internal/uatmock/fixtures.go`
- Create: `apps/server/internal/uatmock/service_test.go`
- Create: `apps/server/internal/uatmock/protocol_test.go`
- Create: `apps/server/cmd/mock-oauth/main.go`
- Create: `apps/server/cmd/mock-oauth/main_test.go`

**Interfaces:**

- Produce `uatmock.Config`, `uatmock.New(Config) (*Service, error)`, and
  `(*Service).Handler() http.Handler`.
- Fixed paths are `/google/.well-known/openid-configuration`,
  `/google/jwks.json`, `/google/token`, and `/__uat/oauth/google/authorize`.
- Fixed account: subject `uat-google-001`, verified email
  `developer@example.invalid`, display name `Development User`.

- [x] **Step 1: Write protocol RED tests**

Test the complete real handler: exact callback/client/response type, state,
nonce, PKCE S256, one-use code, token redirect equality, issuer, audience,
signature, verified account claims, bounded fields, and method/content-type
rejections.

```go
func TestGoogleFlowConsumesCodeOnce(t *testing.T) {
    svc := newTestService(t)
    code := authorizeThroughHandler(t, svc.Handler(), validAuthorizeQuery())
    first := exchangeThroughHandler(t, svc.Handler(), code, validVerifier())
    if first.Code != http.StatusOK { t.Fatalf("first = %d", first.Code) }
    second := exchangeThroughHandler(t, svc.Handler(), code, validVerifier())
    if second.Code != http.StatusBadRequest { t.Fatalf("second = %d", second.Code) }
}
```

- [x] **Step 2: Run RED**

Run:

```sh
cd apps/server && go test ./internal/uatmock ./cmd/mock-oauth -count=1
```

Expected: package-not-found failure.

- [x] **Step 3: Implement minimal Google flow**

Use an in-memory mutex-protected code map and an ephemeral RSA-2048 key. The
authorization GET renders an accessible form; POST validates the request, stores
only the code binding, and redirects with `code` and the exact `state`. The
token endpoint atomically consumes before PKCE verification and returns an RS256
ID token. Never echo secrets or codes in errors.

```go
type Config struct {
    IssuerURL, PublicOrigin, RedirectURL string
    ClientID, ClientSecret string
    Now func() time.Time
    Random io.Reader
}
```

- [x] **Step 4: Add command boundary and GREEN**

The command accepts only `LISTEN_HOST=127.0.0.1`, `PORT=20442`, the exact five
mock values supplied by the lifecycle, and shuts down on SIGINT/SIGTERM.

Run:

```sh
cd apps/server && go test ./internal/uatmock ./cmd/mock-oauth -count=1
cd apps/server && go test ./internal/uatmock ./cmd/mock-oauth -race -count=1
```

Expected: pass without retries or external network.

- [x] **Step 5: Format and hand off**

Run `gofmt`, scan owned files for trailing whitespace and real credentials, and
report RED/GREEN evidence. The owner commits
`feat(auth): add deterministic local OAuth mock` after staged gitleaks.

### Task 3: Same-Origin HTTPS Authorization Allowlist

**Files:**

- Modify: `apps/web/app/pages/app/settings/sessions.vue`
- Modify: `apps/web/test/sessions-privileged-start-adversarial.test.ts`

**Interfaces:**

- `authorizeURL` continues exact production-provider matching.
- Add only loopback HTTPS current-page + exact same-origin
  `/__uat/oauth/<provider>/authorize` acceptance.

- [x] **Step 1: Write RED cases**

Mount the real component at `https://localhost:20443` and return the same-origin
Google mock URL. Expect top-level navigation. Add literal denials for cross
port, cross host, HTTP downgrade, credentials, fragment, wrong provider path,
and a production hostname using `/__uat/oauth/`.

```ts
it("accepts only the same-origin loopback HTTPS mock path", async () => {
  window.history.replaceState(
    {},
    "",
    "https://localhost:20443/app/settings/sessions",
  );
  authorizeURLs.google = "https://localhost:20443/__uat/oauth/google/authorize";
  // trigger the real reauth action and assert navigateTo receives this URL
});
```

- [x] **Step 2: Run RED**

Run:

```sh
cd apps/web && npx vitest run test/sessions-privileged-start-adversarial.test.ts
```

Expected: the same-origin HTTPS case rejects `invalid OAuth authorize URL`.

- [x] **Step 3: Implement and verify GREEN**

Accept the mock only when both candidate and current URLs use `https:`, the
current hostname is loopback, origins are byte-equal, and pathname equals
`/__uat/oauth/${provider}/authorize`. Preserve the existing HTTP loopback rule
until the full P9 plan removes it.

Run:

```sh
cd apps/web && npx vitest run test/sessions-privileged-start-adversarial.test.ts
cd apps/web && npx eslint app/pages/app/settings/sessions.vue test/sessions-privileged-start-adversarial.test.ts
```

Expected: pass.

- [x] **Step 4: Hand off**

Report the negative matrix and output. The owner commits
`fix(web): allow trusted local HTTPS OAuth` after staged gitleaks.

### Task 4: Native HTTPS Lifecycle

**Files:**

- Create: `scripts/dev-https.sh`
- Create: `scripts/dev-https-test.sh`

**Interfaces:**

- Commands: `up`, `down`, `status`, `logs`.
- State root: `.dev/native-https`; services: `mock-oauth`, `server`, `web`,
  `caddy`.
- Exported CA: `.dev/native-https/input/caddy-root.crt`, mode `0600`.

- [x] **Step 1: Write lifecycle RED tests**

Use a temporary repository plus fake `podman`, `go`, `npm`, `caddy`, `curl`,
`ss`, and `setsid`. Execute the script, not source-text grep. Prove fixed ports,
foreign-listener rejection, active HTTP-stack rejection, exact stop order, no
foreign kill/removal, no sudo/sysctl, no secret output, config-change rejection,
CA mode, and idempotent owned-process handling.

- [x] **Step 2: Run RED**

Run:

```sh
bash scripts/dev-https-test.sh --static
```

Expected: missing-script failure before production implementation.

- [x] **Step 3: Implement lifecycle**

Reuse the established PID/session-leader checks from `dev-native.sh` without
changing that script. Build `mock-oauth`, `server`, and `migrate`; start with
the fixed fake credentials and Google issuer; run Nuxt on 20440; generate a
Caddyfile from the deployed route table with `tls internal` and the mock route.
Verify each textual substitution occurs exactly once.

- [x] **Step 4: Verify GREEN and real static routing**

Run:

```sh
bash -n scripts/dev-https.sh scripts/dev-https-test.sh
bash scripts/dev-https-test.sh --static
cd apps/server && go test ./internal/routetable -count=1
```

Expected: pass. Do not start live services in this task.

- [x] **Step 5: Hand off**

Report exact destructive targets and recovery diagnostics. The owner commits
`feat(dev): add native HTTPS lifecycle` after staged gitleaks.

### Task 5: Trusted Browser Image and Authentication Proof

**Files:**

- Create: `deploy/dev-https-browser/Dockerfile`
- Create: `deploy/dev-https-browser/package.json`
- Create: `deploy/dev-https-browser/package-lock.json`
- Create: `deploy/dev-https-browser/playwright.config.ts`
- Create: `deploy/dev-https-browser/auth.spec.ts`
- Create: `deploy/dev-https-browser/run.sh`
- Create: `deploy/dev-https-browser/static-test.sh`

**Interfaces:**

- Base image digest is
  `sha256:c091b21d9fae78c76e85cd4356431e9b018402f172a214fc7d7a5e9a7e29d8ac`.
- Install exact `libnss3-tools=2:3.98-1ubuntu0.2`.
- Runtime mounts: `/uat-input:ro` and `/evidence:rw`; base URL
  `https://localhost:20443`.

- [x] **Step 1: Create dependency lock and RED browser test**

Create a minimal private Node package with only `@playwright/test: 1.62.1`.
Generate its lockfile with `npm install --package-lock-only --ignore-scripts`.
Write `auth.spec.ts` for the ten design steps and first run it against the
untrusted origin to record certificate failure.

- [x] **Step 2: Write static boundary RED**

`static-test.sh` builds in a temporary context and inspects the resulting image
and a fake-Podman call log. It proves the base digest, exact NSS package,
non-root browser, read-only root, sandboxed Chromium, two closed mounts, host
network, no repo/home/socket mount, no TLS bypass, and absence of update flags.

Run:

```sh
bash deploy/dev-https-browser/static-test.sh
```

Expected: fail before Dockerfile and runner exist.

- [x] **Step 3: Implement trust and browser flow**

`run.sh` sets `HOME=/tmp/home`, creates an empty NSS database, imports
`/uat-input/caddy-root.crt`, verifies it with `certutil -L`, then runs
Playwright. Block every browser request whose origin is not
`https://localhost:20443`. Inspect cookie attributes without attaching values;
disable trace/video and keep failures free of token bodies.

- [x] **Step 4: Verify static GREEN**

Run:

```sh
bash -n deploy/dev-https-browser/run.sh deploy/dev-https-browser/static-test.sh
bash deploy/dev-https-browser/static-test.sh
```

Expected: pass. The live authentication flow waits for Task 6 integration.

- [x] **Step 5: Hand off**

Report the image ID, package version, mount/network inspection, and test output.
The owner commits `test(auth): add trusted HTTPS browser proof` after staged
gitleaks.

### Task 6: Integration, Live Proof, and Records

**Files:**

- Modify: `Makefile`
- Modify: `scripts/test/makefile-safety-test.sh`
- Modify: `docs/plans/phase-9/README.md`
- Modify: `deploy/README.md`
- Modify: `docs/runbooks/local-uat.md`
- Modify: `docs/architecture.md`

**Interfaces:**

- Make targets: `dev-https`, `dev-https-down`, `dev-https-status`,
  `dev-https-logs`, `dev-https-browser-image`, and `dev-https-auth-check`.
- `operational-test` runs only syntax/static checks; live HTTPS stays local.

- [x] **Step 1: Add Make safety RED cases**

Execute the copied Makefile with fake tools. Prove every target delegates to the
exact harness command, a failed preflight causes no build/process/container
mutation, and the browser target uses only the closed mounts and image.

Run:

```sh
bash scripts/test/makefile-safety-test.sh
```

Expected: fail because the targets do not exist.

- [x] **Step 2: Wire Make and documentation**

Add the six phony targets and register syntax/static tests in
`operational-test`. Document `https://localhost:20443`, the shared-DB
precondition, exact start/status/check/down commands, ignored evidence, and the
fact that port-443 P9 UAT remains pending.

- [x] **Step 3: Run integrated static gates**

Run:

```sh
bash scripts/test/makefile-safety-test.sh
make operational-test route-table-test
make server-build server-vet server-test
make web-lint web-typecheck web-test web-build
```

Expected: pass.

- [x] **Step 4: Run live HTTPS authentication proof**

Confirm no heavy worker is running, then run:

```sh
make dev-native-down
make dev-https
make dev-https-status
make dev-https-browser-image
make dev-https-auth-check
make dev-https-down
```

Expected: the browser proof passes once with no retry; teardown leaves ports
20440-20443 free and leaves `aboutme-test-db` running.

- [x] **Step 5: Fresh review and owner commit**

Dispatch one fresh non-author reviewer. It must name TLS trust, provider
endpoint isolation, OAuth state/nonce/PKCE/code consumption, session cookie
attributes, CSRF, secret-free evidence, and destructive targeting. Resolve all
Important findings, rerun affected gates, stage exact paths, run gitleaks, and
commit `feat(dev): enable trusted HTTPS authentication`.

## Completion Gate

The slice is complete when Tasks 1-6 are reviewed, every static gate passes, the
live authentication flow passes without TLS bypass or external network, teardown
is clean, and the records still state that full Phase 9 UAT on port 443 is
pending. Do not run phase-wide `make ci` or `make scan` until this slice joins a
coherent phase candidate.
