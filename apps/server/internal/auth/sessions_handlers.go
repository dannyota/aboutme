package auth

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

	if err := s.sessionMgr.Revoke(ctx, sess.ID); err != nil {
		s.writeSessionAPIInternalError(w, r, "revoke_session", err)
		return
	}

	ClearSessionCookie(w)
	w.Header().Set("Clear-Site-Data", clearSiteDataHeaderValue)

	if _, ok := s.revokeLineagePartners(ctx, sess, w, r); !ok {
		return
	}

	writeNoContent(w)
}

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
	default:
		s.writeSessionAPIInternalError(w, r, "find_successor_session", err)
		return nil, false
	}

	return revokedIDs, true
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
