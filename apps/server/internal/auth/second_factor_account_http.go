package auth

import (
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

type (
	// secondFactorPasskeyBody is one passkey in a second-factor response.
	secondFactorPasskeyBody struct {
		// ID is the passkey's UUID.
		ID string `json:"id"`
		// CreatedAt is when the passkey was registered, RFC 3339 in UTC.
		CreatedAt string `json:"createdAt"`
		// LastUsedAt is the passkey's last successful use, RFC 3339 in UTC;
		// it is null until first use.
		LastUsedAt *string `json:"lastUsedAt"`
	}
	// secondFactorStateBody is the account's second-factor state response.
	secondFactorStateBody struct {
		// Enabled reports whether the account has any active factor.
		Enabled bool `json:"enabled"`
		// Passkeys lists the active passkeys ordered by creation time, then
		// ID; it is an empty array, never null, without passkeys.
		Passkeys []secondFactorPasskeyBody `json:"passkeys"`
		// TOTPEnabled reports whether the account has an active TOTP
		// credential.
		TOTPEnabled bool `json:"totpEnabled"`
		// RecoveryCodesRemaining is the number of unused recovery codes,
		// 0 to 10.
		RecoveryCodesRemaining int `json:"recoveryCodesRemaining"`
	}
	// secondFactorRegistrationBody is the passkey registration response.
	secondFactorRegistrationBody struct {
		// Passkey is the newly registered passkey.
		Passkey secondFactorPasskeyBody `json:"passkey"`
		// RecoveryCodes holds ten new codes only on first enrollment; it is
		// omitted otherwise.
		RecoveryCodes []string `json:"recoveryCodes,omitempty"`
	}
	// secondFactorRecoveryCodesBody is the recovery-code regeneration
	// response.
	secondFactorRecoveryCodesBody struct {
		// RecoveryCodes holds the ten new codes that replace every prior
		// code.
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
	if h.totp != nil {
		if body.TOTPEnabled, err = h.totp.TOTPState(r.Context(), sess.UserID); err != nil {
			h.writeAccountError(w, err)
			return
		}
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
	sess, _, ok := h.admitAccountMutation(w, r, secondFactorWebAuthnBodyBytes, decodeEmptyObject, true)
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
	sess, credential, ok := h.admitAccountMutation(w, r, secondFactorWebAuthnBodyBytes, h.service.DecodePasskeyRegistration, true)
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
	sess, _, ok := h.admitAccountMutation(w, r, secondFactorWebAuthnBodyBytes, nil, false)
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
	sess, _, ok := h.admitAccountMutation(w, r, secondFactorWebAuthnBodyBytes, decodeEmptyObject, false)
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

func passkeyBody(passkey SecondFactorPasskey) secondFactorPasskeyBody {
	body := secondFactorPasskeyBody{ID: passkey.ID.String(), CreatedAt: formatSecondFactorTime(passkey.CreatedAt)}
	if passkey.LastUsedAt != nil {
		lastUsed := formatSecondFactorTime(*passkey.LastUsedAt)
		body.LastUsedAt = &lastUsed
	}
	return body
}
