package user

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/store"
)

var ErrNotFound = errors.New("user: not found")

type Store struct {
	q *store.Queries
}

func New(db store.DBTX) *Store {
	return &Store{q: store.New(db)}
}

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
