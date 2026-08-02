package resume

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// resumeCapViolationCode/Message are the exact SQLSTATE and MESSAGE the D7
// trigger (enforce_resume_cap, migrations/00005_add_resume_cap_trigger.sql)
// raises when it backstops the store's own cap check. Both must match
// exactly -- other CHECK constraints on resumes (e.g.
// resumes_title_length_check) also raise 23514, so the code alone is not
// enough to identify a cap violation.
const (
	resumeCapViolationCode    = "23514"
	resumeCapViolationMessage = "resumes_user_cap_exceeded"
)

// resumeCap is the 3-resume-per-user limit Create enforces (D7). The D7
// database trigger enforces the identical limit as a backstop for any
// writer that bypasses this store.
const resumeCap = 3

// Store is the resume-domain store: create (cap-enforced), get/list
// (projected to docmigrate.CurrentVersion, D18), delete, and the two
// revision-CAS writes (SaveDocument, SaveTitle). Every write passes
// through ValidateForStore before EncodeParts ever marshals to jsonb --
// this package is the single write-path choke point (D16); nothing else
// in apps/server may call EncodeParts.
type Store struct {
	pool *store.Pool
	q    *store.Queries
	proj *docmigrate.Projector
	now  func() time.Time
}

// NewStore builds a Store backed by pool, using proj to project stored
// documents to docmigrate.CurrentVersion on read (D18). Pass
// docmigrate.NewIdentityProjector() until Task 8's real projector exists.
func NewStore(pool *store.Pool, proj *docmigrate.Projector) *Store {
	return &Store{
		pool: pool,
		q:    store.New(pool),
		proj: proj,
		now:  time.Now,
	}
}

// Create validates doc, then in one transaction: locks the owner's users
// row (LockUserForResumeWrite, spec's FOR UPDATE), checks the resume cap,
// and inserts. Thin wrapper (B7) around createTx: begin a transaction,
// build qtx := s.q.WithTx(tx), call createTx, and let pgx.BeginFunc commit
// or roll back based on its returned error.
func (s *Store) Create(ctx context.Context, userID uuid.UUID, title string, doc schema.Resume) (Resume, error) {
	var created Resume
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		r, err := s.createTx(ctx, qtx, userID, title, doc)
		if err != nil {
			return err
		}
		created = r
		return nil
	})
	if err != nil {
		return Resume{}, err
	}
	return created, nil
}

// createTx is Create's tx-scoped core (B7, owner ruling): it takes an
// already-open qtx and performs its writes on it, with NO transaction
// management of its own -- no Begin/Commit/Rollback. Create above is the
// only caller that opens a transaction around it for the common case;
// Task 7's IdempotencyStore.Execute is the other, composing this exact
// cap-check/insert logic inside its own already-open transaction instead
// of reimplementing it.
func (s *Store) createTx(ctx context.Context, qtx *store.Queries, userID uuid.UUID, title string, doc schema.Resume) (Resume, error) {
	// ValidateForStore always validates at docmigrate.CurrentVersion
	// (D19): force it here so a caller's zero-value or stale
	// doc.SchemaVersion can never validate against the wrong schema.
	doc.SchemaVersion = int64(docmigrate.CurrentVersion)
	if err := ValidateForStore(doc); err != nil {
		return Resume{}, err
	}

	// D7: lock the owner row first, matching the trigger's own lock
	// order (belt and suspenders) so the two can never deadlock. This
	// alone does not make the count race-proof by itself -- it is the
	// FOR UPDATE lock plus reading the count only after acquiring it,
	// under READ COMMITTED, that does (see the trigger's own comment).
	if _, err := qtx.LockUserForResumeWrite(ctx, userID); err != nil {
		return Resume{}, fmt.Errorf("resume: create: lock owner row: %w", err)
	}

	count, err := qtx.CountResumesForUser(ctx, userID)
	if err != nil {
		return Resume{}, fmt.Errorf("resume: create: count existing resumes: %w", err)
	}
	if count >= resumeCap {
		return Resume{}, ErrCapExceeded
	}

	personalDetails, content, customization, err := EncodeParts(doc)
	if err != nil {
		return Resume{}, fmt.Errorf("resume: create: encode document: %w", err)
	}

	row, err := qtx.CreateResume(ctx, store.CreateResumeParams{
		UserID:          userID,
		Title:           title,
		SchemaVersion:   docmigrate.CurrentVersion,
		Lng:             nil,
		PersonalDetails: personalDetails,
		Content:         content,
		Customization:   customization,
	})
	if err != nil {
		if isResumeCapExceeded(err) {
			return Resume{}, ErrCapExceeded
		}
		return Resume{}, fmt.Errorf("resume: create: %w", err)
	}

	return toDomain(row, doc), nil
}

// Get returns userID's resume id, with Doc projected to
// docmigrate.CurrentVersion (D18). It returns ErrNotFound both when id
// does not exist and when it belongs to a different user (D17) -- the two
// cases are indistinguishable by design.
func (s *Store) Get(ctx context.Context, userID, id uuid.UUID) (Resume, error) {
	row, err := s.q.GetResumeForUser(ctx, store.GetResumeForUserParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Resume{}, ErrNotFound
		}
		return Resume{}, fmt.Errorf("resume: get: %w", err)
	}
	return s.projectRow(row)
}

// List returns every resume userID owns, ordered by created_at then id
// (a stable tiebreak for rows created in the same instant), each with Doc
// projected to docmigrate.CurrentVersion (D18).
func (s *Store) List(ctx context.Context, userID uuid.UUID) ([]Resume, error) {
	rows, err := s.q.ListResumesForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("resume: list: %w", err)
	}
	out := make([]Resume, len(rows))
	for i, row := range rows {
		r, err := s.projectRow(row)
		if err != nil {
			return nil, err
		}
		out[i] = r
	}
	return out, nil
}

// Delete removes userID's resume id. It returns ErrNotFound both when id
// does not exist and when it belongs to a different user (D17) -- deleting
// zero rows because of a WHERE id/user_id mismatch is not distinguishable
// from deleting zero rows because id never existed.
func (s *Store) Delete(ctx context.Context, userID, id uuid.UUID) error {
	n, err := s.q.DeleteResumeForUser(ctx, store.DeleteResumeForUserParams{ID: id, UserID: userID})
	if err != nil {
		return fmt.Errorf("resume: delete: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// SaveDocument is the CAS document write: ValidateForStore, then
// UpdateResumeDocumentCAS at schema_version = docmigrate.CurrentVersion.
// Thin wrapper (B7) around saveDocumentTx -- see createTx's doc comment
// for the split's rationale.
func (s *Store) SaveDocument(ctx context.Context, userID, id uuid.UUID, doc schema.Resume, expectedRevision int64) (int64, error) {
	var newRevision int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		rev, err := s.saveDocumentTx(ctx, qtx, userID, id, doc, expectedRevision)
		if err != nil {
			return err
		}
		newRevision = rev
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newRevision, nil
}

// saveDocumentTx is SaveDocument's tx-scoped core (B7): no transaction
// management of its own. A 0-row UpdateResumeDocumentCAS (pgx.ErrNoRows,
// since it is a RETURNING :one query) means either id doesn't exist for
// userID, or expectedRevision is stale -- afterCASMiss re-reads inside the
// SAME transaction to tell the two apart and, for the stale case, to
// return the actual winning row.
func (s *Store) saveDocumentTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, doc schema.Resume, expectedRevision int64) (int64, error) {
	doc.SchemaVersion = int64(docmigrate.CurrentVersion)
	if err := ValidateForStore(doc); err != nil {
		return 0, err
	}

	personalDetails, content, customization, err := EncodeParts(doc)
	if err != nil {
		return 0, fmt.Errorf("resume: save document: encode document: %w", err)
	}

	newRevision, err := qtx.UpdateResumeDocumentCAS(ctx, store.UpdateResumeDocumentCASParams{
		ID:              id,
		UserID:          userID,
		Revision:        expectedRevision,
		PersonalDetails: personalDetails,
		Content:         content,
		Customization:   customization,
		SchemaVersion:   docmigrate.CurrentVersion,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("resume: save document: %w", err)
		}
		return 0, s.afterCASMiss(ctx, qtx, userID, id)
	}
	return newRevision, nil
}

// SaveTitle is the CAS title write: UpdateResumeTitleCAS at the caller's
// expectedRevision. Thin wrapper (B7) around saveTitleTx.
func (s *Store) SaveTitle(ctx context.Context, userID, id uuid.UUID, title string, expectedRevision int64) (int64, error) {
	var newRevision int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		rev, err := s.saveTitleTx(ctx, qtx, userID, id, title, expectedRevision)
		if err != nil {
			return err
		}
		newRevision = rev
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newRevision, nil
}

// saveTitleTx is SaveTitle's tx-scoped core (B7): no transaction
// management of its own. Same 0-row CAS-miss handling as saveDocumentTx,
// via the shared afterCASMiss helper.
func (s *Store) saveTitleTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, title string, expectedRevision int64) (int64, error) {
	newRevision, err := qtx.UpdateResumeTitleCAS(ctx, store.UpdateResumeTitleCASParams{
		ID:       id,
		UserID:   userID,
		Revision: expectedRevision,
		Title:    title,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("resume: save title: %w", err)
		}
		return 0, s.afterCASMiss(ctx, qtx, userID, id)
	}
	return newRevision, nil
}

// afterCASMiss re-reads id inside the SAME transaction as a just-failed
// CAS update (0 rows affected), distinguishing "no such resume for this
// user" (ErrNotFound, D17: also "not yours") from "the resume exists but
// expectedRevision was stale" (*RevisionMismatchError, carrying the
// winning revision and projected document read by this very call).
func (s *Store) afterCASMiss(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID) error {
	row, err := qtx.GetResumeForUser(ctx, store.GetResumeForUserParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("resume: re-read after CAS miss: %w", err)
	}
	current, err := s.projectRow(row)
	if err != nil {
		return err
	}
	return &RevisionMismatchError{CurrentRevision: row.Revision, Current: current}
}

// projectRow assembles row's scalar columns with its three jsonb parts
// projected to docmigrate.CurrentVersion (D18) into a Resume.
func (s *Store) projectRow(row store.Resume) (Resume, error) {
	doc, err := s.proj.Project(row.PersonalDetails, row.Content, row.Customization, row.SchemaVersion)
	if err != nil {
		return Resume{}, fmt.Errorf("resume: project document: %w", err)
	}
	return toDomain(row, doc), nil
}

// toDomain pairs row's own scalar columns with an already-projected doc.
func toDomain(row store.Resume, doc schema.Resume) Resume {
	return Resume{
		ID:                  row.ID,
		UserID:              row.UserID,
		Title:               row.Title,
		Slug:                row.Slug,
		Live:                row.Live,
		DownloadEnabled:     row.DownloadEnabled,
		SEOGeoEnabled:       row.SEOGeoEnabled,
		StoredSchemaVersion: row.SchemaVersion,
		Revision:            row.Revision,
		Lng:                 row.Lng,
		Doc:                 doc,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}
}

// isResumeCapExceeded reports whether err is exactly the D7 trigger's cap
// violation: SQLSTATE 23514 AND message "resumes_user_cap_exceeded". Both
// must match -- resumes has other CHECK constraints that also raise 23514
// (e.g. resumes_title_length_check), and those must fall through to a
// plain wrapped error, never ErrCapExceeded.
func isResumeCapExceeded(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == resumeCapViolationCode && pgErr.Message == resumeCapViolationMessage
}
