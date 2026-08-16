package auth

// GET /api/v1/me returns the authenticated user, linked providers, and the
// synchronizer token required for mutating cookie-authenticated requests.

import (
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// meUser is the public user shape returned by GET /me.
type meUser struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Name        string  `json:"name"`
	AvatarKey   *string `json:"avatarKey"`
	HasPassword bool    `json:"hasPassword"`
}

// meIdentity exposes the provider but never its internal subject.
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

// handleMe relies on the session already stored by RequireSession.
func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		// Fail closed if middleware ordering is broken.
		rejectSession(w)
		return
	}

	usr, err := s.q.GetUserByID(ctx, sess.UserID)
	if err != nil {
		s.writeSessionAPIInternalError(w, r, "get_user", err)
		return
	}

	// hasPassword is a single existence probe, never the credential itself.
	hasPassword := false
	if _, err := s.q.GetPasswordCredential(ctx, sess.UserID); err == nil {
		hasPassword = true
	} else if !errors.Is(err, pgx.ErrNoRows) {
		s.writeSessionAPIInternalError(w, r, "get_password_credential", err)
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
			ID:          usr.ID.String(),
			Email:       usr.Email,
			Name:        usr.Name,
			AvatarKey:   usr.AvatarKey,
			HasPassword: hasPassword,
		},
		CSRFToken:  csrfTokenForSession(sess),
		Identities: identities,
	})
}

// csrfTokenForSession encodes the secret the client must echo in
// X-CSRF-Token. It is returned only in the GET /me response body.
func csrfTokenForSession(sess store.Session) string {
	return base64.RawURLEncoding.EncodeToString(sess.CSRFSecret)
}
