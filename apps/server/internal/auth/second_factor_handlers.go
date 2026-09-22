package auth

// Second-factor HTTP routes. Pending POSTs check media type, body size, strict
// JSON, the pending cookie, Origin and pending CSRF, then rate admission before
// work. Account routes check the session, Origin and CSRF, media type, body,
// and rate first. The contract is docs/design/passkey-second-factor-contract.md.

import (
	"encoding/base64"
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
	SecondFactorPendingPath         = "/api/v1/auth/second-factor"
	SecondFactorPendingOptionsPath  = "/api/v1/auth/second-factor/passkey/options"
	SecondFactorPendingPasskeyPath  = "/api/v1/auth/second-factor/passkey/verify"
	SecondFactorPendingRecoveryPath = "/api/v1/auth/second-factor/recovery/verify"
	SecondFactorStatePath           = "/api/v1/me/second-factor"
	SecondFactorPasskeyOptionsPath  = "/api/v1/me/second-factor/passkeys/options"
	SecondFactorPasskeysPath        = "/api/v1/me/second-factor/passkeys"
	SecondFactorRecoveryCodesPath   = "/api/v1/me/second-factor/recovery-codes"
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

// SecondFactorHandlerOptions is the handler dependency set; Logger and
// TrustedProxies may be nil.
type SecondFactorHandlerOptions struct {
	Service        SecondFactorService
	Pending        *PendingAuthenticationManager
	Sessions       *SessionManager
	Limits         *SecondFactorRatePolicies
	PublicOrigin   string
	TrustedProxies api.TrustedProxies
	Clock          func() time.Time
	Logger         *slog.Logger
}

// SecondFactorHandlers serves the pending, state, passkey, and recovery-code
// routes.
type SecondFactorHandlers struct {
	service        SecondFactorService
	pending        *PendingAuthenticationManager
	sessions       *SessionManager
	limits         *SecondFactorRatePolicies
	publicOrigin   string
	trustedProxies api.TrustedProxies
	clock          func() time.Time
	logger         *slog.Logger
}

// NewSecondFactorHandlers rejects a nil dependency or an empty origin.
func NewSecondFactorHandlers(opts SecondFactorHandlerOptions) (*SecondFactorHandlers, error) {
	if opts.Service == nil || opts.Pending == nil || opts.Sessions == nil ||
		opts.Limits == nil || opts.Clock == nil || opts.PublicOrigin == "" {
		return nil, errors.New("auth: second factor handlers: missing dependency")
	}
	return &SecondFactorHandlers{
		service: opts.Service, pending: opts.Pending, sessions: opts.Sessions,
		limits: opts.Limits, publicOrigin: opts.PublicOrigin, trustedProxies: opts.TrustedProxies,
		clock: opts.Clock, logger: opts.Logger,
	}, nil
}

// RegisterRoutes attaches every passkey and recovery route. No TOTP route is
// registered. Registration options answer as an unregistered route while
// enrollment is disabled; every other route ignores the flag.
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
}

type secondFactorPendingStatusBody struct {
	Purpose    string   `json:"purpose"`
	Methods    []string `json:"methods"`
	ExpiresAt  string   `json:"expiresAt"`
	ReturnPath string   `json:"returnPath"`
	CSRFToken  string   `json:"csrfToken"`
}

func (h *SecondFactorHandlers) handlePendingStatus(w http.ResponseWriter, r *http.Request) {
	raw, ok := readPendingCookie(r)
	if !ok {
		writeSecondFactorPendingRequired(w)
		return
	}
	pending, err := h.pending.Authenticate(r.Context(), raw, h.currentSessionID(r))
	if err != nil {
		h.writePendingError(w, err)
		return
	}
	methods, err := h.service.PendingMethods(r.Context(), pending.UserID)
	if err != nil || len(methods) == 0 {
		h.logFailure("pending_status", err)
		writePasswordUnavailable(w)
		return
	}
	api.WriteData(w, http.StatusOK, secondFactorPendingStatusBody{
		Purpose: pending.Purpose, Methods: methods, ExpiresAt: formatSecondFactorTime(pending.ExpiresAt),
		ReturnPath: pending.ReturnPath, CSRFToken: base64.RawURLEncoding.EncodeToString(pending.CSRFSecret),
	})
}

// pendingRequest is a pending POST that passed every check before work.
type pendingRequest struct {
	token      string
	sessionID  *uuid.UUID
	credential SecondFactorCredential
}

// admitPending runs the pending POST checks in contract order. It writes the
// response and returns false on any failure.
func (h *SecondFactorHandlers) admitPending(w http.ResponseWriter, r *http.Request, limit int64, decode func([]byte) (SecondFactorCredential, error)) (pendingRequest, bool) {
	body, err := readSecondFactorBody(w, r, limit, true)
	if err != nil {
		writeSecondFactorBodyError(w, err)
		return pendingRequest{}, false
	}
	credential, err := decode(body)
	if err != nil {
		writePasswordRequestInvalid(w)
		return pendingRequest{}, false
	}
	raw, ok := readPendingCookie(r)
	if !ok {
		writeSecondFactorPendingRequired(w)
		return pendingRequest{}, false
	}
	sessionID := h.currentSessionID(r)
	pending, err := h.pending.Authenticate(r.Context(), raw, sessionID)
	if err != nil {
		h.writePendingError(w, err)
		return pendingRequest{}, false
	}
	token, tokenOK := singletonSecurityHeader(r.Header, CSRFHeaderName)
	decoded, decodeErr := base64.RawURLEncoding.DecodeString(token)
	if !originAllowed(r, h.publicOrigin) || !tokenOK || decodeErr != nil || !PendingCSRFValid(pending, decoded) {
		rejectCSRF(w)
		return pendingRequest{}, false
	}
	if d := h.limits.AdmitAttempt(h.clock(), pending.UserID, h.clientIP(r)); !d.Allowed {
		writePasswordRateLimited(w, d.RetryAfterSeconds)
		return pendingRequest{}, false
	}
	return pendingRequest{token: raw, sessionID: sessionID, credential: credential}, true
}

func (h *SecondFactorHandlers) handlePendingOptions(w http.ResponseWriter, r *http.Request) {
	req, ok := h.admitPending(w, r, secondFactorWebAuthnBodyBytes, decodeEmptyObject)
	if !ok {
		return
	}
	options, err := h.service.StartPasskeyAssertion(r.Context(), req.token, req.sessionID)
	if err != nil {
		h.writePendingError(w, err)
		return
	}
	api.WriteData(w, http.StatusOK, options)
}

// pendingCompletion serves a pending verify route whose body decode is decode.
func (h *SecondFactorHandlers) pendingCompletion(limit int64, decode func([]byte) (SecondFactorCredential, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if req, ok := h.admitPending(w, r, limit, decode); ok {
			h.completePending(w, r, req)
		}
	}
}

func (h *SecondFactorHandlers) decodeRecoveryBody(body []byte) (SecondFactorCredential, error) {
	var code string
	if err := decodeStrictStringObject(body, map[string]*string{"code": &code}); err != nil || code == "" {
		return nil, ErrSecondFactorRequestInvalid
	}
	return h.service.DecodeRecoveryCode(code)
}

func (h *SecondFactorHandlers) completePending(w http.ResponseWriter, r *http.Request, req pendingRequest) {
	client := SecondFactorClient{UserAgent: r.UserAgent(), IP: h.clientIP(r)}
	issue, err := h.service.CompletePending(r.Context(), req.token, req.sessionID, req.credential, client)
	if err != nil {
		h.writePendingError(w, err)
		return
	}
	if issue != nil {
		SetSessionCookie(w, issue.RawToken)
	}
	ClearPendingAuthenticationCookie(w)
	writeNoContent(w)
}

// writePendingError maps pending-route outcomes. Only authentication_required
// and the exhausting failure clear the pending cookie.
func (h *SecondFactorHandlers) writePendingError(w http.ResponseWriter, err error) {
	var failure *PendingVerificationFailure
	switch {
	case errors.As(err, &failure):
		if failure.Exhausted {
			ClearPendingAuthenticationCookie(w)
		}
		api.WriteError(w, http.StatusUnauthorized, "verification_failed", "verification failed")
	case errors.Is(err, ErrPendingAuthenticationRequired):
		writeSecondFactorPendingRequired(w)
	case errors.Is(err, ErrSecondFactorChallengeInvalid):
		writeSecondFactorChallengeInvalid(w)
	case errors.Is(err, ErrSecondFactorNotFound):
		writeSecondFactorNotFound(w)
	default:
		h.logFailure("pending", err)
		writePasswordUnavailable(w)
	}
}

type (
	secondFactorPasskeyBody struct {
		ID         string  `json:"id"`
		CreatedAt  string  `json:"createdAt"`
		LastUsedAt *string `json:"lastUsedAt"`
	}
	secondFactorStateBody struct {
		Enabled                bool                      `json:"enabled"`
		Passkeys               []secondFactorPasskeyBody `json:"passkeys"`
		RecoveryCodesRemaining int                       `json:"recoveryCodesRemaining"`
	}
	secondFactorRegistrationBody struct {
		Passkey       secondFactorPasskeyBody `json:"passkey"`
		RecoveryCodes []string                `json:"recoveryCodes,omitempty"`
	}
	secondFactorRecoveryCodesBody struct {
		RecoveryCodes []string `json:"recoveryCodes"`
	}
)

func (h *SecondFactorHandlers) handleState(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.requireSession(w, r)
	if !ok {
		return
	}
	state, err := h.service.State(r.Context(), sess.UserID)
	if err != nil {
		h.writeAccountError(w, err)
		return
	}
	body := secondFactorStateBody{Enabled: state.Enabled, Passkeys: make([]secondFactorPasskeyBody, 0, len(state.Passkeys)), RecoveryCodesRemaining: state.RecoveryCodesRemaining}
	for _, passkey := range state.Passkeys {
		body.Passkeys = append(body.Passkeys, passkeyBody(passkey))
	}
	api.WriteData(w, http.StatusOK, body)
}

// handleRegistrationOptions answers exactly like an unregistered route while
// enrollment is disabled, before any authentication or state work.
func (h *SecondFactorHandlers) handleRegistrationOptions(w http.ResponseWriter, r *http.Request) {
	if !h.service.PasskeyEnrollmentEnabled() {
		api.NotFound()(w, r)
		return
	}
	route(http.MethodPost, h.startRegistration)(w, r)
}

func (h *SecondFactorHandlers) startRegistration(w http.ResponseWriter, r *http.Request) {
	sess, _, ok := h.admitAccountMutation(w, r, decodeEmptyObject, true)
	if !ok {
		return
	}
	options, err := h.service.StartPasskeyRegistration(r.Context(), sess)
	if err != nil {
		h.writeAccountError(w, err)
		return
	}
	api.WriteData(w, http.StatusOK, options)
}

// handleRegistrationComplete stores a verified passkey. While enrollment is
// disabled every outcome is the uniform not-found response.
func (h *SecondFactorHandlers) handleRegistrationComplete(w http.ResponseWriter, r *http.Request) {
	if !h.service.PasskeyEnrollmentEnabled() {
		h.discardRegistration(w, r)
		return
	}
	sess, credential, ok := h.admitAccountMutation(w, r, h.service.DecodePasskeyRegistration, true)
	if !ok {
		return
	}
	registration, err := h.service.CompletePasskeyRegistration(r.Context(), sess, credential)
	if err != nil {
		if errors.Is(err, ErrSecondFactorEnrollmentDisabled) {
			api.NotFound()(w, r)
			return
		}
		h.writeAccountError(w, err)
		return
	}
	SetSessionCookie(w, registration.Session.RawToken)
	api.WriteData(w, http.StatusCreated, secondFactorRegistrationBody{Passkey: passkeyBody(registration.Passkey), RecoveryCodes: registration.RecoveryCodes})
}

// discardRegistration lets the service consume a disabled completion's
// matching ceremony without storing anything. It applies the same session,
// Origin, CSRF, and management rate checks as an enabled completion, but every
// outcome, a rate rejection included, is the uniform not-found response.
func (h *SecondFactorHandlers) discardRegistration(w http.ResponseWriter, r *http.Request) {
	defer api.NotFound()(w, r)
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return
	}
	// The epoch-checked lookup neither rotates nor touches the session; the
	// service locks and rechecks it before consuming anything.
	sess, err := h.sessions.q.GetSessionByTokenHash(r.Context(), hashSessionToken(cookie.Value))
	if err != nil || RequireLiveSession(sess, h.clock()) != nil || h.checkSessionCSRF(r, sess) != nil {
		return
	}
	if d := h.limits.AdmitManagement(h.clock(), sess.UserID, h.clientIP(r)); !d.Allowed {
		return
	}
	body, err := readSecondFactorBody(nil, r, secondFactorWebAuthnBodyBytes, true)
	if err != nil {
		return
	}
	credential, err := h.service.DecodePasskeyRegistration(body)
	if err != nil {
		return
	}
	if _, completeErr := h.service.CompletePasskeyRegistration(r.Context(), sess, credential); completeErr != nil && !errors.Is(completeErr, ErrSecondFactorEnrollmentDisabled) {
		h.logFailure("discard_registration", completeErr)
	}
}

func (h *SecondFactorHandlers) handleRemovePasskey(w http.ResponseWriter, r *http.Request) {
	sess, _, ok := h.admitAccountMutation(w, r, nil, false)
	if !ok {
		return
	}
	passkeyID, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		writeSecondFactorNotFound(w)
		return
	}
	issue, err := h.service.RemovePasskey(r.Context(), sess, passkeyID)
	if err != nil {
		h.writeAccountError(w, err)
		return
	}
	SetSessionCookie(w, issue.RawToken)
	writeNoContent(w)
}

func (h *SecondFactorHandlers) handleRegenerate(w http.ResponseWriter, r *http.Request) {
	sess, _, ok := h.admitAccountMutation(w, r, decodeEmptyObject, false)
	if !ok {
		return
	}
	codes, err := h.service.RegenerateRecoveryCodes(r.Context(), sess)
	if err != nil {
		h.writeAccountError(w, err)
		return
	}
	SetSessionCookie(w, codes.Session.RawToken)
	api.WriteData(w, http.StatusOK, secondFactorRecoveryCodesBody{RecoveryCodes: codes.Codes})
}

// admitAccountMutation authenticates the session, checks Origin and CSRF,
// then the media type, bounded body, and strict JSON, then rate admission. It
// returns the decoded body. decode may be nil for a bodiless route. When
// requireBody is false an absent body is accepted; a present body must still
// decode.
func (h *SecondFactorHandlers) admitAccountMutation(w http.ResponseWriter, r *http.Request, decode func([]byte) (SecondFactorCredential, error), requireBody bool) (store.Session, SecondFactorCredential, bool) {
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
		body, err := readSecondFactorBody(w, r, secondFactorWebAuthnBodyBytes, false)
		if err != nil {
			writeSecondFactorBodyError(w, err)
			return store.Session{}, nil, false
		}
		if credential, err = decode(body); err != nil {
			writePasswordRequestInvalid(w)
			return store.Session{}, nil, false
		}
	}
	if d := h.limits.AdmitManagement(h.clock(), sess.UserID, h.clientIP(r)); !d.Allowed {
		writePasswordRateLimited(w, d.RetryAfterSeconds)
		return store.Session{}, nil, false
	}
	return sess, credential, true
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

func passkeyBody(passkey SecondFactorPasskey) secondFactorPasskeyBody {
	body := secondFactorPasskeyBody{ID: passkey.ID.String(), CreatedAt: formatSecondFactorTime(passkey.CreatedAt)}
	if passkey.LastUsedAt != nil {
		lastUsed := formatSecondFactorTime(*passkey.LastUsedAt)
		body.LastUsedAt = &lastUsed
	}
	return body
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
