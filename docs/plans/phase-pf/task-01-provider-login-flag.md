# Task 01 — `PROVIDER_LOGIN_ENABLED` and provider route gating

**Acceptance:** AC-AUTH-017 (Go clauses).

**Depends on:** T00 (the environment name and ADR 0027 exist).

**Owned paths:** `apps/server/internal/config/config.go`,
`apps/server/internal/config/config_test.go`,
`apps/server/internal/auth/service.go` (or wherever `NewService` builds the
`Service` struct), `apps/server/internal/auth/handlers.go`,
`apps/server/internal/auth/handlers_test.go`.

## Interfaces

- Consumes: `config.Load(getenv)` and `auth.NewService(logger, cfg, pool)` as
  they exist today.
- Produces: `config.Config.ProviderLoginEnabled bool`; `auth.Service` skips the
  six provider `mux.Handle` calls when it is false. T02 reads the field to build
  the capabilities response.

## Contract

The flag parses exactly like `MCP_ENABLED`: blank or `false` is off, `true` is
on, anything else fails `Load` with an error that names the variable and never
the value. The provider callback paths, both start methods (`GET` anonymous
login start and `POST` authenticated link/reauth start), and nothing else are
gated. Unregistered paths fall through to the router's `NotFound` handler, so
they return the same body as any unknown path.

## Steps

- [ ] **Step 1: Write the failing config tests**

Add to `apps/server/internal/config/config_test.go`, next to the `MCP_ENABLED`
cases:

```go
func TestLoad_ProviderLoginFlag(t *testing.T) {
    t.Parallel()
    for _, tc := range []struct {
        name    string
        raw     string
        enabled bool
    }{
        {name: "blank is off", raw: "", enabled: false},
        {name: "false is off", raw: "false", enabled: false},
        {name: "true is on", raw: "true", enabled: true},
    } {
        t.Run(tc.name, func(t *testing.T) {
            t.Parallel()
            vars := validDevEnv()
            vars["PROVIDER_LOGIN_ENABLED"] = tc.raw
            got, err := config.Load(env(vars))
            if err != nil {
                t.Fatalf("Load() error = %v", err)
            }
            if got.ProviderLoginEnabled != tc.enabled {
                t.Fatalf("ProviderLoginEnabled = %t, want %t", got.ProviderLoginEnabled, tc.enabled)
            }
        })
    }
}

func TestLoad_ProviderLoginFlagRejectsInvalidValueWithoutEcho(t *testing.T) {
    t.Parallel()
    vars := validDevEnv()
    vars["PROVIDER_LOGIN_ENABLED"] = "yes-secret-sentinel"
    _, err := config.Load(env(vars))
    if err == nil {
        t.Fatal("Load() error = nil, want PROVIDER_LOGIN_ENABLED rejection")
    }
    if !strings.Contains(err.Error(), "PROVIDER_LOGIN_ENABLED") || strings.Contains(err.Error(), vars["PROVIDER_LOGIN_ENABLED"]) {
        t.Fatalf("Load() error = %q, want variable name without raw value", err)
    }
}
```

- [ ] **Step 2: Run them and watch them fail**

```sh
cd apps/server && go test ./internal/config/ -run 'TestLoad_ProviderLoginFlag' -count=1
```

Expected: compile error `got.ProviderLoginEnabled undefined`.

- [ ] **Step 3: Implement the flag in `config.go`**

Add the field to `Config` after `AgentAccess`:

```go
    // ProviderLoginEnabled registers the Google, GitHub, and LinkedIn login,
    // callback, link, and reauthentication routes. It is off for v1 (ADR 0027);
    // the provider code stays so the flag can turn on without a code change.
    ProviderLoginEnabled bool
```

Add the parser beside `loadAgentAccessConfig`:

```go
func loadProviderLoginFlag(raw string) (bool, error) {
    switch strings.TrimSpace(raw) {
    case "", "false":
        return false, nil
    case "true":
        return true, nil
    default:
        return false, errors.New("config: PROVIDER_LOGIN_ENABLED must be true or false")
    }
}
```

In `Load`, after `agentAccess, err := loadAgentAccessConfig(...)`:

```go
    providerLogin, err := loadProviderLoginFlag(getenv("PROVIDER_LOGIN_ENABLED"))
    if err != nil {
        return Config{}, err
    }
```

and set `ProviderLoginEnabled: providerLogin,` in the `Config{...}` literal.

- [ ] **Step 4: Run the config tests to GREEN**

```sh
cd apps/server && go test ./internal/config/ -count=1
```

Expected: `ok`.

- [ ] **Step 5: Write the failing route-gating tests**

In `apps/server/internal/auth/handlers_test.go`, next to
`TestService_RegisterRoutes_GoogleStartAndCallback_RespondToGET`. The test
service builder `newTestService(t, opts...)` builds a `config.Config`; add an
option in the same shape as `withGoogleIssuer` that sets
`ProviderLoginEnabled = false`, and make the builder's default `true` so every
existing provider test keeps its behavior:

```go
// withProviderLoginDisabled turns PROVIDER_LOGIN_ENABLED off for one test.
func withProviderLoginDisabled() testServiceOption {
    return func(cfg *config.Config) { cfg.ProviderLoginEnabled = false }
}
```

(If the option type in the file has a different name or shape, follow the file's
existing option pattern exactly; only the intent above is fixed.)

```go
func TestService_RegisterRoutes_ProviderLoginDisabled_ProviderPathsAre404(t *testing.T) {
    t.Parallel()

    p := oidctest.NewProvider(t)
    handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL), withProviderLoginDisabled())

    paths := []string{
        auth.GoogleStartPath, auth.GoogleCallbackPath,
        auth.GitHubStartPath, auth.GitHubCallbackPath,
        auth.LinkedInStartPath, auth.LinkedInCallbackPath,
    }
    for _, path := range paths {
        for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodHead} {
            req := httptest.NewRequest(method, path, nil)
            rec := httptest.NewRecorder()
            handler.ServeHTTP(rec, req)
            if rec.Code != http.StatusNotFound {
                t.Errorf("%s %s status = %d, want 404 with provider login disabled", method, path, rec.Code)
            }
        }
    }

    // Session routes are unaffected: an anonymous /me is 401, not 404.
    req := httptest.NewRequest(http.MethodGet, auth.MePath, nil)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)
    if rec.Code != http.StatusUnauthorized {
        t.Fatalf("GET %s status = %d, want 401 (session routes stay registered)", auth.MePath, rec.Code)
    }
}

func TestService_RegisterRoutes_ProviderLoginDisabled_NotFoundBodyIsUniform(t *testing.T) {
    t.Parallel()

    p := oidctest.NewProvider(t)
    handler, _ := newTestService(t, withGoogleIssuer(p.URL), withProviderLoginDisabled())

    body := func(path string) string {
        req := httptest.NewRequest(http.MethodGet, path, nil)
        rec := httptest.NewRecorder()
        handler.ServeHTTP(rec, req)
        return rec.Body.String()
    }
    if got, want := body(auth.GoogleStartPath), body("/api/v1/definitely-not-a-route"); got != want {
        t.Fatalf("disabled provider path body = %q, want the uniform not-found body %q", got, want)
    }
}
```

If `newTestService` returns a handler built from a bare `http.ServeMux` rather
than `api.New`, the uniform-body test must wrap the same way the composition
root does
(`api.New(logger, pinger, api.Options{}, publicStub, svc.RegisterRoutes)`) so
the `NotFound` fallback is in the chain. Reuse the existing helper the file
already has for that if one exists.

- [ ] **Step 6: Run the route tests and watch them fail**

```sh
cd apps/server && go test ./internal/auth/ -run 'ProviderLoginDisabled' -count=1
```

Expected: compile error on `withProviderLoginDisabled`, then after adding the
option, `status = 302, want 404`.

- [ ] **Step 7: Gate the routes**

Give `Service` a field `providerLogin bool`, set in `NewService` from
`cfg.ProviderLoginEnabled`. In `RegisterRoutes`:

```go
func (s *Service) RegisterRoutes(mux *http.ServeMux) {
    if s.providerLogin {
        // All providers share one bounded start-route limiter.
        startLimit := s.startRateLimit()
        mux.Handle(GoogleStartPath, s.startRoute(ProviderGoogle, s.buildGoogleAuthorizeURL, startLimit))
        mux.Handle(GoogleCallbackPath, route(http.MethodGet, s.handleGoogleCallback))
        mux.Handle(GitHubStartPath, s.startRoute(ProviderGitHub, s.buildGitHubAuthorizeURL, startLimit))
        mux.Handle(GitHubCallbackPath, route(http.MethodGet, s.handleGitHubCallback))
        mux.Handle(LinkedInStartPath, s.startRoute(ProviderLinkedIn, s.buildLinkedInAuthorizeURL, startLimit))
        mux.Handle(LinkedInCallbackPath, route(http.MethodGet, s.handleLinkedInCallback))
    }

    // Session routes authenticate before CSRF enforcement.
    mux.Handle(MePath, route(http.MethodGet, s.sessionChain(s.handleMe)))
    mux.Handle(LogoutPath, route(http.MethodPost, s.sessionChain(s.handleLogout)))
    // Collection method dispatch runs before authentication, matching route.
    mux.Handle(SessionsPath, http.HandlerFunc(s.handleSessionsCollection))
    mux.Handle(SessionsPath+"/{id}", route(http.MethodDelete, s.sessionChain(s.handleRevokeSession)))
}
```

Make `newTestService` default `ProviderLoginEnabled: true`.

- [ ] **Step 8: Run the whole auth package with the race detector**

```sh
cd apps/server && go test ./internal/auth/ -race -count=1
```

Expected: `ok`, including every pre-existing provider, link, and password test.

- [ ] **Step 9: Build and vet**

```sh
make server-build server-vet
```

Expected: both pass.

## Adversarial checklist

- `HEAD` on a disabled path is 404, not 405: the route never enters the method
  switch.
- The `POST` link/reauth start with a valid session and CSRF token is 404 when
  disabled; the session and CSRF middleware never run for it.
- No test sets the flag through the process environment; every case injects it
  through `validDevEnv()` or the service option, so tests stay parallel.

## Handoff

Report RED and GREEN outputs for steps 2, 6, and 8, the exact option name you
added, and the file that holds the `Service` struct. Suggested commit:
`feat(auth): gate provider login behind a server flag`.
