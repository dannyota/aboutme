package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// errRotationPredecessorRevoked marks a successor insert aborted because the
// predecessor was revoked after its admission update committed, the reset
// fence under the user-row lock. It is a closed, non-error outcome for the
// rotation loser.
var errRotationPredecessorRevoked = errors.New("auth: rotation predecessor revoked")

// tryRotate admits at most one rotation winner. The admission update and
// successor insert are separate statements: a lost insert leaves the
// predecessor usable only to its parked deadline, while a lost response leaves
// an unreachable successor. See docs/adr/0015-accounts-passwords-and-sessions.md.
//
// A pool-backed manager inserts the successor in a short transaction that locks
// the user row and re-reads the predecessor as live, so a reset that already
// revoked the predecessor (RevokeAllSessions under the same user lock) cannot
// mint a successor past that fence. An epoch that advanced after admission
// returns ErrSessionInvalid: the predecessor neither rotates nor authenticates
// the request. A query-only manager (nil pool) inserts the successor directly.
func (m *SessionManager) tryRotate(ctx context.Context, predecessor store.Session, now time.Time) (successor store.Session, raw string, won bool, err error) {
	graceUntil := now.Add(rotationAge)
	if graceUntil.After(predecessor.AbsoluteExpiresAt) {
		graceUntil = predecessor.AbsoluteExpiresAt
	}
	_, err = m.q.BeginSessionRotation(ctx, store.BeginSessionRotationParams{
		ID:                 predecessor.ID,
		RotationGraceUntil: &graceUntil,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.Session{}, "", false, nil
		}
		return store.Session{}, "", false, fmt.Errorf("auth: authenticate: begin rotation: %w", err)
	}

	if m.pool == nil {
		successor, raw, err = m.createRotationSuccessor(ctx, m.q, predecessor, now)
		if err != nil {
			return store.Session{}, "", false, err
		}
		return successor, raw, true, nil
	}

	if m.rotationProbe != nil {
		m.rotationProbe()
	}

	err = pgx.BeginFunc(ctx, m.pool, func(tx pgx.Tx) error {
		qtx := m.q.WithTx(tx)
		user, lockErr := qtx.GetUserForUpdate(ctx, predecessor.UserID)
		if lockErr != nil {
			return fmt.Errorf("auth: rotate session: lock user: %w", lockErr)
		}
		live, readErr := qtx.GetSessionByIDForUpdate(ctx, predecessor.ID)
		if readErr != nil {
			return fmt.Errorf("auth: rotate session: re-read predecessor: %w", readErr)
		}
		if live.AuthEpoch != user.AuthEpoch {
			return ErrSessionInvalid
		}
		if live.RevokedAt != nil {
			return errRotationPredecessorRevoked
		}
		var createErr error
		successor, raw, createErr = m.createRotationSuccessor(ctx, qtx, live, now)
		return createErr
	})
	if err != nil {
		if errors.Is(err, errRotationPredecessorRevoked) {
			return store.Session{}, "", false, nil
		}
		return store.Session{}, "", false, err
	}
	return successor, raw, true, nil
}

// createRotationSuccessor mints the successor row, inheriting identity,
// absolute expiry, reauth time, user agent, and IP from the predecessor while
// setting the database-enforced lineage link. Rotation extends none of them.
func (m *SessionManager) createRotationSuccessor(ctx context.Context, qtx *store.Queries, predecessor store.Session, now time.Time) (successor store.Session, raw string, err error) {
	raw, err = randomSessionToken()
	if err != nil {
		return store.Session{}, "", fmt.Errorf("auth: rotate session: %w", err)
	}
	csrfSecret, err := randomCSRFSecret()
	if err != nil {
		return store.Session{}, "", fmt.Errorf("auth: rotate session: %w", err)
	}

	successor, err = qtx.CreateSession(ctx, store.CreateSessionParams{
		UserID:            predecessor.UserID,
		TokenHash:         hashSessionToken(raw),
		CSRFSecret:        csrfSecret,
		CreatedAt:         now,
		LastSeenAt:        now,
		ReauthenticatedAt: predecessor.ReauthenticatedAt,
		AbsoluteExpiresAt: predecessor.AbsoluteExpiresAt,
		UA:                predecessor.UA,
		IP:                predecessor.IP,
		// RotatedFrom is the database-enforced lineage link used when revoking
		// either half of a rotation pair.
		RotatedFrom:            &predecessor.ID,
		AuthEpoch:              predecessor.AuthEpoch,
		SecondFactorVerifiedAt: predecessor.SecondFactorVerifiedAt,
	})
	if err != nil {
		return store.Session{}, "", fmt.Errorf("auth: rotate session: create successor: %w", err)
	}
	return successor, raw, nil
}

// startPredecessorGrace shortens the parked deadline on a successor's first
// use. A successor with last_seen_at equal to created_at is unused; the forced
// last-seen write changes that marker despite the normal activity throttle.
// Both updates are safe to repeat and failures reject authentication.
func (m *SessionManager) startPredecessorGrace(ctx context.Context, sess *store.Session, now time.Time) error {
	if sess.RotatedFrom == nil || !sess.LastSeenAt.Equal(sess.CreatedAt) {
		return nil
	}

	if err := m.q.StartSessionRotationGrace(ctx, store.StartSessionRotationGraceParams{
		ID:         *sess.RotatedFrom,
		GraceUntil: now.Add(rotationGrace),
	}); err != nil {
		return fmt.Errorf("auth: authenticate: start predecessor rotation grace: %w", err)
	}

	if now.After(sess.LastSeenAt) {
		if err := m.q.TouchLastSeenAt(ctx, store.TouchLastSeenAtParams{ID: sess.ID, LastSeenAt: now}); err != nil {
			return fmt.Errorf("auth: authenticate: mark successor first use: %w", err)
		}
		sess.LastSeenAt = now
	}
	return nil
}
