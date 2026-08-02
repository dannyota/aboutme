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
// DD-C14 (fix round 1, finding I2, design owner ruling), extended by
// DD-C14b (fix round 2, owner ruling): revoking ANY session -- the
// caller's own current one via logout, or ANY id named via DELETE
// /sessions/{id} -- also revokes its rotation LINEAGE partner(s): the row
// it was rotated FROM (if it's a successor) and the row that was rotated
// FROM it (if it's a predecessor with a still-live successor). See
// revokeLineagePartners' own doc comment for the full mechanism,
// including why the predecessor direction alone (DD-C14) was
// insufficient: a rotation race-loser request legitimately authenticates
// AS the predecessor (holding its own correct CSRF secret), so a caller
// can revoke a predecessor and get a clean 204 while its successor stays
// live for its full idle/absolute lifetime -- logout claiming success
// without ending the session. DELETE /sessions (handleRevokeAllSessions)
// needs no equivalent: it already sweeps every one of the caller's
// sessions, every lineage member included.

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
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

	// DD-C14/DD-C14b (fix round 1 finding I2, fix round 2 finding
	// DD-C14b): logout kills the whole credential LINEAGE, not just
	// sess's own row -- see revokeLineagePartners' own doc comment.
	predecessorID, hasPredecessor := PredecessorSessionIDFromContext(ctx)
	if !s.revokeLineagePartners(ctx, sess, predecessorID, hasPredecessor, w, r) {
		return
	}

	writeNoContent(w)
}

// revokeLineagePartners revokes the session(s) directly linked to row by
// rotation -- the row it was rotated FROM (if any) and the row that was
// rotated FROM it (if any) -- so revoking ANY one row in a rotation pair
// kills the OTHER half too (DD-C14, fix round 1 finding I2; DD-C14b, fix
// round 2 owner ruling). It is a no-op (returning true immediately,
// touching nothing) for the overwhelmingly common case where row was
// never involved in a rotation at all. Callers must stop and return
// immediately when this reports false -- it has already written the
// response.
//
// Two directions, both checked, since either one alone left a real gap:
//
//   - Predecessor direction (row is a SUCCESSOR): closes DD-C14. Two
//     mechanisms, checked in order -- ctxPredecessorID/hasCtxPredecessor
//     (RequireSession's PredecessorSessionIDFromContext fast path,
//     populated ONLY when its OWN Authenticate call, for THIS exact
//     request, performed the rotation -- task-9-brief.md's fix round 1
//     literal description), then FindImmediatePredecessorSession (a
//     database lookup via the EXACT, not heuristic, timing relationship
//     session.go's tryRotate establishes). The context fast path turns
//     out to be unreachable for every real request through handleLogout/
//     handleRevokeSession specifically, since both sit behind
//     RequireCSRF, which validates against the POST-rotation session --
//     a client cannot hold a correct CSRF token for a secret that doesn't
//     exist until this exact request mints it. Kept anyway: correct,
//     cheap when it does apply, and literally what fix round 1 was asked
//     for. The database fallback is what actually closes the exposure in
//     practice (see task-9-report.md's fix round 1 section).
//   - Successor direction (row is a PREDECESSOR with rotation_grace_until
//     set): closes DD-C14b. A rotation race-loser request -- one that
//     presents an old token still within ITS OWN grace window, after some
//     OTHER request already won the rotation -- authenticates AS the
//     predecessor (Authenticate's `RotationGraceUntil == nil` guard never
//     re-attempts rotation for a row already mid-rotation), and that
//     request legitimately holds the predecessor's own correct CSRF
//     secret. Revoking only that predecessor would leave its successor
//     authenticating for its own full idle/absolute lifetime -- logout
//     claiming success without ending the session. FindImmediateSuccessorSession
//     solves the SAME exact timing identity for the other variable.
func (s *Service) revokeLineagePartners(ctx context.Context, row store.Session, ctxPredecessorID uuid.UUID, hasCtxPredecessor bool, w http.ResponseWriter, r *http.Request) bool {
	predecessorID := ctxPredecessorID
	foundPredecessor := hasCtxPredecessor
	if !foundPredecessor {
		predRow, err := s.q.FindImmediatePredecessorSession(ctx, store.FindImmediatePredecessorSessionParams{
			UserID:                row.UserID,
			PredecessorGraceUntil: row.CreatedAt.Add(rotationGrace),
		})
		switch {
		case err == nil:
			predecessorID, foundPredecessor = predRow.ID, true
		case errors.Is(err, pgx.ErrNoRows):
			// No predecessor -- row was never a rotation successor.
		default:
			s.writeSessionAPIInternalError(w, r, "find_predecessor_session", err)
			return false
		}
	}
	if foundPredecessor {
		if err := s.sessionMgr.Revoke(ctx, predecessorID); err != nil {
			s.writeSessionAPIInternalError(w, r, "revoke_predecessor_session", err)
			return false
		}
	}

	if row.RotationGraceUntil != nil {
		succRow, err := s.q.FindImmediateSuccessorSession(ctx, store.FindImmediateSuccessorSessionParams{
			UserID:             row.UserID,
			SuccessorCreatedAt: row.RotationGraceUntil.Add(-rotationGrace),
		})
		switch {
		case err == nil:
			if revokeErr := s.sessionMgr.Revoke(ctx, succRow.ID); revokeErr != nil {
				s.writeSessionAPIInternalError(w, r, "revoke_successor_session", revokeErr)
				return false
			}
		case errors.Is(err, pgx.ErrNoRows):
			// row started rotating (rotation_grace_until is set) but its
			// successor is already gone (revoked some other way) -- not
			// an error, nothing left to sweep.
		default:
			s.writeSessionAPIInternalError(w, r, "find_successor_session", err)
			return false
		}
	}

	return true
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
// partner(s) if any (DD-C14b -- see revokeLineagePartners). Requires a
// recent reauthentication (DD-C11), checked BEFORE RevokeForUser touches
// a single row -- see this file's own top-of-file doc comment. Revoking
// the caller's OWN current session also clears its cookie in this same
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

	// DD-C14/DD-C14b (fix round 1 finding I2, fix round 2 owner ruling):
	// revoking ANY session -- the caller's own current one, or a
	// different one merely named by id -- also revokes its rotation
	// lineage partner(s): a caller who sees BOTH halves of a still-open
	// rotation pair in their own device list (a within-grace predecessor
	// and its successor, task-9-brief.md's grace-window visibility rule)
	// must not be able to leave the other half live by picking just one
	// row to revoke.
	if targetID == sess.ID {
		// The target IS the request's own governing session: sess is
		// already fully in hand from context (no extra read needed), and
		// the context-based predecessor fast path only ever applies to
		// THIS exact session -- see revokeLineagePartners' own doc
		// comment.
		ClearSessionCookie(w)
		predecessorID, hasPredecessor := PredecessorSessionIDFromContext(ctx)
		if !s.revokeLineagePartners(ctx, sess, predecessorID, hasPredecessor, w, r) {
			return
		}
	} else {
		// The target is a DIFFERENT session the caller merely named by
		// id: RevokeForUser only reports a row count, not the row
		// itself, so its own created_at/rotation_grace_until/user_id
		// (needed to find ITS lineage partner(s)) require a second read.
		// The context-based predecessor fast path never applies here --
		// it only ever describes THIS request's own governing session's
		// rotation, never an arbitrary target's.
		targetRow, getErr := s.q.GetSessionByID(ctx, targetID)
		if getErr != nil {
			s.writeSessionAPIInternalError(w, r, "get_revoked_session", getErr)
			return
		}
		if !s.revokeLineagePartners(ctx, targetRow, uuid.Nil, false, w, r) {
			return
		}
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
