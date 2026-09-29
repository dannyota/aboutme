package auth

// The unauthenticated "view" OAuth purpose gates one public resume for sign
// in to view rather than any account (docs/design/viewer-analytics/
// sign-in-to-view.md "Sign-in flow"; ADR 0022; AC-VIEW-004, AC-VIEW-005,
// AC-VIEW-006). Its start binds a transaction to a resume ID instead of a
// login return path and requests scope openid only, so the provider returns
// no name or email. Its callback never looks up, creates, links, or signs in
// an account and never creates a session, CSRF token, or pending-auth
// cookie: it re-reads the resume by ID, discards every claim, and sets only
// the pass cookie.

import (
	"context"
	"errors"
	"net/http"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/publicroots"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/viewpass"
)

// viewOpenIDScope is the only scope the view purpose requests
// (design "Sign-in flow"; AC-VIEW-004).
var viewOpenIDScope = []string{oidc.ScopeOpenID}

// handleViewStart validates the slug, decides whether the gate offers
// provider, and either redirects to the resume with no transaction or
// begins a view transaction and redirects to the provider. GitHub never
// appears on the gate (design "Sign-in flow"; the gate text and privacy
// notice name only Google and LinkedIn).
func (s *Service) handleViewStart(w http.ResponseWriter, r *http.Request, provider Provider) {
	slug := r.URL.Query().Get("slug")
	if !publicroots.ValidSlug(slug) {
		markStartRejection(r.Context(), reasonStartSlugInvalid)
		api.WriteError(w, http.StatusBadRequest, "bad_request", "slug is missing or invalid")
		return
	}
	if provider == ProviderGitHub {
		markStartRejection(r.Context(), reasonStartViewProviderUnsupported)
		api.WriteError(w, http.StatusBadRequest, "bad_request", "purpose=view is not offered for "+string(provider))
		return
	}
	if provider == ProviderLinkedIn && !s.signInToViewLinkedInEnabled {
		s.redirectToSlugNoTransaction(w, r, slug)
		return
	}

	ctx := withProviderHTTPClient(r.Context())
	s.reapExpiredOAuthTransactions(ctx, r, provider)

	resume, err := s.q.GetPublicResumeBySlug(ctx, slug)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			s.redirectToSlugNoTransaction(w, r, slug)
			return
		}
		s.writeInternalError(w, r, provider, "view_start_read_resume", err)
		return
	}
	if !resume.SignInToView {
		s.redirectToSlugNoTransaction(w, r, slug)
		return
	}

	var handle, authURL, op string
	switch provider {
	case ProviderGoogle:
		handle, authURL, op, err = s.buildGoogleViewAuthorizeURL(ctx, resume.ID)
	case ProviderLinkedIn:
		handle, authURL, op, err = s.buildLinkedInViewAuthorizeURL(ctx, resume.ID)
	}
	if err != nil {
		s.writeInternalError(w, r, provider, op, err)
		return
	}

	SetOAuthTxCookie(w, handle)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// redirectToSlugNoTransaction sends the viewer back to the public page with
// no transaction: the slug is not live with the switch on, or the offered
// provider is not offered for view (design "Sign-in flow").
func (s *Service) redirectToSlugNoTransaction(w http.ResponseWriter, r *http.Request, slug string) {
	//nolint:gosec // G710: slug already passed publicroots.ValidSlug (lowercase letters, digits, single internal hyphens, 4-30 chars), so it carries no scheme, host, or path separator that could redirect off s.publicOrigin.
	http.Redirect(w, r, s.publicOrigin+"/"+slug, http.StatusFound)
}

// buildGoogleViewAuthorizeURL binds PKCE S256 and an OIDC nonce like an
// ordinary Google start, but to a view transaction and the openid scope only.
func (s *Service) buildGoogleViewAuthorizeURL(ctx context.Context, resumeID uuid.UUID) (handle, authURL, op string, err error) {
	provider, err := s.googleProvider(ctx)
	if err != nil {
		return "", "", "google_provider_discovery", err
	}
	redirectURI := s.googleRedirectURL()
	handle, tx, err := s.tx.beginView(ctx, ProviderGoogle, resumeID, redirectURI)
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

// buildLinkedInViewAuthorizeURL sends no PKCE, like an ordinary LinkedIn
// start, but to a view transaction and the openid scope only.
func (s *Service) buildLinkedInViewAuthorizeURL(ctx context.Context, resumeID uuid.UUID) (handle, authURL, op string, err error) {
	provider, err := s.linkedinProvider(ctx)
	if err != nil {
		return "", "", "linkedin_provider_discovery", err
	}
	redirectURI := s.linkedinRedirectURL()
	handle, tx, err := s.tx.beginView(ctx, ProviderLinkedIn, resumeID, redirectURI)
	if err != nil {
		return "", "", "begin_transaction", err
	}
	oauth2Cfg := s.linkedinOAuth2Config(provider.Endpoint(), redirectURI)
	oauth2Cfg.Scopes = viewOpenIDScope
	return handle, oauth2Cfg.AuthCodeURL(tx.State, oidc.Nonce(tx.Nonce)), "", nil
}

// handleViewCallback runs once the ID token and its nonce have verified. It
// never decodes claims, looks up an identity, or creates a session: it
// re-reads the resume by ID and either sets a pass or redirects with none.
// tx's own transaction cookie was already cleared by the caller's callback
// funnel before dispatch is decided in redirectWithError, so this success
// path clears it here directly (docs/design/viewer-analytics/
// sign-in-to-view.md "Sign-in flow"; AC-VIEW-005, AC-VIEW-006).
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
	target := s.publicOrigin + "/" + slugOrEmpty(state.Slug)
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

// redirectViewFailure sends a view-purpose callback failure to its own gate
// instead of the login page, re-reading the resume for its current slug
// (design "Sign-in flow"). A read failure falls back to an opaque 500; a
// missing or unpublished resume redirects home, exactly as success would.
func (s *Service) redirectViewFailure(w http.ResponseWriter, r *http.Request, tx Transaction, code string) {
	state, notFound, err := s.viewGateResumeState(r.Context(), tx.ResumeID)
	if err != nil {
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
	http.Redirect(w, r, s.publicOrigin+"/"+slugOrEmpty(state.Slug)+"?signin="+message, http.StatusFound)
}

// viewGateResumeState re-reads a view transaction's resume by ID. notFound
// covers both a missing row and an unpublished resume, since a view-purpose
// callback redirects home for either; err is non-nil only for an unexpected
// database failure.
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

// slugOrEmpty returns the dereferenced slug, or "" for a nil pointer -- a
// live resume always carries a slug, so "" is unreachable except as a
// defensive fallback.
func slugOrEmpty(slug *string) string {
	if slug == nil {
		return ""
	}
	return *slug
}
