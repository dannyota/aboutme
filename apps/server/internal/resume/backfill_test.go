package resume_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

func bfSeedOldVersionRow(ctx context.Context, t *testing.T, s *resume.Store, userID uuid.UUID, title string) resume.Resume {
	t.Helper()
	created, err := s.Create(ctx, userID, title, validDocForTest(t))
	if err != nil {
		t.Fatalf("Create(%q): %v", title, err)
	}
	return created
}

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

func TestStore_Integration_BackfillOne_OldVersionRow_Applied(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := pjStore(t, pjSyntheticProjector(t))
	userID := createTestUser(t, q)
	created := bfSeedOldVersionRow(ctx, t, s, userID, "backfill-applied")

	before := pjSnapshot(ctx, t, pool, created.ID)
	if before.SchemaVersion != 6 {
		t.Fatalf("seeded schema_version = %d, want 6 (the row must start BELOW the projector's current)", before.SchemaVersion)
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
	if after.SchemaVersion != 7 {
		t.Errorf("schema_version after backfill = %d, want 7", after.SchemaVersion)
	}
	if after.Revision != before.Revision {
		t.Errorf("revision moved %d -> %d; a backfill must not bump it", before.Revision, after.Revision)
	}
	if !after.UpdatedAt.Equal(before.UpdatedAt) {
		t.Errorf("updated_at moved %v -> %v; a backfill is not a user-visible change", before.UpdatedAt, after.UpdatedAt)
	}
	if after.PersonalDetails == before.PersonalDetails {
		t.Errorf("personal_details was not rewritten; the backfill persisted nothing new:\n %s", after.PersonalDetails)
	}

	docAfter := bfProjectedDoc(ctx, t, s, userID, created.ID)
	if string(docBefore) != string(docAfter) {
		t.Errorf("the projected document changed across the backfill:\n before %s\n after  %s", docBefore, docAfter)
	}

	again, err := s.BackfillOne(ctx, created.ID)
	if err != nil {
		t.Fatalf("second BackfillOne() error: %v", err)
	}
	if again != resume.BackfillSkippedCurrent {
		t.Errorf("second BackfillOne() = %v, want %v", again, resume.BackfillSkippedCurrent)
	}
	pjAssertRowUntouched(t, after, pjSnapshot(ctx, t, pool, created.ID), "after the second BackfillOne")
}

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
	if after.SchemaVersion != 6 {
		t.Errorf("schema_version = %d, want 6 (the backfill must have written nothing)", after.SchemaVersion)
	}

	retry, err := s.BackfillOne(ctx, created.ID)
	if err != nil {
		t.Fatalf("retry BackfillOne() error: %v", err)
	}
	if retry != resume.BackfillApplied {
		t.Fatalf("retry BackfillOne() = %v, want %v", retry, resume.BackfillApplied)
	}
	if v := bfSchemaVersion(ctx, t, pool, created.ID); v != 7 {
		t.Errorf("schema_version after the retry = %d, want 7", v)
	}
	if final := pjSnapshot(ctx, t, pool, created.ID); final.Revision != after.Revision {
		t.Errorf("the retry bumped revision %d -> %d; a backfill must not", after.Revision, final.Revision)
	}
}

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
	if after.SchemaVersion != 6 {
		t.Errorf("schema_version = %d, want 6: a title write never migrates a document, so this lost race is a retry signal, not completion", after.SchemaVersion)
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
	if v := bfSchemaVersion(ctx, t, pool, created.ID); v != 7 {
		t.Errorf("schema_version after the retry = %d, want 7", v)
	}
}

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

func TestStore_Integration_BackfillOne_UnknownID_NotFound(t *testing.T) {
	t.Parallel()
	s, _, _, ctx := pjStore(t, pjSyntheticProjector(t))

	if _, err := s.BackfillOne(ctx, uuid.New()); !errors.Is(err, resume.ErrNotFound) {
		t.Fatalf("BackfillOne(unknown id) error = %v, want ErrNotFound", err)
	}
}
