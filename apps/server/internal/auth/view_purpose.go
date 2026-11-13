package auth

import (
	"context"
	"errors"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/publicroots"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/viewpass"
)

var viewOpenIDScope = []string{oidc.ScopeOpenID}

func (s *Service) handleViewStart(w http.ResponseWriter, r *http.Request, provider Provider) {
	slug := r.URL.Query().Get("slug")
	if !publicroots.ValidSlug(slug) {
		markStartRejection(r.Context(), reasonStartSlugInvalid)
		api.WriteError(w, http.StatusBadRequest, "bad_request", "slug is missing or invalid")
		return
	}
	pageSuffix, ok := viewPage(r)
	if !ok {
		markStartRejection(r.Context(), reasonStartPageInvalid)
		api.WriteError(w, http.StatusBadRequest, "bad_request", "page is invalid")
		return
	}
	if provider == ProviderGitHub {
		markStartRejection(r.Context(), reasonStartViewProviderUnsupported)
		api.WriteError(w, http.StatusBadRequest, "bad_request", "purpose=view is not offered for "+string(provider))
		return
	}

	ctx := withProviderHTTPClient(r.Context())
	s.reapExpiredOAuthTransactions(ctx, r, provider)

	resume, err := s.q.GetLiveViewResumeBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.redirectToSlugNoTransaction(w, r, slug, pageSuffix)
			return
		}
		s.writeInternalError(w, r, provider, "view_start_read_resume", err)
		return
	}
	if !resume.SignInToView {
		s.redirectToSlugNoTransaction(w, r, slug, pageSuffix)
		return
	}

	var handle, authURL, op string
	switch provider {
	case ProviderGoogle:
		handle, authURL, op, err = s.buildGoogleViewAuthorizeURL(ctx, resume.ID, pageSuffix)
	case ProviderLinkedIn:
		handle, authURL, op, err = s.buildLinkedInViewAuthorizeURL(ctx, resume.ID, pageSuffix)
	}
	if err != nil {
		s.writeInternalError(w, r, provider, op, err)
		return
	}

	SetOAuthTxCookie(w, handle)
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Service) redirectToSlugNoTransaction(w http.ResponseWriter, r *http.Request, slug, pageSuffix string) {
	//nolint:gosec // G710: slug already passed publicroots.ValidSlug (lowercase letters, digits, single internal hyphens, 4-30 chars) and pageSuffix viewPage's closed grammar, so neither carries a scheme, host, or escape that could redirect off s.publicOrigin.
	http.Redirect(w, r, s.publicOrigin+"/"+slug+pageSuffix, http.StatusFound)
}

func (s *Service) buildGoogleViewAuthorizeURL(ctx context.Context, resumeID uuid.UUID, pageSuffix string) (handle, authURL, op string, err error) {
	provider, err := s.googleProvider(ctx)
	if err != nil {
		return "", "", "google_provider_discovery", err
	}
	redirectURI := s.googleRedirectURL()
	handle, tx, err := s.tx.beginView(ctx, ProviderGoogle, resumeID, redirectURI, pageSuffix)
	if err != nil {
		return "", "", "begin_transaction", err
	}
	oauth2Cfg := s.googleOAuth2Config(provider.Endpoint(), redirectURI)
	oauth2Cfg.Scopes = viewOpenIDScope
	return handle, oauth2Cfg.AuthCodeURL(tx.State,
		oauth2.S256ChallengeOption(tx.PKCEVerifier),
		oidc.Nonce(tx.Nonce),
	), "", nil
}

func (s *Service) buildLinkedInViewAuthorizeURL(ctx context.Context, resumeID uuid.UUID, pageSuffix string) (handle, authURL, op string, err error) {
	provider, err := s.linkedinProvider(ctx)
	if err != nil {
		return "", "", "linkedin_provider_discovery", err
	}
	redirectURI := s.linkedinRedirectURL()
	handle, tx, err := s.tx.beginView(ctx, ProviderLinkedIn, resumeID, redirectURI, pageSuffix)
	if err != nil {
		return "", "", "begin_transaction", err
	}
	oauth2Cfg := s.linkedinOAuth2Config(provider.Endpoint(), redirectURI)
	oauth2Cfg.Scopes = viewOpenIDScope
	return handle, oauth2Cfg.AuthCodeURL(tx.State, oidc.Nonce(tx.Nonce)), "", nil
}

func (s *Service) handleViewCallback(w http.ResponseWriter, r *http.Request, provider Provider, tx Transaction) {
	ClearOAuthTxCookie(w)
	state, notFound, err := s.viewGateResumeState(r.Context(), tx.ResumeID)
	if err != nil {
		s.logInternalError(r, provider, "view_callback_read_resume", err)
		api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
		return
	}
	if notFound {
		http.Redirect(w, r, s.publicOrigin+"/", http.StatusFound)
		return
	}
	target := s.publicOrigin + "/" + slugOrEmpty(state.Slug) + viewReturnSuffix(tx.ReturnPath)
	if !state.SignInToView {
		http.Redirect(w, r, target, http.StatusFound)
		return
	}

	now := s.tx.now()
	sealed := viewpass.Seal(s.viewPassKey, state.ID, state.ViewPassEpoch, now.Add(viewpass.Lifetime))
	existing := ""
	if cookie, cookieErr := r.Cookie(viewpass.CookieName); cookieErr == nil {
		existing = cookie.Value
	}
	cookie := viewpass.Add(s.viewPassKey, existing, sealed, now)
	http.SetCookie(w, &cookie)
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Service) redirectViewFailure(w http.ResponseWriter, r *http.Request, tx Transaction, code string) {
	state, notFound, err := s.viewGateResumeState(r.Context(), tx.ResumeID)
	if err != nil {
		s.logInternalError(r, tx.Provider, "view_failure_read_resume", err)
		api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
		return
	}
	if notFound {
		http.Redirect(w, r, s.publicOrigin+"/", http.StatusFound)
		return
	}
	message := "failed"
	if code == cancelledErrorCode {
		message = "cancelled" //nolint:misspell // Exact wire value uses double-L "cancelled".
	}
	http.Redirect(w, r, s.publicOrigin+"/"+slugOrEmpty(state.Slug)+viewReturnSuffix(tx.ReturnPath)+"?signin="+message, http.StatusFound)
}

func (s *Service) viewGateResumeState(ctx context.Context, resumeID uuid.UUID) (state store.GetResumeViewGateStateRow, notFound bool, err error) {
	state, err = s.q.GetResumeViewGateState(ctx, resumeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return store.GetResumeViewGateStateRow{}, true, nil
	}
	if err != nil {
		return store.GetResumeViewGateStateRow{}, false, err
	}
	return state, !state.Live, nil
}

func slugOrEmpty(slug *string) string {
	if slug == nil {
		return ""
	}
	return *slug
}
