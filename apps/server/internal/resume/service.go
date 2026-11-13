package resume

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

const MaxLngCharacters = 35

var ErrLngTooLong = errors.New("resume: language tag exceeds 35 characters")

func (s *Store) CreateTx(ctx context.Context, qtx *store.Queries, userID uuid.UUID, title string, lng *string, doc schema.Resume) (Resume, error) {
	if err := validateTitle(title); err != nil {
		return Resume{}, err
	}
	if err := validateLng(lng); err != nil {
		return Resume{}, err
	}

	doc.SchemaVersion = int64(docmigrate.CurrentVersion)
	if err := ValidateForStore(doc); err != nil {
		return Resume{}, err
	}

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
	if err = checkProjectLinksTx(ctx, qtx, userID, doc); err != nil {
		return Resume{}, err
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

func (s *Store) GetTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID) (Resume, error) {
	row, err := qtx.GetResumeForUser(ctx, store.GetResumeForUserParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Resume{}, ErrNotFound
		}
		return Resume{}, fmt.Errorf("resume: get: %w", err)
	}
	got, err := s.projectRow(row)
	if err != nil {
		return Resume{}, err
	}
	if err = attachShowcase(ctx, qtx, &got); err != nil {
		return Resume{}, err
	}
	return got, nil
}

func (s *Store) ListTx(ctx context.Context, qtx *store.Queries, userID uuid.UUID) ([]Resume, error) {
	rows, err := qtx.ListResumesForUser(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("resume: list: %w", err)
	}
	out := make([]Resume, len(rows))
	for i, row := range rows {
		r, projectErr := s.projectRow(row)
		if projectErr != nil {
			return nil, projectErr
		}
		out[i] = r
	}
	if err = attachShowcases(ctx, qtx, userID, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) SaveDocumentTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, doc schema.Resume, expectedRevision int64) (Resume, error) {
	doc.SchemaVersion = int64(docmigrate.CurrentVersion)
	if err := ValidateForStore(doc); err != nil {
		return Resume{}, err
	}
	if err := checkProjectLinksTx(ctx, qtx, userID, doc); err != nil {
		return Resume{}, err
	}

	personalDetails, content, customization, err := encodeParts(doc)
	if err != nil {
		return Resume{}, fmt.Errorf("resume: save document: encode document: %w", err)
	}

	row, err := qtx.UpdateResumeDocumentCAS(ctx, store.UpdateResumeDocumentCASParams{
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
			return Resume{}, fmt.Errorf("resume: save document: %w", err)
		}
		return Resume{}, s.afterCASMiss(ctx, qtx, userID, id)
	}
	if err = s.syncShowcaseTx(ctx, qtx, id); err != nil {
		return Resume{}, fmt.Errorf("resume: save document: %w", err)
	}
	saved, err := s.projectRow(row)
	if err != nil {
		return Resume{}, err
	}
	if err = attachShowcase(ctx, qtx, &saved); err != nil {
		return Resume{}, err
	}
	return saved, nil
}

func (s *Store) AdvanceRevisionTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, expectedRevision int64) (Resume, error) {
	row, err := qtx.AdvanceResumeRevisionCAS(ctx, store.AdvanceResumeRevisionCASParams{
		ID: id, UserID: userID, ExpectedRevision: expectedRevision,
	})
	if err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return Resume{}, fmt.Errorf("resume: advance revision: %w", err)
		}
		return Resume{}, s.afterCASMiss(ctx, qtx, userID, id)
	}
	return s.projectRow(row)
}

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
	if err := checkProjectLinksTx(ctx, qtx, userID, doc); err != nil {
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
	if err = s.syncShowcaseTx(ctx, qtx, id); err != nil {
		return 0, fmt.Errorf("resume: save metadata and document: %w", err)
	}
	return newRevision, nil
}

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

func (s *Store) EnqueueMediaDeletionTx(ctx context.Context, qtx *store.Queries, resumeID uuid.UUID, key string) error {
	if _, err := qtx.EnqueueMediaDeletionJob(ctx, store.EnqueueMediaDeletionJobParams{
		ResumeID:  resumeID,
		ObjectKey: key,
	}); err != nil {
		return fmt.Errorf("resume: enqueue media deletion job: %w", err)
	}
	return nil
}
