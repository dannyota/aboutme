package auth

// Every handler assumes sessionChain has authenticated the request and checked
// CSRF where required. See docs/design/security.md and
// docs/adr/0015-accounts-passwords-and-sessions.md.

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/store"
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
	// ID is the session's UUID.
	ID string `json:"id"`
	// CreatedAt is when the session was issued, in UTC.
	CreatedAt time.Time `json:"createdAt"`
	// LastSeenAt is when the session was last used, in UTC.
	LastSeenAt time.Time `json:"lastSeenAt"`
	// UA is the User-Agent header recorded when the session was issued, or
	// nil when none was sent.
	UA *string `json:"ua"`
	// IP is the client address recorded when the session was issued, or nil
	// when none could be determined.
	IP *string `json:"ip"`
	// Current reports whether this is the session that authenticated the
	// request.
	Current bool `json:"current"`
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

// ipToString preserves null or returns the bare address.
func ipToString(ip *netip.Addr) *string {
	if ip == nil {
		return nil
	}
	s := ip.String()
	return &s
}
