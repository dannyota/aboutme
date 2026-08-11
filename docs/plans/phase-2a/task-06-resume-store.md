# Task 6: Resume store — create (cap), get/list (projected), delete, revision CAS

> **Owner correction 5 (2026-08-03) — how far the write-path choke point is
> actually enforced.** D16 calls `internal/resume` the single write-path choke
> point, and Task 6's review correctly objected that a doc comment asserting
> that guarantee is not the same as providing it. What this phase enforces, and
> what it does not:
>
> - **Enforced:** `encodeParts` is **unexported**, so no package outside
>   `internal/resume` can produce the three jsonb values. Tests reach it through
>   the existing `export_test.go` seam.
> - **Not enforced, and deliberately named as convention:** sqlc generates
>   `store.Queries.CreateResume` / `UpdateResumeDocumentCAS` /
>   `UpdateResumeTitleCAS` as exported methods that any package may call. They
>   cannot be unexported without hand-editing generated code, which this repo
>   forbids. `AssembleCanonical` also stays exported — it marshals and never
>   writes, and Task 11's blind suite consumes it by name.
>
> A `forbidigo`-style lint rule restricting those three generated methods to
> `internal/resume` is the real closure and is **recorded as a phase-gate
> follow-up**, not silently skipped. Until it lands, `store.go`'s package
> comment must describe the convention as a convention — an unenforced invariant
> stated as a guarantee is what a future implementer will trust.

Satisfies the store half of **AC-DOC-001** and builds the write-safety primitive
AC-SAVE-001 (P2B) will surface over HTTP.

**Files:** create `apps/server/internal/resume/{resume.go,store.go}`,
`store_test.go`; extend `export_test.go`.

**Interfaces.** Produces:

```go
package resume

type Resume struct {
    ID              uuid.UUID
    UserID          uuid.UUID
    Title           string
    Slug            *string
    Live            bool
    DownloadEnabled bool
    SEOGeoEnabled   bool
    StoredSchemaVersion int32 // D18: pre-projection version, observable
    Revision        int64     // API serializes as string — P2B's concern
    Lng             *string
    Doc             schema.Resume // always projected to CurrentVersion
    CreatedAt, UpdatedAt time.Time
}

var (
    ErrNotFound       = errors.New("resume: not found") // D17: also "not yours"
    ErrCapExceeded    = errors.New("resume: user resume cap exceeded")
    ErrTitleTooLong   = errors.New("resume: title exceeds 160 characters")
)

const MaxTitleCharacters = 160 // budgets.md; Unicode code points

type RevisionMismatchError struct {
    CurrentRevision int64
    Current         Resume // for the 412 body P2B must return (spec §4)
}
func (e *RevisionMismatchError) Error() string

type Store struct {
    pool *store.Pool
    q    *store.Queries
    proj *docmigrate.Projector // Task 8; identity projection until then
    now  func() time.Time
}
func NewStore(pool *store.Pool, proj *docmigrate.Projector) *Store

// Create validates doc, then in one tx: LockUserForResumeWrite (spec's
// FOR UPDATE), CountResumesForUser >= 3 → ErrCapExceeded, else insert.
// Title validation runs before opening the transaction and defensively in the
// tx-scoped core; empty is allowed and 161 Unicode code points fail closed.
// The D7 trigger backstops it; a 23514 'resumes_user_cap_exceeded' from
// the insert also maps to ErrCapExceeded. Thin wrapper (B7): begin tx,
// build qtx := s.q.WithTx(tx), call createTx, commit.
func (s *Store) Create(ctx context.Context, userID uuid.UUID, title string, doc schema.Resume) (Resume, error)

func (s *Store) Get(ctx context.Context, userID, id uuid.UUID) (Resume, error)
func (s *Store) List(ctx context.Context, userID uuid.UUID) ([]Resume, error)
func (s *Store) Delete(ctx context.Context, userID, id uuid.UUID) error

// SaveDocument is the CAS write: ValidateForStore, then
// UpdateResumeDocumentCAS at schema_version = docmigrate.CurrentVersion.
// 0 rows → re-read inside the same tx: absent → ErrNotFound; present →
// *RevisionMismatchError carrying the current (projected) doc + revision.
// Thin wrapper (B7) around saveDocumentTx — see below.
func (s *Store) SaveDocument(ctx context.Context, userID, id uuid.UUID, doc schema.Resume, expectedRevision int64) (newRevision int64, err error)

// Thin wrapper (B7) around saveTitleTx — see below.
func (s *Store) SaveTitle(ctx context.Context, userID, id uuid.UUID, title string, expectedRevision int64) (int64, error)

// createTx / saveDocumentTx / saveTitleTx (B7, owner ruling): the tx-scoped
// cores. Each takes an already-open *store.Queries (qtx) and does its
// writes on it, performing NO transaction management of its own — no
// Begin/Commit/Rollback. The pool-based Create/SaveDocument/SaveTitle above
// are the only callers that open a tx around them for the common case. This
// split exists so Task 7's IdempotencyStore.Execute can compose its mutate
// closure with the REAL cap-check/CAS logic inside its own transaction
// (mutate(qtx) calling s.createTx(ctx, qtx, …) etc.), instead of
// reimplementing the cap check or the CAS predicate a second time.
func (s *Store) createTx(ctx context.Context, qtx *store.Queries, userID uuid.UUID, title string, doc schema.Resume) (Resume, error)
func (s *Store) saveDocumentTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, doc schema.Resume, expectedRevision int64) (newRevision int64, err error)
func (s *Store) saveTitleTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, title string, expectedRevision int64) (int64, error)
```

Task ordering note: `docmigrate.Projector` is Task 8; to keep tasks
independently landable, Task 6 defines a minimal
`docmigrate.NewIdentityProjector()` stub in `docmigrate/docmigrate.go` (current
version passthrough, `CurrentVersion = 1`) that Task 8 completes — declared here
so file ownership stays disjoint-by-time, not overlapping.

- [x] **Step 1: failing happy-path integration tests** (all via
      `testutil.RequireMigratedTestDatabaseURL`, table-driven): create → get
      round-trip (doc byte-stable through codec; revision 1; defaults
      live=false/download=true/seo=false); list ordering stable
      (`created_at, id`); delete → `ErrNotFound` on re-get; get/delete with the
      wrong user → `ErrNotFound` (D17 — the other user's row untouched, assert
      full-row equality before/after).
- [x] **Step 2: failing cap tests.** 3 creates succeed, 4th → `ErrCapExceeded`;
      delete one → create succeeds again; a second user is unaffected. (The
      N-way concurrency race is Suite A's, Task 9 — the author writes the
      sequential cases only; do not pre-empt the blind suite.)
- [x] **Step 3: failing CAS tests.** Save with correct revision → revision 2,
      doc updated; stale revision → `*RevisionMismatchError` with current
      revision + current doc (assert the doc is the _winning_ content); unknown
      id → `ErrNotFound`; invalid doc → `*ValidationError`, row untouched
      (full-row comparison — validation must run before any write); `SaveTitle`
      same matrix.
- [x] **Step 4: implement; green.** Implement the `…Tx` cores first (B7:
      `createTx`/`saveDocumentTx`/`saveTitleTx` — no tx management inside them),
      then `Create`/`SaveDocument`/`SaveTitle` as thin begin-tx/`WithTx`/commit
      wrappers around them; re-run Steps 1–3's tests unmodified against the
      wrapper form (they must still pass — the split is an internal refactor,
      not a behavior change). pgx error mapping via
      `pgconn.PgError{Code: "23514", Message: "resumes_user_cap_exceeded"}`
      (exact match on both — D7).
- [x] **Step 5: gate (dev-loop evidence, not phase-exit evidence — B11).**
      `make test-db-up && make server-test-db` — note `internal/resume` is not
      yet in that target's package list; the Makefile handoff (Integration
      handoffs table; owner applies it once this task lands, formally reported
      in Task 12) is what turns this into phase-exit evidence. Until it lands,
      ALSO run
      `cd apps/server && REQUIRE_TEST_DB=1 TEST_DATABASE_URL=postgres://aboutme:aboutme_dev@127.0.0.1:5432/aboutme?sslmode=disable go test ./internal/resume/... -race -count=1 -v`
      and record the non-skipped case tally as **interim** gate evidence (P1's
      exit-criteria convention) — per the owner's B11 ruling this local
      invocation is never a substitute for the landed Makefile edit plus a green
      CI run at phase exit.
- [x] **Step 6: commit** —
      `git commit -m "feat(resume): add resume store with cap enforcement and revision CAS" -- apps/server/internal/resume`
