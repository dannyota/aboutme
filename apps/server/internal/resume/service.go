// service.go is the P2B transaction seam: the exported tx-scoped mirrors
// of Store's pool-backed methods, plus the full-aggregate metadata CAS, the
// revision-CAS delete, and the transactional media deletion-job enqueue.
//
// Each method takes the transaction-bound *store.Queries that
// IdempotencyStore.Execute supplies to its callback, so a read-modify-write
// and its idempotency record commit or roll back together (P2B D15, ADR
// 0016). None of these methods manages a transaction of its own — no
// Begin/Commit/Rollback. The pool-backed methods in store.go are thin
// wrappers over these, so each behavior has exactly one implementation.
//
// Every method is scoped by id AND userID: a wrong-owner id is ErrNotFound,
// identical to a nonexistent one (P2A D17 — no existence oracle).
package resume

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	schema "github.com/dannyota/aboutme/packages/schema/gen/go"

	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// MaxLngCharacters is the budgets.md stored language-tag limit. It is
// measured in Unicode code points to match PostgreSQL char_length(text).
const MaxLngCharacters = 35

// ErrLngTooLong is returned before a transaction or statement when an
// already-canonicalized language tag exceeds MaxLngCharacters. Syntax and
// canonicalization belong to the HTTP boundary.
var ErrLngTooLong = errors.New("resume: language tag exceeds 35 characters")

// CreateTx is Create's transaction-scoped core: validate, lock the owner's
// users row, check the resume cap, insert. lng is the already-canonicalized
// BCP 47 tag, or nil for unset — the HTTP boundary owns parsing and
// canonicalization (D17); this store enforces only the stored-length bound,
// before any statement runs.
func (s *Store) CreateTx(ctx context.Context, qtx *store.Queries, userID uuid.UUID, title string, lng *string, doc schema.Resume) (Resume, error) {
	if err := validateTitle(title); err != nil {
		return Resume{}, err
	}
	if err := validateLng(lng); err != nil {
		return Resume{}, err
	}

	// Force the current version so a caller's zero-value or stale
	// doc.SchemaVersion can never validate against the wrong schema.
	doc.SchemaVersion = int64(docmigrate.CurrentVersion)
	if err := ValidateForStore(doc); err != nil {
		return Resume{}, err
	}

	// Lock the owner row first, matching the trigger's own lock
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

	personalDetails, content, customization, err := encodeParts(doc)
	if err != nil {
		return Resume{}, fmt.Errorf("resume: create: encode document: %w", err)
	}

	row, err := qtx.CreateResume(ctx, store.CreateResumeParams{
		UserID:          userID,
		Title:           title,
		SchemaVersion:   docmigrate.CurrentVersion,
		Lng:             lng,
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

// GetTx is Get's transaction-scoped mirror: userID's resume id with Doc
// projected to docmigrate.CurrentVersion, or ErrNotFound for both a
// missing and a differently owned id.
func (s *Store) GetTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID) (Resume, error) {
	row, err := qtx.GetResumeForUser(ctx, store.GetResumeForUserParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Resume{}, ErrNotFound
		}
		return Resume{}, fmt.Errorf("resume: get: %w", err)
	}
	return s.projectRow(row)
}

// ListTx is List's transaction-scoped mirror: every resume userID owns,
// ordered by created_at then id, each with Doc projected to
// docmigrate.CurrentVersion.
func (s *Store) ListTx(ctx context.Context, qtx *store.Queries, userID uuid.UUID) ([]Resume, error) {
	rows, err := qtx.ListResumesForUser(ctx, userID)
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

// SaveDocumentTx is SaveDocument's transaction-scoped core: ValidateForStore,
// then UpdateResumeDocumentCAS at schema_version = docmigrate.CurrentVersion.
// A 0-row CAS (pgx.ErrNoRows, since it is a RETURNING :one query) means
// either id doesn't exist for userID, or expectedRevision is stale --
// afterCASMiss re-reads inside the SAME transaction to tell the two apart
// and, for the stale case, to return the actual winning row.
func (s *Store) SaveDocumentTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, doc schema.Resume, expectedRevision int64) (int64, error) {
	doc.SchemaVersion = int64(docmigrate.CurrentVersion)
	if err := ValidateForStore(doc); err != nil {
		return 0, err
	}

	personalDetails, content, customization, err := encodeParts(doc)
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

// SaveMetadataAndDocumentTx writes title, lng, and the caller-supplied
// already-projected, sanitized, validated current-version aggregate under
// ONE revision CAS (P2B D15/D17). The store persists the document
// unchanged — validation here is the same store validation every document
// write gets, not a second sanitizer. lng is stored as given: nil clears,
// a non-nil canonical tag over 35 characters is rejected before any
// statement runs. A stale expectedRevision changes nothing and surfaces
// the winning row as *RevisionMismatchError; a wrong-owner or missing id
// is ErrNotFound.
func (s *Store) SaveMetadataAndDocumentTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID,
	title string, lng *string, doc schema.Resume, expectedRevision int64,
) (int64, error) {
	if err := validateTitle(title); err != nil {
		return 0, err
	}
	if err := validateLng(lng); err != nil {
		return 0, err
	}
	doc.SchemaVersion = int64(docmigrate.CurrentVersion)
	if err := ValidateForStore(doc); err != nil {
		return 0, err
	}

	personalDetails, content, customization, err := encodeParts(doc)
	if err != nil {
		return 0, fmt.Errorf("resume: save metadata and document: encode document: %w", err)
	}

	newRevision, err := qtx.UpdateResumeMetadataAndDocumentCAS(ctx, store.UpdateResumeMetadataAndDocumentCASParams{
		ID:              id,
		UserID:          userID,
		Revision:        expectedRevision,
		Title:           title,
		Lng:             lng,
		PersonalDetails: personalDetails,
		Content:         content,
		Customization:   customization,
		SchemaVersion:   docmigrate.CurrentVersion,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("resume: save metadata and document: %w", err)
		}
		return 0, s.afterCASMiss(ctx, qtx, userID, id)
	}
	return newRevision, nil
}

// DeleteTx is the revision-CAS delete. It returns the deleted row —
// projected like any read — so its caller can validate the stored photo
// key and enqueue media cleanup in the same transaction. On a CAS miss it
// re-reads the scoped winner so the HTTP layer can produce 412; a wrong
// owner and a missing id remain indistinguishable (ErrNotFound).
func (s *Store) DeleteTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, expectedRevision int64) (Resume, error) {
	row, err := qtx.DeleteResumeForUserCAS(ctx, store.DeleteResumeForUserCASParams{
		ID:       id,
		UserID:   userID,
		Revision: expectedRevision,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return Resume{}, fmt.Errorf("resume: delete: %w", err)
		}
		return Resume{}, s.afterCASMiss(ctx, qtx, userID, id)
	}
	return s.projectRow(row)
}

// EnqueueMediaDeletionTx records exact-key cleanup work in the caller's
// transaction (ADR 0019, D13). The caller validates key against resumeID
// before this call; the ledger's own database check (D11 grammar with the
// embedded canonical resume ID equal to resume_id) is the fail-closed
// backstop, so a malformed or cross-resume key errors here and aborts the
// caller's transaction. Duplicate enqueue of the immutable key is
// idempotent. The document, not this job, remains the media ownership
// authority.
func (s *Store) EnqueueMediaDeletionTx(ctx context.Context, qtx *store.Queries, resumeID uuid.UUID, key string) error {
	if _, err := qtx.EnqueueMediaDeletionJob(ctx, store.EnqueueMediaDeletionJobParams{
		ResumeID:  resumeID,
		ObjectKey: key,
	}); err != nil {
		return fmt.Errorf("resume: enqueue media deletion job: %w", err)
	}
	return nil
}
