# Task 11 — Public dispatch, readiness, and server composition

**Owner:** Integration owner in serialized window W6.

**Acceptance:** AC-PUB-003/004 and AC-OPS-005 integration. This task implements
coverage for Task 02's rows 20 and 21; Task 02 owns the names.

**Authorities:** All four Phase 5A docs, Design v4 API/deployment/security, ADR
0017, ADR 0022, and Tasks 00–10 interfaces.

**Files:** The Task 11 row in `file-structure.md`. Do not edit producer
packages, OpenAPI/generated client, topology/Caddy, manifests, or native proof.

**Interfaces:** Consumes Task 02 coordinator, Task 05 reader/origin, Task 07
resume service, and Task 10 client. Produces the exact Step 1 `PublicRoutes`,
expanded `api.New`, composite readiness, and server composition.

## Step 1 — RED full-router order and composite readiness

- [ ] Compile directly against Task 02's Coordinator/recovery contract and Task
      05's Reader contract. Repeat Task 05's normalized-origin contract and Task
      10's consumer surface verbatim:

  ```go
  type PublicOrigin struct { value string }
  func ParsePublicOrigin(raw, environment string) (PublicOrigin, error)
  func (o PublicOrigin) String() string
  func (o PublicOrigin) Resolve(path string) string
  ```

  ```go
  type CoordinatorConfig struct { DiscoveryGeneration int64; Now func() time.Time }
  func NewCoordinator(CoordinatorConfig) (*Coordinator, error)
  func (c *Coordinator) Ready() error
  ```

  ```go
  const PublicRenderMode = "continuous"
  type PublicRenderRequest struct {
    PublicResume publicresume.PublicResume `json:"publicResume"`
    Mode string `json:"mode"`
    CanonicalOrigin string `json:"canonicalOrigin"`
    DiscoveryEnabled bool `json:"discoveryEnabled"`
  }
  type Result struct { HTML []byte }
  type RenderOrigin struct { value string }
  type Client struct { origin RenderOrigin; http *http.Client }
  var ErrRenderUnavailable = errors.New("direct render unavailable")
  type RenderStatusError struct { Status int }
  func (e *RenderStatusError) Error() string
  type RenderResponseTooLargeError struct { Limit int64 }
  func (e *RenderResponseTooLargeError) Error() string
  func ParseRenderOrigin(raw, environment string) (RenderOrigin, error)
  func (o RenderOrigin) String() string
  func New(origin RenderOrigin, client *http.Client) *Client
  func (c *Client) Render(context.Context, PublicRenderRequest) (Result, error)
  func (c *Client) Probe(context.Context, PublicRenderRequest) error
  ```

  ```go
  type HTMLDependencies struct {
    Reader *publicresume.Reader
    Cache *publiccache.Cache
    Renderer *directrender.Client
    PublicOrigin publicresume.PublicOrigin
    AppDigest string
    RendererDigest string
  }
  func NewHTMLHandler(HTMLDependencies) (http.Handler, error)
  ```

  Consume, do not wrap or redeclare, each producer in implementation. Compile
  `var _ store.PublicReadQueries = (*store.Queries)(nil)` and initialize the
  coordinator only from its exact `GetPublicState` result.

- [ ] Add these fields to existing `config.Config`, and keep its existing
      `PublicOrigin string` field for auth/CSRF consumers. The composition root
      converts the two strings with distinct parsers exactly once:

  ```go
  type publicRuntime struct {
    PublicOrigin publicresume.PublicOrigin
    RenderOrigin directrender.RenderOrigin
    AppDigest string
    RendererDigest string
  }
  func parsePublicRuntime(
    publicOrigin string,
    renderOrigin string,
    environment string,
    appDigest string,
    rendererDigest string,
  ) (publicRuntime, error)
  ```

  `config.Config` gains exactly `PublicRenderOrigin string`,
  `AppBuildDigest string`, and `PublicRendererBuildDigest string`. `config.Load`
  reads all four named variables. `parsePublicRuntime` invokes
  `ParsePublicOrigin(publicOrigin, environment)` and
  `ParseRenderOrigin(renderOrigin, environment)`; neither parser is used for the
  other value.

- [ ] Compile this exact seam. `PublicRoutes` and `New` are package `api`;
      readiness types/functions are package `publicstate`:

  ```go
  type PublicRoutes interface {
    Recognizes(escapedPath string) bool
    ServeHTTP(http.ResponseWriter, *http.Request)
  }
  type ReadinessDependencies struct {
    PingDatabase func(context.Context) error
    ProbeRenderer func(context.Context) error
  }
  func New(
    logger *slog.Logger,
    pinger DBPinger,
    options Options,
    public PublicRoutes,
    register ...func(*http.ServeMux),
  ) http.Handler
  func NewReadiness(*Coordinator, ReadinessDependencies) *Readiness
  func (r *Readiness) Ping(context.Context) error
  ```

- [ ] In full `api.New` router tests, place a hostile or oversize body on POST,
      PUT, and PATCH to every recognized public fixed/dynamic path. Assert exact
      route-family `405` and `Allow: GET, HEAD` before default BodyLimit/default
      rate middleware reads the body. Assert valid GET/HEAD then receives its
      public limiter/body controls and exact API/health/private behavior
      remains.
- [ ] Test malformed/nested/percent paths, fixed roots, `.md`, API isolation,
      Caddy-derived dispatch parity, private uniform absence, conditionals after
      admission, and viewer public routes never fall through to Nuxt.
- [ ] Test readiness false for invalid/missing public state, unresolved
      recovery, fence invariant, PostgreSQL failure, and bounded direct-Nuxt
      failure; existing one-second single-flight wraps the composite once.
      `/healthz` stays independent.
- [ ] Run RED:

  ```sh
  (cd apps/server && go test ./internal/publicapi/... ./internal/api/... ./internal/config/... ./cmd/server/... -race -count=1 -run 'Test(PublicDispatch|Readiness|PublicRenderConfig|ServerComposition|RestartGeneration|OriginAdmission)')
  ```

  Expected: public dispatch is absent or occurs behind default middleware.

## Step 2 — GREEN composition in exact middleware position

- [ ] In `api.New`, keep health dispatch first. Immediately next call
      `PublicRoutes.Recognizes(r.URL.EscapedPath())`; if true call public routes
      before constructing/entering the default BodyLimit/rate chain. Within
      public routes, resolve the recognized route, emit wrong-method 405/Allow,
      then apply public-specific GET/HEAD controls and resource admission.
- [ ] Validate `PUBLIC_ORIGIN` only with Task 05 `ParsePublicOrigin` and
      `PUBLIC_RENDER_ORIGIN` only with Task 10 `ParseRenderOrigin`. Require
      printable nonempty app/renderer digests. Errors name the variable but not
      its value. Pass `PublicOrigin.String()` into every render DTO.
- [ ] Compose store, coordinator/recovery/readiness, transition-aware resumeapi,
      public service, media backend, and direct client. Eagerly load global
      public state and resolve retained ambiguity before ready; initialize
      resume fences lazily from exact reads, never scans or guessed revision.
- [ ] Prove restart uses committed generation even with stale cache/invalidation
      failure. Prove each origin validation/revalidation admitted after a close
      sees only new state, while already admitted response follows lease rules.
- [ ] Run GREEN:

  ```sh
  (cd apps/server && go test ./internal/publicapi/... ./internal/api/... ./internal/config/... ./cmd/server/... -race -count=1)
  make server-build server-vet server-test
  ```

## Executable RED → GREEN checkpoints

- [ ] Dispatch RED: send oversize POST/PUT/PATCH bodies to every recognized
      fixed/dynamic public path through full `api.New`; run
      `(cd apps/server && go test ./internal/api -race -count=1 -run TestPublicDispatch)`
      and observe default BodyLimit returns `413`. GREEN: after health dispatch,
      branch on `public.Recognizes(r.URL.EscapedPath())` before
      building/entering the default body/rate chain; rerun and expect exact
      `405` plus `Allow: GET, HEAD` without reading the body.
- [ ] Config RED: table-test the external HTTPS origin, ECS loopback renderer,
      Compose `web:3000`, swapped variables, 513-byte/control/non-ASCII values,
      credentials, paths, and blank digests; run
      `(cd apps/server && go test ./internal/config ./cmd/server -race -count=1 -run TestPublicRenderConfig)`.
      GREEN: add the three config fields and implement the shown
      `parsePublicRuntime` with the two distinct parsers and printable digests;
      rerun the command.
- [ ] Readiness RED: make coordinator, database, and direct renderer fail one at
      a time and issue concurrent `/readyz` requests; run
      `(cd apps/server && go test ./internal/publicstate ./internal/api -race -count=1 -run TestReadiness)`
      and observe incomplete/split caching. GREEN: implement
      `NewReadiness`/`Ping` and pass that composite once through the existing
      one-second single-flight cache; rerun the command.
- [ ] Composition RED: restart with durable generation 41 plus a stale cache and
      unresolved recovery proof; run
      `(cd apps/server && go test ./cmd/server ./internal/publicapi -race -count=1 -run 'Test(ServerComposition|RestartGeneration|OriginAdmission)')`.
      GREEN: compose the exact D8/D10 constructors, load durable state before
      ready, purge only as optimization, and retain closed ambiguity; rerun the
      command, then `make server-build server-vet server-test`.

## Completion

- [ ] Return middleware order, readiness dependency list, and exact report.
- [ ] Suggest commit: `feat(server): compose public routes and readiness`.
