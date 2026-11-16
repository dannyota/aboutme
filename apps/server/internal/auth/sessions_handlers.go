package auth

import (
	"fmt"
	"net/http"
	"net/netip"
	"time"

	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/store"
)

const clearSiteDataHeaderValue = `"cookies", "storage"`

func writeNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		rejectSession(w)
		return
	}

	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		qtx := s.q.WithTx(tx)
		user, lockErr := lockAccountUser(ctx, qtx, sess.UserID)
		if lockErr != nil {
			return lockErr
		}
		current, now, gateErr := lockCallerSessionAt(ctx, qtx, user, sess.ID, s.sessionMgr.now)
		if gateErr != nil {
			return gateErr
		}
		if revokeErr := qtx.RevokeSession(ctx, store.RevokeSessionParams{ID: current.ID, RevokedAt: &now}); revokeErr != nil {
			return fmt.Errorf("revoke session: %w", revokeErr)
		}
		if _, sweepErr := revokeLineagePartnersTx(ctx, qtx, current, now); sweepErr != nil {
			return sweepErr
		}
		s.sessionMgr.AccountSessionsEndedLocked(user.ID)
		return nil
	})
	if err != nil {
		if !writeSensitiveGateError(w, err) {
			s.writeSessionAPIInternalError(w, r, "revoke_session", err)
		}
		return
	}

	ClearSessionCookie(w)
	w.Header().Set("Clear-Site-Data", clearSiteDataHeaderValue)
	writeNoContent(w)
}

type sessionDeviceEntry struct {
	ID         string    `json:"id"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	UA         *string   `json:"ua"`
	IP         *string   `json:"ip"`
	Current    bool      `json:"current"`
}

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

func ipToString(ip *netip.Addr) *string {
	if ip == nil {
		return nil
	}
	s := ip.String()
	return &s
}
