package auth

// Second-factor HTTP routes. Pending POSTs check media type, body size, strict
// JSON, the pending cookie, Origin and pending CSRF, then rate admission before
// work. Account routes check the session, Origin and CSRF, media type, body,
// and rate first. The contract is docs/design/passkey-second-factor-contract.md.

import (
	"errors"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// Second-factor route paths.
const (
	// SecondFactorPendingPath reports the pending authentication's status
	// (GET).
	SecondFactorPendingPath = "/api/v1/auth/second-factor"
	// SecondFactorPendingOptionsPath starts a passkey assertion for the
	// pending authentication (POST).
	SecondFactorPendingOptionsPath = "/api/v1/auth/second-factor/passkey/options"
	// SecondFactorPendingPasskeyPath completes the pending authentication
	// with a passkey assertion (POST).
	SecondFactorPendingPasskeyPath = "/api/v1/auth/second-factor/passkey/verify"
	// SecondFactorPendingRecoveryPath completes the pending authentication
	// with a recovery code (POST).
	SecondFactorPendingRecoveryPath = "/api/v1/auth/second-factor/recovery/verify"
	// SecondFactorStatePath reports the signed-in account's second-factor
	// state (GET).
	SecondFactorStatePath = "/api/v1/me/second-factor"
	// SecondFactorPasskeyOptionsPath starts passkey registration (POST). It
	// answers as an unregistered route while passkey enrollment is off.
	SecondFactorPasskeyOptionsPath = "/api/v1/me/second-factor/passkeys/options"
	// SecondFactorPasskeysPath completes passkey registration (POST);
	// SecondFactorPasskeysPath+"/{id}" removes one passkey (DELETE).
	SecondFactorPasskeysPath = "/api/v1/me/second-factor/passkeys"
	// SecondFactorRecoveryCodesPath replaces the account's recovery codes
	// (POST).
	SecondFactorRecoveryCodesPath = "/api/v1/me/second-factor/recovery-codes"
)

// Second-factor route bounds and rate budgets from docs/design/budgets.md.
const (
	secondFactorWebAuthnBodyBytes    = 32768
	secondFactorRecoveryBodyBytes    = 4096
	secondFactorAttemptAccountLimit  = 10
	secondFactorAttemptAccountWindow = 15 * time.Minute
	secondFactorAttemptIPLimit       = 30
	secondFactorAttemptIPWindow      = time.Minute
	secondFactorManagementLimit      = 10
	secondFactorManagementWindow     = time.Hour
)

// SecondFactorHandlerOptions is the handler dependency set; Logger,
// TrustedProxies, and TOTP may be nil. A nil TOTP makes every TOTP route
// answer as an unregistered route.
type SecondFactorHandlerOptions struct {
	// Service runs the passkey and recovery-code operations. Required.
	Service SecondFactorService
	// TOTP runs the authenticator-app operations. Optional.
	TOTP TOTPSecondFactorService
	// Pending authenticates the pending-authentication cookie. Required.
	Pending *PendingAuthenticationManager
	// Sessions authenticates the session cookie. Required.
	Sessions *SessionManager
	// Limits holds the attempt and management rate budgets. Required.
	Limits *SecondFactorRatePolicies
	// PublicOrigin is the exact origin that pending POSTs and account
	// mutations must carry. Required.
	PublicOrigin string
	// TrustedProxies selects the client address used in rate keys; nil
	// trusts no proxy.
	TrustedProxies api.TrustedProxies
	// Clock returns the current time for rate admission and session
	// liveness checks. Required.
	Clock func() time.Time
	// Logger receives fixed, secret-free failure warnings. Optional.
	Logger *slog.Logger
}

// SecondFactorHandlers serves the pending, state, passkey, recovery-code,
// and TOTP routes.
type SecondFactorHandlers struct {
	service        SecondFactorService
	totp           TOTPSecondFactorService
	pending        *PendingAuthenticationManager
	sessions       *SessionManager
	limits         *SecondFactorRatePolicies
	publicOrigin   string
	trustedProxies api.TrustedProxies
	clock          func() time.Time
	logger         *slog.Logger
}

// NewSecondFactorHandlers rejects a nil dependency or an empty origin. TOTP
// is optional.
func NewSecondFactorHandlers(opts SecondFactorHandlerOptions) (*SecondFactorHandlers, error) {
	if opts.Service == nil || opts.Pending == nil || opts.Sessions == nil ||
		opts.Limits == nil || opts.Clock == nil || opts.PublicOrigin == "" {
		return nil, errors.New("auth: second factor handlers: missing dependency")
	}
	return &SecondFactorHandlers{
		service: opts.Service, totp: opts.TOTP, pending: opts.Pending, sessions: opts.Sessions,
		limits: opts.Limits, publicOrigin: opts.PublicOrigin, trustedProxies: opts.TrustedProxies,
		clock: opts.Clock, logger: opts.Logger,
	}, nil
}

// RegisterRoutes attaches every passkey, recovery, and TOTP route. Passkey
// registration options and TOTP enrollment start answer as an unregistered
// route while their flag is off; every other route ignores it. A nil TOTP
// dependency makes every TOTP route answer as unregistered too.
func (h *SecondFactorHandlers) RegisterRoutes(mux *http.ServeMux) {
	noStore := api.NoStoreCache()
	mux.Handle(SecondFactorPendingPath, noStore(route(http.MethodGet, h.handlePendingStatus)))
	mux.Handle(SecondFactorPendingOptionsPath, noStore(route(http.MethodPost, h.handlePendingOptions)))
	mux.Handle(SecondFactorPendingPasskeyPath, noStore(route(http.MethodPost, h.pendingCompletion(secondFactorWebAuthnBodyBytes, h.service.DecodePasskeyAssertion))))
	mux.Handle(SecondFactorPendingRecoveryPath, noStore(route(http.MethodPost, h.pendingCompletion(secondFactorRecoveryBodyBytes, h.decodeRecoveryBody))))
	mux.Handle(SecondFactorStatePath, noStore(route(http.MethodGet, h.handleState)))
	mux.Handle(SecondFactorPasskeyOptionsPath, noStore(http.HandlerFunc(h.handleRegistrationOptions)))
	mux.Handle(SecondFactorPasskeysPath, noStore(route(http.MethodPost, h.handleRegistrationComplete)))
	mux.Handle(SecondFactorPasskeysPath+"/{id}", noStore(route(http.MethodDelete, h.handleRemovePasskey)))
	mux.Handle(SecondFactorRecoveryCodesPath, noStore(route(http.MethodPost, h.handleRegenerate)))
	h.registerTOTPRoutes(mux, noStore)
}

// admitAccountMutation authenticates the session, checks Origin and CSRF,
// then the media type, a body bounded to bodyLimit, and strict JSON, then
// rate admission. It returns the decoded body. decode may be nil for a
// bodiless route. When requireBody is false an absent body is accepted; a
// present body must still decode.
func (h *SecondFactorHandlers) admitAccountMutation(w http.ResponseWriter, r *http.Request, bodyLimit int64, decode func([]byte) (SecondFactorCredential, error), requireBody bool) (store.Session, SecondFactorCredential, bool) {
	sess, ok := h.requireSession(w, r)
	if !ok {
		return store.Session{}, nil, false
	}
	if err := h.checkSessionCSRF(r, sess); err != nil {
		writeSecondFactorBodyError(w, err)
		return store.Session{}, nil, false
	}
	var credential SecondFactorCredential
	if requireBody || hasBody(r) {
		if decode == nil {
			writePasswordRequestInvalid(w)
			return store.Session{}, nil, false
		}
		body, err := readSecondFactorBody(w, r, bodyLimit, false)
		if err != nil {
			writeSecondFactorBodyError(w, err)
			return store.Session{}, nil, false
		}
		if credential, err = decode(body); err != nil {
			writeSecondFactorDecodeError(w, err)
			return store.Session{}, nil, false
		}
	}
	if d := h.limits.AdmitManagement(h.clock(), sess.UserID, h.clientIP(r)); !d.Allowed {
		writePasswordRateLimited(w, d.RetryAfterSeconds)
		return store.Session{}, nil, false
	}
	return sess, credential, true
}

// writeSecondFactorDecodeError maps a decode failure to its status code.
// Every existing decode function returns exactly ErrSecondFactorRequestInvalid,
// so this preserves prior behavior for passkey and recovery routes while
// giving TOTP's enrollment-ID shape check its own enrollment_invalid code
// ("Enrollment and replacement API").
func writeSecondFactorDecodeError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrTOTPEnrollmentInvalid) {
		api.WriteError(w, http.StatusBadRequest, "enrollment_invalid", "the enrollment is invalid or expired")
		return
	}
	writePasswordRequestInvalid(w)
}

// checkSessionCSRF applies exact Origin and the session synchronizer token.
func (h *SecondFactorHandlers) checkSessionCSRF(r *http.Request, sess store.Session) error {
	token, ok := singletonSecurityHeader(r.Header, CSRFHeaderName)
	if !originAllowed(r, h.publicOrigin) || !ok || !validCSRFToken(token, sess.CSRFSecret) {
		return errSecondFactorCSRFRejected
	}
	if hasBody(r) && !jsonContentTypeAllowed(r.Header) {
		return errSecondFactorMediaTypeRejected
	}
	return nil
}

// requireSession authenticates the session cookie and delivers any rotated
// cookie. It writes the 401 or 503 response and returns false on failure.
func (h *SecondFactorHandlers) requireSession(w http.ResponseWriter, r *http.Request) (store.Session, bool) {
	sess, rotated, err := readAndAuthenticateSession(r, h.sessions)
	if err != nil {
		if errors.Is(err, ErrSessionInvalid) {
			writePasswordAuthenticationRequired(w)
			return store.Session{}, false
		}
		h.logFailure("session", err)
		writePasswordUnavailable(w)
		return store.Session{}, false
	}
	if rotated != "" {
		SetSessionCookie(w, rotated)
	}
	return sess, true
}

func (h *SecondFactorHandlers) writeAccountError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrSessionInvalid):
		writePasswordAuthenticationRequired(w)
	case errors.Is(err, ErrReauthRequired):
		writePasswordReauthRequired(w)
	case errors.Is(err, ErrSecondFactorChallengeInvalid):
		writeSecondFactorChallengeInvalid(w)
	case errors.Is(err, ErrSecondFactorVerificationFailed):
		api.WriteError(w, http.StatusBadRequest, "verification_failed", "verification failed")
	case errors.Is(err, ErrSecondFactorNotFound):
		writeSecondFactorNotFound(w)
	case errors.Is(err, ErrSecondFactorLimitReached):
		api.WriteError(w, http.StatusConflict, "passkey_limit_reached", "the passkey limit is reached")
	case errors.Is(err, ErrSecondFactorRequestInvalid):
		writePasswordRequestInvalid(w)
	default:
		h.logFailure("account", err)
		writePasswordUnavailable(w)
	}
}

// currentSessionID returns the browser's session row ID without rotating or
// touching it. The pending manager locks and rechecks the row itself.
func (h *SecondFactorHandlers) currentSessionID(r *http.Request) *uuid.UUID {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	sess, err := h.sessions.q.GetSessionByTokenHash(r.Context(), hashSessionToken(cookie.Value))
	if err != nil {
		return nil
	}
	return &sess.ID
}

func (h *SecondFactorHandlers) clientIP(r *http.Request) string {
	ip, ok := api.ClientIP(r, h.trustedProxies)
	if !ok {
		return ""
	}
	return ip
}

// logFailure writes a fixed, secret-free warning.
func (h *SecondFactorHandlers) logFailure(op string, err error) {
	if h.logger != nil && err != nil {
		h.logger.Warn("second factor request failed", "op", op)
	}
}

func readPendingCookie(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(pendingAuthenticationCookieName)
	if err != nil || cookie.Value == "" {
		return "", false
	}
	return cookie.Value, true
}

// readSecondFactorBody optionally checks the media type, then reads at most
// limit bytes. w may be nil.
func readSecondFactorBody(w http.ResponseWriter, r *http.Request, limit int64, checkMediaType bool) ([]byte, error) {
	if checkMediaType && !jsonContentTypeAllowed(r.Header) {
		return nil, errSecondFactorMediaTypeRejected
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, errSecondFactorBodyTooLarge
		}
		return nil, ErrSecondFactorRequestInvalid
	}
	return body, nil
}

func writeSecondFactorBodyError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errSecondFactorMediaTypeRejected):
		writePasswordMediaTypeUnsupported(w)
	case errors.Is(err, errSecondFactorBodyTooLarge):
		writePasswordBodyTooLarge(w)
	case errors.Is(err, errSecondFactorCSRFRejected):
		rejectCSRF(w)
	default:
		writePasswordRequestInvalid(w)
	}
}

// decodeEmptyObject accepts exactly one empty JSON object.
func decodeEmptyObject(body []byte) (SecondFactorCredential, error) {
	if err := decodeStrictStringObject(body, map[string]*string{}); err != nil {
		return nil, ErrSecondFactorRequestInvalid
	}
	return nil, nil
}

func formatSecondFactorTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func writeSecondFactorPendingRequired(w http.ResponseWriter) {
	ClearPendingAuthenticationCookie(w)
	api.WriteError(w, http.StatusUnauthorized, passwordCodeAuthenticationRequired, passwordMsgAuthenticationRequired)
}

func writeSecondFactorChallengeInvalid(w http.ResponseWriter) {
	api.WriteError(w, http.StatusBadRequest, "challenge_invalid", "the ceremony is invalid or expired")
}

func writeSecondFactorNotFound(w http.ResponseWriter) {
	api.WriteError(w, http.StatusNotFound, "factor_not_found", "no such factor")
}
