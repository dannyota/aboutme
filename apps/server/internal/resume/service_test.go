// service_test.go exercises the P2B transaction seam on resume.Store
// against a live Postgres database: the exported tx-scoped mirrors
// (CreateTx, GetTx, ListTx, SaveDocumentTx), the full-aggregate metadata
// CAS (SaveMetadataAndDocumentTx), the revision-CAS delete (DeleteTx), and
// the transactional media deletion-job enqueue (EnqueueMediaDeletionTx).
//
// Every method is driven through a hand-rolled pgx transaction, proving:
// (a) identical behavior to the pool-backed method it mirrors, including
// every error shape; (b) rollback leaves ZERO observable effect; (c) a
// wrong-owner id and a nonexistent id are byte-identical on every method
// (P2A D17 — no existence oracle). Shares store_test.go's helpers
// (newIntegrationStore, createTestUser, assertResumeRowEqual) and
// validate_test.go's validDocForTest.
package resume_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// beginSeamTx opens a hand-rolled transaction for driving the tx-scoped
// seam directly. The cleanup rollback is a safety net; tests that commit
// do so explicitly first (rolling back a committed tx is a no-op error we
// ignore).
func beginSeamTx(ctx context.Context, t *testing.T, pool *store.Pool) (pgx.Tx, *store.Queries) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin seam tx: %v", err)
	}
	cleanupContext := context.WithoutCancel(ctx)
	t.Cleanup(func() { rollbackSeamTx(cleanupContext, t, tx) })
	return tx, store.New(pool).WithTx(tx)
}

func rollbackSeamTx(ctx context.Context, t *testing.T, tx pgx.Tx) {
	t.Helper()
	if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
		t.Errorf("roll back seam transaction: %v", err)
	}
}

// closedSeamTx returns a *store.Queries bound to an already rolled-back
// transaction. Any statement through it fails loudly with pgx.ErrTxClosed,
// so a method that returns a domain validation error through it has
// provably validated BEFORE running any statement.
func closedSeamTx(ctx context.Context, t *testing.T, pool *store.Pool) *store.Queries {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin throwaway tx: %v", err)
	}
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("roll back throwaway tx: %v", rollbackErr)
	}
	return store.New(pool).WithTx(tx)
}

// seamRowSnapshot returns the resumes row's complete textual representation
// (every column, including revision and updated_at), or "<absent>" when no
// row exists. Byte-compare two snapshots to prove a rejected or rolled-back
// write left literally nothing behind.
func seamRowSnapshot(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID) string {
	t.Helper()
	var snap string
	err := pool.QueryRow(ctx, `SELECT r::text FROM resumes r WHERE id = $1`, id).Scan(&snap)
	if errors.Is(err, pgx.ErrNoRows) {
		return "<absent>"
	}
	if err != nil {
		t.Fatalf("snapshot resumes row %v: %v", id, err)
	}
	return snap
}

func seamJobCount(ctx context.Context, t *testing.T, pool *store.Pool, resumeID uuid.UUID) int64 {
	t.Helper()
	var n int64
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM media_deletion_jobs WHERE resume_id = $1`, resumeID).Scan(&n); err != nil {
		t.Fatalf("count media_deletion_jobs for %v: %v", resumeID, err)
	}
	return n
}

// seamPhotoKey builds the exact D11 canonical key for resumeID.
func seamPhotoKey(resumeID uuid.UUID) string {
	return "resumes/" + resumeID.String() + "/photo-0123456789abcdef0123456789abcdef.jpg"
}

// --- Step 1: tx-scoped mirrors of the pool-backed methods ---

func TestCreateTx_CommitPersistsAndMirrorsCreate(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	tx, qtx := beginSeamTx(ctx, t, pool)
	created, err := s.CreateTx(ctx, qtx, userID, "Seam Create", strp("en-GB"), doc)
	if err != nil {
		t.Fatalf("CreateTx() error: %v", err)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit: %v", commitErr)
	}

	if created.Revision != 1 {
		t.Errorf("CreateTx().Revision = %d, want 1", created.Revision)
	}
	if created.Lng == nil || *created.Lng != "en-GB" {
		t.Errorf("CreateTx().Lng = %v, want en-GB", created.Lng)
	}

	got, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() after committed CreateTx: %v", err)
	}
	assertResumeRowEqual(t, got, created)
}

func TestCreateTx_RollbackLeavesZeroEffect(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	tx, qtx := beginSeamTx(ctx, t, pool)
	created, err := s.CreateTx(ctx, qtx, userID, "Rolled Back", nil, doc)
	if err != nil {
		t.Fatalf("CreateTx() error: %v", err)
	}
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback: %v", rollbackErr)
	}

	if snap := seamRowSnapshot(ctx, t, pool, created.ID); snap != "<absent>" {
		t.Errorf("resumes row after rollback = %s, want absent", snap)
	}
	list, err := s.List(ctx, userID)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(list) != 0 {
		t.Errorf("List() after rollback = %d resumes, want 0", len(list))
	}
}

func TestCreateTx_MirrorsCapAndTitleErrors(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	// Title over 160 code points is rejected before ANY statement: the
	// closed transaction would fail loudly on any query.
	longTitle := strings.Repeat("x", 161)
	if _, err := s.CreateTx(ctx, closedSeamTx(ctx, t, pool), userID, longTitle, nil, doc); !errors.Is(err, resume.ErrTitleTooLong) {
		t.Errorf("CreateTx(161-char title) error = %v, want ErrTitleTooLong before any statement", err)
	}

	// lng over 35 characters is likewise rejected before any statement.
	longLng := strings.Repeat("a", 36)
	if _, err := s.CreateTx(ctx, closedSeamTx(ctx, t, pool), userID, "ok", strp(longLng), doc); !errors.Is(err, resume.ErrLngTooLong) {
		t.Errorf("CreateTx(36-char lng) error = %v, want ErrLngTooLong before any statement", err)
	}

	// The boundary values pass.
	tx, qtx := beginSeamTx(ctx, t, pool)
	if _, err := s.CreateTx(ctx, qtx, userID, strings.Repeat("x", 160), strp(strings.Repeat("a", 35)), doc); err != nil {
		t.Errorf("CreateTx(160-char title, 35-char lng) error = %v, want nil", err)
	}
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback: %v", rollbackErr)
	}

	// The cap error mirrors the pool-backed Create exactly.
	for i := range 3 {
		if _, err := s.Create(ctx, userID, fmt.Sprintf("Resume %d", i), doc); err != nil {
			t.Fatalf("Create() #%d error: %v", i, err)
		}
	}
	tx2, qtx2 := beginSeamTx(ctx, t, pool)
	if _, err := s.CreateTx(ctx, qtx2, userID, "Fourth", nil, doc); !errors.Is(err, resume.ErrCapExceeded) {
		t.Errorf("CreateTx() 4th resume error = %v, want ErrCapExceeded", err)
	}
	rollbackSeamTx(ctx, t, tx2)
}

func TestGetTxListTx_MirrorPoolBackedAndHideOwnership(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	otherID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Seam Get", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	tx, qtx := beginSeamTx(ctx, t, pool)
	defer rollbackSeamTx(ctx, t, tx)

	gotTx, err := s.GetTx(ctx, qtx, userID, created.ID)
	if err != nil {
		t.Fatalf("GetTx() error: %v", err)
	}
	gotPool, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	assertResumeRowEqual(t, gotTx, gotPool)

	listTx, err := s.ListTx(ctx, qtx, userID)
	if err != nil {
		t.Fatalf("ListTx() error: %v", err)
	}
	listPool, err := s.List(ctx, userID)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(listTx) != len(listPool) || len(listTx) != 1 {
		t.Fatalf("ListTx() = %d rows, List() = %d rows, want 1 and 1", len(listTx), len(listPool))
	}
	assertResumeRowEqual(t, listTx[0], listPool[0])

	// Wrong owner and nonexistent id are byte-identical.
	_, wrongOwnerErr := s.GetTx(ctx, qtx, otherID, created.ID)
	_, missingErr := s.GetTx(ctx, qtx, otherID, uuid.New())
	if !errors.Is(wrongOwnerErr, resume.ErrNotFound) || !errors.Is(missingErr, resume.ErrNotFound) {
		t.Fatalf("GetTx() wrong-owner/missing errors = %v / %v, want ErrNotFound for both", wrongOwnerErr, missingErr)
	}
	if fmt.Sprintf("%v", wrongOwnerErr) != fmt.Sprintf("%v", missingErr) {
		t.Errorf("GetTx() wrong-owner error %q differs from missing error %q", wrongOwnerErr, missingErr)
	}

	// A wrong owner's ListTx simply does not contain the resume.
	otherList, err := s.ListTx(ctx, qtx, otherID)
	if err != nil {
		t.Fatalf("ListTx(other) error: %v", err)
	}
	if len(otherList) != 0 {
		t.Errorf("ListTx(other) = %d rows, want 0", len(otherList))
	}
}

func TestSaveDocumentTx_MirrorsSaveDocument(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Seam Save", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	updated := validDocForTest(t)
	updated.PersonalDetails.FullName = strp("Seam Updated")

	tx, qtx := beginSeamTx(ctx, t, pool)
	newRev, err := s.SaveDocumentTx(ctx, qtx, userID, created.ID, updated, created.Revision)
	if err != nil {
		t.Fatalf("SaveDocumentTx() error: %v", err)
	}
	if newRev != created.Revision+1 {
		t.Errorf("SaveDocumentTx() revision = %d, want %d", newRev, created.Revision+1)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit: %v", commitErr)
	}

	got, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Revision != newRev {
		t.Errorf("committed revision = %d, want %d", got.Revision, newRev)
	}
	if got.Doc.PersonalDetails.FullName == nil || *got.Doc.PersonalDetails.FullName != "Seam Updated" {
		t.Errorf("committed FullName = %v, want Seam Updated", got.Doc.PersonalDetails.FullName)
	}

	// Stale revision: *RevisionMismatchError carrying the WINNING document,
	// exactly like the pool-backed SaveDocument.
	tx2, qtx2 := beginSeamTx(ctx, t, pool)
	defer rollbackSeamTx(ctx, t, tx2)
	_, staleErr := s.SaveDocumentTx(ctx, qtx2, userID, created.ID, doc, created.Revision)
	var mismatch *resume.RevisionMismatchError
	if !errors.As(staleErr, &mismatch) {
		t.Fatalf("SaveDocumentTx(stale) error = %v, want *RevisionMismatchError", staleErr)
	}
	if mismatch.CurrentRevision != newRev {
		t.Errorf("mismatch.CurrentRevision = %d, want winning %d", mismatch.CurrentRevision, newRev)
	}
	assertResumeRowEqual(t, mismatch.Current, got)
}

func TestSaveDocumentTx_RollbackLeavesZeroEffect(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Seam Save Rollback", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	before := seamRowSnapshot(ctx, t, pool, created.ID)

	updated := validDocForTest(t)
	updated.PersonalDetails.FullName = strp("Must Vanish")

	tx, qtx := beginSeamTx(ctx, t, pool)
	if _, err := s.SaveDocumentTx(ctx, qtx, userID, created.ID, updated, created.Revision); err != nil {
		t.Fatalf("SaveDocumentTx() error: %v", err)
	}
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback: %v", rollbackErr)
	}

	// Row count, row bytes, revision, and updated_at all unchanged.
	if after := seamRowSnapshot(ctx, t, pool, created.ID); after != before {
		t.Errorf("row after rollback changed\n before: %s\n  after: %s", before, after)
	}
}

func TestSaveDocumentTx_WrongOwnerVsMissing_ByteIdentical(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	otherID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Owned", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	before := seamRowSnapshot(ctx, t, pool, created.ID)

	tx, qtx := beginSeamTx(ctx, t, pool)
	defer rollbackSeamTx(ctx, t, tx)
	_, wrongOwnerErr := s.SaveDocumentTx(ctx, qtx, otherID, created.ID, doc, created.Revision)
	_, missingErr := s.SaveDocumentTx(ctx, qtx, otherID, uuid.New(), doc, created.Revision)
	if !errors.Is(wrongOwnerErr, resume.ErrNotFound) || !errors.Is(missingErr, resume.ErrNotFound) {
		t.Fatalf("errors = %v / %v, want ErrNotFound for both", wrongOwnerErr, missingErr)
	}
	if fmt.Sprintf("%v", wrongOwnerErr) != fmt.Sprintf("%v", missingErr) {
		t.Errorf("wrong-owner error %q differs from missing error %q", wrongOwnerErr, missingErr)
	}
	if after := seamRowSnapshot(ctx, t, pool, created.ID); after != before {
		t.Errorf("wrong-owner attempt changed the row\n before: %s\n  after: %s", before, after)
	}
}

// --- Step 2: full-aggregate metadata CAS and revision-CAS delete ---

func TestSaveMetadataAndDocumentTx_WritesAllUnderOneCAS(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Before", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	updated := validDocForTest(t)
	updated.PersonalDetails.FullName = strp("Metadata Winner")

	tx, qtx := beginSeamTx(ctx, t, pool)
	newRev, err := s.SaveMetadataAndDocumentTx(ctx, qtx, userID, created.ID, "After", strp("vi"), updated, created.Revision)
	if err != nil {
		t.Fatalf("SaveMetadataAndDocumentTx() error: %v", err)
	}
	if newRev != created.Revision+1 {
		t.Errorf("new revision = %d, want %d", newRev, created.Revision+1)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit: %v", commitErr)
	}

	got, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Title != "After" {
		t.Errorf("Title = %q, want After", got.Title)
	}
	if got.Lng == nil || *got.Lng != "vi" {
		t.Errorf("Lng = %v, want vi", got.Lng)
	}
	if got.Revision != newRev {
		t.Errorf("Revision = %d, want %d", got.Revision, newRev)
	}
	if got.Doc.PersonalDetails.FullName == nil || *got.Doc.PersonalDetails.FullName != "Metadata Winner" {
		t.Errorf("FullName = %v, want Metadata Winner (the caller-supplied aggregate must persist unchanged)", got.Doc.PersonalDetails.FullName)
	}
	if !got.UpdatedAt.After(created.UpdatedAt) && !got.UpdatedAt.Equal(created.UpdatedAt) {
		t.Errorf("UpdatedAt went backwards: %v -> %v", created.UpdatedAt, got.UpdatedAt)
	}
}

func TestSaveMetadataAndDocumentTx_RollbackLeavesZeroEffect(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Before Rollback", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	before := seamRowSnapshot(ctx, t, pool, created.ID)

	updated := validDocForTest(t)
	updated.PersonalDetails.FullName = strp("Must Vanish")
	tx, qtx := beginSeamTx(ctx, t, pool)
	newRevision, err := s.SaveMetadataAndDocumentTx(
		ctx,
		qtx,
		userID,
		created.ID,
		"After Rollback",
		strp("vi"),
		updated,
		created.Revision,
	)
	if err != nil {
		t.Fatalf("SaveMetadataAndDocumentTx() error: %v", err)
	}
	if newRevision != created.Revision+1 {
		t.Errorf("new revision = %d, want %d", newRevision, created.Revision+1)
	}
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback: %v", rollbackErr)
	}

	if after := seamRowSnapshot(ctx, t, pool, created.ID); after != before {
		t.Errorf("row after metadata rollback changed\n before: %s\n  after: %s", before, after)
	}
}

func TestSaveMetadataAndDocumentTx_ClearsLng(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	tx0, qtx0 := beginSeamTx(ctx, t, pool)
	created, err := s.CreateTx(ctx, qtx0, userID, "Has lng", strp("en"), doc)
	if err != nil {
		t.Fatalf("CreateTx() error: %v", err)
	}
	if commitErr := tx0.Commit(ctx); commitErr != nil {
		t.Fatalf("commit create: %v", commitErr)
	}

	tx, qtx := beginSeamTx(ctx, t, pool)
	if _, saveErr := s.SaveMetadataAndDocumentTx(ctx, qtx, userID, created.ID, "Has lng", nil, doc, created.Revision); saveErr != nil {
		t.Fatalf("SaveMetadataAndDocumentTx(nil lng) error: %v", saveErr)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit: %v", commitErr)
	}

	got, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Lng != nil {
		t.Errorf("Lng after nil write = %q, want cleared (nil)", *got.Lng)
	}
}

func TestSaveMetadataAndDocumentTx_StaleRevisionChangesNothing(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Winner Title", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	// Move the revision forward so created.Revision is stale.
	winningRev, err := s.SaveDocument(ctx, userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("SaveDocument() error: %v", err)
	}
	before := seamRowSnapshot(ctx, t, pool, created.ID)

	tx, qtx := beginSeamTx(ctx, t, pool)
	_, staleErr := s.SaveMetadataAndDocumentTx(ctx, qtx, userID, created.ID, "Loser Title", strp("fr"), doc, created.Revision)
	var mismatch *resume.RevisionMismatchError
	if !errors.As(staleErr, &mismatch) {
		t.Fatalf("SaveMetadataAndDocumentTx(stale) error = %v, want *RevisionMismatchError", staleErr)
	}
	if mismatch.CurrentRevision != winningRev {
		t.Errorf("mismatch.CurrentRevision = %d, want %d", mismatch.CurrentRevision, winningRev)
	}
	if mismatch.Current.Title != "Winner Title" {
		t.Errorf("mismatch winner title = %q, want Winner Title", mismatch.Current.Title)
	}
	// Even committing the transaction persists nothing: the CAS matched
	// zero rows.
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if after := seamRowSnapshot(ctx, t, pool, created.ID); after != before {
		t.Errorf("stale metadata CAS changed the row\n before: %s\n  after: %s", before, after)
	}
}

func TestSaveMetadataAndDocumentTx_BoundsBeforeAnyStatement(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Bounds", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// 161-code-point title and 36-character lng fail through a CLOSED
	// transaction: proof the validation runs before any statement.
	if _, err := s.SaveMetadataAndDocumentTx(ctx, closedSeamTx(ctx, t, pool), userID, created.ID,
		strings.Repeat("x", 161), nil, doc, created.Revision); !errors.Is(err, resume.ErrTitleTooLong) {
		t.Errorf("161-char title error = %v, want ErrTitleTooLong before any statement", err)
	}
	if _, err := s.SaveMetadataAndDocumentTx(ctx, closedSeamTx(ctx, t, pool), userID, created.ID,
		"ok", strp(strings.Repeat("a", 36)), doc, created.Revision); !errors.Is(err, resume.ErrLngTooLong) {
		t.Errorf("36-char lng error = %v, want ErrLngTooLong before any statement", err)
	}

	// The exact boundary values succeed.
	tx, qtx := beginSeamTx(ctx, t, pool)
	if _, err := s.SaveMetadataAndDocumentTx(ctx, qtx, userID, created.ID,
		strings.Repeat("x", 160), strp(strings.Repeat("a", 35)), doc, created.Revision); err != nil {
		t.Errorf("160-char title + 35-char lng error = %v, want nil", err)
	}
	rollbackSeamTx(ctx, t, tx)
}

func TestSaveMetadataAndDocumentTx_WrongOwnerVsMissing_ByteIdentical(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	otherID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Owned", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	tx, qtx := beginSeamTx(ctx, t, pool)
	defer rollbackSeamTx(ctx, t, tx)
	_, wrongOwnerErr := s.SaveMetadataAndDocumentTx(ctx, qtx, otherID, created.ID, "t", nil, doc, created.Revision)
	_, missingErr := s.SaveMetadataAndDocumentTx(ctx, qtx, otherID, uuid.New(), "t", nil, doc, created.Revision)
	if !errors.Is(wrongOwnerErr, resume.ErrNotFound) || !errors.Is(missingErr, resume.ErrNotFound) {
		t.Fatalf("errors = %v / %v, want ErrNotFound for both", wrongOwnerErr, missingErr)
	}
	if fmt.Sprintf("%v", wrongOwnerErr) != fmt.Sprintf("%v", missingErr) {
		t.Errorf("wrong-owner error %q differs from missing error %q", wrongOwnerErr, missingErr)
	}
}

func TestDeleteTx_ReturnsDeletedRowAndDeletes(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "To Delete", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	preRead, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}

	tx, qtx := beginSeamTx(ctx, t, pool)
	deleted, err := s.DeleteTx(ctx, qtx, userID, created.ID, created.Revision)
	if err != nil {
		t.Fatalf("DeleteTx() error: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// The returned row is the deleted row, complete — so the caller can
	// validate its photo key and enqueue cleanup in the same transaction.
	assertResumeRowEqual(t, deleted, preRead)

	if _, err := s.Get(ctx, userID, created.ID); !errors.Is(err, resume.ErrNotFound) {
		t.Errorf("Get() after committed DeleteTx error = %v, want ErrNotFound", err)
	}
}

func TestDeleteTx_StaleRevisionKeepsRowAndCarriesWinner(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Keep Me", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	winningRev, err := s.SaveDocument(ctx, userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("SaveDocument() error: %v", err)
	}
	before := seamRowSnapshot(ctx, t, pool, created.ID)

	tx, qtx := beginSeamTx(ctx, t, pool)
	_, staleErr := s.DeleteTx(ctx, qtx, userID, created.ID, created.Revision)
	var mismatch *resume.RevisionMismatchError
	if !errors.As(staleErr, &mismatch) {
		t.Fatalf("DeleteTx(stale) error = %v, want *RevisionMismatchError (so the HTTP layer can produce 412)", staleErr)
	}
	if mismatch.CurrentRevision != winningRev {
		t.Errorf("mismatch.CurrentRevision = %d, want %d", mismatch.CurrentRevision, winningRev)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if after := seamRowSnapshot(ctx, t, pool, created.ID); after != before {
		t.Errorf("stale DeleteTx changed the row\n before: %s\n  after: %s", before, after)
	}
}

func TestDeleteTx_RollbackKeepsRow(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Survives Rollback", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	before := seamRowSnapshot(ctx, t, pool, created.ID)

	tx, qtx := beginSeamTx(ctx, t, pool)
	if _, err := s.DeleteTx(ctx, qtx, userID, created.ID, created.Revision); err != nil {
		t.Fatalf("DeleteTx() error: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if after := seamRowSnapshot(ctx, t, pool, created.ID); after != before {
		t.Errorf("rolled-back DeleteTx changed the row\n before: %s\n  after: %s", before, after)
	}
}

func TestDeleteTx_WrongOwnerVsMissing_ByteIdentical(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	otherID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Owned", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	before := seamRowSnapshot(ctx, t, pool, created.ID)

	tx, qtx := beginSeamTx(ctx, t, pool)
	defer rollbackSeamTx(ctx, t, tx)
	_, wrongOwnerErr := s.DeleteTx(ctx, qtx, otherID, created.ID, created.Revision)
	_, missingErr := s.DeleteTx(ctx, qtx, otherID, uuid.New(), created.Revision)
	if !errors.Is(wrongOwnerErr, resume.ErrNotFound) || !errors.Is(missingErr, resume.ErrNotFound) {
		t.Fatalf("errors = %v / %v, want ErrNotFound for both", wrongOwnerErr, missingErr)
	}
	if fmt.Sprintf("%v", wrongOwnerErr) != fmt.Sprintf("%v", missingErr) {
		t.Errorf("wrong-owner error %q differs from missing error %q", wrongOwnerErr, missingErr)
	}
	if after := seamRowSnapshot(ctx, t, pool, created.ID); after != before {
		t.Errorf("cross-user delete attempt changed the row\n before: %s\n  after: %s", before, after)
	}
}

// --- Step 3: the composition every wave-3 route relies on ---

func TestExecuteComposition_CreateAndSaveCommit_FailureLeavesNothing(t *testing.T) {
	t.Parallel()
	idem, s, q, _, ctx := newIntegrationIdempotencyStore(t, testutil.NewClockAtEpoch().Now)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	// Execute #1: CreateTx inside the callback.
	var createdID uuid.UUID
	createRes, err := idem.Execute(ctx, userID, "createResume-op", uuid.New(), sha256.Sum256([]byte("create")),
		func(qtx *store.Queries) (resume.StoredResponse, error) {
			created, createErr := s.CreateTx(ctx, qtx, userID, "Composed", nil, doc)
			if createErr != nil {
				return resume.StoredResponse{}, createErr
			}
			createdID = created.ID
			return resume.StoredResponse{Status: 201, Body: []byte(`{"ok":true}`)}, nil
		})
	if err != nil {
		t.Fatalf("Execute(create) error: %v", err)
	}
	if createRes.Replayed || createRes.Outcome != resume.CommitCommitted {
		t.Fatalf("Execute(create) = %+v, want fresh CommitCommitted", createRes)
	}

	// Execute #2 (different key): SaveDocumentTx inside the callback.
	updated := validDocForTest(t)
	updated.PersonalDetails.FullName = strp("Composed Save")
	saveRes, err := idem.Execute(ctx, userID, "saveResume-op", uuid.New(), sha256.Sum256([]byte("save")),
		func(qtx *store.Queries) (resume.StoredResponse, error) {
			if _, saveErr := s.SaveDocumentTx(ctx, qtx, userID, createdID, updated, 1); saveErr != nil {
				return resume.StoredResponse{}, saveErr
			}
			return resume.StoredResponse{Status: 200, Body: []byte(`{"rev":2}`)}, nil
		})
	if err != nil {
		t.Fatalf("Execute(save) error: %v", err)
	}
	if saveRes.Replayed || saveRes.Outcome != resume.CommitCommitted {
		t.Fatalf("Execute(save) = %+v, want fresh CommitCommitted", saveRes)
	}
	got, err := s.Get(ctx, userID, createdID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Revision != 2 {
		t.Errorf("revision after both Executes = %d, want 2", got.Revision)
	}

	// Execute #3: callback fails AFTER SaveDocumentTx — neither the
	// mutation nor an idempotency record survives.
	failKey := uuid.New()
	errAfterSave := errors.New("test: fails after save")
	failRes, err := idem.Execute(ctx, userID, "saveResume-op", failKey, sha256.Sum256([]byte("fail")),
		func(qtx *store.Queries) (resume.StoredResponse, error) {
			clobbered := validDocForTest(t)
			clobbered.PersonalDetails.FullName = strp("Must Not Survive")
			if _, saveErr := s.SaveDocumentTx(ctx, qtx, userID, createdID, clobbered, 2); saveErr != nil {
				return resume.StoredResponse{}, saveErr
			}
			return resume.StoredResponse{}, errAfterSave
		})
	if !errors.Is(err, errAfterSave) {
		t.Fatalf("Execute(fail) error = %v, want the callback's own error", err)
	}
	if failRes.Outcome != resume.CommitDefinitelyRolledBack {
		t.Errorf("Execute(fail).Outcome = %v, want CommitDefinitelyRolledBack", failRes.Outcome)
	}
	after, err := s.Get(ctx, userID, createdID)
	if err != nil {
		t.Fatalf("Get() after failed Execute: %v", err)
	}
	if after.Revision != 2 {
		t.Errorf("revision after failed Execute = %d, want 2 (mutation rolled back)", after.Revision)
	}
	if after.Doc.PersonalDetails.FullName == nil || *after.Doc.PersonalDetails.FullName != "Composed Save" {
		t.Errorf("document after failed Execute = %v, want the previous winner", after.Doc.PersonalDetails.FullName)
	}
	assertIdempotencyRecordAbsent(ctx, t, q, userID, "saveResume-op", failKey)
}

// --- Step 3e: transactional media deletion-job enqueue ---

func TestEnqueueMediaDeletionTx_CommitPersistsBoth_RollbackNeither(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	// Rollback case first.
	victim, err := s.Create(ctx, userID, "Rollback Victim", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	key := seamPhotoKey(victim.ID)
	tx, qtx := beginSeamTx(ctx, t, pool)
	if _, err := s.DeleteTx(ctx, qtx, userID, victim.ID, victim.Revision); err != nil {
		t.Fatalf("DeleteTx() error: %v", err)
	}
	if err := s.EnqueueMediaDeletionTx(ctx, qtx, victim.ID, key); err != nil {
		t.Fatalf("EnqueueMediaDeletionTx() error: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if _, err := s.Get(ctx, userID, victim.ID); err != nil {
		t.Errorf("Get() after rollback error = %v, want the resume back", err)
	}
	if n := seamJobCount(ctx, t, pool, victim.ID); n != 0 {
		t.Errorf("deletion jobs after rollback = %d, want 0", n)
	}

	// Commit case: both the aggregate change and the job persist.
	tx2, qtx2 := beginSeamTx(ctx, t, pool)
	if _, err := s.DeleteTx(ctx, qtx2, userID, victim.ID, victim.Revision); err != nil {
		t.Fatalf("DeleteTx() (second) error: %v", err)
	}
	if err := s.EnqueueMediaDeletionTx(ctx, qtx2, victim.ID, key); err != nil {
		t.Fatalf("EnqueueMediaDeletionTx() (second) error: %v", err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if _, err := s.Get(ctx, userID, victim.ID); !errors.Is(err, resume.ErrNotFound) {
		t.Errorf("Get() after commit error = %v, want ErrNotFound", err)
	}
	if n := seamJobCount(ctx, t, pool, victim.ID); n != 1 {
		t.Errorf("deletion jobs after commit = %d, want exactly 1", n)
	}

	// The job row records the exact key with sane defaults.
	var gotKey string
	var attempts int64
	if err := pool.QueryRow(ctx,
		`SELECT object_key, attempt_count FROM media_deletion_jobs WHERE resume_id = $1`,
		victim.ID).Scan(&gotKey, &attempts); err != nil {
		t.Fatalf("read job row: %v", err)
	}
	if gotKey != key || attempts != 0 {
		t.Errorf("job = (%q, attempts=%d), want (%q, 0)", gotKey, attempts, key)
	}

	// The pending job survives resume deletion (already deleted) AND
	// account deletion: no cascade may remove queued physical cleanup.
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	if n := seamJobCount(ctx, t, pool, victim.ID); n != 1 {
		t.Errorf("deletion jobs after account deletion = %d, want 1", n)
	}
}

func TestEnqueueMediaDeletionTx_DuplicateEnqueueIsIdempotent(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Dup Enqueue", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	key := seamPhotoKey(created.ID)

	tx, qtx := beginSeamTx(ctx, t, pool)
	if err := s.EnqueueMediaDeletionTx(ctx, qtx, created.ID, key); err != nil {
		t.Fatalf("first EnqueueMediaDeletionTx() error: %v", err)
	}
	if err := s.EnqueueMediaDeletionTx(ctx, qtx, created.ID, key); err != nil {
		t.Fatalf("same-tx duplicate EnqueueMediaDeletionTx() error: %v, want nil (idempotent)", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	// A later transaction re-enqueueing the immutable key is also a no-op.
	tx2, qtx2 := beginSeamTx(ctx, t, pool)
	if err := s.EnqueueMediaDeletionTx(ctx, qtx2, created.ID, key); err != nil {
		t.Fatalf("cross-tx duplicate EnqueueMediaDeletionTx() error: %v, want nil (idempotent)", err)
	}
	if err := tx2.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if n := seamJobCount(ctx, t, pool, created.ID); n != 1 {
		t.Errorf("deletion jobs after duplicate enqueues = %d, want exactly 1", n)
	}
}

func TestEnqueueMediaDeletionTx_PhotoReferenceRemovalCommitsWithJob(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Photo Removal", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	key := seamPhotoKey(created.ID)
	withPhoto := doc
	withPhoto.PersonalDetails.Photo = &schema.Photo{Key: key}
	photoRevision, err := s.SaveDocument(ctx, userID, created.ID, withPhoto, created.Revision)
	if err != nil {
		t.Fatalf("SaveDocument(add photo) error: %v", err)
	}

	withoutPhoto := withPhoto
	withoutPhoto.PersonalDetails.Photo = nil
	tx, qtx := beginSeamTx(ctx, t, pool)
	removedRevision, err := s.SaveDocumentTx(ctx, qtx, userID, created.ID, withoutPhoto, photoRevision)
	if err != nil {
		t.Fatalf("SaveDocumentTx(remove photo) error: %v", err)
	}
	if enqueueErr := s.EnqueueMediaDeletionTx(ctx, qtx, created.ID, key); enqueueErr != nil {
		t.Fatalf("EnqueueMediaDeletionTx() error: %v", enqueueErr)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit: %v", commitErr)
	}

	got, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() after photo removal: %v", err)
	}
	if got.Revision != removedRevision || got.Doc.PersonalDetails.Photo != nil {
		t.Errorf("resume after photo removal = revision %d, photo %#v; want revision %d and nil photo", got.Revision, got.Doc.PersonalDetails.Photo, removedRevision)
	}
	if n := seamJobCount(ctx, t, pool, created.ID); n != 1 {
		t.Fatalf("deletion jobs after photo removal = %d, want 1", n)
	}
	var gotKey string
	if err := pool.QueryRow(ctx,
		`SELECT object_key FROM media_deletion_jobs WHERE resume_id = $1`,
		created.ID,
	).Scan(&gotKey); err != nil {
		t.Fatalf("read photo deletion job: %v", err)
	}
	if gotKey != key {
		t.Errorf("photo deletion key = %q, want %q", gotKey, key)
	}
}

func TestEnqueueMediaDeletionTx_MalformedAndCrossResumeChangeNothing(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	resumeA, err := s.Create(ctx, userID, "A", doc)
	if err != nil {
		t.Fatalf("Create(A) error: %v", err)
	}
	resumeB, err := s.Create(ctx, userID, "B", doc)
	if err != nil {
		t.Fatalf("Create(B) error: %v", err)
	}
	beforeA := seamRowSnapshot(ctx, t, pool, resumeA.ID)
	beforeB := seamRowSnapshot(ctx, t, pool, resumeB.ID)

	badKeys := []struct{ name, key string }{
		{"malformed", "not-a-canonical-key"},
		{"traversal", "resumes/" + resumeA.ID.String() + "/../photo-0123456789abcdef0123456789abcdef.jpg"},
		{"cross-resume", seamPhotoKey(resumeB.ID)},
	}
	for _, bad := range badKeys {
		tx, qtx := beginSeamTx(ctx, t, pool)
		// A realistic caller sequence: mutate the aggregate, then enqueue.
		if _, err := s.SaveDocumentTx(ctx, qtx, userID, resumeA.ID, doc, resumeA.Revision); err != nil {
			t.Fatalf("%s: SaveDocumentTx() error: %v", bad.name, err)
		}
		err := s.EnqueueMediaDeletionTx(ctx, qtx, resumeA.ID, bad.key)
		if err == nil {
			t.Fatalf("%s: EnqueueMediaDeletionTx(%q) error = nil, want a key-validation error", bad.name, bad.key)
		}
		for _, sensitive := range []string{bad.key, resumeA.ID.String(), resumeB.ID.String()} {
			if strings.Contains(err.Error(), sensitive) {
				t.Errorf("%s: production error exposes object-key material %q: %v", bad.name, sensitive, err)
			}
		}
		// The enqueue failure aborts the transaction; neither the aggregate
		// write nor any queue row survives.
		rollbackSeamTx(ctx, t, tx)
		if after := seamRowSnapshot(ctx, t, pool, resumeA.ID); after != beforeA {
			t.Errorf("%s: aggregate changed\n before: %s\n  after: %s", bad.name, beforeA, after)
		}
	}
	if after := seamRowSnapshot(ctx, t, pool, resumeB.ID); after != beforeB {
		t.Errorf("neighbor resume changed: %s -> %s", beforeB, after)
	}
	if n := seamJobCount(ctx, t, pool, resumeA.ID); n != 0 {
		t.Errorf("deletion jobs for A = %d, want 0", n)
	}
	if n := seamJobCount(ctx, t, pool, resumeB.ID); n != 0 {
		t.Errorf("deletion jobs for B = %d, want 0", n)
	}
}

func TestEnqueueMediaDeletionTx_StaleRevisionEnqueuesNothing(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Stale Guard", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	winningRev, err := s.SaveDocument(ctx, userID, created.ID, doc, created.Revision)
	if err != nil {
		t.Fatalf("SaveDocument() error: %v", err)
	}
	_ = winningRev
	before := seamRowSnapshot(ctx, t, pool, created.ID)

	// The caller pattern: DeleteTx first; on a stale revision it fails, so
	// the enqueue never happens and the whole transaction rolls back.
	tx, qtx := beginSeamTx(ctx, t, pool)
	_, staleErr := s.DeleteTx(ctx, qtx, userID, created.ID, created.Revision)
	var mismatch *resume.RevisionMismatchError
	if !errors.As(staleErr, &mismatch) {
		t.Fatalf("DeleteTx(stale) error = %v, want *RevisionMismatchError", staleErr)
	}
	rollbackSeamTx(ctx, t, tx)

	if after := seamRowSnapshot(ctx, t, pool, created.ID); after != before {
		t.Errorf("stale delete changed the row\n before: %s\n  after: %s", before, after)
	}
	if n := seamJobCount(ctx, t, pool, created.ID); n != 0 {
		t.Errorf("deletion jobs after stale delete = %d, want 0", n)
	}
}

func TestMediaDeletionJobs_DueOrderIndexSupportsBoundedClaim(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "Index Proof", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	tx, qtx := beginSeamTx(ctx, t, pool)
	if enqueueErr := s.EnqueueMediaDeletionTx(ctx, qtx, created.ID, seamPhotoKey(created.ID)); enqueueErr != nil {
		t.Fatalf("EnqueueMediaDeletionTx() error: %v", enqueueErr)
	}
	if commitErr := tx.Commit(ctx); commitErr != nil {
		t.Fatalf("commit: %v", commitErr)
	}

	// Query-plan evidence: the bounded oldest-due claim shape P8-priv uses
	// is supported by the (next_attempt_at, id) index. enable_seqscan off
	// makes the planner's index choice deterministic on a small table.
	planTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin plan tx: %v", err)
	}
	defer rollbackSeamTx(ctx, t, planTx)
	if _, execErr := planTx.Exec(ctx, `SET LOCAL enable_seqscan = off`); execErr != nil {
		t.Fatalf("disable seqscan: %v", execErr)
	}
	rows, err := planTx.Query(ctx,
		`EXPLAIN SELECT id FROM media_deletion_jobs
		 WHERE next_attempt_at <= now() ORDER BY next_attempt_at, id LIMIT 200`)
	if err != nil {
		t.Fatalf("EXPLAIN error: %v", err)
	}
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan line: %v", err)
		}
		plan.WriteString(line)
		plan.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("plan rows error: %v", err)
	}
	if !strings.Contains(plan.String(), "media_deletion_jobs_next_attempt_idx") {
		t.Errorf("bounded oldest-due claim plan does not use media_deletion_jobs_next_attempt_idx:\n%s", plan.String())
	}
}
