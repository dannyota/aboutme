package resume

// export_test.go exposes package-private internals under test-only names,
// for tests that must prove something about the REAL configuration this
// package uses (not a reimplementation of it) without widening resume's
// actual public API. Compiled only into test binaries (the _test.go
// suffix), never shipped.

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// CompileCountForTest reports how many times mustCompileSchema has run.
// Package-level var initializers run exactly once, at package init, before
// any test executes -- so a value of 1 here (checked before any of this
// package's exported functions are even called) already proves the schema
// was compiled once at init, not lazily on first use. D1 condition (c).
func CompileCountForTest() int {
	return compileCount
}

// CompiledSchemaPointerForTest exposes compiledSchema's pointer identity, so
// a test can prove ValidateForStore reuses the same *jsonschema.Schema
// across repeated calls instead of recompiling per call.
func CompiledSchemaPointerForTest() *jsonschema.Schema {
	return compiledSchema
}

// NewSchemaCompilerForTest exposes the exact compiler construction
// (AssertFormat + no URL loader) ValidateForStore's package-init compile
// uses, so a test can prove the no-URL-loader condition (D1 condition (b))
// against the real configuration rather than a similar-looking stand-in.
func NewSchemaCompilerForTest() *jsonschema.Compiler {
	return newSchemaCompiler()
}

// IsResumeCapExceededForTest exposes isResumeCapExceeded (store.go) to
// package resume_test, so a test can prove the D7 cap-violation mapping
// requires an EXACT match on both the SQLSTATE and the message -- not the
// SQLSTATE alone, which other CHECK constraints on resumes also raise --
// without going through a live database.
func IsResumeCapExceededForTest(err error) bool {
	return isResumeCapExceeded(err)
}

// EncodePartsForTest exposes encodeParts (codec.go), now package-private
// (fix round 1, owner ruling: it is the function that actually produces
// the three jsonb values a write persists, so it is the half of the D16
// choke point the compiler can enforce), so tests can still exercise the
// exact function ValidateForStore's own callers use, rather than a
// reimplementation of it.
func EncodePartsForTest(doc schema.Resume) (personalDetails, content, customization json.RawMessage, err error) {
	return encodeParts(doc)
}

// CreateTxForTest exposes (*Store).createTx (store.go, B7's tx-scoped create
// core) to package resume_test, so Task 7's IdempotencyStore composition
// tests (idempotency_test.go) can call the REAL cap-checked create logic --
// composed inside IdempotencyStore.Execute's own transaction exactly as
// P2B's eventual caller will -- rather than a hand-rolled INSERT stand-in
// that would prove nothing about the composition.
func (s *Store) CreateTxForTest(ctx context.Context, qtx *store.Queries, userID uuid.UUID, title string, doc schema.Resume) (Resume, error) {
	return s.createTx(ctx, qtx, userID, title, doc)
}

// SaveDocumentTxForTest exposes (*Store).saveDocumentTx solely so Task 7's
// idempotency contention tests can compose Execute with the real revision-CAS
// write inside Execute's supplied transaction. Production callers use
// SaveDocument; this test-only seam does not widen the shipped API.
func (s *Store) SaveDocumentTxForTest(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, doc schema.Resume, expectedRevision int64) (int64, error) {
	return s.saveDocumentTx(ctx, qtx, userID, id, doc, expectedRevision)
}

// NewIdempotencyStoreForTest builds an IdempotencyStore backed by pool that
// uses now instead of the real wall clock, so tests in package resume_test
// (which, by this package's own convention, cannot reach IdempotencyStore's
// unexported clock field directly) can exercise Execute's IdempotencyTTL
// expiry logic deterministically -- advancing a fake clock, never a real
// sleep. Every non-test caller uses NewIdempotencyStore.
func NewIdempotencyStoreForTest(pool *store.Pool, now func() time.Time) *IdempotencyStore {
	return &IdempotencyStore{pool: pool, q: store.New(pool), now: now}
}

// BackfillOneForTest exposes (*Store).backfillOne's pause seam, so Task 8's
// backfill_test.go can stage the read/CAS interleavings D12 depends on
// deterministically instead of racing them. pause runs after the document has
// been read, projected, validated and re-encoded, and immediately before the
// CAS -- exactly the window a concurrent autosave or title write occupies.
// Production callers use BackfillOne, which passes a nil pause.
func (s *Store) BackfillOneForTest(ctx context.Context, id uuid.UUID, pause func()) (BackfillResult, error) {
	return s.backfillOne(ctx, id, pause)
}
