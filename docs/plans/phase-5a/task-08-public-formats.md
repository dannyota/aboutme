# Task 08 — Exact Markdown, discovery, JSON-LD, and CSP formats

**Owner:** Format author in W3.

**Acceptance:** AC-PUB-003/004 representation prerequisites. This task has no
revocation-row test ownership.

**Authorities:** `public-formats.md`, `public-contract.md`, Design v4 web/API,
ADR 0005, ADR 0017, and ADR 0022.

**Files:** The Task 08 row in `file-structure.md`. Do not edit projection, HTML,
Nuxt, Caddy, store/sqlc, or router/main.

**Interfaces:** Consumes Task 05 DTO/origin/cache/response and Task 01/02 global
admission. Produces the exact Step 2 publicformat functions, format versions,
route adapters, and language-neutral goldens.

## Step 1 — RED exact language-neutral bytes

- [ ] Add byte goldens for minimal/full/max documents, every contact/entry/date,
      hostile punctuation/Unicode/control characters, rich-text/list nesting,
      absent/empty fields, photo, discovery on/off, and zero/one/many slugs.
- [ ] Pin exact Markdown escaping/line folding/uppercase destination encoding,
      section/layout order, metadata, rich-text subset, no raw HTML/trailing
      space, at most one blank line, and one final LF.
- [ ] Pin sitemap XML escaping/bytewise slugs and no private fields; exact
      robots bytes; llms fixed heading/URLs; compact ordered JSON-LD, later
      duplicate removal, HTTPS sameAs, exact script bytes, and exactly one CSP
      hash.
- [ ] Compile against Task 05's closed `PublicResume` document/leaf DTO and
      repeat its normalized-origin consumer contract verbatim:

  ```go
  type PublicOrigin struct { value string }
  func ParsePublicOrigin(raw, environment string) (PublicOrigin, error)
  func (o PublicOrigin) String() string
  func (o PublicOrigin) Resolve(path string) string
  ```

  Compile `var _ store.PublicDiscoveryQueries = (*store.Queries)(nil)`;
  aggregate handlers use its exact `GetPublicDiscoverySnapshot` signature and no
  owner-list query. That single generated statement reads the durable generation
  and bytewise eligible-slug snapshot together.

  Use no owner schema type except the retained customization leaf.

- [ ] Run RED:

  ```sh
  (cd apps/server && go test ./internal/publicformat/... ./internal/publicapi/... -race -count=1 -run 'Test(Markdown|Sitemap|Robots|LLMS|JSONLD|CSP)')
  ```

  Expected: functions and shared goldens are absent.

## Step 2 — GREEN exact format interfaces

- [ ] Implement and expose exactly this producer surface, repeated by Tasks 09
      and 10:

  ```go
  type JSONLDResult struct { JSON []byte; Script []byte; CSP string }
  func Markdown(publicresume.PublicResume) ([]byte, error)
  func Sitemap(publicresume.PublicOrigin, []string) ([]byte, error)
  func Robots(publicresume.PublicOrigin) []byte
  func LLMS(publicresume.PublicOrigin, []string) ([]byte, error)
  func JSONLD(publicresume.PublicResume, publicresume.PublicOrigin, bool) (JSONLDResult, error)
  ```

- [ ] Write focused pure encoders with explicit decimal format versions. Sort
      copies bytewise. Consume only the sanitized public DTO. JSON-LD false
      returns nil JSON/script and base CSP; true hashes only compact JSON text-
      node bytes and appends one hash.
- [ ] Add Markdown and discovery route adapters using Task 05 response/cache
      helpers. Aggregate reads take durable generation plus eligible slug
      snapshot, then global lease with one mismatch retry, held through
      response.
- [ ] Run GREEN:

  ```sh
  (cd apps/server && go test ./internal/publicformat/... ./internal/publicapi/... -race -count=1)
  npx prettier --check --ignore-path /dev/null testdata/public-format
  ```

## Executable RED → GREEN checkpoints

- [ ] Markdown RED: load the full hostile golden, call `Markdown(resume)`, and
      compare with `bytes.Equal`; run
      `(cd apps/server && go test ./internal/publicformat -race -count=1 -run TestMarkdown)`
      and observe the function is absent. GREEN: implement the DTO-only encoder
      with explicit section order, escaping, folding, and one final LF; rerun
      the command.
- [ ] Discovery RED: add exact zero/one/many slug goldens and run
      `(cd apps/server && go test ./internal/publicformat -race -count=1 -run 'Test(Sitemap|Robots|LLMS)')`.
      GREEN: implement `Sitemap`, `Robots`, and `LLMS` from
      `PublicOrigin.Resolve` and a sorted copy of slugs; rerun the command.
- [ ] JSON-LD RED: assert `JSONLD(public, origin, false)` returns nil
      JSON/script plus base CSP and `true` returns compact fixed bytes with one
      matching SHA-256 source; run the package test with
      `-run 'Test(JSONLD|CSP)'`. GREEN: build `JSONLDResult` from the closed
      DTO, hash only JSON text-node bytes, and append exactly one hash; rerun
      that command.
- [ ] Routes RED: block global lease admission and assert no cache hit or
      encoder call; run
      `(cd apps/server && go test ./internal/publicapi -race -count=1 -run 'Test(Markdown|Discovery)')`.
      GREEN: add adapters using Task 05 cache/response helpers and one
      generation mismatch retry; rerun the command and the focused Prettier
      golden check.

## Completion

- [ ] Return format versions and fixture names in the exact report.
- [ ] Suggest commit: `feat(public): encode text and discovery formats`.
