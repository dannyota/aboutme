package auth

// me.go implements GET /api/v1/me (design spec §3, "CSRF" row; Task 9):
// the authenticated caller's own user record, a fresh CSRF token, and the
// list of OAuth providers this user has a linked identity for. This is
// the ONLY place a CSRF token is ever produced -- never a header, cookie,
// URL, or log line (see csrfTokenForSession) -- pairing with csrf.go's
// RequireCSRF, which reads the matching X-CSRF-Token header back on every
// mutating cookie-authenticated request.

import (
	"encoding/base64"
	"net/http"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// meUser is GET /me's "user" sub-object, pinned to exactly this shape
// (task-9-brief.md's corrected contract): id, email, name, and avatarKey
// (nullable) -- matching store.User's own public fields one-to-one, and
// nothing else (never token_hash/csrf_secret -- which don't exist on
// store.User at all -- nor created_at/updated_at, which the contract
// doesn't call for).
type meUser struct {
	ID        string  `json:"id"`
	Email     string  `json:"email"`
	Name      string  `json:"name"`
	AvatarKey *string `json:"avatarKey"`
}

// meIdentity is one entry of GET /me's "identities" list: which provider
// this user has a linked identity for. Just the provider -- never the
// provider's own subject/user id, an internal correlation key with no
// reason to ever reach the client.
type meIdentity struct {
	Provider string `json:"provider"`
}

// meResponse is GET /me's full "data" payload:
// {data:{user, csrfToken, identities:[{provider}]}}.
type meResponse struct {
	User       meUser       `json:"user"`
	CSRFToken  string       `json:"csrfToken"`
	Identities []meIdentity `json:"identities"`
}

// handleMe implements GET /api/v1/me. It reads the session RequireSession
// already authenticated and put in context -- it never re-authenticates
// or reads the __Host-session cookie itself.
func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		// Defense in depth: RequireSession (handlers.go) is documented to
		// always run first and populate this. Never reachable through
		// RegisterRoutes' real wiring, but fail closed rather than panic
		// if that ordering were ever violated.
		rejectSession(w)
		return
	}

	usr, err := s.q.GetUserByID(ctx, sess.UserID)
	if err != nil {
		s.writeSessionAPIInternalError(w, r, "get_user", err)
		return
	}

	identityRows, err := s.q.ListIdentitiesByUserID(ctx, usr.ID)
	if err != nil {
		s.writeSessionAPIInternalError(w, r, "list_identities", err)
		return
	}

	identities := make([]meIdentity, len(identityRows))
	for i, row := range identityRows {
		identities[i] = meIdentity{Provider: row.Provider}
	}

	api.WriteData(w, http.StatusOK, meResponse{
		User: meUser{
			ID:        usr.ID.String(),
			Email:     usr.Email,
			Name:      usr.Name,
			AvatarKey: usr.AvatarKey,
		},
		CSRFToken:  csrfTokenForSession(sess),
		Identities: identities,
	})
}

// csrfTokenForSession returns the CSRF token a legitimate client holding
// sess must echo back on the X-CSRF-Token header (csrf.go's
// validCSRFToken): sess's raw csrf_secret, base64.RawURLEncoding-encoded
// -- the exact encoding validCSRFToken decodes before its constant-time
// compare. GET /me is the only production call site: the design spec's
// CSRF row is explicit that this token is returned in the response body
// here and nowhere else (never a header, cookie, URL, or log line).
func csrfTokenForSession(sess store.Session) string {
	return base64.RawURLEncoding.EncodeToString(sess.CSRFSecret)
}
