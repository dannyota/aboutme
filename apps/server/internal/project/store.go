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
	// ErrNotFound covers both absent and foreign project IDs.
	ErrNotFound = errors.New("project not found")
	// ErrLimit reports a full account library.
	ErrLimit = errors.New("project limit reached")
	// ErrSlugTaken reports an existing account-local claim.
	ErrSlugTaken = errors.New("project slug taken")
	// ErrSlugHeld reports a released slug still held by this account.
	ErrSlugHeld = errors.New("project slug held")
	// ErrSlugRateLimited reports a slug claim refused by the shared budget.
	ErrSlugRateLimited = errors.New("project slug claims rate limited")
)

// Project is an owner project with its stored content and revision.
type Project struct {
	// ID is the server-generated UUIDv7 identity.
	ID uuid.UUID `json:"id"`
	// Slug is unique within the owner's library.
	Slug string `json:"slug"`
	// Revision is a positive compare-and-swap counter, encoded as decimal text.
	Revision int64 `json:"revision,string"`
	// Document contains every replaceable field.
	Document
	// CreatedAt is the creation time in UTC.
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt is the last write time in UTC.
	UpdatedAt time.Time `json:"updatedAt"`
}

// RevisionMismatch carries the current owned project for a failed precondition.
type RevisionMismatch struct {
	// Current is the complete current project.
	Current Project
}

// Error returns a closed mismatch description.
func (e *RevisionMismatch) Error() string { return "project revision mismatch" }

// Store validates complete library writes. Callers hold the owner row lock
// and use the same transaction for mutation and replay storage.
type Store struct{}

// NewStore constructs the project aggregate boundary.
func NewStore() *Store { return &Store{} }

// GetTx reads only the owner's project, treating foreign IDs as absent.
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

// ListTx lists one library in creation order.
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

// SlugAdmission spends one slug claim from a shared budget and reports
// whether the claim may proceed. Nil admits every claim.
type SlugAdmission func() bool

func (a SlugAdmission) admit() error {
	if a != nil && !a() {
		return ErrSlugRateLimited
	}
	return nil
}

// CreateTx sanitizes and validates content, enforces the cap and claims a
// slug. The claim is admitted only after the complete document is valid.
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

// ReplaceTx replaces every content field and advances the matched revision.
// A slug change is admitted only after the complete document is valid.
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

// Change is how a complete replacement changes a project's stored forms.
type Change int

const (
	// Unchanged stores the same slug and the same document.
	Unchanged Change = iota
	// DocumentChanged keeps the slug and stores a different document.
	DocumentChanged
	// SlugChanged stores a different slug, with or without a document change.
	SlugChanged
)

// ChangeTx reports how ReplaceTx with the same arguments would change the
// stored project. It matches the revision, prepares d as ReplaceTx does, and
// compares the stored slug and the stored document with what the
// replacement would store, so a request that differs only in formatting or
// in what sanitizing removes is Unchanged.
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

// DeleteTx removes a matched owned row and holds its released slug.
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

// MatchTx checks ownership before evaluating a revision precondition.
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

// FromRow decodes a stored project without exposing its owner identity.
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
