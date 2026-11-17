package githubevidence

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

var (
	errGenerationMismatch = errors.New("githubevidence: generation mismatch")
	errRegistryClosed     = errors.New("githubevidence: evidence sessions closed")
	errDifferentAccount   = errors.New("githubevidence: a different GitHub account with evidence attached")
)

const (
	outcomeConnected        = "connected"
	outcomeFailed           = "failed"
	outcomeDifferentAccount = "different_account"
)

type callbackReturn struct {
	user    uuid.UUID
	project uuid.UUID
}

func (s *Service) handleCallback(w http.ResponseWriter, r *http.Request) {
	auth.ClearOAuthTxCookie(w)
	var back callbackReturn
	handle, err := auth.ReadOAuthTxCookie(r)
	if err != nil {
		s.fail(w, r, back, "transaction_cookie_missing")
		return
	}
	tx, err := s.transactions.ConsumeEvidence(r.Context(), handle)
	if err != nil {
		if !errors.Is(err, auth.ErrTransactionInvalid) {
			s.logInternal(r, "consume_transaction", err)
		}
		s.fail(w, r, back, "transaction_invalid")
		return
	}
	sess, ok := auth.SessionFromContext(r.Context())
	sameUser := ok && sess.UserID == tx.LinkingUserID
	if sameUser {
		back = callbackReturn{user: tx.LinkingUserID, project: tx.EvidenceProjectID}
	}
	if tx.RedirectURI != s.callbackURL {
		s.fail(w, r, back, "redirect_uri_mismatch")
		return
	}
	query := r.URL.Query()
	if state := query.Get("state"); state == "" || state != tx.State {
		s.fail(w, r, back, "state_mismatch")
		return
	}
	if query.Has("error") {
		s.fail(w, r, back, "provider_error")
		return
	}
	code := query.Get("code")
	if code == "" {
		s.fail(w, r, back, "code_missing")
		return
	}
	if !sameUser {
		s.fail(w, r, back, "session_mismatch")
		return
	}
	token, issued, err := s.issueToken(r.Context(), sess.UserID, code, tx.PKCEVerifier)
	if !issued {
		s.fail(w, r, back, "unavailable")
		return
	}
	if err != nil {
		s.registry.release(token)
		s.fail(w, r, back, exchangeReason(err))
		return
	}
	profile, err := s.client.profile(r.Context(), sess.UserID, token)
	if err != nil {
		s.registry.release(token)
		s.fail(w, r, back, profileReason(err))
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
		case errors.Is(err, errDifferentAccount):
			s.refuse(w, r, back, "different_account", outcomeDifferentAccount)
		case errors.Is(err, errGenerationMismatch):
			s.fail(w, r, back, "generation_mismatch")
		case errors.Is(err, auth.ErrSessionInvalid):
			s.fail(w, r, back, "session_ended")
		case errors.Is(err, errRegistryClosed):
			s.fail(w, r, back, "unavailable")
		default:
			s.logInternal(r, "install_connection", err)
			s.fail(w, r, back, "internal")
		}
		return
	}
	if s.hooks.connectionCommitted != nil {
		s.hooks.connectionCommitted()
	}
	s.redirect(w, r, back, outcomeConnected)
}

func (s *Service) installConnection(ctx context.Context, a account, want int64, profile githubProfile, token *heldToken) (*liveSession, error) {
	var stored int64
	if a.found {
		stored = a.connection.Generation
	}
	if stored != want {
		return nil, errGenerationMismatch
	}
	if a.found && a.connection.GithubUserID != nil && *a.connection.GithubUserID != profile.ID {
		hasEvidence, err := a.q.AccountHasProjectEvidence(ctx, a.user.ID)
		if err != nil {
			return nil, err
		}
		if hasEvidence {
			return nil, errDifferentAccount
		}
	}
	connection, err := a.q.InstallGitHubConnection(ctx, store.InstallGitHubConnectionParams{
		UserID:       a.user.ID,
		GithubUserID: profile.ID,
		Login:        profile.Login,
		ConnectedAt:  a.now,
	})
	if err != nil {
		return nil, err
	}
	session, ok := s.registry.install(a.user.ID, token, a.user.AuthEpoch, connection.Generation, a.now)
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

func (s *Service) fail(w http.ResponseWriter, r *http.Request, back callbackReturn, reason string) {
	s.refuse(w, r, back, reason, outcomeFailed)
}

func (s *Service) refuse(w http.ResponseWriter, r *http.Request, back callbackReturn, reason, outcome string) {
	s.logger.WarnContext(r.Context(), "github evidence: callback rejected",
		"request_id", api.RequestIDFromContext(r.Context()), "reason", reason)
	s.redirect(w, r, back, outcome)
}

func (s *Service) redirect(w http.ResponseWriter, r *http.Request, back callbackReturn, outcome string) {
	http.Redirect(w, r, s.publicOrigin+s.returnPath(r, back)+"?evidence="+outcome, http.StatusFound)
}

func (s *Service) returnPath(r *http.Request, back callbackReturn) string {
	if back.project == uuid.Nil || back.user == uuid.Nil {
		return settingsPath
	}
	owned, err := s.q.ProjectBelongsToUser(r.Context(), store.ProjectBelongsToUserParams{ProjectID: back.project, UserID: back.user})
	if err != nil {
		s.logInternal(r, "read_return_project", err)
		return settingsPath
	}
	if !owned {
		return settingsPath
	}
	return projectPathPrefix + back.project.String()
}
