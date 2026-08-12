// backfill_test.go proves the CAS backfill's three outcomes against a live
// database and, above all, what BackfillLostRace means:
// a retry signal, never "already done".
//
// The interleavings are staged, not raced: BackfillOneForTest (export_test.go)
// runs the real backfillOne with a pause callback between the read and the
// CAS, so "a concurrent autosave landed in the gap" is a deterministic fact of
// the test rather than a scheduling accident. There is no sleep anywhere in
// this file.
//
// Every identifier here is prefixed `bf` so this file never collides with the
// sibling suites that share package resume_test. It reuses projection_test.go's
// `pj` helpers for the synthetic two-version projector and the row snapshots:
// a row the store itself wrote with this suite's fixture is at v2. Pointing the
// store at the suite's synthetic current-v3 projector makes that row eligible
// for backfill without coupling these tests to the production font converter.
package resume_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// bfSeedOldVersionRow creates a resume through s (whose projector's current
// version is 3) and returns it. The row lands at production schema_version 2,
// so it is exactly the "stored
// below current" shape the backfill exists for.
func bfSeedOldVersionRow(ctx context.Context, t *testing.T, s *resume.Store, userID uuid.UUID, title string) resume.Resume {
	t.Helper()
	created, err := s.Create(ctx, userID, title, validDocForTest(t))
	if err != nil {
		t.Fatalf("Create(%q): %v", title, err)
	}
	return created
}

// bfProjectedDoc marshals what a reader is served for id right now, so "the
// backfill changed nothing observable" can be compared as bytes.
func bfProjectedDoc(ctx context.Context, t *testing.T, s *resume.Store, userID, id uuid.UUID) []byte {
	t.Helper()
	got, err := s.Get(ctx, userID, id)
	if err != nil {
		t.Fatalf("Get(%s): %v", id, err)
	}
	raw, err := json.Marshal(got.Doc)
	if err != nil {
		t.Fatalf("marshal projected document: %v", err)
	}
	return raw
}

func bfSchemaVersion(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID) int32 {
	t.Helper()
	var v int32
	if err := pool.QueryRow(ctx, `SELECT schema_version FROM resumes WHERE id = $1`, id).Scan(&v); err != nil {
		t.Fatalf("read schema_version for %s: %v", id, err)
	}
	return v
}

// --- Applied ---

// TestStore_Integration_BackfillOne_OldVersionRow_Applied proves the row's
// stored bytes move to the current version while revision
// and updated_at do not move at all, and -- the assertion that makes
// "nothing observable changes" more than a claim -- a reader is served
// byte-identical documents immediately before and immediately after.
func TestStore_Integration_BackfillOne_OldVersionRow_Applied(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := pjStore(t, pjSyntheticProjector(t))
	userID := createTestUser(t, q)
	created := bfSeedOldVersionRow(ctx, t, s, userID, "backfill-applied")

	before := pjSnapshot(ctx, t, pool, created.ID)
	if before.SchemaVersion != 2 {
		t.Fatalf("seeded schema_version = %d, want 2 (the row must start BELOW the projector's current)", before.SchemaVersion)
	}
	docBefore := bfProjectedDoc(ctx, t, s, userID, created.ID)

	result, err := s.BackfillOne(ctx, created.ID)
	if err != nil {
		t.Fatalf("BackfillOne() error: %v", err)
	}
	if result != resume.BackfillApplied {
		t.Fatalf("BackfillOne() = %v, want %v", result, resume.BackfillApplied)
	}

	after := pjSnapshot(ctx, t, pool, created.ID)
	if after.SchemaVersion != 3 {
		t.Errorf("schema_version after backfill = %d, want 3", after.SchemaVersion)
	}
	if after.Revision != before.Revision {
		t.Errorf("revision moved %d -> %d; a backfill must not bump it (D12)", before.Revision, after.Revision)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updated_at moved %v -> %v; a backfill is not a user-visible change (D12)", before.UpdatedAt, after.UpdatedAt)
	}
	if after.PersonalDetails == before.PersonalDetails {
		t.Errorf("personal_details was not rewritten; the backfill persisted nothing new:\n %s", after.PersonalDetails)
	}

	// The projected document a reader is served is byte-identical
	// across the backfill. Before, it was converted on the fly; after, it is
	// read straight from storage.
	docAfter := bfProjectedDoc(ctx, t, s, userID, created.ID)
	if string(docBefore) != string(docAfter) {
		t.Errorf("the projected document changed across the backfill:\n before %s\n after  %s", docBefore, docAfter)
	}

	// Running it again is a no-op: the row is now current.
	again, err := s.BackfillOne(ctx, created.ID)
	if err != nil {
		t.Fatalf("second BackfillOne() error: %v", err)
	}
	if again != resume.BackfillSkippedCurrent {
		t.Errorf("second BackfillOne() = %v, want %v", again, resume.BackfillSkippedCurrent)
	}
	pjAssertRowUntouched(t, after, pjSnapshot(ctx, t, pool, created.ID), "after the second BackfillOne")
}

// --- SkippedCurrent ---

// TestStore_Integration_BackfillOne_AlreadyCurrent_SkippedCurrent proves the
// already-current path writes NOTHING: not the parts, not schema_version, not
// revision, not updated_at.
func TestStore_Integration_BackfillOne_AlreadyCurrent_SkippedCurrent(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := pjStore(t, docmigrate.NewIdentityProjector())
	userID := createTestUser(t, q)
	created := bfSeedOldVersionRow(ctx, t, s, userID, "already-current")

	before := pjSnapshot(ctx, t, pool, created.ID)
	result, err := s.BackfillOne(ctx, created.ID)
	if err != nil {
		t.Fatalf("BackfillOne() error: %v", err)
	}
	if result != resume.BackfillSkippedCurrent {
		t.Fatalf("BackfillOne() = %v, want %v", result, resume.BackfillSkippedCurrent)
	}
	pjAssertRowUntouched(t, before, pjSnapshot(ctx, t, pool, created.ID), "after BackfillOne on an already-current row")
}

// --- LostRace: a concurrent autosave in the read/CAS gap ---

// TestStore_Integration_BackfillOne_ConcurrentAutosaveInGap_LostRace stages the
// required interleaving: the backfill reads (schema_version=vOld,
// revision=R), a user's autosave commits R+1, and only then does the CAS run.
// The CAS matches no row, nothing is written, and the caller is told to
// re-observe -- not that the row is done.
func TestStore_Integration_BackfillOne_ConcurrentAutosaveInGap_LostRace(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := pjStore(t, pjSyntheticProjector(t))
	userID := createTestUser(t, q)
	created := bfSeedOldVersionRow(ctx, t, s, userID, "autosave-in-gap")

	var saved bool
	result, err := s.BackfillOneForTest(ctx, created.ID, func() {
		if saved {
			return
		}
		saved = true
		if _, saveErr := s.SaveDocument(ctx, userID, created.ID, validDocForTest(t), created.Revision); saveErr != nil {
			t.Errorf("autosave in the read/CAS gap: %v", saveErr)
		}
	})
	if err != nil {
		t.Fatalf("BackfillOne() error: %v", err)
	}
	if result != resume.BackfillLostRace {
		t.Fatalf("BackfillOne() = %v, want %v", result, resume.BackfillLostRace)
	}

	after := pjSnapshot(ctx, t, pool, created.ID)
	if after.Revision != created.Revision+1 {
		t.Errorf("revision = %d, want %d (only the autosave wrote)", after.Revision, created.Revision+1)
	}
	// The lost race is NOT "already done": the row is still at the old
	// version, so a job that treated BackfillLostRace as terminal would
	// abandon a row that still needs migrating.
	if after.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2 (the backfill must have written nothing)", after.SchemaVersion)
	}

	// Retryable: a fresh observation succeeds.
	retry, err := s.BackfillOne(ctx, created.ID)
	if err != nil {
		t.Fatalf("retry BackfillOne() error: %v", err)
	}
	if retry != resume.BackfillApplied {
		t.Fatalf("retry BackfillOne() = %v, want %v", retry, resume.BackfillApplied)
	}
	if v := bfSchemaVersion(ctx, t, pool, created.ID); v != 3 {
		t.Errorf("schema_version after the retry = %d, want 3", v)
	}
	if final := pjSnapshot(ctx, t, pool, created.ID); final.Revision != after.Revision {
		t.Errorf("the retry bumped revision %d -> %d; a backfill must not (D12)", after.Revision, final.Revision)
	}
}

// --- LostRace: a title-only write in the read/CAS gap ---

// TestStore_Integration_BackfillOne_TitleOnlyWriteInGap_LostRace proves a title
// write touches `title` and `revision` and never `schema_version`, so the CAS
// misses on the revision leg alone. This is the case that proves
// BackfillLostRace cannot be read as "the row became current underneath me":
// nothing about this interleaving migrated anything.
func TestStore_Integration_BackfillOne_TitleOnlyWriteInGap_LostRace(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := pjStore(t, pjSyntheticProjector(t))
	userID := createTestUser(t, q)
	created := bfSeedOldVersionRow(ctx, t, s, userID, "title-in-gap")
	before := pjSnapshot(ctx, t, pool, created.ID)

	var titled bool
	result, err := s.BackfillOneForTest(ctx, created.ID, func() {
		if titled {
			return
		}
		titled = true
		if _, saveErr := s.SaveTitle(ctx, userID, created.ID, "renamed in the gap", created.Revision); saveErr != nil {
			t.Errorf("SaveTitle in the read/CAS gap: %v", saveErr)
		}
	})
	if err != nil {
		t.Fatalf("BackfillOne() error: %v", err)
	}
	if result != resume.BackfillLostRace {
		t.Fatalf("BackfillOne() = %v, want %v", result, resume.BackfillLostRace)
	}

	after := pjSnapshot(ctx, t, pool, created.ID)
	if after.Revision != created.Revision+1 {
		t.Errorf("revision = %d, want %d (only SaveTitle wrote)", after.Revision, created.Revision+1)
	}
	if after.SchemaVersion != 2 {
		t.Errorf("schema_version = %d, want 2: a title write never migrates a document, so this lost race is a retry signal, not completion", after.SchemaVersion)
	}
	if after.PersonalDetails != before.PersonalDetails || after.Content != before.Content || after.Customization != before.Customization {
		t.Error("the losing backfill rewrote a jsonb part; a lost CAS must write nothing")
	}

	retry, err := s.BackfillOne(ctx, created.ID)
	if err != nil {
		t.Fatalf("retry BackfillOne() error: %v", err)
	}
	if retry != resume.BackfillApplied {
		t.Fatalf("retry BackfillOne() = %v, want %v", retry, resume.BackfillApplied)
	}
	if v := bfSchemaVersion(ctx, t, pool, created.ID); v != 3 {
		t.Errorf("schema_version after the retry = %d, want 3", v)
	}
}

// --- An autosave AFTER a successful backfill still holds its revision ---

// TestStore_Integration_BackfillOne_AutosaveAfterBackfill_KeepsRevision is the
// user-visible consequence of not bumping revision: a client that read the row
// before the backfill, and saves after it, must NOT be told its revision is
// stale. A backfill that bumped revision would reject every open
// editor on the migrated row.
func TestStore_Integration_BackfillOne_AutosaveAfterBackfill_KeepsRevision(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := pjStore(t, pjSyntheticProjector(t))
	userID := createTestUser(t, q)
	created := bfSeedOldVersionRow(ctx, t, s, userID, "save-after-backfill")

	result, err := s.BackfillOne(ctx, created.ID)
	if err != nil {
		t.Fatalf("BackfillOne() error: %v", err)
	}
	if result != resume.BackfillApplied {
		t.Fatalf("BackfillOne() = %v, want %v", result, resume.BackfillApplied)
	}

	// created.Revision was observed BEFORE the backfill ran.
	newRev, err := s.SaveDocument(ctx, userID, created.ID, validDocForTest(t), created.Revision)
	if err != nil {
		var mismatch *resume.RevisionMismatchError
		if errors.As(err, &mismatch) {
			t.Fatalf("SaveDocument at the pre-backfill revision returned a revision mismatch (current %d); "+
				"the backfill bumped a revision it must leave alone", mismatch.CurrentRevision)
		}
		t.Fatalf("SaveDocument() after backfill: %v", err)
	}
	if newRev != created.Revision+1 {
		t.Errorf("SaveDocument() newRevision = %d, want %d", newRev, created.Revision+1)
	}
	if v := bfSchemaVersion(ctx, t, pool, created.ID); v == 0 {
		t.Errorf("schema_version = %d, want a non-zero version", v)
	}
}

// --- A corrupt projection surfaces; it is never persisted ---

// TestStore_Integration_BackfillOne_CorruptDocument_ErrorsWithoutWriting: a row
// whose stored bytes cannot be projected and strictly decoded must fail the
// backfill loudly. Silently persisting whatever survived would launder
// corruption into the current version, where nothing would ever look at it
// again.
func TestStore_Integration_BackfillOne_CorruptDocument_ErrorsWithoutWriting(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := pjStore(t, pjSyntheticProjector(t))
	userID := createTestUser(t, q)
	created := bfSeedOldVersionRow(ctx, t, s, userID, "corrupt")

	if _, execErr := pool.Exec(ctx,
		`UPDATE resumes SET personal_details = personal_details || '{"unknownField":1}'::jsonb WHERE id = $1`,
		created.ID); execErr != nil {
		t.Fatalf("inject unknown field: %v", execErr)
	}
	before := pjSnapshot(ctx, t, pool, created.ID)

	result, err := s.BackfillOne(ctx, created.ID)
	if err == nil {
		t.Fatalf("BackfillOne() returned nil error for a corrupt document (result %v)", result)
	}
	pjAssertRowUntouched(t, before, pjSnapshot(ctx, t, pool, created.ID), "after a failed BackfillOne")
}

// --- An id that does not exist ---

func TestStore_Integration_BackfillOne_UnknownID_NotFound(t *testing.T) {
	t.Parallel()
	s, _, _, ctx := pjStore(t, pjSyntheticProjector(t))

	if _, err := s.BackfillOne(ctx, uuid.New()); !errors.Is(err, resume.ErrNotFound) {
		t.Fatalf("BackfillOne(unknown id) error = %v, want ErrNotFound", err)
	}
}
