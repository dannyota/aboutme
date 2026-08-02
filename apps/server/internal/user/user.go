// Package user provides the user-domain operations (create, look up by ID
// or email) that internal/auth and later account-management endpoints
// build on, wrapping the sqlc-generated internal/store.Queries with typed
// errors so callers never need to know pgx's driver-level error shapes.
package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// ErrNotFound is returned by GetByID and GetByEmail when no user row
// matches.
var ErrNotFound = errors.New("user: not found")

// Store provides user-domain operations over a store.DBTX (a pool,
// connection, or transaction).
type Store struct {
	q *store.Queries
}

// New wraps db in a Store.
func New(db store.DBTX) *Store {
	return &Store{q: store.New(db)}
}

// Create inserts a new user row with the given email, name, and optional
// avatarKey, and returns the created row. The caller must handle a
// unique-violation error when email already exists (constraint
// users_email_key); this method does not translate that into a typed
// error because callers need the underlying constraint name to
// distinguish it from other failures.
func (s *Store) Create(ctx context.Context, email, name string, avatarKey *string) (store.User, error) {
	u, err := s.q.CreateUser(ctx, store.CreateUserParams{
		Email:     email,
		Name:      name,
		AvatarKey: avatarKey,
	})
	if err != nil {
		return store.User{}, fmt.Errorf("user: create: %w", err)
	}
	return u, nil
}

// GetByID returns the user with id, or ErrNotFound if none exists.
func (s *Store) GetByID(ctx context.Context, id uuid.UUID) (store.User, error) {
	u, err := s.q.GetUserByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.User{}, ErrNotFound
		}
		return store.User{}, fmt.Errorf("user: get by id: %w", err)
	}
	return u, nil
}

// GetByEmail returns the user with email, or ErrNotFound if none exists.
// The comparison is case-insensitive because users.email is citext.
func (s *Store) GetByEmail(ctx context.Context, email string) (store.User, error) {
	u, err := s.q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.User{}, ErrNotFound
		}
		return store.User{}, fmt.Errorf("user: get by email: %w", err)
	}
	return u, nil
}
