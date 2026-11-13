package resume

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	resumeCapViolationCode    = "23514"
	resumeCapViolationMessage = "resumes_user_cap_exceeded"
)

const resumeCap = 3

type Store struct {
	pool     *store.Pool
	q        *store.Queries
	proj     *docmigrate.Projector
	now      func() time.Time
	showcase ShowcaseSync
}

func NewStore(pool *store.Pool, proj *docmigrate.Projector, options ...StoreOption) *Store {
	s := &Store{
		pool: pool,
		q:    store.New(pool),
		proj: proj,
		now:  time.Now,
	}
	for _, option := range options {
		option(s)
	}
	return s
}

func (s *Store) Create(ctx context.Context, userID uuid.UUID, title string, doc schema.Resume) (Resume, error) {
	if err := validateTitle(title); err != nil {
		return Resume{}, err
	}

	var created Resume
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		r, err := s.CreateTx(ctx, s.q.WithTx(tx), userID, title, nil, doc)
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

func (s *Store) Get(ctx context.Context, userID, id uuid.UUID) (Resume, error) {
	return s.GetTx(ctx, s.q, userID, id)
}

func (s *Store) GetWithProjectLinks(ctx context.Context, userID, id uuid.UUID) (Resume, json.RawMessage, error) {
	row, err := s.q.GetResumeForUserWithProjectLinks(ctx, store.GetResumeForUserWithProjectLinksParams{ID: id, UserID: userID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Resume{}, nil, ErrNotFound
		}
		return Resume{}, nil, fmt.Errorf("resume: get with project links: %w", err)
	}
	got, err := s.projectRow(row.Resume)
	if err != nil {
		return Resume{}, nil, err
	}
	return got, row.ProjectLinks, nil
}

func (s *Store) List(ctx context.Context, userID uuid.UUID) ([]Resume, error) {
	return s.ListTx(ctx, s.q, userID)
}

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

func (s *Store) SaveDocument(ctx context.Context, userID, id uuid.UUID, doc schema.Resume, expectedRevision int64) (newRevision int64, err error) {
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		saved, txErr := s.SaveDocumentTx(ctx, s.q.WithTx(tx), userID, id, doc, expectedRevision)
		if txErr != nil {
			return txErr
		}
		newRevision = saved.Revision
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newRevision, nil
}

func (s *Store) SaveTitle(ctx context.Context, userID, id uuid.UUID, title string, expectedRevision int64) (newRevision int64, err error) {
	if err = validateTitle(title); err != nil {
		return 0, err
	}

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		rev, txErr := s.saveTitleTx(ctx, s.q.WithTx(tx), userID, id, title, expectedRevision)
		if txErr != nil {
			return txErr
		}
		newRevision = rev
		return nil
	})
	if err != nil {
		return 0, err
	}
	return newRevision, nil
}

func (s *Store) saveTitleTx(ctx context.Context, qtx *store.Queries, userID, id uuid.UUID, title string, expectedRevision int64) (int64, error) {
	if err := validateTitle(title); err != nil {
		return 0, err
	}

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

func validateTitle(title string) error {
	if utf8.RuneCountInString(title) > MaxTitleCharacters {
		return ErrTitleTooLong
	}
	return nil
}

func validateLng(lng *string) error {
	if lng != nil && utf8.RuneCountInString(*lng) > MaxLngCharacters {
		return ErrLngTooLong
	}
	return nil
}

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
	if err = attachShowcase(ctx, qtx, &current); err != nil {
		return err
	}
	return &RevisionMismatchError{CurrentRevision: row.Revision, Current: current}
}

func (s *Store) projectRow(row store.Resume) (Resume, error) {
	pd, content, customization, err := s.proj.Project(row.PersonalDetails, row.Content, row.Customization, row.SchemaVersion)
	if err != nil {
		return Resume{}, fmt.Errorf("resume: project document: %w", err)
	}
	doc, err := DecodeParts(pd, content, customization, s.proj.CurrentVersion())
	if err != nil {
		return Resume{}, fmt.Errorf("resume: decode projected document: %w", err)
	}
	return toDomain(row, doc), nil
}

func toDomain(row store.Resume, doc schema.Resume) Resume {
	return Resume{
		ID:                  row.ID,
		UserID:              row.UserID,
		Title:               row.Title,
		Slug:                row.Slug,
		Live:                row.Live,
		DownloadEnabled:     row.DownloadEnabled,
		SEOGeoEnabled:       row.SEOGeoEnabled,
		PublicTitle:         row.PublicTitle,
		FaviconEmoji:        row.FaviconEmoji,
		SignInToView:        row.SignInToView,
		ViewPassEpoch:       row.ViewPassEpoch,
		StoredSchemaVersion: row.SchemaVersion,
		Revision:            row.Revision,
		Lng:                 row.Lng,
		Doc:                 doc,
		CreatedAt:           row.CreatedAt,
		UpdatedAt:           row.UpdatedAt,
	}
}

func isResumeCapExceeded(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == resumeCapViolationCode && pgErr.Message == resumeCapViolationMessage
}
