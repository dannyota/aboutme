# Task 02 — `GET /api/v1/capabilities`

**Acceptance:** AC-AUTH-018.

**Depends on:** T01 (`config.Config.ProviderLoginEnabled`).

**Owned paths:** `docs/api/openapi.yaml`, `docs/api/test/capabilities.test.ts`,
generated `apps/web/app/api/generated/openapi.ts`,
`apps/server/internal/api/capabilities.go`,
`apps/server/internal/api/capabilities_test.go`,
`apps/server/cmd/server/main.go`, `apps/server/cmd/server/main_test.go`.

## Interfaces

- Consumes: `config.Config.ProviderLoginEnabled` (T01) and
  `config.Config.AgentAccess.Enabled`; `api.WriteData`; the unexported
  `route(method, handler)` helper in `internal/api`.
- Produces: OpenAPI `getCapabilities` and schema `Capabilities`; Go
  `api.Capabilities{ProviderLogin, AgentAccess bool}` and
  `api.CapabilitiesHandler(api.Capabilities) http.Handler`;
  `capabilitiesRegistrar(cfg) func(*http.ServeMux)` in `main.go`. T05 uses
  `components['schemas']['Capabilities']` from the generated client.

## Contract

Unauthenticated, `GET` only,
`{"data":{"providerLogin":bool,"agentAccess":bool}}`, both required, no other
property. The router's default `NoStoreCache` chain supplies
`Cache-Control: no-store`; the handler adds no cache header of its own. Other
methods get the router's 405. The response contains no environment names,
versions, or other configuration.

## Steps

- [ ] **Step 1: Write the failing OpenAPI contract test**

Create `docs/api/test/capabilities.test.ts`:

```ts
import { readFileSync } from "node:fs";
import { parse } from "yaml";
import { describe, expect, it } from "vitest";

const doc = parse(readFileSync("docs/api/openapi.yaml", "utf8")) as any;

describe("GET /capabilities", () => {
  const op = doc.paths["/capabilities"]?.get;

  it("exists, is unauthenticated, and is the only method on its path", () => {
    expect(op?.operationId).toBe("getCapabilities");
    expect(op.security).toEqual([]);
    expect(Object.keys(doc.paths["/capabilities"])).toEqual(["get"]);
  });

  it("returns exactly two required booleans in the data envelope", () => {
    const schema = doc.components.schemas.Capabilities;
    expect(schema.type).toBe("object");
    expect(schema.additionalProperties).toBe(false);
    expect(schema.required.sort()).toEqual(["agentAccess", "providerLogin"]);
    expect(schema.properties.providerLogin.type).toBe("boolean");
    expect(schema.properties.agentAccess.type).toBe("boolean");
    const ok = op.responses["200"].content["application/json"].schema;
    const data = ok.allOf.find((part: any) => part.properties?.data);
    expect(data.properties.data.$ref).toBe("#/components/schemas/Capabilities");
  });

  it("documents no-store caching", () => {
    expect(op.description).toMatch(/no-store/);
  });
});

describe("provider operations are conditional", () => {
  for (const path of [
    "/auth/google/start",
    "/auth/github/start",
    "/auth/linkedin/start",
    "/auth/google/callback",
    "/auth/github/callback",
    "/auth/linkedin/callback",
  ]) {
    it(`${path} says it is registered only when PROVIDER_LOGIN_ENABLED is true`, () => {
      for (const method of Object.keys(doc.paths[path])) {
        expect(doc.paths[path][method].description).toMatch(
          /PROVIDER_LOGIN_ENABLED/,
        );
      }
    });
  }
});
```

- [ ] **Step 2: Run it and watch it fail**

```sh
npx vitest run --dir docs/api/test capabilities
```

Expected: `operationId` is `undefined`.

- [ ] **Step 3: Add the operation and schema to `docs/api/openapi.yaml`**

After the `/me` path:

```yaml
/capabilities:
  get:
    operationId: getCapabilities
    tags: [auth]
    security: []
    summary: Which optional sign-in and agent surfaces this deployment enables
    description: >-
      Unauthenticated read of the two configuration flags the web needs before
      it renders a sign-in or settings page: `providerLogin`
      (`PROVIDER_LOGIN_ENABLED`) and `agentAccess` (`MCP_ENABLED`). The response
      is `Cache-Control: no-store` so a configuration change is visible on the
      next request. It reveals no other configuration.
    responses:
      "200":
        description: The enabled optional surfaces.
        content:
          application/json:
            schema:
              allOf:
                - $ref: "#/components/schemas/Envelope"
                - type: object
                  properties:
                    data:
                      $ref: "#/components/schemas/Capabilities"
            example:
              data:
                providerLogin: false
                agentAccess: false
```

Under `components.schemas`, beside `User`:

```yaml
Capabilities:
  type: object
  additionalProperties: false
  required: [providerLogin, agentAccess]
  properties:
    providerLogin:
      type: boolean
      description:
        Google, GitHub, and LinkedIn login and linking routes are registered.
    agentAccess:
      type: boolean
      description:
        The OAuth authorization server, `/mcp`, and connected-agent settings are
        registered.
```

Prepend to the `description` of each of the six provider operations (both
methods on each `start` path and the `get` on each `callback` path): "Registered
only when `PROVIDER_LOGIN_ENABLED` is true; otherwise this path returns the
uniform not-found response (ADR 0027). "

- [ ] **Step 4: Regenerate the client and run the contract gate**

```sh
make api-gen
make api-check
```

Expected: the generated `openapi.ts` gains `"/capabilities"` and `Capabilities`;
`api-check` passes including the new test and the drift check. Run
`cd apps/web && npx vitest run test/api-client.test.ts` as well; if its
compile-time fixture enumerates operations, add `getCapabilities` where it lists
the versioned surface.

- [ ] **Step 5: Write the failing Go handler test**

Create `apps/server/internal/api/capabilities_test.go`:

```go
package api_test

import (
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/dannyota/aboutme/apps/server/internal/api"
)

func TestCapabilitiesHandler_ReflectsFlagsAndRejectsOtherMethods(t *testing.T) {
    t.Parallel()
    h := api.CapabilitiesHandler(api.Capabilities{ProviderLogin: true, AgentAccess: false})

    rec := httptest.NewRecorder()
    h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
    if rec.Code != http.StatusOK {
        t.Fatalf("GET status = %d, want 200", rec.Code)
    }
    var body struct {
        Data map[string]any `json:"data"`
    }
    if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
        t.Fatalf("decode: %v", err)
    }
    if len(body.Data) != 2 || body.Data["providerLogin"] != true || body.Data["agentAccess"] != false {
        t.Fatalf("data = %v, want exactly providerLogin=true agentAccess=false", body.Data)
    }

    for _, method := range []string{http.MethodPost, http.MethodHead, http.MethodPut, http.MethodDelete} {
        rec := httptest.NewRecorder()
        h.ServeHTTP(rec, httptest.NewRequest(method, "/api/v1/capabilities", nil))
        if rec.Code != http.StatusMethodNotAllowed {
            t.Errorf("%s status = %d, want 405", method, rec.Code)
        }
    }
}
```

Add a router-chain test in the same file that builds the handler the way
`main.go` does and asserts the cache header. Use the same `api.New` construction
the existing `router_test.go` uses for its handler-under-test (logger, pinger
stub, `api.Options{}`, public-routes stub), passing
`func(mux *http.ServeMux) { mux.Handle("/api/v1/capabilities", api.CapabilitiesHandler(api.Capabilities{})) }`
as the registrar:

```go
func TestCapabilitiesHandler_IsNoStoreThroughTheRouter(t *testing.T) {
    t.Parallel()
    handler := newRouterForTest(t, func(mux *http.ServeMux) {
        mux.Handle("/api/v1/capabilities", api.CapabilitiesHandler(api.Capabilities{}))
    })
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
    if rec.Code != http.StatusOK {
        t.Fatalf("status = %d, want 200", rec.Code)
    }
    if got := rec.Header().Get("Cache-Control"); got != "no-store" && got != "no-store, no-transform" {
        t.Fatalf("Cache-Control = %q, want the router's no-store policy", got)
    }
}
```

`newRouterForTest` is whatever helper `router_test.go` already uses; if it has
another name, call that one. Do not add a second router construction path.

- [ ] **Step 6: Run and watch it fail**

```sh
cd apps/server && go test ./internal/api/ -run Capabilities -count=1
```

Expected: `undefined: api.CapabilitiesHandler`.

- [ ] **Step 7: Implement `internal/api/capabilities.go`**

```go
package api

import "net/http"

// Capabilities is the closed, unauthenticated feature-flag read the web uses
// before rendering sign-in and settings surfaces (docs/design/api.md,
// "Endpoint groups"). It carries flags only, never configuration values.
type Capabilities struct {
    ProviderLogin bool `json:"providerLogin"`
    AgentAccess   bool `json:"agentAccess"`
}

// CapabilitiesHandler serves GET /api/v1/capabilities. The router's default
// NoStoreCache chain supplies Cache-Control; this handler sets none.
func CapabilitiesHandler(c Capabilities) http.Handler {
    return route(http.MethodGet, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
        WriteData(w, http.StatusOK, c)
    }))
}
```

If `route` takes `http.HandlerFunc` rather than `http.Handler`, drop the
`http.HandlerFunc(...)` wrapper to match.

- [ ] **Step 8: Run to GREEN**

```sh
cd apps/server && go test ./internal/api/ -count=1
```

Expected: `ok`.

- [ ] **Step 9: Write the failing composition test**

In `apps/server/cmd/server/main_test.go`:

```go
func TestCapabilitiesRegistrarReflectsConfig(t *testing.T) {
    t.Parallel()
    cfg := config.Config{ProviderLoginEnabled: true}
    cfg.AgentAccess.Enabled = false
    mux := http.NewServeMux()
    capabilitiesRegistrar(cfg)(mux)
    rec := httptest.NewRecorder()
    mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/capabilities", nil))
    if rec.Code != http.StatusOK {
        t.Fatalf("status = %d, want 200", rec.Code)
    }
    if got := rec.Body.String(); got != "{\"data\":{\"providerLogin\":true,\"agentAccess\":false}}\n" {
        t.Fatalf("body = %q", got)
    }
}
```

- [ ] **Step 10: Run and watch it fail**

```sh
cd apps/server && go test ./cmd/server/ -run TestCapabilitiesRegistrar -count=1
```

Expected: `undefined: capabilitiesRegistrar`.

- [ ] **Step 11: Wire it in `main.go`**

Add beside `newAgentRouteRegistrar`:

```go
// capabilitiesRegistrar exposes the two optional-surface flags to the web
// without a second source of truth (ADR 0027).
func capabilitiesRegistrar(cfg config.Config) func(*http.ServeMux) {
    handler := api.CapabilitiesHandler(api.Capabilities{
        ProviderLogin: cfg.ProviderLoginEnabled,
        AgentAccess:   cfg.AgentAccess.Enabled,
    })
    return func(mux *http.ServeMux) { mux.Handle("/api/v1/capabilities", handler) }
}
```

and pass `capabilitiesRegistrar(cfg)` as one more registrar in the
`api.New(...)` call after `agentRoutes`.

- [ ] **Step 12: Run to GREEN, build, vet**

```sh
cd apps/server && go test ./cmd/server/ -count=1
make server-build server-vet
```

Expected: `ok`, build and vet pass.

## Adversarial checklist

- The body has exactly two keys; a future field must go through OpenAPI first
  (the contract test pins `additionalProperties: false` and the Go test pins
  `len == 2`).
- `HEAD` is 405, not an empty 200; crawlers cannot cache a headless variant.
- The handler never reads the request: no query, header, or cookie affects the
  answer, so there is no per-user oracle.

## Handoff

Report RED and GREEN outputs for steps 2, 6, and 10, the `api-check` output, and
the router test helper name you reused. Suggested commits:
`feat(api): add capabilities read` and `feat(server): serve capabilities`.
