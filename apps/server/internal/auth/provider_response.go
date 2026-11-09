package auth

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// finishProviderLogin ends a successful provider login. An unenrolled account
// receives its session, and any stale pending cookie is cleared. An enrolled
// account receives a pending login and no session, then continues at the
// factor page. A pending row that cannot be created grants nothing and uses the
// closed auth_failed outcome. See docs/design/passkey-second-factor-contract.md.
func (s *Service) finishProviderLogin(w http.ResponseWriter, r *http.Request, provider Provider, tx Transaction, login providerLoginOutcome) {
	if !login.enrolled {
		SetSessionCookie(w, login.sessionRaw)
		if _, ok := readPendingCookie(r); ok {
			ClearPendingAuthenticationCookie(w)
		}
		ClearOAuthTxCookie(w)
		http.Redirect(w, r, s.callbackSuccessRedirect(tx), http.StatusFound)
		return
	}
	issued, err := s.pending.Create(r.Context(), PendingAuthenticationRequest{
		UserID:            login.userID,
		Purpose:           PendingAuthenticationPurposeLogin,
		PrimaryVerifiedAt: login.verifiedAt,
		ReturnPath:        validatedLoginReturnPath(tx.ReturnPath),
		PreviousRawToken:  previousPendingToken(r),
	})
	if err != nil {
		s.logInternalError(r, provider, "create_pending_authentication", err)
		ClearOAuthTxCookie(w)
		http.Redirect(w, r, s.callbackErrorRedirectBase(tx.Purpose)+"?error="+url.QueryEscape(authFailedErrorCode), http.StatusFound)
		return
	}
	SetPendingAuthenticationCookie(w, issued.RawToken)
	ClearOAuthTxCookie(w)
	http.Redirect(w, r, s.publicOrigin+secondFactorPagePath, http.StatusFound)
}

// emailLocalPart returns the text before "@", or the input when absent.
func emailLocalPart(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return email[:i]
	}
	return email
}

// redirectWithError clears the transaction cookie, logs the rejection, and
// redirects to the purpose-specific error page. Unknown-purpose failures use
// the login page. A view-purpose transaction whose tx was already consumed
// redirects to its own gate instead
// (docs/design/viewer-analytics/sign-in-to-view.md "Sign-in flow").
func (s *Service) redirectWithError(w http.ResponseWriter, r *http.Request, provider Provider, tx Transaction, code string, reason rejectReason) {
	ClearOAuthTxCookie(w)
	s.logRejection(r, provider, code, reason)
	if tx.Purpose == PurposeView {
		s.redirectViewFailure(w, r, tx, code)
		return
	}
	http.Redirect(w, r, s.callbackErrorRedirectBase(tx.Purpose)+"?error="+url.QueryEscape(code), http.StatusFound)
}

// redirectAuthFailed exposes one code while retaining a typed, server-only
// reason for operators.
func (s *Service) redirectAuthFailed(w http.ResponseWriter, r *http.Request, provider Provider, tx Transaction, reason rejectReason) {
	s.redirectWithError(w, r, provider, tx, authFailedErrorCode, reason)
}

// redirectEmailAlreadyRegistered names only the attempted provider, never the
// provider already attached to the account. See docs/design/security.md.
func (s *Service) redirectEmailAlreadyRegistered(w http.ResponseWriter, r *http.Request, provider Provider) {
	ClearOAuthTxCookie(w)
	s.logRejection(r, provider, emailAlreadyRegisteredErrorCode, reasonEmailAlreadyRegistered)
	target := s.publicOrigin + "/login?error=" + url.QueryEscape(emailAlreadyRegisteredErrorCode) +
		"&provider=" + url.QueryEscape(string(provider))
	http.Redirect(w, r, target, http.StatusFound)
}

// callbackErrorRedirectBase returns the PublicOrigin-prefixed page a
// callback rejection redirects to before ?error=code is appended.
func (s *Service) callbackErrorRedirectBase(purpose Purpose) string {
	if purpose == PurposeLink || purpose == PurposeReauth {
		return s.publicOrigin + settingsSessionsPath
	}
	return s.publicOrigin + "/login"
}

// callbackSuccessRedirect selects the purpose-specific success page.
func (s *Service) callbackSuccessRedirect(tx Transaction) string {
	if tx.Purpose == PurposeLink || tx.Purpose == PurposeReauth {
		return s.publicOrigin + settingsSessionsPath
	}
	return s.publicOrigin + validatedLoginReturnPath(tx.ReturnPath)
}

// writeInternalError clears any transaction cookie, logs fixed failure data,
// and writes an opaque 500.
func (s *Service) writeInternalError(w http.ResponseWriter, r *http.Request, provider Provider, op string, err error) {
	ClearOAuthTxCookie(w)
	s.logInternalError(r, provider, op, err)
	api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
}

// logRejection logs only fixed vocabulary and request metadata, never OAuth or
// identity values. String converts the integer-backed reason to its stable token.
func (s *Service) logRejection(r *http.Request, provider Provider, code string, reason rejectReason) {
	if s.logger == nil {
		return
	}
	s.logger.WarnContext(r.Context(), "auth: callback rejected",
		"request_id", api.RequestIDFromContext(r.Context()),
		"provider", provider,
		"error_code", code,
		"reason", reason.String(),
	)
}

// logInternalError records fixed metadata and an optional SQLSTATE. Database
// error text may contain bound values and must never be logged.
func (s *Service) logInternalError(r *http.Request, provider Provider, op string, err error) {
	if s.logger == nil {
		return
	}
	attrs := []any{
		"request_id", api.RequestIDFromContext(r.Context()),
		"provider", provider,
		"op", op,
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		attrs = append(attrs, "sqlstate", pgErr.Code)
	}
	s.logger.ErrorContext(r.Context(), "auth: callback internal error", attrs...)
}
