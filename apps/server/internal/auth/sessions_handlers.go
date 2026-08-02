package auth

// sessions_handlers.go implements Task 9's remaining session-management
// HTTP surface (design spec §3, AC-AUTH-005): POST /api/v1/auth/logout,
// GET+DELETE /api/v1/sessions (device list, logout-everywhere), and
// DELETE /api/v1/sessions/{id} (per-session revoke). Every handler here
// assumes RequireSession has already authenticated the request and put
// its session in context, and -- for every mutating one -- that
// RequireCSRF has already validated the request's CSRF token; see
// handlers.go's RegisterRoutes/sessionChain for the exact chain each is
// wired behind.
//
// DD-C11 (spec-corrected, void-ing the earlier plan's invented
// POST /sessions/revoke-all): DELETE /sessions/{id} and DELETE /sessions
// both require a RECENT reauthentication (RequireRecentReauth,
// session.go/task-7-brief.md), checked as the very first thing each
// handler does -- before RevokeForUser/RevokeAll ever touch a row. A stale
// reauth must never revoke anything and then discover it should have
// refused.

import (
	"net/http"
	"net/netip"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// clearSiteDataHeaderValue is the exact Clear-Site-Data value task-9-brief.md
// pins for logout and logout-everywhere (the W3C Clear-Site-Data header,
// https://www.w3.org/TR/clear-site-data/): quoted "cookies" (the session
// cookie itself, belt-and-suspenders alongside the explicit
// ClearSessionCookie call) and "storage" (localStorage/IndexedDB/etc a
// client-side app may hold). Deliberately not "cache" or
// "executionContexts" -- the contract doesn't call for either.
const clearSiteDataHeaderValue = `"cookies", "storage"`

// writeNoContent finishes a mutating endpoint's successful response with
// 204 No Content and no body (coordinator addendum DD-C13, 2026-08-02):
// the {"data":...} envelope applies to bodied responses only, and
// logout/revoke/revoke-all have nothing left to describe once the target
// session is gone. Callers MUST set every response header
// (Clear-Site-Data, Set-Cookie) BEFORE calling this -- WriteHeader must be
// the last thing written to a response; any header set after it is
// silently dropped.
func writeNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// handleLogout implements POST /api/v1/auth/logout: revokes the caller's
// own current session, clears its cookie, and sets Clear-Site-Data so the
// browser drops cached credential-derived state too.
func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		rejectSession(w)
		return
	}

	// sess is the caller's OWN session -- RequireSession authenticated it
	// from this exact request's own __Host-session cookie -- so Revoke's
	// ownership precondition ("caller already knows sessionID is the
	// caller's own") is satisfied without a separate ownership check; see
	// Revoke's own doc comment (session.go) for why RevokeForUser exists
	// for the OTHER call site (DELETE /sessions/{id}, an id merely named
	// by the caller) instead.
	if err := s.sessionMgr.Revoke(ctx, sess.ID); err != nil {
		s.writeSessionAPIInternalError(w, r, "revoke_session", err)
		return
	}

	ClearSessionCookie(w)
	w.Header().Set("Clear-Site-Data", clearSiteDataHeaderValue)
	writeNoContent(w)
}

// sessionDeviceEntry is one row of GET /sessions' device list
// (task-9-brief.md's pinned shape): id, createdAt, lastSeenAt, ua, ip,
// current. ua/ip are nullable (a session issued with an empty user
// agent/IP stores SQL NULL -- session.go's stringParam/parseSessionIP),
// so both serialize as JSON null rather than an empty string when absent.
// CreatedAt/LastSeenAt are normalized to UTC before assignment (see
// handleSessionsList) so they always serialize as RFC 3339 UTC ("Z"
// suffix, per DD-C13) regardless of the time.Time location pgx happens to
// return a timestamptz column in.
type sessionDeviceEntry struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	UA         *string   `json:"ua"`
	IP         *string   `json:"ip"`
	Current    bool      `json:"current"`
}

// handleSessionsCollection dispatches GET (device list) and DELETE
// (logout-everywhere) on /api/v1/sessions -- the two methods share one
// path, so RegisterRoutes wires this single handler instead of
// handlers.go's own single-method route() helper. The method check runs
// BEFORE either branch's sessionChain, so an unsupported method never
// reaches RequireSession and always cleanly 405s regardless of the
// caller's auth state -- matching route()'s own method-before-side-effect
// ordering.
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

// handleSessionsList implements GET /api/v1/sessions: every LIVE session
// belonging to the caller. Explicitly revoked rows are never returned --
// and neither are Task 7's grace-dead rotation predecessors (rows with
// rotation_grace_until in the past but revoked_at still NULL: see
// ListLiveSessionsForUser's own doc comment, sql/queries.sql) -- a
// superseded predecessor must never masquerade as a live device the
// caller could still revoke.
func (s *Service) handleSessionsList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		rejectSession(w)
		return
	}

	rows, err := s.q.ListLiveSessionsForUser(ctx, store.ListLiveSessionsForUserParams{
		UserID: sess.UserID,
		Now:    s.sessionMgr.now(),
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
// logout-everywhere. Requires a recent reauthentication (DD-C11), checked
// BEFORE RevokeAll touches a single row -- see this file's own top-of-file
// doc comment.
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

// notFoundCode/notFoundMessage back DELETE /sessions/{id}'s uniform 404
// (DD-C5): an id that doesn't parse as a UUID, one that parses but names
// no row at all, and one that names another user's session are all
// indistinguishable from the response -- never a distinct 400/403 that
// would confirm which case applies (the same no-oracle reasoning as
// ErrSessionInvalid, csrfRejectedCode, and RevokeForUser's own doc
// comment).
const notFoundCode = "not_found"

// handleRevokeSession implements DELETE /api/v1/sessions/{id}: revokes
// one session that must belong to the caller. Requires a recent
// reauthentication (DD-C11), checked BEFORE RevokeForUser touches a
// single row -- see this file's own top-of-file doc comment. Revoking the
// caller's OWN current session also clears its cookie in this same
// response.
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
		// A malformed id can never resolve to a real row -- the same
		// uniform 404 as one that parses but doesn't exist, or belongs to
		// someone else (see notFoundCode above): never a distinct 400 that
		// would leak which case this is.
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

	if targetID == sess.ID {
		ClearSessionCookie(w)
	}
	writeNoContent(w)
}

// ipToString formats ip -- sessions.ip, nullable -- as a *string for JSON:
// nil in, nil out (serializes as null); otherwise ip's own bare-address
// String() form, matching how it was originally recorded (session.go's
// parseSessionIP; api.ClientIP's own bare-address contract).
func ipToString(ip *netip.Addr) *string {
	if ip == nil {
		return nil
	}
	s := ip.String()
	return &s
}
