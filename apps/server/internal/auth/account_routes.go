package auth

import (
	"net/http"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// SetAccountDeleteHandler installs the account service's protected deletion
// handler before RegisterRoutes publishes /api/v1/me.
func (s *Service) SetAccountDeleteHandler(handler http.Handler) {
	s.accountDeleteHandler = handler
}

func (s *Service) handleAccount(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.sessionChain(s.handleMe).ServeHTTP(w, r)
	case http.MethodDelete:
		if s.accountDeleteHandler == nil {
			api.WriteError(w, http.StatusServiceUnavailable, "account_unavailable", "account operation is unavailable")
			return
		}
		s.accountDeleteHandler.ServeHTTP(w, r)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodDelete)
		api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed on this route")
	}
}
