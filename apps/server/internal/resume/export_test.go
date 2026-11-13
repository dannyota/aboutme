package resume

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/santhosh-tekuri/jsonschema/v6"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/store"
)

func CompileCountForTest() int {
	return compileCount
}

func CompiledSchemaPointerForTest() *jsonschema.Schema {
	return compiledSchema
}

func ResumeSchemaIDForTest() string {
	return resumeSchemaID
}

func NewSchemaCompilerForTest() *jsonschema.Compiler {
	return newSchemaCompiler()
}

func IsResumeCapExceededForTest(err error) bool {
	return isResumeCapExceeded(err)
}

func EncodePartsForTest(doc schema.Resume) (personalDetails, content, customization json.RawMessage, err error) {
	return encodeParts(doc)
}

func (s *Store) CreateTxForTest(ctx context.Context, qtx *store.Queries, userID uuid.UUID, title string, doc schema.Resume) (Resume, error) {
	return s.CreateTx(ctx, qtx, userID, title, nil, doc)
}

func (s *Store) SaveDocumentTxForTest(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, doc schema.Resume, expectedRevision int64) (int64, error) {
	saved, err := s.SaveDocumentTx(ctx, qtx, userID, id, doc, expectedRevision)
	return saved.Revision, err
}

func NewIdempotencyStoreForTest(pool *store.Pool, now func() time.Time) *IdempotencyStore {
	return &IdempotencyStore{pool: pool, q: store.New(pool), now: now}
}

func NewIdempotencyStoreWithHooksForTest(pool *store.Pool, now func() time.Time,
	beginTx func(context.Context) (pgx.Tx, error),
	commitTx func(context.Context, pgx.Tx) error,
) *IdempotencyStore {
	return &IdempotencyStore{
		pool: pool, q: store.New(pool), now: now,
		beginTx: beginTx, commitTx: commitTx,
	}
}

func NewIdempotencyStoreWithRecheckBlockerForTest(pool *store.Pool, now func() time.Time,
	afterLock func(),
) *IdempotencyStore {
	return &IdempotencyStore{
		pool: pool, q: store.New(pool), now: now, afterRecheckLock: afterLock,
	}
}

func (s *IdempotencyStore) ExecuteForTest(ctx context.Context, userID uuid.UUID,
	operation string, key uuid.UUID, requestHash [32]byte,
	mutate func(*store.Queries) (StoredResponse, error),
) (StoredResponse, bool, error) {
	result, err := s.Execute(ctx, userID, operation, key, requestHash, mutate)
	return result.Response, result.Replayed, err
}

func (s *Store) BackfillOneForTest(ctx context.Context, id uuid.UUID, pause func()) (BackfillResult, error) {
	return s.backfillOne(ctx, id, pause)
}
