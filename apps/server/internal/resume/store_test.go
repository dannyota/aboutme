// store_test.go exercises resume.Store against a live Postgres database
// (spec §9): create (cap-enforced), get/list (projected), delete, and the
// revision-CAS writes. Every DB-backed test here is skipped, not failed,
// when TEST_DATABASE_URL is unset -- UNLESS REQUIRE_TEST_DB=1 is also set,
// in which case internal/testutil.RequireMigratedTestDatabaseURL fails
// closed instead (see that helper's own doc comment). Test setup goes
// through that same shared helper internal/auth, internal/user, and
// internal/store use, so this package's tests never depend on another
// package's test binary having already applied migrations first.
package resume_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

// newIntegrationStore returns a resume.Store backed by a fresh connection
// pool against TEST_DATABASE_URL, with the database's schema brought to
// head first. Unlike internal/user's single-enclosing-transaction test
// helper, resume.Store opens its OWN transactions per write (B7), so tests
// need a real pool, not a pre-opened tx -- isolation instead comes from
// every test creating its own throwaway user (createTestUser) and scoping
// all assertions to that user's own resumes.
func newIntegrationStore(t *testing.T) (*resume.Store, *store.Queries, context.Context) {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })

	s := resume.NewStore(pool, docmigrate.NewIdentityProjector())
	return s, store.New(pool), ctx
}

// createTestUser inserts a minimal users row and returns its ID, so tests
// that need a real userID (resumes.user_id REFERENCES users(id)) have one
// to point at. The random email (matching internal/auth's
// transaction_test.go convention) only needs to be globally unique, never
// reproducible -- nothing asserts against its value.
func createTestUser(t *testing.T, q *store.Queries) uuid.UUID {
	t.Helper()
	u, err := q.CreateUser(context.Background(), store.CreateUserParams{
		Email: uuid.NewString() + "@example.com",
		Name:  "Test User",
	})
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	return u.ID
}

// assertResumeRowEqual compares every column of got against want,
// including the doc via canonical-byte comparison (not reflect.DeepEqual --
// see codec_test.go's own byte-stability comments for why). Used by the
// D17 wrong-user tests and the CAS invalid-doc test to prove a rejected
// write left the row completely untouched.
func assertResumeRowEqual(t *testing.T, got, want resume.Resume) {
	t.Helper()

	if got.ID != want.ID {
		t.Errorf("ID = %v, want %v", got.ID, want.ID)
	}
	if got.UserID != want.UserID {
		t.Errorf("UserID = %v, want %v", got.UserID, want.UserID)
	}
	if got.Title != want.Title {
		t.Errorf("Title = %q, want %q", got.Title, want.Title)
	}
	switch {
	case (got.Slug == nil) != (want.Slug == nil):
		t.Errorf("Slug = %v, want %v", got.Slug, want.Slug)
	case got.Slug != nil && *got.Slug != *want.Slug:
		t.Errorf("Slug = %q, want %q", *got.Slug, *want.Slug)
	}
	if got.Live != want.Live {
		t.Errorf("Live = %v, want %v", got.Live, want.Live)
	}
	if got.DownloadEnabled != want.DownloadEnabled {
		t.Errorf("DownloadEnabled = %v, want %v", got.DownloadEnabled, want.DownloadEnabled)
	}
	if got.SEOGeoEnabled != want.SEOGeoEnabled {
		t.Errorf("SEOGeoEnabled = %v, want %v", got.SEOGeoEnabled, want.SEOGeoEnabled)
	}
	if got.StoredSchemaVersion != want.StoredSchemaVersion {
		t.Errorf("StoredSchemaVersion = %d, want %d", got.StoredSchemaVersion, want.StoredSchemaVersion)
	}
	if got.Revision != want.Revision {
		t.Errorf("Revision = %d, want %d", got.Revision, want.Revision)
	}
	switch {
	case (got.Lng == nil) != (want.Lng == nil):
		t.Errorf("Lng = %v, want %v", got.Lng, want.Lng)
	case got.Lng != nil && *got.Lng != *want.Lng:
		t.Errorf("Lng = %q, want %q", *got.Lng, *want.Lng)
	}
	if !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("CreatedAt = %v, want %v", got.CreatedAt, want.CreatedAt)
	}
	if !got.UpdatedAt.Equal(want.UpdatedAt) {
		t.Errorf("UpdatedAt = %v, want %v", got.UpdatedAt, want.UpdatedAt)
	}

	gotDoc, err := resume.AssembleCanonical(got.Doc)
	if err != nil {
		t.Fatalf("AssembleCanonical(got.Doc): %v", err)
	}
	wantDoc, err := resume.AssembleCanonical(want.Doc)
	if err != nil {
		t.Fatalf("AssembleCanonical(want.Doc): %v", err)
	}
	if !bytes.Equal(gotDoc, wantDoc) {
		t.Errorf("Doc canonical = %s, want %s", gotDoc, wantDoc)
	}
}

// --- Step 1: happy-path integration tests ---

func TestStore_Integration_CreateGetRoundTrip(t *testing.T) {
	t.Parallel()
	s, q, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	created, err := s.Create(ctx, userID, "My Resume", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	if created.Revision != 1 {
		t.Errorf("Create().Revision = %d, want 1", created.Revision)
	}
	if created.Live {
		t.Errorf("Create().Live = true, want false (default)")
	}
	if !created.DownloadEnabled {
		t.Errorf("Create().DownloadEnabled = false, want true (default)")
	}
	if created.SEOGeoEnabled {
		t.Errorf("Create().SEOGeoEnabled = true, want false (default)")
	}
	if created.Title != "My Resume" {
		t.Errorf("Create().Title = %q, want %q", created.Title, "My Resume")
	}
	if created.UserID != userID {
		t.Errorf("Create().UserID = %v, want %v", created.UserID, userID)
	}

	wantDoc, err := resume.AssembleCanonical(doc)
	if err != nil {
		t.Fatalf("AssembleCanonical(doc): %v", err)
	}
	createdDoc, err := resume.AssembleCanonical(created.Doc)
	if err != nil {
		t.Fatalf("AssembleCanonical(created.Doc): %v", err)
	}
	if !bytes.Equal(wantDoc, createdDoc) {
		t.Errorf("Create().Doc canonical = %s, want %s", createdDoc, wantDoc)
	}

	got, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	assertResumeRowEqual(t, got, created)
}

func TestStore_Integration_ListOrderingStable(t *testing.T) {
	t.Parallel()
	s, q, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	var wantIDs []uuid.UUID
	for i := 0; i < 3; i++ {
		created, err := s.Create(ctx, userID, fmt.Sprintf("Resume %d", i), doc)
		if err != nil {
			t.Fatalf("Create() #%d error: %v", i, err)
		}
		wantIDs = append(wantIDs, created.ID)
	}

	list, err := s.List(ctx, userID)
	if err != nil {
		t.Fatalf("List() error: %v", err)
	}
	if len(list) != len(wantIDs) {
		t.Fatalf("List() returned %d resumes, want %d", len(list), len(wantIDs))
	}
	for i, r := range list {
		if r.ID != wantIDs[i] {
			t.Errorf("List()[%d].ID = %v, want %v (creation order: created_at, id)", i, r.ID, wantIDs[i])
		}
	}
}

func TestStore_Integration_DeleteThenGetNotFound(t *testing.T) {
	t.Parallel()
	s, q, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)

	created, err := s.Create(ctx, userID, "Doomed", validDocForTest(t))
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	if err := s.Delete(ctx, userID, created.ID); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}

	if _, err := s.Get(ctx, userID, created.ID); !errors.Is(err, resume.ErrNotFound) {
		t.Errorf("Get() after Delete() error = %v, want resume.ErrNotFound", err)
	}

	// Deleting an already-gone id is ErrNotFound too, not a silent no-op.
	if err := s.Delete(ctx, userID, created.ID); !errors.Is(err, resume.ErrNotFound) {
		t.Errorf("second Delete() error = %v, want resume.ErrNotFound", err)
	}
}

func TestStore_Integration_WrongUser_NotFoundAndRowUntouched(t *testing.T) {
	t.Parallel()
	s, q, ctx := newIntegrationStore(t)
	ownerID := createTestUser(t, q)
	otherID := createTestUser(t, q)

	created, err := s.Create(ctx, ownerID, "Owner's Resume", validDocForTest(t))
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	// D17: a real id belonging to a DIFFERENT user is ErrNotFound, not a
	// distinguishable "forbidden" -- no existence oracle.
	_, err = s.Get(ctx, otherID, created.ID)
	if !errors.Is(err, resume.ErrNotFound) {
		t.Errorf("Get() with wrong user error = %v, want resume.ErrNotFound", err)
	}
	err = s.Delete(ctx, otherID, created.ID)
	if !errors.Is(err, resume.ErrNotFound) {
		t.Errorf("Delete() with wrong user error = %v, want resume.ErrNotFound", err)
	}

	// The owner's row must be byte-for-byte identical to right after Create.
	afterAttack, err := s.Get(ctx, ownerID, created.ID)
	if err != nil {
		t.Fatalf("Get() by owner after wrong-user attempts error: %v", err)
	}
	assertResumeRowEqual(t, afterAttack, created)
}

// --- Step 2: cap tests ---

func TestStore_Integration_CapEnforcement(t *testing.T) {
	t.Parallel()
	s, q, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)

	var created []resume.Resume
	for i := 0; i < 3; i++ {
		r, err := s.Create(ctx, userID, fmt.Sprintf("Resume %d", i), doc)
		if err != nil {
			t.Fatalf("Create() #%d error: %v", i, err)
		}
		created = append(created, r)
	}

	if _, err := s.Create(ctx, userID, "Resume 4 (over cap)", doc); !errors.Is(err, resume.ErrCapExceeded) {
		t.Fatalf("4th Create() error = %v, want resume.ErrCapExceeded", err)
	}

	// Deleting one frees a slot for this same user.
	if err := s.Delete(ctx, userID, created[0].ID); err != nil {
		t.Fatalf("Delete() error: %v", err)
	}
	if _, err := s.Create(ctx, userID, "Resume 4 (after delete)", doc); err != nil {
		t.Fatalf("Create() after Delete() error: %v", err)
	}

	// A second user's cap is independent of the first's.
	otherUserID := createTestUser(t, q)
	if _, err := s.Create(ctx, otherUserID, "Other user's first resume", doc); err != nil {
		t.Fatalf("Create() for a second, unrelated user error: %v", err)
	}
}

// --- Step 3: CAS tests ---

func TestStore_Integration_SaveDocument_CAS(t *testing.T) {
	t.Parallel()
	s, q, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)

	created, err := s.Create(ctx, userID, "CAS Doc", validDocForTest(t))
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	winningDoc := validDocForTest(t)
	winningDoc.PersonalDetails.FullName = strp("Updated By Winner")

	newRev, err := s.SaveDocument(ctx, userID, created.ID, winningDoc, created.Revision)
	if err != nil {
		t.Fatalf("SaveDocument() error: %v", err)
	}
	if newRev != created.Revision+1 {
		t.Errorf("SaveDocument() newRevision = %d, want %d", newRev, created.Revision+1)
	}

	got, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Revision != newRev {
		t.Errorf("Get().Revision = %d, want %d", got.Revision, newRev)
	}
	wantWinningDoc, err := resume.AssembleCanonical(winningDoc)
	if err != nil {
		t.Fatalf("AssembleCanonical(winningDoc): %v", err)
	}
	gotDoc, err := resume.AssembleCanonical(got.Doc)
	if err != nil {
		t.Fatalf("AssembleCanonical(got.Doc): %v", err)
	}
	if !bytes.Equal(wantWinningDoc, gotDoc) {
		t.Errorf("Get().Doc canonical after save = %s, want %s", gotDoc, wantWinningDoc)
	}

	// Stale revision (the ORIGINAL, pre-save revision): must fail with the
	// WINNING revision/doc just written above, not the loser's own content.
	loserDoc := validDocForTest(t)
	loserDoc.PersonalDetails.FullName = strp("Should Never Land")
	_, err = s.SaveDocument(ctx, userID, created.ID, loserDoc, created.Revision)
	var mismatch *resume.RevisionMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("stale SaveDocument() error = %v (%T), want *resume.RevisionMismatchError", err, err)
	}
	if mismatch.CurrentRevision != newRev {
		t.Errorf("mismatch.CurrentRevision = %d, want %d", mismatch.CurrentRevision, newRev)
	}
	mismatchDoc, err := resume.AssembleCanonical(mismatch.Current.Doc)
	if err != nil {
		t.Fatalf("AssembleCanonical(mismatch.Current.Doc): %v", err)
	}
	if !bytes.Equal(wantWinningDoc, mismatchDoc) {
		t.Errorf("mismatch.Current.Doc canonical = %s, want the WINNING doc %s", mismatchDoc, wantWinningDoc)
	}

	// Unknown id -> ErrNotFound.
	_, err = s.SaveDocument(ctx, userID, uuid.New(), winningDoc, 1)
	if !errors.Is(err, resume.ErrNotFound) {
		t.Errorf("SaveDocument() unknown id error = %v, want resume.ErrNotFound", err)
	}

	// Invalid doc -> *ValidationError, row completely untouched.
	beforeInvalid, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() before invalid-doc attempt error: %v", err)
	}
	invalidDoc := validDocForTest(t)
	invalidDoc.PersonalDetails.Details[0].ID = "not-a-uuid"
	_, err = s.SaveDocument(ctx, userID, created.ID, invalidDoc, beforeInvalid.Revision)
	var valErr *resume.ValidationError
	if !errors.As(err, &valErr) {
		t.Fatalf("invalid-doc SaveDocument() error = %v (%T), want *resume.ValidationError", err, err)
	}
	afterInvalid, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() after invalid-doc attempt error: %v", err)
	}
	assertResumeRowEqual(t, afterInvalid, beforeInvalid)
}

func TestStore_Integration_SaveTitle_CAS(t *testing.T) {
	t.Parallel()
	s, q, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)

	created, err := s.Create(ctx, userID, "Original Title", validDocForTest(t))
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}

	newRev, err := s.SaveTitle(ctx, userID, created.ID, "Winning Title", created.Revision)
	if err != nil {
		t.Fatalf("SaveTitle() error: %v", err)
	}
	if newRev != created.Revision+1 {
		t.Errorf("SaveTitle() newRevision = %d, want %d", newRev, created.Revision+1)
	}

	got, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if got.Title != "Winning Title" {
		t.Errorf("Get().Title = %q, want %q", got.Title, "Winning Title")
	}
	if got.Revision != newRev {
		t.Errorf("Get().Revision = %d, want %d", got.Revision, newRev)
	}

	// Stale revision (the ORIGINAL, pre-save revision): must fail with the
	// WINNING revision/title just written above.
	_, err = s.SaveTitle(ctx, userID, created.ID, "Loser Title", created.Revision)
	var mismatch *resume.RevisionMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("stale SaveTitle() error = %v (%T), want *resume.RevisionMismatchError", err, err)
	}
	if mismatch.CurrentRevision != newRev {
		t.Errorf("mismatch.CurrentRevision = %d, want %d", mismatch.CurrentRevision, newRev)
	}
	if mismatch.Current.Title != "Winning Title" {
		t.Errorf("mismatch.Current.Title = %q, want the WINNING title %q", mismatch.Current.Title, "Winning Title")
	}

	// Unknown id -> ErrNotFound.
	if _, err := s.SaveTitle(ctx, userID, uuid.New(), "Doesn't matter", 1); !errors.Is(err, resume.ErrNotFound) {
		t.Errorf("SaveTitle() unknown id error = %v, want resume.ErrNotFound", err)
	}
}

// --- Cap-error mapping: pure unit test, no database ---

// TestIsResumeCapExceeded_ExactMatchOnBothCodeAndMessage proves the D7
// mapping requires an EXACT match on both SQLSTATE 23514 and the message
// "resumes_user_cap_exceeded" -- resumes has other CHECK constraints that
// also raise 23514 (e.g. resumes_title_length_check), and those must never
// be mistaken for a cap violation.
func TestIsResumeCapExceeded_ExactMatchOnBothCodeAndMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "exact cap violation",
			err:  &pgconn.PgError{Code: "23514", Message: "resumes_user_cap_exceeded"},
			want: true,
		},
		{
			name: "wrapped exact cap violation still matches via errors.As",
			err:  fmt.Errorf("wrapped: %w", &pgconn.PgError{Code: "23514", Message: "resumes_user_cap_exceeded"}),
			want: true,
		},
		{
			name: "same SQLSTATE, different CHECK constraint message",
			err:  &pgconn.PgError{Code: "23514", Message: "resumes_title_length_check"},
			want: false,
		},
		{
			name: "same message text, different SQLSTATE",
			err:  &pgconn.PgError{Code: "23505", Message: "resumes_user_cap_exceeded"},
			want: false,
		},
		{
			name: "unrelated error",
			err:  errors.New("boom"),
			want: false,
		},
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resume.IsResumeCapExceededForTest(tt.err); got != tt.want {
				t.Errorf("IsResumeCapExceededForTest(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
