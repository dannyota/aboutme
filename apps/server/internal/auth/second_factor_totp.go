package auth

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/store"
)

const (
	SecondFactorPendingTOTPPath    = "/api/v1/auth/second-factor/totp/verify"
	SecondFactorTOTPEnrollmentPath = "/api/v1/me/second-factor/totp/enrollment"
	SecondFactorTOTPPath           = "/api/v1/me/second-factor/totp"
)

const SecondFactorMethodTOTP = "totp"

var ErrTOTPEnrollmentInvalid = errors.New("auth: totp enrollment invalid")

type totpCoolingDown struct{ retryAfterSeconds int }

func (e *totpCoolingDown) Error() string { return "auth: totp cool-down active" }

func NewTOTPCoolingDownError(retryAfterSeconds int) error {
	return &totpCoolingDown{retryAfterSeconds: retryAfterSeconds}
}

type TOTPEnrollmentStart struct {
	EnrollmentID    string
	Secret          string
	ProvisioningURI string
	ExpiresAt       time.Time
}

type TOTPEnrollmentComplete struct {
	RecoveryCodes []string
	Session       SessionIssue
}

type TOTPSecondFactorService interface {
	TOTPEnrollmentEnabled() bool
	DecodeTOTPCode(body []byte) (SecondFactorCredential, error)
	DecodeTOTPEnrollmentCompletion(body []byte) (SecondFactorCredential, error)
	TOTPState(ctx context.Context, userID uuid.UUID) (bool, error)
	StartTOTPEnrollment(ctx context.Context, sess store.Session) (TOTPEnrollmentStart, error)
	CompleteTOTPEnrollment(ctx context.Context, sess store.Session, credential SecondFactorCredential) (TOTPEnrollmentComplete, error)
	RemoveTOTP(ctx context.Context, sess store.Session) (SessionIssue, error)
}

type (
	totpEnrollmentStartBody struct {
		EnrollmentID    string `json:"enrollmentId"`
		Secret          string `json:"secret"`
		ProvisioningURI string `json:"provisioningUri"`
		ExpiresAt       string `json:"expiresAt"`
	}
	totpEnrollmentCompleteBody struct {
		TOTPEnabled   bool     `json:"totpEnabled"`
		RecoveryCodes []string `json:"recoveryCodes,omitempty"`
	}
)

func (h *SecondFactorHandlers) registerTOTPRoutes(mux *http.ServeMux, noStore func(http.Handler) http.Handler) {
	if h.totp == nil {
		notFound := noStore(api.NotFound())
		for _, path := range []string{SecondFactorPendingTOTPPath, SecondFactorTOTPEnrollmentPath, SecondFactorTOTPPath} {
			mux.Handle(path, notFound)
		}
		return
	}
	mux.Handle(SecondFactorPendingTOTPPath, noStore(route(http.MethodPost, h.pendingCompletion(secondFactorRecoveryBodyBytes, h.decodeTOTPBody))))
	mux.Handle(SecondFactorTOTPEnrollmentPath, noStore(http.HandlerFunc(h.handleTOTPEnrollment)))
	mux.Handle(SecondFactorTOTPPath, noStore(route(http.MethodDelete, h.handleRemoveTOTP)))
}

func (h *SecondFactorHandlers) decodeTOTPBody(body []byte) (SecondFactorCredential, error) {
	return h.totp.DecodeTOTPCode(body)
}

func (h *SecondFactorHandlers) handleTOTPEnrollment(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		h.handleTOTPEnrollmentStart(w, r)
	case http.MethodPut:
		h.handleTOTPEnrollmentComplete(w, r)
	default:
		w.Header().Set("Allow", "POST, PUT")
		api.WriteError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed; use POST or PUT")
	}
}

func (h *SecondFactorHandlers) handleTOTPEnrollmentStart(w http.ResponseWriter, r *http.Request) {
	if h.totp == nil || !h.totp.TOTPEnrollmentEnabled() {
		api.NotFound()(w, r)
		return
	}
	sess, _, ok := h.admitAccountMutation(w, r, secondFactorRecoveryBodyBytes, decodeEmptyObject, true)
	if !ok {
		return
	}
	start, err := h.totp.StartTOTPEnrollment(r.Context(), sess)
	if err != nil {
		if errors.Is(err, ErrSecondFactorEnrollmentDisabled) {
			api.NotFound()(w, r)
			return
		}
		h.writeAccountError(w, err)
		return
	}
	api.WriteData(w, http.StatusOK, totpEnrollmentStartBody{
		EnrollmentID: start.EnrollmentID, Secret: start.Secret, ProvisioningURI: start.ProvisioningURI,
		ExpiresAt: formatSecondFactorTime(start.ExpiresAt),
	})
}

func (h *SecondFactorHandlers) handleTOTPEnrollmentComplete(w http.ResponseWriter, r *http.Request) {
	if h.totp == nil {
		api.NotFound()(w, r)
		return
	}
	if !h.totp.TOTPEnrollmentEnabled() {
		h.discardTOTPEnrollment(w, r)
		return
	}
	sess, credential, ok := h.admitAccountMutation(w, r, secondFactorRecoveryBodyBytes, h.totp.DecodeTOTPEnrollmentCompletion, true)
	if !ok {
		return
	}
	if d := h.limits.AdmitAttempt(h.clock(), sess.UserID, h.clientIP(r)); !d.Allowed {
		writePasswordRateLimited(w, d.RetryAfterSeconds)
		return
	}
	result, err := h.totp.CompleteTOTPEnrollment(r.Context(), sess, credential)
	if err != nil {
		h.writeTOTPCompletionError(w, r, err)
		return
	}
	SetSessionCookie(w, result.Session.RawToken)
	api.WriteData(w, http.StatusOK, totpEnrollmentCompleteBody{TOTPEnabled: true, RecoveryCodes: result.RecoveryCodes})
}

func (h *SecondFactorHandlers) discardTOTPEnrollment(w http.ResponseWriter, r *http.Request) {
	defer api.NotFound()(w, r)
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return
	}
	sess, err := h.sessions.q.GetSessionByTokenHash(r.Context(), hashSessionToken(cookie.Value))
	if err != nil || RequireLiveSession(sess, h.clock()) != nil || h.checkSessionCSRF(r, sess) != nil {
		return
	}
	if d := h.limits.AdmitManagement(h.clock(), sess.UserID, h.clientIP(r)); !d.Allowed {
		return
	}
	if d := h.limits.AdmitAttempt(h.clock(), sess.UserID, h.clientIP(r)); !d.Allowed {
		return
	}
	body, err := readSecondFactorBody(nil, r, secondFactorRecoveryBodyBytes, true)
	if err != nil {
		return
	}
	credential, err := h.totp.DecodeTOTPEnrollmentCompletion(body)
	if err != nil {
		return
	}
	if _, completeErr := h.totp.CompleteTOTPEnrollment(r.Context(), sess, credential); completeErr != nil && !errors.Is(completeErr, ErrSecondFactorEnrollmentDisabled) {
		h.logFailure("discard_totp_enrollment", completeErr)
	}
}

func (h *SecondFactorHandlers) handleRemoveTOTP(w http.ResponseWriter, r *http.Request) {
	sess, _, ok := h.admitAccountMutation(w, r, secondFactorRecoveryBodyBytes, nil, false)
	if !ok {
		return
	}
	if h.totp == nil {
		writeSecondFactorNotFound(w)
		return
	}
	issue, err := h.totp.RemoveTOTP(r.Context(), sess)
	if err != nil {
		h.writeAccountError(w, err)
		return
	}
	SetSessionCookie(w, issue.RawToken)
	writeNoContent(w)
}

func (h *SecondFactorHandlers) writeTOTPCompletionError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrTOTPEnrollmentInvalid):
		api.WriteError(w, http.StatusBadRequest, "enrollment_invalid", "the enrollment is invalid or expired")
	case errors.Is(err, ErrSecondFactorVerificationFailed):
		api.WriteError(w, http.StatusUnauthorized, "verification_failed", "verification failed")
	case errors.Is(err, ErrSecondFactorEnrollmentDisabled):
		api.NotFound()(w, r)
	default:
		h.writeAccountError(w, err)
	}
}
