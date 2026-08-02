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
//
// DD-C14 (fix round 1, finding I2), DD-C14b (fix round 2), DD-C14c (fix
// round 3, owner rulings, all extending each other): revoking ANY
// session -- the caller's own current one via logout, or ANY id named via
// DELETE /sessions/{id} -- also revokes its rotation LINEAGE partner(s):
// the row it was rotated FROM (if it's a successor) and the row that was
// rotated FROM it (if it's a predecessor with a still-live successor).
// See revokeLineagePartners' own doc comment for the full mechanism.
// DD-C14c replaced fix round 1/2's timestamp-reconstruction lookups with
// an exact, database-enforced link (sessions.rotated_from,
// sql/schema.sql) after fix round 2's own query was found to silently
// match ANY same-user session sharing a microsecond -- see
// task-9-report.md's fix round 3 section. DELETE /sessions
// (handleRevokeAllSessions) needs no lineage-sweep equivalent: it already
// sweeps every one of the caller's sessions, every lineage member
// included.

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

// clearSiteDataHeaderValue is the exact Clear-Site-Data value task-9-brief.md
// pins for logout and logout-everywhere (the W3C Clear-Site-Data header,
// https://www.w3.org/TR/clear-site-data/): quoted "cookies" (the session
// cookie itself, belt-and-suspenders alongside the explicit
// ClearSessionCookie call) and "storage" (localStorage/IndexedDB/etc a
// client-side app may hold). Deliberately not "cache" or
// "executionContexts" -- the contract doesn't call for either. Also set
// by DELETE /sessions/{id} whenever the caller's own current session
// dies as part of that request (fix round 3, DD-C14c item 6), directly
// targeted or via its lineage partner.
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
// own current session (and its full rotation lineage -- see
// revokeLineagePartners), clears its cookie, and sets Clear-Site-Data so
// the browser drops cached credential-derived state too.
//
// Ordering (fix round 2, finding N1): ClearSessionCookie and
// Clear-Site-Data are written BEFORE the (fallible) lineage sweep, not
// after. This is a deliberate choice, not an oversight: sess.ID -- the
// row the client is actually using RIGHT NOW -- is already durably
// revoked by the time either header is set, so the caller's own browser
// is safe to tell "you're logged out" regardless of what happens next.
// The lineage sweep is best-effort cleanup of a SEPARATE row the client
// isn't currently presenting; if it fails, writeSessionAPIInternalError
// below still overwrites the status/body with a 500, but net/http only
// honors headers set before the first WriteHeader call, so the
// already-queued Set-Cookie/Clear-Site-Data headers still reach the
// client on that 500 too -- graceful degradation over an all-or-nothing
// response the caller's own session revocation doesn't actually need.
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

	// DD-C14/DD-C14b/DD-C14c: logout kills the whole credential LINEAGE,
	// not just sess's own row -- see revokeLineagePartners' own doc
	// comment.
	if _, ok := s.revokeLineagePartners(ctx, sess, w, r); !ok {
		return
	}

	writeNoContent(w)
}

// revokeLineagePartners revokes the session(s) directly linked to row by
// rotation -- the row it was rotated FROM (if any) and the row that was
// rotated FROM it (if any) -- so revoking ANY one row in a rotation pair
// kills the OTHER half too (DD-C14, DD-C14b, DD-C14c). It reports the ids
// of every additional session it revoked (0, 1, or 2 -- row itself is
// NOT included, since callers already revoked that separately) and
// whether it succeeded; on failure the response has already been
// written and callers must stop and return immediately.
//
// Exact by construction (fix round 3, DD-C14c owner ruling), not a
// heuristic:
//
//   - Predecessor direction (row is a SUCCESSOR): row.RotatedFrom
//     (sessions.rotated_from, sql/schema.sql) -- set once, at INSERT
//     time, by tryRotate's successor insert (session.go) -- IS the
//     predecessor's id. No query at all: whichever row a caller already
//     has in hand (sess from context, or a fetched target row) already
//     carries the answer.
//   - Successor direction (row is a PREDECESSOR with a live successor):
//     FindLiveSuccessorSession (sql/queries.sql), an exact `rotated_from
//     = row.ID` lookup, unique by construction via
//     sessions_rotated_from_key's partial UNIQUE index -- a predecessor
//     has AT MOST ONE successor, enforced by the database itself.
//
// Fix rounds 1 and 2 solved both directions by reconstructing the
// rotation link from rotation_grace_until/created_at TIMING alone (no
// rotated_from column existed yet). That approach was proven, by a
// flaky required test under `-shuffle=on`, to silently match ANY
// same-user session sharing the same microsecond -- pgx's `:one` takes
// the first row of an ambiguous match without complaint -- which broke
// exactly the blast-radius guarantee those fixes were supposed to
// provide. See task-9-report.md's fix round 3 section for the full
// incident. DD-C14c's schema change (sessions.rotated_from +
// sessions_rotated_from_key) replaces that reconstruction entirely.
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
		// row has no live successor -- either it was never rotated at
		// all, or its successor is already gone some other way. Nothing
		// left to sweep in this direction.
	default:
		s.writeSessionAPIInternalError(w, r, "find_successor_session", err)
		return nil, false
	}

	return revokedIDs, true
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
// belonging to the caller, mirroring session.go's sessionDead exactly
// (fix round 1, finding I1): explicitly revoked rows are never returned;
// neither are idle-expired or absolute-expired rows; neither are Task 7's
// grace-dead rotation predecessors (rows with rotation_grace_until in the
// past but revoked_at still NULL: see ListLiveSessionsForUser's own doc
// comment, sql/queries.sql) -- a superseded predecessor must never
// masquerade as a live device the caller could still revoke.
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
// one session that must belong to the caller, and its rotation lineage
// partner(s) if any (DD-C14b/DD-C14c -- see revokeLineagePartners).
// Requires a recent reauthentication (DD-C11), checked BEFORE
// RevokeForUser touches a single row -- see this file's own top-of-file
// doc comment.
//
// Clears the caller's own __Host-session cookie and sets Clear-Site-Data
// whenever the caller's OWN current session dies as part of this
// request (fix round 3, DD-C14c item 6) -- whether it was the id named
// directly, OR a DIFFERENT id was named and the caller's current session
// turned out to be THAT target's lineage partner (predecessor or
// successor). Revoking a session that has no relationship at all to the
// caller's own current session leaves it, and these two response
// artifacts, untouched.
//
// Ordering (cheap-win hardening, matching handleLogout's own documented
// choice -- see that function's doc comment): for the DIRECT-target case
// (targetID == sess.ID), the cookie clear and Clear-Site-Data are written
// BEFORE the fallible lineage sweep runs, not after. RevokeForUser has
// already durably revoked the caller's own session row by that point, so
// the browser is safe to be told "that session is gone" regardless of
// what the sweep does next; the OLD ordering instead left a caller with a
// revoked-in-the-database, still-cookied session if the sweep's own
// FindLiveSuccessorSession/Revoke calls failed (a genuine 500, but one
// that -- unlike handleLogout's already-covered case -- previously left
// the direct-target response cookie untouched too). The lineage-PARTNER
// case (a different id was named and the caller's own session turns out
// to be its rotation partner) is discovered only by the sweep itself, so
// it cannot be moved earlier; that ordering is unchanged.
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

	// Direct-target case: the caller's own current session is already
	// durably revoked above -- tell the browser now, before the fallible
	// sweep below runs (see this function's own doc comment).
	directTarget := targetID == sess.ID
	if directTarget {
		ClearSessionCookie(w)
		w.Header().Set("Clear-Site-Data", clearSiteDataHeaderValue)
	}

	// The row to sweep lineage FROM: the caller's own session (already in
	// hand, no extra read) if that's what was targeted, otherwise the
	// target itself, freshly read -- RevokeForUser only reports a row
	// count, not the row, and a named target is not always the caller's
	// own current session.
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

	// Lineage-partner case: only discoverable by the sweep just above, so
	// it cannot be written any earlier than this.
	if !directTarget && slices.Contains(revokedIDs, sess.ID) {
		ClearSessionCookie(w)
		w.Header().Set("Clear-Site-Data", clearSiteDataHeaderValue)
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
