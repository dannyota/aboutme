package githubevidence

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

var (
	errSessionEnded       = errors.New("githubevidence: session ended")
	errGenerationMismatch = errors.New("githubevidence: generation mismatch")
)

const (
	outcomeConnected = "connected"
	outcomeFailed    = "failed"
)

func (s *Service) handleCallback(w http.ResponseWriter, r *http.Request) {
	auth.ClearOAuthTxCookie(w)
	handle, err := auth.ReadOAuthTxCookie(r)
	if err != nil {
		s.fail(w, r, "transaction_cookie_missing")
		return
	}
	tx, err := s.transactions.ConsumeEvidence(r.Context(), handle)
	if err != nil {
		if !errors.Is(err, auth.ErrTransactionInvalid) {
			s.logInternal(r, "consume_transaction", err)
		}
		s.fail(w, r, "transaction_invalid")
		return
	}
	if tx.RedirectURI != s.callbackURL {
		s.fail(w, r, "redirect_uri_mismatch")
		return
	}
	query := r.URL.Query()
	if state := query.Get("state"); state == "" || state != tx.State {
		s.fail(w, r, "state_mismatch")
		return
	}
	if query.Has("error") {
		s.fail(w, r, "provider_error")
		return
	}
	code := query.Get("code")
	if code == "" {
		s.fail(w, r, "code_missing")
		return
	}
	sess, ok := auth.SessionFromContext(r.Context())
	if !ok || sess.UserID != tx.LinkingUserID {
		s.fail(w, r, "session_mismatch")
		return
	}
	reserved, ok := s.registry.reserve(sess.UserID)
	if !ok {
		s.fail(w, r, "unavailable")
		return
	}
	token, err := s.client.exchange(r.Context(), sess.UserID, code, s.callbackURL, tx.PKCEVerifier)
	if err != nil {
		s.registry.abandon(reserved, token)
		s.fail(w, r, exchangeReason(err))
		return
	}
	profile, err := s.client.profile(r.Context(), sess.UserID, token)
	if err != nil {
		s.registry.abandon(reserved, token)
		s.fail(w, r, profileReason(err))
		return
	}
	epoch, generation, err := s.installConnection(r.Context(), sess.ID, sess.UserID, tx.EvidenceGeneration, profile)
	if err != nil {
		s.registry.abandon(reserved, token)
		switch {
		case errors.Is(err, errGenerationMismatch):
			s.fail(w, r, "generation_mismatch")
		case errors.Is(err, errSessionEnded):
			s.fail(w, r, "session_ended")
		default:
			s.logInternal(r, "install_connection", err)
			s.fail(w, r, "internal")
		}
		return
	}
	if _, installed := s.registry.install(reserved, token, epoch, generation); !installed {
		s.fail(w, r, "session_ended")
		return
	}
	s.redirect(w, r, outcomeConnected)
}

func (s *Service) installConnection(ctx context.Context, sessionID, userID uuid.UUID, want int64, profile githubProfile) (int64, int64, error) {
	var epoch, generation int64
	err := pgx.BeginFunc(ctx, s.pool, func(dbTx pgx.Tx) error {
		qtx := s.q.WithTx(dbTx)
		user, err := qtx.GetUserForUpdate(ctx, userID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errSessionEnded
		}
		if err != nil {
			return err
		}
		current, err := qtx.GetSessionByIDForUpdate(ctx, sessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return errSessionEnded
		}
		if err != nil {
			return err
		}
		if current.UserID != user.ID || current.AuthEpoch != user.AuthEpoch || auth.RequireLiveSession(current, s.now()) != nil {
			return errSessionEnded
		}
		var stored int64
		row, err := qtx.GetGitHubConnectionForUpdate(ctx, user.ID)
		switch {
		case err == nil:
			stored = row.Generation
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		if stored != want {
			return errGenerationMismatch
		}
		installed, err := qtx.InstallGitHubConnection(ctx, store.InstallGitHubConnectionParams{
			UserID:       user.ID,
			GithubUserID: profile.ID,
			Login:        profile.Login,
			ConnectedAt:  s.now(),
		})
		if err != nil {
			return err
		}
		epoch, generation = user.AuthEpoch, installed.Generation
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	return epoch, generation, nil
}

func exchangeReason(err error) string {
	switch {
	case errors.Is(err, errScopeRejected):
		return "scope_rejected"
	case errors.Is(err, errRequestLimit):
		return "request_limit"
	case errors.Is(err, errTimeout):
		return "exchange_timeout"
	default:
		return "exchange_failed"
	}
}

func profileReason(err error) string {
	switch {
	case errors.Is(err, errRequestLimit):
		return "request_limit"
	case errors.Is(err, errTimeout):
		return "profile_timeout"
	default:
		return "profile_failed"
	}
}

func (s *Service) fail(w http.ResponseWriter, r *http.Request, reason string) {
	s.logger.WarnContext(r.Context(), "github evidence: callback rejected",
		"request_id", api.RequestIDFromContext(r.Context()), "reason", reason)
	s.redirect(w, r, outcomeFailed)
}

func (s *Service) redirect(w http.ResponseWriter, r *http.Request, outcome string) {
	http.Redirect(w, r, s.publicOrigin+settingsPath+"?evidence="+outcome, http.StatusFound)
}
