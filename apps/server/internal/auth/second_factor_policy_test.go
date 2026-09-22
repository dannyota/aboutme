package auth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestRequireRecentReauth_RejectsMissingPrimaryProof(t *testing.T) {
	now := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	sess := store.Session{ReauthenticatedAt: now.Add(-15*time.Minute - time.Nanosecond)}
	if err := auth.RequireRecentReauth(sess, now); !errors.Is(err, auth.ErrReauthRequired) {
		t.Fatalf("RequireRecentReauth() error = %v, want ErrReauthRequired", err)
	}
}

func TestRequireRecentSecondFactorReauth_EnrolledAccountNeedsFactorProof(t *testing.T) {
	now := time.Now().UTC()
	user := store.User{ID: uuid.New(), AuthEpoch: 3}
	policy := store.SecondFactorPolicy{UserID: user.ID}
	sess := store.Session{UserID: user.ID, AuthEpoch: user.AuthEpoch, LastSeenAt: now, AbsoluteExpiresAt: now.Add(time.Hour), ReauthenticatedAt: now}
	if err := auth.RequireRecentSecondFactorReauth(user, &policy, sess, now); !errors.Is(err, auth.ErrReauthRequired) {
		t.Fatalf("RequireRecentSecondFactorReauth(missing factor proof) error = %v, want ErrReauthRequired", err)
	}
	sess.SecondFactorVerifiedAt = &now
	if err := auth.RequireRecentSecondFactorReauth(user, &policy, sess, now); err != nil {
		t.Fatalf("RequireRecentSecondFactorReauth(fresh factor proof) error = %v", err)
	}
}

func TestRequireRecentSecondFactorReauth_EnrolledAndUnenrolledBoundaries(t *testing.T) {
	now := time.Now().UTC()
	user := store.User{ID: uuid.New(), AuthEpoch: 7}
	factorAt := now.Add(-15 * time.Minute)
	sess := store.Session{UserID: user.ID, AuthEpoch: user.AuthEpoch, LastSeenAt: now, AbsoluteExpiresAt: now.Add(time.Hour), ReauthenticatedAt: factorAt, SecondFactorVerifiedAt: &factorAt}
	if err := auth.RequireRecentSecondFactorReauth(user, nil, sess, now); err != nil {
		t.Fatalf("RequireRecentSecondFactorReauth(unenrolled) error = %v", err)
	}
	policy := store.SecondFactorPolicy{UserID: user.ID}
	if err := auth.RequireRecentSecondFactorReauth(user, &policy, sess, now); err != nil {
		t.Fatalf("RequireRecentSecondFactorReauth(exact boundary) error = %v", err)
	}
	sess.AuthEpoch--
	if err := auth.RequireRecentSecondFactorReauth(user, &policy, sess, now); !errors.Is(err, auth.ErrSessionInvalid) {
		t.Fatalf("RequireRecentSecondFactorReauth(stale epoch) error = %v, want ErrSessionInvalid", err)
	}
}
