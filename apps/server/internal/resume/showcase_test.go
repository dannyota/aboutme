package resume_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// recordingSync records each showcase sync and what the transaction it runs in
// can see.
type recordingSync struct {
	err      error
	calls    int
	resumeID uuid.UUID
	revision int64
}

func (s *recordingSync) SyncTx(ctx context.Context, qtx *store.Queries, resumeID uuid.UUID) error {
	s.calls++
	s.resumeID = resumeID
	row, err := qtx.GetResumeByID(ctx, resumeID)
	if err != nil {
		return err
	}
	s.revision = row.Revision
	return s.err
}

func revisionOf(ctx context.Context, t *testing.T, pool *store.Pool, id uuid.UUID) int64 {
	t.Helper()
	var revision int64
	if err := pool.QueryRow(ctx, `SELECT revision FROM resumes WHERE id = $1`, id).Scan(&revision); err != nil {
		t.Fatalf("read revision: %v", err)
	}
	return revision
}

// AC-SHOW-006: a document write runs the showcase sync inside its own
// transaction, after the row update, so a failed sync rolls the write back.
func TestDocumentWritesRunShowcaseSyncInTheSameTransaction(t *testing.T) {
	t.Parallel()
	_, q, pool, ctx := newIntegrationStore(t)
	sync := &recordingSync{}
	s := resume.NewStore(pool, docmigrate.NewIdentityProjector(), resume.WithShowcaseSync(sync))
	userID := createTestUser(t, q)
	doc := validDocForTest(t)
	created, err := s.Create(ctx, userID, "Hooked", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	if sync.calls != 0 {
		t.Fatalf("Create() ran the sync %d times, want 0", sync.calls)
	}

	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		revision, saveErr := s.SaveDocumentTx(ctx, q.WithTx(tx), userID, created.ID, doc, created.Revision)
		if saveErr != nil {
			return saveErr
		}
		if revision != created.Revision+1 || sync.calls != 1 || sync.resumeID != created.ID || sync.revision != revision {
			t.Errorf("revision %d, sync calls %d id %v saw revision %d, want one sync that saw %d", revision, sync.calls, sync.resumeID, sync.revision, created.Revision+1)
		}
		if outside := revisionOf(ctx, t, pool, created.ID); outside != created.Revision {
			t.Errorf("revision outside the transaction = %d before commit, want %d", outside, created.Revision)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("SaveDocumentTx transaction: %v", err)
	}

	current, err := s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		_, saveErr := s.SaveMetadataAndDocumentTx(ctx, q.WithTx(tx), userID, created.ID, "Renamed", nil, doc, current.Revision)
		return saveErr
	})
	if err != nil || sync.calls != 2 {
		t.Fatalf("SaveMetadataAndDocumentTx err=%v sync calls=%d, want one more sync", err, sync.calls)
	}

	// A title-only write cannot change what the listing shows.
	current, err = s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	if _, saveErr := s.SaveTitle(ctx, userID, created.ID, "Retitled", current.Revision); saveErr != nil || sync.calls != 2 {
		t.Fatalf("SaveTitle err=%v sync calls=%d, want no sync", saveErr, sync.calls)
	}

	// A stale write never reaches the sync.
	if _, staleErr := s.SaveDocument(ctx, userID, created.ID, doc, created.Revision); staleErr == nil || sync.calls != 2 {
		t.Fatalf("stale SaveDocument err=%v sync calls=%d, want a mismatch and no sync", staleErr, sync.calls)
	}

	// A failing sync rolls the document write back.
	current, err = s.Get(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Get() error: %v", err)
	}
	sync.err = errors.New("sync failed")
	if _, saveErr := s.SaveDocument(ctx, userID, created.ID, doc, current.Revision); saveErr == nil {
		t.Fatal("SaveDocument() succeeded although the showcase sync failed")
	}
	if got := revisionOf(ctx, t, pool, created.ID); got != current.Revision {
		t.Fatalf("revision after a failed sync = %d, want %d", got, current.Revision)
	}
}

func insertTestShowcase(ctx context.Context, t *testing.T, pool *store.Pool, resumeID uuid.UUID, role *string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		INSERT INTO resume_showcase (resume_id, requested_at, role, card_version)
		VALUES ($1, $2, $3, 'fedcba9876543210')`, resumeID, time.Now().UTC(), role); err != nil {
		t.Fatalf("insert showcase row: %v", err)
	}
}

// AC-SHOW-001: the owner reads null without an opt-in and the listed state and
// role with one, through Get, List, and the stale-revision winner.
func TestStoreReadsCarryTheOwnersShowcase(t *testing.T) {
	t.Parallel()
	s, q, pool, ctx := newIntegrationStore(t)
	userID := createTestUser(t, q)
	doc := validDocForTest(t)
	optedIn, err := s.Create(ctx, userID, "Opted in", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	plain, err := s.Create(ctx, userID, "Plain", doc)
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	role := "backend"
	insertTestShowcase(ctx, t, pool, optedIn.ID, &role)

	got, err := s.Get(ctx, userID, plain.ID)
	if err != nil || got.Showcase != nil {
		t.Fatalf("Get(plain) showcase = %+v err=%v, want nil", got.Showcase, err)
	}
	got, err = s.Get(ctx, userID, optedIn.ID)
	if err != nil || got.Showcase == nil || got.Showcase.State != resume.ShowcaseListed || got.Showcase.Role == nil || *got.Showcase.Role != role {
		t.Fatalf("Get(opted in) showcase = %+v err=%v, want listed backend", got.Showcase, err)
	}

	listed, err := s.List(ctx, userID)
	if err != nil || len(listed) != 2 {
		t.Fatalf("List() = %d resumes err=%v, want 2", len(listed), err)
	}
	for _, item := range listed {
		switch item.ID {
		case optedIn.ID:
			if item.Showcase == nil || item.Showcase.State != resume.ShowcaseListed {
				t.Errorf("List() opted in showcase = %+v, want listed", item.Showcase)
			}
		case plain.ID:
			if item.Showcase != nil {
				t.Errorf("List() plain showcase = %+v, want nil", item.Showcase)
			}
		}
	}

	_, err = s.SaveDocument(ctx, userID, optedIn.ID, doc, optedIn.Revision+5)
	var mismatch *resume.RevisionMismatchError
	if !errors.As(err, &mismatch) || mismatch.Current.Showcase == nil {
		t.Fatalf("stale SaveDocument() error = %v, want a mismatch carrying the showcase state", err)
	}
}
