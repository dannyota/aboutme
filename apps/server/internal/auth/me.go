package auth

import (
	"encoding/base64"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/store"
)

type meUser struct {
	ID          string  `json:"id"`
	Email       string  `json:"email"`
	Name        string  `json:"name"`
	AvatarKey   *string `json:"avatarKey"`
	HasPassword bool    `json:"hasPassword"`
}

type meIdentity struct {
	ID        string    `json:"id"`
	Provider  string    `json:"provider"`
	CreatedAt time.Time `json:"createdAt"`
}

type meResponse struct {
	User       meUser       `json:"user"`
	CSRFToken  string       `json:"csrfToken"`
	Identities []meIdentity `json:"identities"`
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	sess, ok := SessionFromContext(ctx)
	if !ok {
		rejectSession(w)
		return
	}

	usr, err := s.q.GetUserByID(ctx, sess.UserID)
	if err != nil {
		s.writeSessionAPIInternalError(w, r, "get_user", err)
		return
	}

	hasPassword := false
	if _, credErr := s.q.GetPasswordCredential(ctx, sess.UserID); credErr == nil {
		hasPassword = true
	} else if !errors.Is(credErr, pgx.ErrNoRows) {
		s.writeSessionAPIInternalError(w, r, "get_password_credential", credErr)
		return
	}

	identityRows, err := s.q.ListIdentitiesByUserID(ctx, usr.ID)
	if err != nil {
		s.writeSessionAPIInternalError(w, r, "list_identities", err)
		return
	}

	identities := make([]meIdentity, len(identityRows))
	for i, row := range identityRows {
		identities[i] = meIdentity{ID: row.ID.String(), Provider: row.Provider, CreatedAt: row.CreatedAt.UTC()}
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

func csrfTokenForSession(sess store.Session) string {
	return base64.RawURLEncoding.EncodeToString(sess.CSRFSecret)
}
