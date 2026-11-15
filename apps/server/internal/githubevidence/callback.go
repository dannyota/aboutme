package githubevidence

import (
	"context"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

var (
	errGenerationMismatch = errors.New("githubevidence: generation mismatch")
	errRegistryClosed     = errors.New("githubevidence: evidence sessions closed")
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
	if !s.registry.beginIssue() {
		s.fail(w, r, "unavailable")
		return
	}
	token, err := s.client.exchange(r.Context(), sess.UserID, code, s.callbackURL, tx.PKCEVerifier)
	s.registry.endIssue()
	if err != nil {
		s.registry.release(token)
		s.fail(w, r, exchangeReason(err))
		return
	}
	profile, err := s.client.profile(r.Context(), sess.UserID, token)
	if err != nil {
		s.registry.release(token)
		s.fail(w, r, profileReason(err))
		return
	}
	var installed *liveSession
	err = s.inAccount(r.Context(), sess, func(ctx context.Context, a account) error {
		var installErr error
		installed, installErr = s.installConnection(ctx, a, tx.EvidenceGeneration, profile, token)
		return installErr
	})
	if err != nil {
		if installed != nil {
			s.registry.endSession(sess.UserID, installed)
		}
		s.registry.release(token)
		switch {
		case errors.Is(err, errGenerationMismatch):
			s.fail(w, r, "generation_mismatch")
		case errors.Is(err, auth.ErrSessionInvalid):
			s.fail(w, r, "session_ended")
		case errors.Is(err, errRegistryClosed):
			s.fail(w, r, "unavailable")
		default:
			s.logInternal(r, "install_connection", err)
			s.fail(w, r, "internal")
		}
		return
	}
	if s.hooks.connectionCommitted != nil {
		s.hooks.connectionCommitted()
	}
	s.redirect(w, r, outcomeConnected)
}

func (s *Service) installConnection(ctx context.Context, a account, want int64, profile githubProfile, token *heldToken) (*liveSession, error) {
	var stored int64
	row, err := a.q.GetGitHubConnectionForUpdate(ctx, a.user.ID)
	switch {
	case err == nil:
		stored = row.Generation
	case !errors.Is(err, pgx.ErrNoRows):
		return nil, err
	}
	if stored != want {
		return nil, errGenerationMismatch
	}
	connection, err := a.q.InstallGitHubConnection(ctx, store.InstallGitHubConnectionParams{
		UserID:       a.user.ID,
		GithubUserID: profile.ID,
		Login:        profile.Login,
		ConnectedAt:  s.now(),
	})
	if err != nil {
		return nil, err
	}
	session, ok := s.registry.install(a.user.ID, token, a.user.AuthEpoch, connection.Generation)
	if !ok {
		return nil, errRegistryClosed
	}
	return session, nil
}

func exchangeReason(err error) string {
	switch {
	case errors.Is(err, errAmbiguousToken):
		return "ambiguous_token"
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
