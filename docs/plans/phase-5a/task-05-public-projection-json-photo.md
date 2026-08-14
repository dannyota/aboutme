# Task 05 — Public projection, cache, JSON, and photo

**Owner:** Public read author in W2.

**Acceptance:** AC-PUB-003/004 and AC-SEC-001 primitives. This task implements
integration coverage for Task 02's rows 2 and 19; Task 02 owns their names.

**Authorities:** `public-contract.md`, `public-formats.md`, `revocation.md`,
Design v4 data/security, ADR 0005, ADR 0017, and ADR 0022.

**Files:** The Task 05 row in `file-structure.md`. Do not edit OpenAPI,
store/sqlc, formats, directrender, router, or owner mutation handlers.

**Interfaces:** Consumes Task 01 public reads, Task 02 admission, and Task 04
wire schema. Produces the exact Step 1 `PublicOrigin`, DTO, `Snapshot`,
`Project`, and `Reader` API plus cache/response helpers.

## Step 1 — RED the closed DTO, origin, and admission order

- [ ] Add projection goldens for absent details, explicit empty details,
      present-but-fully-filtered details, every schema section/contact/entry,
      hidden/empty pruning, layout pruning, absence-versus-empty, current Go
      sanitizer output, hostile content, and photo URL/crop. Assert no owner,
      account, resume ID, timestamp, flag, hidden marker, or storage key
      appears.
- [ ] Add `ParsePublicOrigin` tests for ASCII HTTP(S), at most 512 bytes, no
      userinfo/non-root path/query/fragment/trailing slash, production/staging
      HTTPS, dev HTTP only on loopback, and resolution without viewer input.
- [ ] Repeat Task 02's exact admission types/method signatures. Add a compile
      assertion against `publicstate.Coordinator`, not a local adapter or alias.
- [ ] The admission compile assertion uses this verbatim subset:

  ```go
  type Representation string
  const (
    RepresentationJSON Representation = "json"
    RepresentationPhoto Representation = "photo"
    RepresentationHTML Representation = "html"
    RepresentationMarkdown Representation = "markdown"
    RepresentationSitemap Representation = "sitemap"
    RepresentationRobots Representation = "robots"
    RepresentationLLMS Representation = "llms"
  )
  func (c *Coordinator) AcquireResume(
    ctx context.Context, id uuid.UUID, expected int64, rep Representation,
  ) (*Lease, error)
  func (l *Lease) Context() context.Context
  func (l *Lease) OnCancel(func()) error
  func (l *Lease) Release()
  ```

- [ ] RED these Task 04-governed producer signatures, repeated by Tasks 08, 10,
      and 11:

  ```go
  type PublicOrigin struct { value string }
  func ParsePublicOrigin(raw, environment string) (PublicOrigin, error)
  func (o PublicOrigin) String() string
  func (o PublicOrigin) Resolve(path string) string

  type PublicResume struct {
    Slug string `json:"slug"`
    Revision string `json:"revision"`
    Lng string `json:"lng"`
    DownloadEnabled bool `json:"downloadEnabled"`
    Document PublicResumeDocument `json:"document"`
  }
  type PublicResumeDocument struct {
    SchemaVersion int64 `json:"schemaVersion"`
    PersonalDetails PublicPersonalDetails `json:"personalDetails"`
    Content PublicContent `json:"content"`
    Customization schema.Customization `json:"customization"`
  }
  type PublicPersonalDetails struct {
    Details PublicDetails; FullName string `json:"fullName"`
    Headline *string `json:"headline,omitempty"`; Photo *PublicPhoto `json:"photo,omitempty"`
  }
  type PublicDetails struct { present bool; value []PublicPersonalDetail }
  func AbsentPublicDetails() PublicDetails
  func PresentPublicDetails([]PublicPersonalDetail) PublicDetails
  func (d PublicDetails) Present() bool
  func (d PublicDetails) Value() []PublicPersonalDetail
  func (p PublicPersonalDetails) MarshalJSON() ([]byte, error)
  type PublicPersonalDetail struct {
    ID string `json:"id"`; Label *string `json:"label,omitempty"`
    Type string `json:"type"`; Value string `json:"value"`
  }
  type PublicPhoto struct { URL string `json:"url"`; Crop *PublicPhotoCrop `json:"crop,omitempty"` }
  type PublicPhotoCrop struct { Height float64 `json:"height"`; Width float64 `json:"width"`; X float64 `json:"x"`; Y float64 `json:"y"` }
  type PublicYearMonth struct { M *int64 `json:"m,omitempty"`; Y int64 `json:"y"` }
  type PublicDateRange struct { End *PublicYearMonth `json:"end"`; Present bool `json:"present"`; Start PublicYearMonth `json:"start"` }
  type PublicProfileEntry struct { ID string `json:"id"`; Text *string `json:"text,omitempty"` }
  type PublicWorkEntry struct {
    City *string `json:"city,omitempty"`; Country *string `json:"country,omitempty"`
    Dates *PublicDateRange `json:"dates,omitempty"`; Description *string `json:"description,omitempty"`
    Employer *string `json:"employer,omitempty"`; EmployerLink *string `json:"employerLink,omitempty"`
    ID string `json:"id"`; JobTitle *string `json:"jobTitle,omitempty"`
  }
  type PublicEducationEntry struct {
    City *string `json:"city,omitempty"`; Country *string `json:"country,omitempty"`
    Dates *PublicDateRange `json:"dates,omitempty"`; Degree *string `json:"degree,omitempty"`
    Description *string `json:"description,omitempty"`; ID string `json:"id"`
    School *string `json:"school,omitempty"`; SchoolLink *string `json:"schoolLink,omitempty"`
  }
  type PublicSkillEntry struct { ID string `json:"id"`; InfoHTML *string `json:"infoHtml,omitempty"`; Level *int64 `json:"level,omitempty"`; Name *string `json:"name,omitempty"` }
  type PublicLanguageEntry struct { ID string `json:"id"`; Level *int64 `json:"level,omitempty"`; Name *string `json:"name,omitempty"` }
  type PublicCertificateEntry struct {
    Date *PublicYearMonth `json:"date,omitempty"`; Description *string `json:"description,omitempty"`
    ID string `json:"id"`; Issuer *string `json:"issuer,omitempty"`
    Title *string `json:"title,omitempty"`; TitleLink *string `json:"titleLink,omitempty"`
  }
  type PublicProjectEntry struct {
    Dates *PublicDateRange `json:"dates,omitempty"`; Description *string `json:"description,omitempty"`
    ID string `json:"id"`; Link *string `json:"link,omitempty"`; Title *string `json:"title,omitempty"`
  }
  type PublicCustomEntry struct {
    City *string `json:"city,omitempty"`; Dates *PublicDateRange `json:"dates,omitempty"`
    Description *string `json:"description,omitempty"`; ID string `json:"id"`
    Subtitle *string `json:"subtitle,omitempty"`; Title *string `json:"title,omitempty"`
    TitleLink *string `json:"titleLink,omitempty"`
  }
  type PublicSection struct {
    SectionType string; DisplayName *string; IconKey *string
    ProfileEntries []PublicProfileEntry; WorkEntries []PublicWorkEntry
    EducationEntries []PublicEducationEntry; SkillEntries []PublicSkillEntry
    LanguageEntries []PublicLanguageEntry; CertificateEntries []PublicCertificateEntry
    ProjectEntries []PublicProjectEntry; CustomEntries []PublicCustomEntry
  }
  type PublicContent map[string]PublicSection
  func (s PublicSection) MarshalJSON() ([]byte, error)
  type Snapshot struct { ResumeID uuid.UUID; Revision int64; DiscoveryEnabled bool; Public PublicResume; photoKey string }
  type ReaderDependencies struct {
    Store store.PublicReadQueries
    Projector *docmigrate.Projector
    Coordinator *publicstate.Coordinator
    Media media.Backend
    Origin PublicOrigin
  }
  type Reader struct {
    store store.PublicReadQueries
    projector *docmigrate.Projector
    coordinator *publicstate.Coordinator
    media media.Backend
    origin PublicOrigin
  }
  func NewReader(ReaderDependencies) (*Reader, error)
  func Project(source resume.Resume, origin PublicOrigin) (PublicResume, error)
  func (r *Reader) ReadResume(context.Context, string, publicstate.Representation) (Snapshot, *publicstate.Lease, error)
  func (r *Reader) ReadPhoto(context.Context, Snapshot) ([]byte, string, error)
  ```

  The concrete DTO fields/tags follow Task 04 exactly; the skeleton fixes every
  named type and forbids owner structs or raw JSON as cross-task substitutes.

- [ ] Compile these exact `publiccache` and `publicapi` producer surfaces; Tasks
      10 and 11 consume them without alternate constructors or writers:

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

- [ ] Test DB-read → acquire → one mismatch reread/retry → unavailable; cache
      and conditional only after lease; media read on the same lease; release
      only after complete response/abort. Pin uniform error bytes and no
      validators.
- [ ] Run RED:

  ```sh
  (cd apps/server && go test ./internal/publicresume/... ./internal/publiccache/... ./internal/publicapi/... -race -count=1 -run 'Test(Origin|Projection|PublicJSON|PublicPhoto|Cache|Conditional)')
  ```

## Step 2 — GREEN projection and exact bytes

- [ ] Implement the closed DTO and projection from current schema types; sort
      map keys explicitly and sanitize every retained rich-text value at this
      boundary. Construct photo URL only from normalized origin plus slug.
- [ ] Implement private cache key fields: route class, representation, variant,
      resume ID, generation, format version, app digest, renderer digest when
      rendered; TTL at most 60 seconds. Admission precedes lookup.
- [ ] Build standard JSON envelope plus one LF and stream exact photo bytes.
      GET/HEAD share selected bytes/status/headers/Content-Length. Conditional
      evaluation follows live admission; ETag is quoted lowercase SHA-256 of
      exact unencoded body. Never send `Set-Cookie`.
- [ ] Register response deadline cancellation with the lease and hold it through
      body completion. Media backends see only the stored key inside Reader.
- [ ] Run GREEN:

  ```sh
  (cd apps/server && go test ./internal/publicresume/... ./internal/publiccache/... ./internal/publicapi/... -race -count=1 -run 'Test(Origin|Projection|PublicJSON|PublicPhoto|Cache|Conditional)')
  make server-build server-vet server-test
  ```

## Executable RED → GREEN checkpoints

Do not batch the projection, admission, and response cycles.

- [ ] Origin/details RED: add `TestPublicOriginEnvironmentRules` and three
      `TestProjectionDetailsPresence` goldens; run
      `(cd apps/server && go test ./internal/publicresume -race -count=1 -run 'Test(PublicOrigin|ProjectionDetailsPresence)')`
      and observe missing types. GREEN: implement `ParsePublicOrigin`,
      `PublicDetails`, and `PublicPersonalDetails.MarshalJSON`; rerun the same
      command.
- [ ] Projection RED: add one current-schema fixture whose hidden and hostile
      values cover every leaf and call `Project(source, origin)`; run the same
      package command with `-run 'TestProjection(Golden|Privacy|Sanitizer)'` and
      observe private fields or unsanitized bytes. GREEN: construct only the
      closed DTO, call the current sanitizer for each retained rich-text leaf,
      prune layout, and sort map keys; rerun that command.
- [ ] Admission/cache RED: implement Task 02 catalog row 19 by blocking
      `AcquireResume` and asserting `Cache.Get` and conditional evaluation
      remain untouched; run
      `(cd apps/server && go test ./internal/publiccache ./internal/publicapi -race -count=1 -run 'Test(CacheAndConditional|PublicReadMismatch)')`.
      GREEN: implement `publiccache.New/Get/Put/Purge`, then `Reader.ReadResume`
      with one exact-revision retry before cache/conditional work; rerun that
      command.
- [ ] Bytes RED: add exact JSON-LF, photo, GET/HEAD/304, strong ETag, and abort
      cases; run
      `(cd apps/server && go test ./internal/publicapi -race -count=1 -run 'Test(PublicJSON|PublicPhoto|SelectedResponse)')`.
      GREEN: construct every response through `NewSelectedResponse` and keep the
      lease until `ServeHTTP` completes or its cancel hook joins; rerun that
      command, then `make server-build server-vet server-test`.

## Completion

- [ ] Return DTO field list, format versions, cache dimensions, and exact
      report.
- [ ] Suggest commit: `feat(public): project and serve public resume data`.
