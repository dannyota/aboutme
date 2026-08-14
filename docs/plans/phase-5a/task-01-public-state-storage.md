# Task 01 — Public-state storage and slug transactions

**Owner:** Integration owner in serialized window W0b.

**Acceptance:** AC-PUB-001/002/004 storage prerequisites. This task supplies
primitive coverage consumed by Task 02's named rows 16–18 and 20; it owns no
revocation-row test name.

**Authorities:** `design.md`, `public-contract.md`, `revocation.md`, ADR 0004,
ADR 0016, ADR 0019, and ADR 0022.

**Files:** The Task 01 row in `file-structure.md`. Do not edit resumeapi, public
handlers, OpenAPI, or non-generated root files.

**Interfaces:** Produces migration 00007 and the exact sqlc query names in
Step 2. Consumes existing resume/idempotency tables and `public.citext` mapping.

## Step 1 — RED the migration and transaction surface

- [ ] Add migration/query tests for singleton positive generation, initial 1,
      monotonic update, down/up, public lookup uniformity, bytewise eligible
      slug order, exact proof rows, and rollback of every publish/delete write.
- [ ] Pin this advisory-lock expression and old/new raw UTF-8 slug ordering:

  ```sql
  SELECT pg_advisory_xact_lock(
    hashtextextended('aboutme.slug.v1:' || sqlc.arg(slug)::text, 0)
  );
  ```

- [ ] Generate these exact package `store` types/signatures and compile
      `public_contract.go`; Tasks 05/07/08/11 consume the named interfaces:

  ```go
  type PublicState struct { Singleton bool; DiscoveryGeneration int64 }
  type GetPublicResumeByOwnerParams struct { UserID uuid.UUID; ID uuid.UUID }
  type ConsumeExpiredSlugTombstoneParams struct { Slug string; ReusableAt time.Time }
  type InsertSlugTombstoneParams struct {
    Slug string
    ReleasedByUserID *uuid.UUID
    ReleasedAt time.Time
  }
  type PublishResumeCASParams struct {
    ID uuid.UUID
    UserID uuid.UUID
    ExpectedRevision int64
    Slug *string
    Live bool
    DownloadEnabled bool
    SEOGeoEnabled bool
    UpdatedAt time.Time
  }
  type DeleteResumePublicCASParams struct {
    ID uuid.UUID
    UserID uuid.UUID
    ExpectedRevision int64
  }
  type PublicReadQueries interface {
    GetPublicState(context.Context) (PublicState, error)
    GetPublicResumeBySlug(context.Context, string) (Resume, error)
    GetPublicResumeByOwner(context.Context, GetPublicResumeByOwnerParams) (Resume, error)
    ListEligiblePublicSlugs(context.Context) ([]string, error)
  }
  type PublicDiscoveryQueries interface {
    GetPublicDiscoverySnapshot(context.Context) (GetPublicDiscoverySnapshotRow, error)
  }
  type PublicMutationQueries interface {
    PublicReadQueries
    LockPublicState(context.Context) (PublicState, error)
    AdvanceDiscoveryGeneration(context.Context) (int64, error)
    LockSlugClaim(context.Context, string) error
    GetSlugClaim(context.Context, string) (uuid.UUID, error)
    GetSlugTombstoneForUpdate(context.Context, string) (SlugTombstone, error)
    ConsumeExpiredSlugTombstone(context.Context, ConsumeExpiredSlugTombstoneParams) (uuid.UUID, error)
    InsertSlugTombstone(context.Context, InsertSlugTombstoneParams) (SlugTombstone, error)
    PublishResumeCAS(context.Context, PublishResumeCASParams) (Resume, error)
    DeleteResumePublicCAS(context.Context, DeleteResumePublicCASParams) (Resume, error)
  }
  var _ PublicMutationQueries = (*Queries)(nil)
  var _ PublicDiscoveryQueries = (*Queries)(nil)
  ```

  SQL aliases and annotations must generate these names and Go types exactly; an
  incompatible generated signature is fixed here before W0b releases.

- [ ] Test collision serialization and tombstone rejection strictly before
      `released_at + interval '180 days'`, acceptance at the exact boundary,
      atomic consume/claim, and no conflicting timestamp refresh.
- [ ] Run RED:

  ```sh
  make test-db-up
  make server-migration-test
  (cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' go test ./internal/store/... -race -count=1 -run 'Test(PublicState|Slug|Tombstone|PublicRead)' -v)
  ```

  Expected: migration 00007 and named queries do not exist.

## Step 2 — GREEN the smallest relational contract

- [ ] Add append-only `00007_add_public_state.sql` with one
      `singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton)` row and
      `discovery_generation bigint NOT NULL CHECK (discovery_generation > 0)`.
      Insert `(true, 1)` in up and drop the table in down.
- [ ] Add and generate these sqlc names exactly:

  ```text
  GetPublicState LockPublicState AdvanceDiscoveryGeneration
  GetPublicResumeBySlug GetPublicResumeByOwner ListEligiblePublicSlugs
  LockSlugClaim GetSlugClaim GetSlugTombstoneForUpdate
  ConsumeExpiredSlugTombstone InsertSlugTombstone
  PublishResumeCAS DeleteResumePublicCAS
  ```

- [ ] Keep owner authorization in owner queries and uniform absence in public
      queries. Publish writes flags/claim/revision/proof atomically.
      Slug-bearing delete writes tombstone, exact photo job, discovery
      generation, delete, and proof atomically; the existing idempotency
      transaction remains owner.
- [ ] Advance discovery only for slug/live/SEO change or slug-bearing delete.
      Content, language, title, photo/crop, download-only, never-slugged delete,
      and byte-identical stored values do not advance it.
- [ ] Preserve sqlc's `public.citext` override; never hand-edit generated files.
- [ ] Run GREEN alone:

  ```sh
  make sqlc-check server-test-db server-test-integration server-migration-test
  make server-build server-vet server-test
  ```

## Executable RED → GREEN checkpoints

Do not batch the two sections above.

- [ ] State RED: add `TestPublicStateStartsAtOne`, run
      `make server-migration-test`, and observe relation `public_state` is
      absent. GREEN: add the exact singleton migration with
      `INSERT INTO public_state (singleton, discovery_generation) VALUES (true, 1);`;
      rerun `make server-migration-test`.
- [ ] Reads RED: add `TestPublicReadUniformAbsence` and
      `TestEligibleSlugsBytewise`, run
      `(cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL='postgres://aboutme:aboutme_dev@127.0.0.1:20432/aboutme?sslmode=disable' go test ./internal/store -race -count=1 -run 'Test(PublicReadUniformAbsence|EligibleSlugsBytewise)')`,
      and observe the missing generated methods. GREEN: add
      `GetPublicState :one`, `GetPublicResumeBySlug :one`, and
      `ListEligiblePublicSlugs :many` with explicit bytewise ordering; run
      `make sqlc-check`, then rerun the test.
- [ ] Mutation RED: add `TestPublishRollbackRestoresClaimAndGeneration` and
      `TestDeleteRollbackRestoresRowTombstoneAndJob`; run the same package
      command with `-run 'Test(PublishRollback|DeleteRollback)'` and observe the
      absent CAS queries. GREEN: add the named lock/tombstone/CAS queries,
      generate the store, and rerun that command plus
      `make server-test-integration server-migration-test`.

## Adversarial cases and completion

- [ ] Prove singleton corruption, generation regression, cross-rename deadlock,
      reclaim collision, exact tombstone boundary, rollback after consumption,
      claim-oracle columns, eligibility drift, and media-job mismatch.
- [ ] Return query names/generated paths in the exact handoff report.
- [ ] Suggest commit: `feat(storage): add public state and slug transactions`.
