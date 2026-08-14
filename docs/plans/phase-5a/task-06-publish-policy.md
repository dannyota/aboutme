# Task 06 — Publish decoding, completeness, and slug policy

**Owner:** Publish policy author in W1.

**Acceptance:** AC-PUB-001/002 policy prerequisites. This task implements
integration coverage for Task 02's row 7; Task 02 owns its name.

**Authorities:** `design.md`, `public-contract.md`, Design v4 data/security, ADR
0004, ADR 0016, ADR 0019, and ADR 0022.

**Files:** New `resumeapi/publish.go`, `completeness.go`, `slug_limiter.go`, and
same-basename tests only. Do not register the route or edit existing resumeapi
files; Task 07 owns integration.

**Interfaces:** Consumes Task 00 `publicroots.Reserved`, Task 04 request shape,
and current `schema.Resume`. Produces the exact Step 2 package-private types,
functions, and limiter interface for Task 07.

## Step 1 — RED the pure publish policy

- [ ] Add strict decode tests for required booleans and omitted/nonempty slug.
      Reject `slug:null`, `slug:""`, singleton-header violations, unknown/
      duplicate fields, wrong types, trailing data, and body overflow as exact
      `400 request_invalid` before policy or limiter work.
- [ ] Add a complete table for every issue in `public-contract.md`. Assert exact
      path/code/message, UTF-8 path then code then message order, deduplication,
      and collection of independent failures without short circuit.
- [ ] Add slug grammar/length, generated `publicroots.Reserved`, flag relation,
      current/changed slug classification, and recent-reauth preflight tests.
- [ ] Add a fixed-clock per-account rolling-hour limiter. The first 30 changed-
      slug attempts proceed; the 31st returns the approved `429` before any
      availability/tombstone/owner detail. Omitted/unchanged slug does not
      consume capacity; shape-invalid empty slug never reaches it; denied valid
      changed attempts count and buckets are bounded.
- [ ] Run RED:

  ```sh
  (cd apps/server && go test ./internal/resumeapi/... -race -count=1 -run 'Test(PublishDecode|Completeness|SlugPolicy|SlugAttemptLimiter|CheapPreflight)')
  ```

  Expected: the three policy modules do not exist.

## Step 2 — GREEN exact package-private seams

- [ ] Implement these package-private producer names, consumed directly by Task
      07 in the same package:

  ```go
  type optionalSlug struct { Present bool; Value string }
  type publishInput struct {
    Slug optionalSlug
    Live bool
    DownloadEnabled bool
    SEOGeoEnabled bool
  }
  type currentPublish struct {
    Slug *string
    Live bool
    DownloadEnabled bool
    SEOGeoEnabled bool
    Revision int64
  }
  type publishPrepared struct {
    Effective currentPublish
    ChangedSlug bool
    Issues []publishIssue
  }
  type publishIssue struct { Path string; Code string; Message string }
  type publishShapeError struct { Field string }
  func (e *publishShapeError) Error() string
  func decodePublish(io.Reader) (publishInput, error)
  func validatePublish(schema.Resume, currentPublish, publishInput) publishPrepared
  type slugAttemptLimiter interface {
    AllowChangedSlug(accountID uuid.UUID, now time.Time) bool
  }
  ```

- [ ] Keep ordering explicit and bytewise. Use the current schema's typed
      section discriminators, required-field rules, whitespace semantics, and
      visible-entry definition; reserved-root validation calls the generated
      `publicroots.Reserved` function. Do not read claims or mutate storage
      here.
- [ ] Make all failures closed typed values mapped by Task 07; error text/logs
      contain no resume content, candidate slug ownership, or session data.
- [ ] Run GREEN:

  ```sh
  (cd apps/server && go test ./internal/resumeapi/... -race -count=1 -run 'Test(PublishDecode|Completeness|SlugPolicy|SlugAttemptLimiter|CheapPreflight)')
  make server-build server-vet server-test
  ```

## Executable RED → GREEN checkpoints

- [ ] Decode RED: add table rows
      `{"live":true,"downloadEnabled":true,"seoGeoEnabled":false}`, the same
      with `"slug":null`, and the same with `"slug":""`; run
      `(cd apps/server && go test ./internal/resumeapi -race -count=1 -run TestPublishDecode)`
      and observe `decodePublish` is absent. GREEN: decode tokens into
      `optionalSlug{Present:false}` only for omission and return
      `publishShapeError{Field:"slug"}` for null/empty; rerun the command.
- [ ] Policy RED: construct `currentPublish{Slug:&old, Revision:9}` separately
      from `schema.Resume`, run the package test with
      `-run 'Test(Completeness|SlugPolicy|CheapPreflight)'`, and observe
      unchanged slug/flags are misclassified. GREEN: implement
      `validatePublish(source, current, input)` with the complete
      sorted/deduplicated issue table and generated `publicroots.Reserved`;
      rerun that command.
- [ ] Limiter RED: drive a fixed clock through 31 distinct valid changed-slug
      attempts and run the package test with `-run TestSlugAttemptLimiter`;
      observe no rolling-hour bound. GREEN: implement a bounded per-account
      timestamp queue behind `AllowChangedSlug`, count denied changed attempts,
      and skip omitted/unchanged inputs; rerun that command with `-race`, then
      `make server-build server-vet server-test`.

## Completion

- [ ] Return issue-table count/order and limiter boundary in the exact report.
- [ ] Suggest commit: `feat(publish): validate completeness and slug policy`.
