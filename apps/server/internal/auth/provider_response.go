package auth

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"danny.vn/aboutme/apps/server/internal/api"
)

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

func emailLocalPart(email string) string {
	if i := strings.IndexByte(email, '@'); i > 0 {
		return email[:i]
	}
	return email
}

func (s *Service) redirectWithError(w http.ResponseWriter, r *http.Request, provider Provider, tx Transaction, code string, reason rejectReason) {
	ClearOAuthTxCookie(w)
	s.logRejection(r, provider, code, reason)
	if tx.Purpose == PurposeView {
		s.redirectViewFailure(w, r, tx, code)
		return
	}
	http.Redirect(w, r, s.callbackErrorRedirectBase(tx.Purpose)+"?error="+url.QueryEscape(code), http.StatusFound)
}

func (s *Service) redirectAuthFailed(w http.ResponseWriter, r *http.Request, provider Provider, tx Transaction, reason rejectReason) {
	s.redirectWithError(w, r, provider, tx, authFailedErrorCode, reason)
}

func (s *Service) redirectEmailAlreadyRegistered(w http.ResponseWriter, r *http.Request, provider Provider) {
	ClearOAuthTxCookie(w)
	s.logRejection(r, provider, emailAlreadyRegisteredErrorCode, reasonEmailAlreadyRegistered)
	target := s.publicOrigin + "/login?error=" + url.QueryEscape(emailAlreadyRegisteredErrorCode) +
		"&provider=" + url.QueryEscape(string(provider))
	http.Redirect(w, r, target, http.StatusFound)
}

func (s *Service) callbackErrorRedirectBase(purpose Purpose) string {
	if purpose == PurposeLink || purpose == PurposeReauth {
		return s.publicOrigin + settingsSessionsPath
	}
	return s.publicOrigin + "/login"
}

func (s *Service) callbackSuccessRedirect(tx Transaction) string {
	if tx.Purpose == PurposeLink || tx.Purpose == PurposeReauth {
		return s.publicOrigin + settingsSessionsPath
	}
	return s.publicOrigin + validatedLoginReturnPath(tx.ReturnPath)
}

func (s *Service) writeInternalError(w http.ResponseWriter, r *http.Request, provider Provider, op string, err error) {
	ClearOAuthTxCookie(w)
	s.logInternalError(r, provider, op, err)
	api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
}

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
