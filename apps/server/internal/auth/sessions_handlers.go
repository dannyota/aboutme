package auth

// Every handler assumes sessionChain has authenticated the request and checked
// CSRF where required. Targeted and global revocation recheck the caller's
// session, epoch, and recent proofs under the user lock before any write.
// Targeted revocation also revokes exact rotation partners. See
// docs/design/security.md, docs/design/second-factor-authentication.md, and
// docs/adr/0015-session-rotation-delivery.md.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// clearSiteDataHeaderValue clears cookies and storage when the current session
// is revoked, directly or through rotation lineage.
const clearSiteDataHeaderValue = `"cookies", "storage"`

// writeNoContent writes the final 204. Callers must set all headers first.
func writeNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// handleLogout revokes the current session before clearing client state. It
// queues the clear headers before the fallible lineage sweep so they also reach
// the client if that sweep returns an error.
func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		rejectSession(w)
		return
	}

	// RequireSession proves that sess.ID belongs to this request.
	if err := s.sessionMgr.Revoke(ctx, sess.ID); err != nil {
		s.writeSessionAPIInternalError(w, r, "revoke_session", err)
		return
	}

	ClearSessionCookie(w)
	w.Header().Set("Clear-Site-Data", clearSiteDataHeaderValue)

	// Logout revokes the live rotation partner as well as the current row.
	if _, ok := s.revokeLineagePartners(ctx, sess, w, r); !ok {
		return
	}

	writeNoContent(w)
}

// revokeLineagePartners revokes the row's exact predecessor and live successor.
// The predecessor ID is stored on row; a unique rotated_from lookup finds at
// most one successor. Returned IDs exclude row itself. On failure this function
// writes the response and callers must stop.
func (s *Service) revokeLineagePartners(ctx context.Context, row store.Session, w http.ResponseWriter, r *http.Request) (revokedIDs []uuid.UUID, ok bool) {
	if row.RotatedFrom != nil {
		if err := s.sessionMgr.Revoke(ctx, *row.RotatedFrom); err != nil {
			s.writeSessionAPIInternalError(w, r, "revoke_predecessor_session", err)
			return nil, false
		}
		revokedIDs = append(revokedIDs, *row.RotatedFrom)
	}

	succRow, err := s.q.FindLiveSuccessorSession(ctx, &row.ID)
	switch {
	case err == nil:
		if revokeErr := s.sessionMgr.Revoke(ctx, succRow.ID); revokeErr != nil {
			s.writeSessionAPIInternalError(w, r, "revoke_successor_session", revokeErr)
			return nil, false
		}
		revokedIDs = append(revokedIDs, succRow.ID)
	case errors.Is(err, pgx.ErrNoRows):
		// No live successor remains.
	default:
		s.writeSessionAPIInternalError(w, r, "find_successor_session", err)
		return nil, false
	}

	return revokedIDs, true
}

// sessionDeviceEntry preserves null user-agent/IP values. Times are normalized
// to UTC before assignment.
type sessionDeviceEntry struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	UA         *string   `json:"ua"`
	IP         *string   `json:"ip"`
	Current    bool      `json:"current"`
}

// handleSessionsCollection rejects unsupported methods before authentication.
func (s *Service) handleSessionsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.sessionChain(s.handleSessionsList)(w, r)
	case http.MethodDelete:
		s.sessionChain(s.handleRevokeAllSessions)(w, r)
	default:
		w.Header().Set("Allow", "GET, DELETE")
		api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed",
			"method not allowed on "+r.URL.Path+"; use GET or DELETE")
	}
}

// handleSessionsList returns only the caller's live sessions. Revoked, expired,
// and grace-dead predecessors are excluded.
func (s *Service) handleSessionsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		rejectSession(w)
		return
	}

	now := s.sessionMgr.now()
	rows, err := s.q.ListLiveSessionsForUser(ctx, store.ListLiveSessionsForUserParams{
		UserID:     sess.UserID,
		IdleCutoff: now.Add(-idleTimeout),
		Now:        now,
	})
	if err != nil {
		s.writeSessionAPIInternalError(w, r, "list_sessions", err)
		return
	}

	entries := make([]sessionDeviceEntry, len(rows))
	for i, row := range rows {
		entries[i] = sessionDeviceEntry{
			ID:         row.ID.String(),
			CreatedAt:  row.CreatedAt.UTC(),
			LastSeenAt: row.LastSeenAt.UTC(),
			UA:         row.UA,
			IP:         ipToString(row.IP),
			Current:    row.ID == sess.ID,
		}
	}

	api.WriteData(w, http.StatusOK, entries)
}

// handleRevokeAllSessions implements DELETE /api/v1/sessions:
// logout-everywhere. One transaction takes the user lock, locks and rechecks
// the caller's session and factor proofs, then revokes every session. Session
// issuers, rotation successors, and epoch-change replacements take the same
// user lock, so none can commit a session that this revocation misses. See
// docs/design/second-factor-authentication.md.
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
		if _, gateErr := lockSensitiveSession(ctx, qtx, user, sess.ID, now); gateErr != nil {
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

// notFoundCode collapses malformed, absent, and foreign session IDs.
const notFoundCode = "not_found"

// errSessionTargetNotFound collapses absent, foreign, and dead revoke targets.
var errSessionTargetNotFound = errors.New("auth: session target not found")

// handleRevokeSession requires ownership and recent reauth before revoking the
// target and its rotation partners. One transaction takes the user lock, locks
// and rechecks the caller's session and factor proofs, revokes the target, and
// sweeps its exact rotation partners, so a concurrent rotation successor or
// epoch-change replacement cannot survive. It clears client state whenever the
// current session dies.
func (s *Service) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		rejectSession(w)
		return
	}

	now := s.sessionMgr.now()
	// This precheck avoids a transaction for a stale primary proof. The
	// locked recheck below is authoritative.
	if err := RequireRecentReauth(sess, now); err != nil {
		api.WriteError(w, http.StatusForbidden, reauthRequiredCode, "recent reauthentication is required")
		return
	}

	targetID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		// Malformed IDs use the same no-oracle 404 as absent or foreign IDs.
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
		if _, gateErr := lockSensitiveSession(ctx, qtx, user, sess.ID, now); gateErr != nil {
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
		// RevokeSessionForUser returns only a count, so read the lineage.
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

// revokeLineagePartnersTx revokes row's exact predecessor and live successor in
// the caller's transaction and returns their IDs.
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
		// No live successor remains.
	default:
		return nil, fmt.Errorf("find successor session: %w", err)
	}
	return revokedIDs, nil
}

// lockAccountUser takes the user-row lock that every session issuer and
// sensitive mutation serializes on. A missing account reads as an invalid
// session.
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

// lockCallerSession locks the caller's concrete session after the user lock
// and requires it to be live, owned by user, and at the current epoch.
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

// lockSecondFactorPolicy locks the account's factor policy row. It returns nil
// for an unenrolled account.
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

// lockSensitiveSession runs after the caller locked user. It locks the
// caller's session and then the factor policy, in the lock order of
// docs/design/second-factor-authentication.md, and requires a live
// current-epoch session with a recent primary proof and, for an enrolled
// account, a recent factor proof. It returns ErrSessionInvalid or
// ErrReauthRequired on a failed gate.
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

// recheckSensitiveSession applies lockSensitiveSession in its own transaction
// for a route that checks authority before it starts an external step.
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

// writeSensitiveGateError writes the session API response for a failed
// sensitive-mutation gate and reports whether err was one.
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

// ipToString preserves null or returns the bare address.
func ipToString(ip *netip.Addr) *string {
	if ip == nil {
		return nil
	}
	s := ip.String()
	return &s
}
