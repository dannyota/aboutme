# Phase 5A implementation decisions

These decisions close implementation seams without changing the four approved
Phase 5A authorities. Task files repeat each compile contract in full at its
producer and consumers.

## D1 — Topology, routes, and build identity

Compose adds internal `render` network `10.91.0.0/28` with only server and web.
Server also joins edge at fixed `10.90.0.2` and binds Go only there. Web joins
frontend/render but never edge, so web-to-Go is refused and `render` is never
trusted or published. Go calls `http://web:3000`; Caddy and web share frontend,
so Caddy security is tested as route denial before fallback, not network
impossibility. Native origins are `127.0.0.1:20030` and `127.0.0.1:20440`; ECS
remains same-task `127.0.0.1:3000`.

`packages/publicroots/public-roots.v4.json` is a closed object with integer
`version: 4` and `roots`. Each closed row has unique string `root` and dispatch
from `reserved|go|nuxt|deny`. These are the exact 14 authority-order rows:

| Root              | Dispatch   |
| ----------------- | ---------- |
| `admin`           | `reserved` |
| `api`             | `go`       |
| `app`             | `nuxt`     |
| `healthz`         | `go`       |
| `_nuxt`           | `nuxt`     |
| `internal-render` | `deny`     |
| `llms.txt`        | `go`       |
| `login`           | `nuxt`     |
| `people`          | `reserved` |
| `print`           | `deny`     |
| `readyz`          | `go`       |
| `robots.txt`      | `go`       |
| `sitemap.xml`     | `go`       |
| `u`               | `reserved` |

Caddy applies deny trees, fixed Go roots, fixed Nuxt trees, reserved namespace
404, valid 4–30-byte slug or slug plus `.md` to Go, then Nuxt fallback. Invalid
unregistered segments fall through; reserved-only roots never do.

T00 owns closed `packages/publicroots/app-build-sources.v1.json` and
`renderer-build-sources.v1.json`. App membership is `apps/server/cmd/server` and
`apps/server/internal` recursive; `packages/schema/gen/go` recursive; and files
`apps/server/go.mod`, `apps/server/go.sum`, and the v4 registry. Renderer
membership is `apps/web/app`, `apps/web/public`, and `apps/web/server`
recursive; and files `apps/web/nuxt.config.ts`, `apps/web/package.json`,
`apps/web/package-lock.json`, and the v4 registry. Roots are raw-byte sorted;
recursive roots include all regular descendants and reject symlinks.

Local digest input is ASCII `aboutme.source-manifest.v1\x00`, then
`u64be(len(raw manifest))`, raw manifest, then for each raw-byte-sorted POSIX
path: `u32be(len(path))`, path, `u64be(len(file))`, and file bytes. The value is
`sha256:` plus lowercase hex. Production injects the server/web image OCI digest
from the release. Native/HTTPS temporary Caddyfiles inline the verified fragment
and record raw source, fragment, and effective-file SHA-256 values.

## D2 — Public DTO, external origin, and presence

Task 04 OpenAPI is wire authority. Task 05 owns the identically shaped closed Go
DTO; Task 05 repeats every leaf and JSON tag. No owner DTO crosses this seam.

```go
type PublicOrigin struct { value string }
func ParsePublicOrigin(raw, environment string) (PublicOrigin, error)
func (o PublicOrigin) String() string
func (o PublicOrigin) Resolve(path string) string

type PublicDetails struct { present bool; value []PublicPersonalDetail }
func AbsentPublicDetails() PublicDetails
func PresentPublicDetails([]PublicPersonalDetail) PublicDetails
func (d PublicDetails) Present() bool
func (d PublicDetails) Value() []PublicPersonalDetail
func (p PublicPersonalDetails) MarshalJSON() ([]byte, error)

type ReaderDependencies struct {
  Store store.PublicReadQueries
  Projector *docmigrate.Projector
  Coordinator *publicstate.Coordinator
  Media media.Backend
  Origin PublicOrigin
}
func NewReader(ReaderDependencies) (*Reader, error)
func Project(resume.Resume, PublicOrigin) (PublicResume, error)
```

The parser accepts at most 512 printable ASCII origin bytes, no credentials,
path, query, fragment, or trailing slash. Prod/staging require HTTPS; dev HTTP
requires loopback. It reads only `PUBLIC_ORIGIN`. Details omission represents
source absence; explicit source-empty and present-but-fully-filtered both emit
`"details":[]`. Projection re-sanitizes retained rich text and never emits
account identity, owner title, timestamps, hidden data, or photo storage key.

## D3 — Fence and transition state

Task 02 is the exact producer; Tasks 05/07/11 repeat the full `Representation`,
`Plan`, `CommittedState`, recovery, `Coordinator`, `Lease`, and `Transition`
signatures. The typed failures are `ErrAdmissionClosed`,
`*GenerationMismatchError`, `*DrainTimeoutError`, and `*RecoveryUnresolvedError`
with `Unwrap`.

New reads acquire exact revision/generation. Missing resume fences initialize
only from a database-read revision. Non-draining commit retains old admitted
leases but rejects new old-generation admission. Revoking/global close cancels
and joins every retained generation. Global is acquired first, then resume UUID
bytes. One caller-supplied absolute five-second deadline covers all closes and
handler joins. Timeout begins no transaction and reopens exact old state.

Definite proof opens only supplied committed/rolled-back state. Ambiguous state
stays closed until a read-only resolver proves one outcome; mixed/unavailable
proof fails readiness and never reruns mutation. Task 02 alone names the 22 ADR
0022 race tests.

Task 11 reads the exact durable discovery generation once at composition,
constructs one coordinator, and shares that pointer with `resumeapi`, public
readers, and readiness. `resumeapi.Options` also receives the server
`*store.Pool` as `RecoveryPool` so ambiguous recovery can use a new connection.
Missing composition dependencies fail closed before transition or database work;
no consumer guesses an initial generation.

## D4 — Existing idempotency transaction protocol

Task 03 adds `Recheck`; it does not replace existing transaction ownership.

```go
type RecheckDecision uint8
const ( RecheckFresh RecheckDecision = iota; RecheckReplay; RecheckReuse )
type RecheckResult struct { Decision RecheckDecision; Response StoredResponse }
func (s *IdempotencyStore) Recheck(
  context.Context, uuid.UUID, string, uuid.UUID, [32]byte,
) (RecheckResult, error)
func (s *IdempotencyStore) Execute(
  context.Context, uuid.UUID, string, uuid.UUID, [32]byte,
  func(*store.Queries) (StoredResponse, error),
) (ExecuteResult, error)
```

Existing `StoredResponse`, `ExecuteResult`, and `CommitOutcome` remain
unchanged: `CommitNotAttempted`, `CommitDefinitelyRolledBack`,
`CommitCommitted`, and `CommitUnknown`. Task 03 and Task 07 repeat their full
fields and values. Replay/reuse is checked again after transition ownership and
before CAS.

## D5 — Public format functions

```go
type JSONLDResult struct { JSON []byte; Script []byte; CSP string }
func Markdown(publicresume.PublicResume) ([]byte, error)
func Sitemap(publicresume.PublicOrigin, []string) ([]byte, error)
func Robots(publicresume.PublicOrigin) []byte
func LLMS(publicresume.PublicOrigin, []string) ([]byte, error)
func JSONLD(publicresume.PublicResume, publicresume.PublicOrigin, bool) (JSONLDResult, error)
```

Go owns exact Markdown/discovery/JSON-LD bytes. The false JSON-LD case returns
nil JSON/script plus base CSP. The true case hashes only compact JSON text-node
bytes and yields exactly one CSP hash.

## D6 — Direct render origin and client

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
var ErrRenderUnavailable = errors.New("direct render unavailable")
type RenderStatusError struct { Status int }
type RenderResponseTooLargeError struct { Limit int64 }
func ParseRenderOrigin(raw, environment string) (RenderOrigin, error)
func (o RenderOrigin) String() string
func New(RenderOrigin, *http.Client) *Client
func (c *Client) Render(context.Context, PublicRenderRequest) (Result, error)
func (c *Client) Probe(context.Context, PublicRenderRequest) error
```

Render origin reads only `PUBLIC_RENDER_ORIGIN`, accepts at most 512 printable
ASCII bytes and only HTTP, and rejects credentials/path/query/fragment/trailing
slash. Prod/staging allow exactly `127.0.0.1:3000`; dev allows
`127.0.0.1:20030`, `127.0.0.1:20440`, or `web:3000`. Request/response caps are
532,480/2,097,152 bytes. Canonical origin always comes from D2. Failures are
generic public `503`; typed causes stay internal.

## D7 — Joined Nuxt worker and hydration

Vite 8.2.0 plus `@vitejs/plugin-vue` 6.0.8 builds `worker-entry.ts` as Vue SSR.
Nitro emits stable `workers/public-render.mjs`; virtual
`#public-render-worker-url` uses `ROLLUP_FILE_URL`. One Worker receives
immutable four-field `workerData`. Success resolves only after clean exit. Abort
or exact five seconds calls `terminate()` once, awaits exit, and rejects late
output. Malformed/error/nonzero/no-result/oversize all fail generically.

Matching public revision hydrates with `createSSRApp`; differing revision clears
and replaces through `createApp`. Fetch/parse/mount failure leaves the
accessible SSR title, main, text, and links unchanged.

## D8 — Public dispatch and readiness

```go
type PublicRoutes interface {
  Recognizes(string) bool
  ServeHTTP(http.ResponseWriter, *http.Request)
}
type ReadinessDependencies struct {
  PingDatabase func(context.Context) error
  ProbeRenderer func(context.Context) error
}
func NewReadiness(*Coordinator, ReadinessDependencies) *Readiness
func (r *Readiness) Ping(context.Context) error
```

`api.New` checks health, then recognized public paths, before default body/rate
middleware. Public dispatch resolves route and returns wrong-method `405`/Allow
before valid-method controls. The existing one-second single-flight readiness
cache wraps the composite once. Readiness includes database, coordinator and
unresolved recovery, and bounded direct renderer; health stays independent.

## D9 — Isolated native fixture

Capture owns only database `aboutme_p5a_fixture` in the existing container,
state root `.dev/p5a-fixture-runtime`, and media root `.dev/p5a-fixture-media`.
It records/stops normal native state, migrates and seeds before a fresh stack,
kills/joins, cleans/drops/removes/verifies, then restarts untouched
`aboutme_dev` only if it was initially active. This process boundary discards
fixture cache/coordinator state.

The fixture command clock is `2035-01-01T00:00:00Z`; tombstone `p5a-renamed-old`
is released `2034-12-31T23:00:00Z`. Generation is 41. Owner UUID ends `0000`;
exactly three resumes end `0001`–`0003`: discoverable `p5a-live-photo` r11 with
photo and old tombstone, nondiscoverable `p5a-live-noindex` r12, and private
`p5a-private` r13. The current fixture is `resume-v2.json`. Capture is read-only
after seed.

## D10 — Cache, response, and HTML construction

Task 05 repeats the full `publiccache.Cache` fields and methods. Its key is
exactly route class, representation, variant, resume UUID, generation, format
version, app digest, and renderer digest. Renderer digest is empty only for
non-rendered representations. TTL is at most 60 seconds; values copy header and
body on put/get.

```go
type SelectedResponse struct { Status int; Header http.Header; Body []byte }
func NewSelectedResponse(
  int, string, string, []byte, http.Header,
) (SelectedResponse, error)
func (s SelectedResponse) ServeHTTP(http.ResponseWriter, *http.Request)
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

The selected-response constructor is the only GET/HEAD writer and hashes exact
unencoded body bytes. HTML construction rejects nil dependencies/digests and
uses admission → private cache → direct render → complete validation → response.
