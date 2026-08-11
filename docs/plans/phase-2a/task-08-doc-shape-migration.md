# Task 8: Doc-shape migration machinery — projection-only read, CAS write persistence, CAS backfill, wire-version declarations

> **Owner correction 4 (2026-08-03) — `Project` returns bytes, not
> `schema.Resume`.** Task 6's review found that the original typed-return
> signature forced `docmigrate` to decode, which forced either importing
> `resume` (an import cycle) or duplicating the decoder — and the duplicate that
> shipped used plain `json.Unmarshal`, dropping `DisallowUnknownFields` and the
> trailing-data checks that `DecodeParts` applies. That left `DecodeParts` with
> **zero production callers**, made Task 5's strict-decode suite guard dead
> code, and meant a stored part carrying a field the current Go struct does not
> declare would be silently dropped on read and then persisted lossily by the
> next `SaveDocument` — precisely the read/write disagreement strict decoding
> exists to prevent.
>
> The cycle was an artifact of the signature, not of the requirement. With
> `Project` returning parts, `docmigrate` imports nothing from `resume`, there
> is no duplicate decoder, and `internal/resume` keeps a single strict decode at
> the boundary. This also **restores consistency with D13**, which already says
> converters are `func(json.RawMessage) (json.RawMessage, error)` over the full
> assembled document precisely because "typed structs only exist for the current
> version" — a converter chain lifting a v1 document cannot decode it into the
> current Go type at all. Task 8 assembles, runs the chain over the full
> document bytes, and re-splits into the three parts (D4's own decomposition);
> the caller decodes once, strictly.

Implements AC-DOC-010 and AC-DOC-012: the spec §3 doc-migration behavior and
wire-version machinery ("built in P2A … before a second version exists"). Task
2b is a hard prerequisite. **D12(ii) binding:** `docmigrate.go`'s package doc
records, verbatim, that every write path must persist the full document through
the codec — never a granular `jsonb_set`-style PATCH, which would let old-shape
content re-enter storage where the backfill CAS cannot see it. This is P2B's
binding-in-writing condition from D12; Task 12 forwards the sentence to the
owner alongside the other P2B forward-binding notes (as Task 7 does for the
idempotency retry contract).

**Files:** create
`apps/server/internal/resume/docmigrate/{docmigrate.go,backfill.go}`,
`docmigrate_test.go`, `backfill_test.go`, `export_test.go`; modify
`apps/server/internal/resume/store.go` (Get/List call `Projector.Project`;
SaveDocument persists at `CurrentVersion` — completing Task 6's stub).

**Interfaces.** Produces:

```go
package docmigrate

const CurrentVersion int32 = 1

// AcceptedVersions and EmittedVersions are distinct declared sets. With one
// released version both are {1}; a future release changes them deliberately.
// Callers receive copies so they cannot mutate the production declaration.
func AcceptedVersions() []int32
func EmittedVersions() []int32

// ConvertFunc converts one FULL canonical document by exactly one version.
type ConvertFunc func(doc json.RawMessage) (json.RawMessage, error)

// AdjacentConverters is keyed by its lower version N and supplies N→N+1 and
// N+1→N. Both functions are mandatory for every registered pair (D13).
type AdjacentConverters struct {
    Up   ConvertFunc
    Down ConvertFunc
}

// ValidateFunc validates one released-version document against that version's
// immutable schema. Production validators come from schema.RawSchemas.
type ValidateFunc func(doc json.RawMessage) error

type Projector struct { /* pairs, validators, accepted, emitted, current */ }

func NewProjector(pairs map[int32]AdjacentConverters,
    validators map[int32]ValidateFunc, accepted, emitted []int32,
    current int32) (*Projector, error)
func NewIdentityProjector() *Projector // production v1: no adjacent pairs

// Convert validates the source, walks adjacent pairs in either direction,
// validates each target, and fails closed on an unknown/undeclared version,
// missing direction, invalid converter result, or unavailable schema.
func (p *Projector) Convert(doc json.RawMessage, from, to int32) (json.RawMessage, error)

// AcceptWire projects a declared accepted wire version to CurrentVersion.
// EmitWire projects a current document to a declared emitted version.
func (p *Projector) AcceptWire(doc json.RawMessage, version int32) (
    current json.RawMessage, currentVersion int32, err error)
func (p *Projector) EmitWire(doc json.RawMessage, version int32) (json.RawMessage, error)

// Project is PURE (D18): stored parts+version in, current-version parts out.
// It never touches the database or decodes into schema types; internal/resume
// owns the one strict current-version decode (owner correction 4).
func (p *Projector) Project(personalDetails, content, customization json.RawMessage,
    storedVersion int32) (pd, c, cu json.RawMessage, err error)
```

```go
package resume // backfill lives with the store (needs Queries + validation)

type BackfillResult int
const (
    BackfillApplied BackfillResult = iota
    BackfillSkippedCurrent   // already at CurrentVersion
    BackfillLostRace         // observation stale (revision or version moved
                              // since read); no write occurred — RETRYABLE:
                              // re-observe and call BackfillOne again (B6).
                              // Never terminal; the row may still be behind.
)

// BackfillOne: read (version, revision, parts) → Project → validate →
// BackfillResumeDocumentCAS (WHERE id AND schema_version=$old AND
// revision=$observed — the spec's exact predicate). Revision and
// updated_at unchanged (D12). BackfillLostRace means the caller's
// observation went stale between read and CAS (e.g. a concurrent autosave
// OR a title-only write that bumps revision without touching
// schema_version) — it is a retry signal, not "row already current"; the
// (future) background job must re-observe schema_version and retry, not
// treat it as done. ListResumeIDsBelowSchemaVersion pages candidates for
// that job; the job itself is not built here — no scheduler exists until
// PI/P8 infrastructure.
func (s *Store) BackfillOne(ctx context.Context, id uuid.UUID) (BackfillResult, error)
```

- [ ] **Step 1: failing conversion/projection tests.** Identity v1 conversion
      and projection are byte-stable. With injected synthetic schemas and pairs,
      test `1→2`, `2→1`, `1→2→3`, and `3→2→1`; every step validates both its
      source and output. Constructor and conversion fail closed for a missing
      `Up` or `Down`, missing schema validator, unknown/undeclared version,
      invalid source, invalid JSON output, or output invalid for the target
      schema. Returned accepted/emitted slices cannot mutate internal state.
      **Projection purity:** run `Get` against a live row seeded at a synthetic
      old version, assert the returned doc is projected and the row's bytes,
      `revision`, and `updated_at` are bit-identical before/after (D18).
- [ ] **Step 2: failing old-client preparation and emission tests.** Against a
      synthetic current-v2 projector, `AcceptWire` accepts a v1 document and
      returns canonical target-validated v2 bytes plus the current version;
      `EmitWire` converts those same bytes to declared v1, validates immutable
      v1, and proves round-trip preservation of all v1 fields. Undeclared
      input/output versions and lossy conversion fail closed. This is the exact
      transport-agnostic boundary P2B consumes. P2B-owned AC-SAVE-004 adds the
      real HTTP/OpenAPI convert→full-document persist→emit proof; P2A does not
      invent a fake v2 store codec or bypass the typed v1 store to simulate it.
- [ ] **Step 3: failing backfill tests.** Old-version row → `BackfillApplied`:
      `schema_version` now current, parts rewritten, **revision and updated_at
      unchanged** (D12); already-current → `BackfillSkippedCurrent`, zero
      writes; stale observation (bump revision between read and CAS via a second
      connection) → `BackfillLostRace`, row untouched; backfill of a row whose
      projection fails validation → error, no write (a corrupt doc must surface,
      not silently persist). **D12(i), the actual proof of the argument:**
      `Get(id)` called immediately before and immediately after a successful
      `BackfillOne` on the same row returns **byte-identical** projected
      documents (marshal both, compare bytes) — this is the assertion the
      owner's ruling names as what makes "nothing observable changes" more than
      a claim. **B6 — title-only write causes a retryable, non-terminal lost
      race:** seed an old-version row; read it (observe `schema_version=vOld`,
      `revision=R`); call `SaveTitle` (which touches only `title` and bumps
      `revision` to `R+1`, never `schema_version`) between the read and the CAS;
      `BackfillOne` using the stale observation → `BackfillLostRace`, and the
      row's `schema_version` is **still `vOld`** (unlike the concurrent-autosave
      case, the row is not now current) — proving the result is a retry signal,
      not "already done"; a second `BackfillOne` (fresh read) on the same row
      then succeeds with `BackfillApplied`.
- [ ] **Step 3c: a discriminating test for the strict decode on the read path
      (added 2026-08-03 after Task 6's re-review).** Task 6 restored
      `DecodeParts` to the read path, but nothing yet _fails_ if it is removed:
      swapping it back for a plain `json.Unmarshal` at `projectRow` leaves every
      test in the package green — the exact blind spot that let the lax
      duplicate ship in the first place. Insert a row, then
      `UPDATE resumes SET personal_details = personal_details || '{"unknownField":1}'`
      via direct SQL, and assert `Get` returns an error rather than silently
      dropping the field. Without this, the invariant is structural but
      unguarded.
- [ ] **Step 4: implement; green.** `Store.Get`/`List` now project;
      `SaveDocument` persists current version (Task 6's tests still green — run
      them). **Owner ruling:** `List` is fail-closed and atomic: if any row
      cannot be projected or decoded, return `nil` plus a deterministic error
      and expose no partial list. Add the mixed-valid/corrupt-row test; a silent
      omission or partial result would make corruption look like user deletion.
- [ ] **Step 5: gate.** Live-DB command + tally (Task 6 Step 5's form), plus
      `make server-build server-vet server-test schema-check`.
- [ ] **Step 6: commit** —
      `git commit -m "feat(resume): add doc-shape projection, CAS backfill, and wire-version declarations" -- apps/server/internal/resume`
