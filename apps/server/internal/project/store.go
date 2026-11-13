package project

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

var (
	ErrNotFound        = errors.New("project not found")
	ErrLimit           = errors.New("project limit reached")
	ErrSlugTaken       = errors.New("project slug taken")
	ErrSlugHeld        = errors.New("project slug held")
	ErrSlugRateLimited = errors.New("project slug claims rate limited")
)

type Project struct {
	ID       uuid.UUID `json:"id"`
	Slug     string    `json:"slug"`
	Revision int64     `json:"revision,string"`
	Document
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type RevisionMismatch struct {
	Current Project
}

func (e *RevisionMismatch) Error() string { return "project revision mismatch" }

type Store struct{}

func NewStore() *Store { return &Store{} }

func (s *Store) GetTx(ctx context.Context, q *store.Queries, owner, id uuid.UUID) (Project, error) {
	row, err := q.GetProjectForUser(ctx, store.GetProjectForUserParams{ID: id, UserID: owner})
	if errors.Is(err, pgx.ErrNoRows) {
		return Project{}, ErrNotFound
	}
	if err != nil {
		return Project{}, fmt.Errorf("project: read: %w", err)
	}
	return FromRow(row)
}

func (s *Store) ListTx(ctx context.Context, q *store.Queries, owner uuid.UUID) ([]Project, error) {
	rows, err := q.ListProjectsForUser(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("project: list: %w", err)
	}
	out := make([]Project, len(rows))
	for i, row := range rows {
		out[i], err = FromRow(row)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

type SlugAdmission func() bool

func (a SlugAdmission) admit() error {
	if a != nil && !a() {
		return ErrSlugRateLimited
	}
	return nil
}

func (s *Store) CreateTx(ctx context.Context, q *store.Queries, owner uuid.UUID, slug string, d Document, now time.Time, admit SlugAdmission) (Project, error) {
	raw, prepared, err := prepareStored(ctx, q, d)
	if err != nil {
		return Project{}, err
	}
	count, err := q.CountProjectsForUser(ctx, owner)
	if err != nil {
		return Project{}, fmt.Errorf("project: count: %w", err)
	}
	if count >= MaxProjects {
		return Project{}, ErrLimit
	}
	if err = admit.admit(); err != nil {
		return Project{}, err
	}
	if slug == "" {
		slug, err = s.proposeAvailable(ctx, q, owner, prepared.Text[prepared.DefaultLanguage].Name, now)
	} else {
		err = s.checkSlug(ctx, q, owner, uuid.Nil, slug, now)
	}
	if err != nil {
		return Project{}, err
	}
	row, err := q.CreateProject(ctx, store.CreateProjectParams{UserID: owner, Slug: slug, Document: raw, CreatedAt: now})
	if err != nil {
		return Project{}, fmt.Errorf("project: create: %w", err)
	}
	return FromRow(row)
}

func (s *Store) ReplaceTx(ctx context.Context, q *store.Queries, owner, id uuid.UUID, revision int64, slug string, d Document, now time.Time, admit SlugAdmission) (Project, error) {
	current, err := s.MatchTx(ctx, q, owner, id, revision)
	if err != nil {
		return Project{}, err
	}
	raw, _, err := prepareStored(ctx, q, d)
	if err != nil {
		return Project{}, err
	}
	if current.Slug != slug {
		if err = admit.admit(); err != nil {
			return Project{}, err
		}
	}
	if err = s.checkSlug(ctx, q, owner, id, slug, now); err != nil {
		return Project{}, err
	}
	if current.Slug != slug {
		if err = q.HoldProjectSlug(ctx, store.HoldProjectSlugParams{UserID: owner, Slug: current.Slug, ReleasedAt: now}); err != nil {
			return Project{}, fmt.Errorf("project: hold renamed slug: %w", err)
		}
	}
	row, err := q.ReplaceProjectCAS(ctx, store.ReplaceProjectCASParams{ID: id, UserID: owner, ExpectedRevision: revision, Slug: slug, Document: raw, UpdatedAt: now})
	if err != nil {
		return Project{}, fmt.Errorf("project: replace: %w", err)
	}
	return FromRow(row)
}

type Change int

const (
	Unchanged Change = iota
	DocumentChanged
	SlugChanged
)

func (s *Store) ChangeTx(ctx context.Context, q *store.Queries, owner, id uuid.UUID, revision int64, slug string, d Document) (Change, error) {
	current, err := s.MatchTx(ctx, q, owner, id, revision)
	if err != nil {
		return Unchanged, err
	}
	raw, _, err := prepareStored(ctx, q, d)
	if err != nil {
		return Unchanged, err
	}
	sameDocument, err := q.ProjectStoredDocumentEquals(ctx, store.ProjectStoredDocumentEqualsParams{Document: raw, ID: id, UserID: owner})
	if errors.Is(err, pgx.ErrNoRows) {
		return Unchanged, ErrNotFound
	}
	if err != nil {
		return Unchanged, fmt.Errorf("project: compare stored document: %w", err)
	}
	switch {
	case current.Slug != slug:
		return SlugChanged, nil
	case !sameDocument:
		return DocumentChanged, nil
	default:
		return Unchanged, nil
	}
}

func (s *Store) DeleteTx(ctx context.Context, q *store.Queries, owner, id uuid.UUID, revision int64, now time.Time) error {
	current, err := s.MatchTx(ctx, q, owner, id, revision)
	if err != nil {
		return err
	}
	if err = q.HoldProjectSlug(ctx, store.HoldProjectSlugParams{UserID: owner, Slug: current.Slug, ReleasedAt: now}); err != nil {
		return fmt.Errorf("project: hold deleted slug: %w", err)
	}
	n, err := q.DeleteProjectCAS(ctx, store.DeleteProjectCASParams{ID: id, UserID: owner, Revision: revision})
	if err != nil {
		return fmt.Errorf("project: delete: %w", err)
	}
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) MatchTx(ctx context.Context, q *store.Queries, owner, id uuid.UUID, revision int64) (Project, error) {
	current, err := s.GetTx(ctx, q, owner, id)
	if err != nil {
		return Project{}, err
	}
	if revision != current.Revision {
		return Project{}, &RevisionMismatch{Current: current}
	}
	return current, nil
}

func FromRow(row store.Project) (Project, error) {
	var d Document
	if err := json.Unmarshal(row.Document, &d); err != nil {
		return Project{}, errors.New("project: invalid stored document")
	}
	return Project{ID: row.ID, Slug: row.Slug, Revision: row.Revision, Document: d, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt}, nil
}

func prepareStored(ctx context.Context, q *store.Queries, d Document) (json.RawMessage, Document, error) {
	prepared, err := Prepare(d)
	if err != nil {
		return nil, Document{}, err
	}
	raw, err := json.Marshal(prepared)
	if err != nil {
		return nil, Document{}, ErrInvalid
	}
	n, err := q.ProjectDocumentBytes(ctx, raw)
	if err != nil {
		return nil, Document{}, fmt.Errorf("project: measure document: %w", err)
	}
	if n > MaxStoredBytes {
		return nil, Document{}, ErrInvalid
	}
	return raw, prepared, nil
}
