# Task 10 — Direct-render client and HTML gateway

**Owner:** HTML author in W5.

**Acceptance:** AC-PUB-003/004 HTML integration. This task has no revocation-row
test ownership.

**Authorities:** `public-contract.md`, `public-formats.md`, `revocation.md`, ADR
0005, ADR 0017, ADR 0022, and Tasks 05/08/09 contracts.

**Files:** The Task 10 row in `file-structure.md`. Do not edit router,
config/main, Nuxt, Caddy/topology, cache/projection, or formats.

**Interfaces:** Consumes Task 05 DTO/cache/response, Task 08 formats, and Task
09 route. Produces the exact Step 1 request/result/client and HTML handler
constructor for Task 11.

## Step 1 — RED the bounded direct call and complete HTML selection

- [ ] Compile directly against Task 05's closed `PublicResume` DTO; every
      referenced leaf is defined there. Repeat its normalized-origin contract
      verbatim, then Task 08's format consumer surface verbatim:

  ```go
  type PublicOrigin struct { value string }
  func ParsePublicOrigin(raw, environment string) (PublicOrigin, error)
  func (o PublicOrigin) String() string
  func (o PublicOrigin) Resolve(path string) string
  ```

  ```go
  type JSONLDResult struct { JSON []byte; Script []byte; CSP string }
  func Markdown(publicresume.PublicResume) ([]byte, error)
  func Sitemap(publicresume.PublicOrigin, []string) ([]byte, error)
  func Robots(publicresume.PublicOrigin) []byte
  func LLMS(publicresume.PublicOrigin, []string) ([]byte, error)
  func JSONLD(publicresume.PublicResume, publicresume.PublicOrigin, bool) (JSONLDResult, error)
  ```

  Send exactly the same four fields as Task 09 with no local alias or extra
  value.

- [ ] Add `ParseRenderOrigin` tests for the exact environment allowlist, at most
      512 printable ASCII bytes, and rejection of control/non-ASCII,
      credentials, path, query, fragment, trailing slash, and HTTPS. Add client
      tests for exact direct origin and POST path, content type, canonical
      four-field JSON, no ambient cookie/auth/viewer/forwarding header,
      532,480-byte request cap, 2,097,152-byte response cap, exact response
      type, non-200, duplicate header, failure, cancellation, and five seconds.
- [ ] Compile this exact producer interface, repeated by Task 11:

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

- [ ] Consume these Task 05 cache and selected-response signatures verbatim,
      then compile the HTML constructor repeated by Task 11:

  ```go
  type RouteClass string
  type Variant string
  type Key struct {
    RouteClass RouteClass
    Representation publicstate.Representation
    Variant Variant
    ResumeID uuid.UUID
    Generation int64
    FormatVersion int
    AppDigest string
    RendererDigest string
  }
  type Value struct { Status int; Header http.Header; Body []byte }
  type cacheEntry struct { Value Value; ExpiresAt time.Time; Sequence uint64 }
  type Cache struct {
    mu sync.RWMutex
    entries map[Key]cacheEntry
    maxEntries int
    ttl time.Duration
    now func() time.Time
    sequence uint64
  }
  func New(maxEntries int, ttl time.Duration, now func() time.Time) (*Cache, error)
  func (c *Cache) Get(Key) (Value, bool)
  func (c *Cache) Put(Key, Value)
  func (c *Cache) Purge()

  var ErrInvalidSelectedResponse = errors.New("invalid public response")
  type SelectedResponse struct { Status int; Header http.Header; Body []byte }
  func NewSelectedResponse(
    status int, contentType, cacheControl string, body []byte, extra http.Header,
  ) (SelectedResponse, error)
  func (s SelectedResponse) ServeHTTP(http.ResponseWriter, *http.Request)
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

  The client wraps transport, cancellation, malformed-response, and non-200
  failures with `ErrRenderUnavailable`; callers may inspect the two typed causes
  with `errors.As`, but public responses stay the same generic `503`.

- [ ] Add HTML tests for admission → private cache → direct render → complete
      validation → response. Go independently derives expected JSON-LD/CSP,
      accepts exactly one expected data script when discoverable and none when
      not, rejects all other inline script, then computes body-digest ETag.
- [ ] Run RED:

  ```sh
  (cd apps/server && go test ./internal/directrender/... ./internal/publicapi/... -race -count=1 -run 'Test(Client|PublicHTML)')
  ```

  Expected: client and HTML handler are absent.

## Step 2 — GREEN exact complete bytes

- [ ] Implement `Client` with a context derived from viewer lease and bounded by
      the earlier of it or five seconds. Encode to a capped buffer; read limit
      plus one; close response; and wait for HTTP work to return on cancel.
- [ ] Implement HTML handler using Task 05 admission/cache and Task 08 JSON-LD.
      Validate status, type, size, canonical, title, main, DOM revision,
      external assets, and script set before writing success. Hold lease through
      direct work and complete response/abort; renderer failure is uniform
      `503`, never stale.
- [ ] Discoverable success has exact one-hash CSP and no per-resume noindex.
      Nondiscoverable has base CSP, no JSON-LD, and exact
      `X-Robots-Tag: noindex, noarchive`. GET/HEAD/304 and ETags use Task 05's
      exact response helpers and unencoded bytes.
- [ ] Run GREEN:

  ```sh
  (cd apps/server && go test ./internal/directrender/... ./internal/publicapi/... -race -count=1 -run 'Test(Client|PublicHTML)')
  make server-build server-vet server-test
  ```

## Executable RED → GREEN checkpoints

- [ ] Origin/client RED: parse external and render origins through the opposite
      parser, then send a capped request to a recording server; run
      `(cd apps/server && go test ./internal/directrender -race -count=1 -run 'Test(RenderOrigin|Client)')`
      and observe the package is absent. GREEN: implement `ParseRenderOrigin`,
      `New`, and `Render` with only the four-field JSON, direct origin, exact
      caps, no ambient headers, and viewer/five-second context; rerun the
      command.
- [ ] Cancellation RED: block the test transport forever, cancel the lease, and
      assert `Render` does not return until the transport goroutine exits; run
      the directrender test with `-run TestClientCancellationJoin`. GREEN: close
      the response body, cancel the request, join the HTTP call, and wrap the
      cause with `ErrRenderUnavailable`; rerun that command.
- [ ] HTML RED: return documents with wrong canonical/title/main/revision, extra
      script, bad JSON-LD, and oversize bytes; run
      `(cd apps/server && go test ./internal/publicapi -race -count=1 -run TestPublicHTML)`
      and observe no handler. GREEN: implement
      `NewHTMLHandler(HTMLDependencies)` and the admission → cache → render →
      complete-validation → `NewSelectedResponse` path; rerun that command.
- [ ] Variant RED: assert discoverable one-hash CSP and nondiscoverable base CSP
      plus exact robots header, including GET/HEAD/304 ETags; run the publicapi
      test with
      `-run 'TestPublicHTML(Discoverable|Nondiscoverable|Conditional)'`. GREEN:
      derive JSON-LD independently, validate the complete worker output, and
      cache only under all D10 dimensions; rerun the command, then
      `make server-build server-vet server-test`.

## Completion

- [ ] Return envelope byte maxima, validation checks, and exact report.
- [ ] Suggest commit: `feat(public): serve validated direct-rendered HTML`.
