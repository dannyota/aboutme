package auth

// Every handler assumes sessionChain has authenticated the request and checked
// CSRF where required. Targeted and global revocation require recent reauth
// before any write. Targeted revocation also revokes exact rotation partners.
// See docs/design/security.md and docs/adr/0015-session-rotation-delivery.md.

import (
	"context"
	"errors"
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
// logout-everywhere. It checks recent reauthentication before RevokeAll touches
// a row.
func (s *Service) handleRevokeAllSessions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		rejectSession(w)
		return
	}

	if err := RequireRecentReauth(sess, s.sessionMgr.now()); err != nil {
		api.WriteError(w, http.StatusForbidden, reauthRequiredCode, "recent reauthentication is required")
		return
	}

	if _, err := s.sessionMgr.RevokeAll(ctx, sess.UserID); err != nil {
		s.writeSessionAPIInternalError(w, r, "revoke_all_sessions", err)
		return
	}

	ClearSessionCookie(w)
	w.Header().Set("Clear-Site-Data", clearSiteDataHeaderValue)
	writeNoContent(w)
}

// notFoundCode collapses malformed, absent, and foreign session IDs.
const notFoundCode = "not_found"

// handleRevokeSession requires ownership and recent reauth before revoking the
// target and its rotation partners. It clears client state whenever the current
// session dies. Direct-target clear headers precede the fallible lineage sweep;
// partner death is known only after that sweep.
func (s *Service) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		rejectSession(w)
		return
	}

	if err := RequireRecentReauth(sess, s.sessionMgr.now()); err != nil {
		api.WriteError(w, http.StatusForbidden, reauthRequiredCode, "recent reauthentication is required")
		return
	}

	targetID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		// Malformed IDs use the same no-oracle 404 as absent or foreign IDs.
		api.WriteError(w, http.StatusNotFound, notFoundCode, "no such session")
		return
	}

	n, err := s.sessionMgr.RevokeForUser(ctx, targetID, sess.UserID)
	if err != nil {
		s.writeSessionAPIInternalError(w, r, "revoke_session_for_user", err)
		return
	}
	if n == 0 {
		api.WriteError(w, http.StatusNotFound, notFoundCode, "no such session")
		return
	}

	// Queue client-state clearing before the fallible partner sweep.
	directTarget := targetID == sess.ID
	if directTarget {
		ClearSessionCookie(w)
		w.Header().Set("Clear-Site-Data", clearSiteDataHeaderValue)
	}

	// RevokeForUser returns only a count, so fetch a non-current target.
	lineageRow := sess
	if !directTarget {
		lineageRow, err = s.q.GetSessionByID(ctx, targetID)
		if err != nil {
			s.writeSessionAPIInternalError(w, r, "get_revoked_session", err)
			return
		}
	}

	revokedIDs, ok := s.revokeLineagePartners(ctx, lineageRow, w, r)
	if !ok {
		return
	}

	// Partner death is known only after the sweep.
	if !directTarget && slices.Contains(revokedIDs, sess.ID) {
		ClearSessionCookie(w)
		w.Header().Set("Clear-Site-Data", clearSiteDataHeaderValue)
	}
	writeNoContent(w)
}

// ipToString preserves null or returns the bare address.
func ipToString(ip *netip.Addr) *string {
	if ip == nil {
		return nil
	}
	s := ip.String()
	return &s
}
