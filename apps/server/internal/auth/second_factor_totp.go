package auth

// TOTP-specific second-factor HTTP surface: the pending verify route and the
// account enrollment and removal routes. Kept in its own file, alongside
// totp_service.go in internal/secondfactor, because second_factor_handlers.go
// is already near the 700-line non-test file bound
// (docs/standards/engineering.md). The routes and checks follow
// docs/design/totp-second-factor-contract.md.

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// TOTP route paths ("Release surface").
const (
	SecondFactorPendingTOTPPath    = "/api/v1/auth/second-factor/totp/verify"
	SecondFactorTOTPEnrollmentPath = "/api/v1/me/second-factor/totp/enrollment"
	SecondFactorTOTPPath           = "/api/v1/me/second-factor/totp"
)

// SecondFactorMethodTOTP is the pending method name for an authenticator-app
// code ("Release surface": methods list passkey, totp, then recovery).
const SecondFactorMethodTOTP = "totp"

// ErrTOTPEnrollmentInvalid collapses every unknown, malformed, foreign,
// expired, deleted, wrong-session, or wrong-epoch enrollment ID into one
// outcome, matching the pending and passkey sentinels' no-oracle shape
// ("Enrollment and replacement API").
var ErrTOTPEnrollmentInvalid = errors.New("auth: totp enrollment invalid")

// totpCoolingDown reports a live per-account TOTP cool-down with its bounded
// Retry-After seconds ("Per-account TOTP failure budget").
type totpCoolingDown struct{ retryAfterSeconds int }

func (e *totpCoolingDown) Error() string { return "auth: totp cool-down active" }

// NewTOTPCoolingDownError reports a live TOTP cool-down. retryAfterSeconds is
// already bounded to at most 86,400 by the caller.
func NewTOTPCoolingDownError(retryAfterSeconds int) error {
	return &totpCoolingDown{retryAfterSeconds: retryAfterSeconds}
}

// TOTPEnrollmentStart is one-time enrollment start data: the raw enrollment
// ID, the grouped Base32 secret, the provisioning URI, and its expiry
// ("Enrollment and replacement API").
type TOTPEnrollmentStart struct {
	EnrollmentID    string
	Secret          string
	ProvisioningURI string
	ExpiresAt       time.Time
}

// TOTPEnrollmentComplete reports a completed TOTP enrollment or replacement.
// RecoveryCodes is set only when this completion created the account's first
// active factor.
type TOTPEnrollmentComplete struct {
	RecoveryCodes []string
	Session       SessionIssue
}

// TOTPSecondFactorService is the TOTP behavior behind the TOTP routes. It is
// declared separately from SecondFactorService so a composition that has not
// wired TOTP dependencies still satisfies the passkey and recovery contract;
// a single service implementation satisfies both interfaces.
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

// registerTOTPRoutes attaches the pending verify, enrollment, and removal
// routes. It is called from RegisterRoutes in second_factor_handlers.go.
func (h *SecondFactorHandlers) registerTOTPRoutes(mux *http.ServeMux, noStore func(http.Handler) http.Handler) {
	mux.Handle(SecondFactorPendingTOTPPath, noStore(route(http.MethodPost, h.pendingCompletion(secondFactorRecoveryBodyBytes, h.decodeTOTPBody))))
	mux.Handle(SecondFactorTOTPEnrollmentPath, noStore(http.HandlerFunc(h.handleTOTPEnrollment)))
	mux.Handle(SecondFactorTOTPPath, noStore(route(http.MethodDelete, h.handleRemoveTOTP)))
}

// decodeTOTPBody answers the uniform pending decode shape when TOTP is not
// wired, so an unconfigured deployment behaves like every other unwired TOTP
// route rather than panicking on a nil service.
func (h *SecondFactorHandlers) decodeTOTPBody(body []byte) (SecondFactorCredential, error) {
	if h.totp == nil {
		return nil, ErrSecondFactorRequestInvalid
	}
	return h.totp.DecodeTOTPCode(body)
}

// handleTOTPEnrollment dispatches the shared enrollment path to its two
// methods; any other method is closed out with 405, never an arbitrary
// route match.
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

// handleTOTPEnrollmentStart answers as an unregistered route, before any
// session or database work, while TOTP is unwired or its flag is off
// ("Enrollment and replacement API").
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

// handleTOTPEnrollmentComplete proves and installs the proposed secret.
// While TOTP is unwired it answers as an unregistered route; while it is
// wired but disabled it falls to discardTOTPEnrollment, which still consumes
// a matching enrollment.
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

// discardTOTPEnrollment mirrors discardRegistration: it independently checks
// the session, Origin, CSRF, and rate budgets, lets the service consume a
// matching enrollment without installing anything, and always answers as an
// unregistered route ("Enrollment and replacement API": "Completion deletes
// a matching enrollment, installs nothing, and returns the same 404").
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

// writeTOTPCompletionError maps CompleteTOTPEnrollment outcomes that
// writeAccountError does not already cover to their contract status codes.
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
