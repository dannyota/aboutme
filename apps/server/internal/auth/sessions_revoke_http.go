package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/store"
)

func (s *Service) handleRevokeAllSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		rejectSession(w)
		return
	}

	now := s.sessionMgr.now()
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, lockErr := lockAccountUser(ctx, qtx, sess.UserID)
		if lockErr != nil {
			return lockErr
		}
		if _, gateErr := lockCallerSession(ctx, qtx, user, sess.ID, now); gateErr != nil {
			return gateErr
		}
		if _, revokeErr := qtx.RevokeAllSessions(ctx, store.RevokeAllSessionsParams{UserID: user.ID, RevokedAt: &now}); revokeErr != nil {
			return fmt.Errorf("revoke all sessions: %w", revokeErr)
		}
		return nil
	})
	if err != nil {
		if !writeSensitiveGateError(w, err) {
			s.writeSessionAPIInternalError(w, r, "revoke_all_sessions", err)
		}
		return
	}

	ClearSessionCookie(w)
	w.Header().Set("Clear-Site-Data", clearSiteDataHeaderValue)
	writeNoContent(w)
}

const notFoundCode = "not_found"

var errSessionTargetNotFound = errors.New("auth: session target not found")

func (s *Service) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		rejectSession(w)
		return
	}

	now := s.sessionMgr.now()

	targetID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		api.WriteError(w, http.StatusNotFound, notFoundCode, "no such session")
		return
	}

	var revokedIDs []uuid.UUID
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, lockErr := lockAccountUser(ctx, qtx, sess.UserID)
		if lockErr != nil {
			return lockErr
		}
		if _, gateErr := lockCallerSession(ctx, qtx, user, sess.ID, now); gateErr != nil {
			return gateErr
		}
		n, revokeErr := qtx.RevokeSessionForUser(ctx, store.RevokeSessionForUserParams{
			ID:         targetID,
			UserID:     user.ID,
			RevokedAt:  &now,
			IdleCutoff: now.Add(-idleTimeout),
			Now:        now,
		})
		if revokeErr != nil {
			return fmt.Errorf("revoke session for user: %w", revokeErr)
		}
		if n == 0 {
			return errSessionTargetNotFound
		}
		target, readErr := qtx.GetSessionByID(ctx, targetID)
		if readErr != nil {
			return fmt.Errorf("get revoked session: %w", readErr)
		}
		var sweepErr error
		revokedIDs, sweepErr = revokeLineagePartnersTx(ctx, qtx, target, now)
		return sweepErr
	})
	switch {
	case errors.Is(err, errSessionTargetNotFound):
		api.WriteError(w, http.StatusNotFound, notFoundCode, "no such session")
		return
	case err != nil:
		if !writeSensitiveGateError(w, err) {
			s.writeSessionAPIInternalError(w, r, "revoke_session_for_user", err)
		}
		return
	}

	if targetID == sess.ID || slices.Contains(revokedIDs, sess.ID) {
		ClearSessionCookie(w)
		w.Header().Set("Clear-Site-Data", clearSiteDataHeaderValue)
	}
	writeNoContent(w)
}

func revokeLineagePartnersTx(ctx context.Context, qtx *store.Queries, row store.Session, now time.Time) ([]uuid.UUID, error) {
	var revokedIDs []uuid.UUID
	if row.RotatedFrom != nil {
		if err := qtx.RevokeSession(ctx, store.RevokeSessionParams{ID: *row.RotatedFrom, RevokedAt: &now}); err != nil {
			return nil, fmt.Errorf("revoke predecessor session: %w", err)
		}
		revokedIDs = append(revokedIDs, *row.RotatedFrom)
	}
	successor, err := qtx.FindLiveSuccessorSession(ctx, &row.ID)
	switch {
	case err == nil:
		if revokeErr := qtx.RevokeSession(ctx, store.RevokeSessionParams{ID: successor.ID, RevokedAt: &now}); revokeErr != nil {
			return nil, fmt.Errorf("revoke successor session: %w", revokeErr)
		}
		revokedIDs = append(revokedIDs, successor.ID)
	case errors.Is(err, pgx.ErrNoRows):
	default:
		return nil, fmt.Errorf("find successor session: %w", err)
	}
	return revokedIDs, nil
}

func lockAccountUser(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (store.User, error) {
	user, err := qtx.GetUserForUpdate(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.User{}, ErrSessionInvalid
	}
	if err != nil {
		return store.User{}, fmt.Errorf("lock user: %w", err)
	}
	return user, nil
}

func lockCallerSession(ctx context.Context, qtx *store.Queries, user store.User, sessionID uuid.UUID, now time.Time) (store.Session, error) {
	sess, err := qtx.GetSessionByIDForUpdate(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.Session{}, ErrSessionInvalid
	}
	if err != nil {
		return store.Session{}, fmt.Errorf("lock caller session: %w", err)
	}
	if sess.UserID != user.ID || sess.AuthEpoch != user.AuthEpoch || RequireLiveSession(sess, now) != nil {
		return store.Session{}, ErrSessionInvalid
	}
	return sess, nil
}

func lockSecondFactorPolicy(ctx context.Context, qtx *store.Queries, userID uuid.UUID) (*store.SecondFactorPolicy, error) {
	policy, err := qtx.GetSecondFactorPolicyForUpdate(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("lock second factor policy: %w", err)
	}
	return &policy, nil
}

func lockSensitiveSession(ctx context.Context, qtx *store.Queries, user store.User, sessionID uuid.UUID, now time.Time) (store.Session, error) {
	sess, err := lockCallerSession(ctx, qtx, user, sessionID, now)
	if err != nil {
		return store.Session{}, err
	}
	policy, err := lockSecondFactorPolicy(ctx, qtx, user.ID)
	if err != nil {
		return store.Session{}, err
	}
	if err = RequireRecentSecondFactorReauth(user, policy, sess, now); err != nil {
		return store.Session{}, err
	}
	return sess, nil
}

func (s *Service) recheckSensitiveSession(ctx context.Context, sess store.Session) error {
	now := s.sessionMgr.now()
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, err := lockAccountUser(ctx, qtx, sess.UserID)
		if err != nil {
			return err
		}
		_, err = lockSensitiveSession(ctx, qtx, user, sess.ID, now)
		return err
	})
}

func writeSensitiveGateError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, ErrSessionInvalid):
		rejectSession(w)
		return true
	case errors.Is(err, ErrReauthRequired):
		api.WriteError(w, http.StatusForbidden, reauthRequiredCode, "recent reauthentication is required")
		return true
	default:
		return false
	}
}
