package auth

import (
	"encoding/base64"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// secondFactorPendingStatusBody is the pending second-factor status response.
type secondFactorPendingStatusBody struct {
	// Purpose is the pending authentication's purpose, login or reauth.
	Purpose string `json:"purpose"`
	// Methods lists the account's factor methods in the fixed order passkey,
	// totp, recovery; it is never empty.
	Methods []string `json:"methods"`
	// ExpiresAt is when the pending authentication expires, RFC 3339 in UTC.
	ExpiresAt string `json:"expiresAt"`
	// ReturnPath is the validated same-origin path to open after completion.
	ReturnPath string `json:"returnPath"`
	// CSRFToken is the pending row's 32-byte CSRF secret, unpadded
	// base64url, sent back as X-CSRF-Token on every pending POST.
	CSRFToken string `json:"csrfToken"`
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
// and the exhausting failure clear the pending cookie. A live TOTP cool-down
// is 429 rate_limited with a bounded Retry-After
// ("Per-account TOTP failure budget"); it changes no row, so nothing else
// here applies.
func (h *SecondFactorHandlers) writePendingError(w http.ResponseWriter, err error) {
	var failure *PendingVerificationFailure
	var cooling *totpCoolingDown
	switch {
	case errors.As(err, &failure):
		if failure.Exhausted {
			ClearPendingAuthenticationCookie(w)
		}
		api.WriteError(w, http.StatusUnauthorized, "verification_failed", "verification failed")
	case errors.As(err, &cooling):
		writePasswordRateLimited(w, cooling.retryAfterSeconds)
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
