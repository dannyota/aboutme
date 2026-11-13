package auth

import (
	"errors"
	"net/http"

	"danny.vn/aboutme/apps/server/internal/api"
)

type passwordAcceptedBody struct {
	Accepted bool `json:"accepted"`
}

type passwordSecondFactorRequiredBody struct {
	SecondFactorRequired bool `json:"secondFactorRequired"`
}

func writePasswordSecondFactorRequired(w http.ResponseWriter, pendingRaw string) {
	SetPendingAuthenticationCookie(w, pendingRaw)
	api.WriteData(w, http.StatusAccepted, passwordSecondFactorRequiredBody{SecondFactorRequired: true})
}

func (s *PasswordService) requirePasswordSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, rotated, err := readAndAuthenticateSession(r, s.sessions)
		if err != nil {
			if errors.Is(err, ErrSessionInvalid) {
				writePasswordAuthenticationRequired(w)
				return
			}
			writePasswordUnavailable(w)
			return
		}
		if rotated != "" {
			SetSessionCookie(w, rotated)
		}
		ctx := ContextWithSession(r.Context(), sess)
		ctx = api.WithAccountID(ctx, sess.UserID.String())
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requirePasswordCSRF(allowedOrigin string) api.Middleware {
	return requireCSRF(allowedOrigin, jsonContentTypeAllowed, writePasswordMediaTypeUnsupported, false)
}

func (s *PasswordService) passwordSessionChain(h http.HandlerFunc) http.HandlerFunc {
	wrapped := s.requirePasswordSession(requirePasswordCSRF(s.publicOrigin)(h))
	return wrapped.ServeHTTP
}

func (s *PasswordService) decodeUnauthenticatedBody(w http.ResponseWriter, r *http.Request, fields map[string]*string) bool {
	if !jsonContentTypeAllowed(r.Header) {
		writePasswordMediaTypeUnsupported(w)
		return false
	}
	body, err := readPasswordBody(w, r)
	if err != nil {
		return false
	}
	if err := decodeStrictStringObject(body, fields); err != nil {
		writePasswordRequestInvalid(w)
		return false
	}
	if !originAllowed(r, s.publicOrigin) {
		rejectCSRF(w)
		return false
	}
	return true
}

func (s *PasswordService) decodeAuthedBody(w http.ResponseWriter, r *http.Request, fields map[string]*string) bool {
	body, err := readPasswordBody(w, r)
	if err != nil {
		return false
	}
	if err := decodeStrictStringObject(body, fields); err != nil {
		writePasswordRequestInvalid(w)
		return false
	}
	return true
}

func (s *PasswordService) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req passwordRegisterRequest
	if !s.decodeUnauthenticatedBody(w, r, map[string]*string{
		"name":     &req.Name,
		"email":    &req.Email,
		"password": &req.Password,
	}) {
		return
	}
	if err := s.register(r.Context(), req.Name, req.Email, req.Password, s.clientIPString(r)); err != nil {
		writePasswordError(w, err)
		return
	}
	api.WriteData(w, http.StatusAccepted, passwordAcceptedBody{Accepted: true})
}

func (s *PasswordService) handleVerify(w http.ResponseWriter, r *http.Request) {
	var req passwordVerifyRequest
	if !s.decodeUnauthenticatedBody(w, r, map[string]*string{"token": &req.Token}) {
		return
	}
	if err := s.verify(r.Context(), req.Token, s.clientIPString(r)); err != nil {
		writePasswordError(w, err)
		return
	}
	writeNoContent(w)
}

func (s *PasswordService) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req passwordLoginRequest
	var next string
	if !s.decodeUnauthenticatedBody(w, r, map[string]*string{
		"email":    &req.Email,
		"password": &req.Password,
		"next":     &next,
	}) {
		return
	}
	out, err := s.loginWithSecondFactor(r.Context(), passwordLoginInput{
		Email:           req.Email,
		Password:        req.Password,
		UA:              r.UserAgent(),
		ClientIP:        s.clientIPString(r),
		ReturnPath:      validatedLoginReturnPath(next),
		PreviousPending: previousPendingToken(r),
	})
	if err != nil {
		writePasswordError(w, err)
		return
	}
	if out.PendingRaw != "" {
		writePasswordSecondFactorRequired(w, out.PendingRaw)
		return
	}
	SetSessionCookie(w, out.SessionRaw)
	if _, ok := readPendingCookie(r); ok {
		ClearPendingAuthenticationCookie(w)
	}
	writeNoContent(w)
}

func (s *PasswordService) handleForgot(w http.ResponseWriter, r *http.Request) {
	var req passwordForgotRequest
	if !s.decodeUnauthenticatedBody(w, r, map[string]*string{"email": &req.Email}) {
		return
	}
	if err := s.forgot(r.Context(), req.Email, s.clientIPString(r)); err != nil {
		writePasswordError(w, err)
		return
	}
	api.WriteData(w, http.StatusAccepted, passwordAcceptedBody{Accepted: true})
}

func (s *PasswordService) handleReset(w http.ResponseWriter, r *http.Request) {
	var req passwordResetRequest
	if !s.decodeUnauthenticatedBody(w, r, map[string]*string{
		"token":    &req.Token,
		"password": &req.Password,
	}) {
		return
	}
	if err := s.reset(r.Context(), req.Token, req.Password, s.clientIPString(r)); err != nil {
		writePasswordError(w, err)
		return
	}
	writeNoContent(w)
}

func (s *PasswordService) handleReauth(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFromContext(r.Context())
	if !ok {
		writePasswordAuthenticationRequired(w)
		return
	}
	var req passwordReauthRequest
	if !s.decodeAuthedBody(w, r, map[string]*string{"password": &req.Password}) {
		return
	}
	pendingRaw, err := s.reauthWithSecondFactor(r.Context(), sess, req.Password, s.clientIPString(r), previousPendingToken(r))
	if err != nil {
		writePasswordError(w, err)
		return
	}
	if pendingRaw != "" {
		writePasswordSecondFactorRequired(w, pendingRaw)
		return
	}
	writeNoContent(w)
}

func (s *PasswordService) handleChange(w http.ResponseWriter, r *http.Request) {
	sess, ok := SessionFromContext(r.Context())
	if !ok {
		writePasswordAuthenticationRequired(w)
		return
	}
	var req passwordSetRequest
	if !s.decodeAuthedBody(w, r, map[string]*string{"password": &req.Password}) {
		return
	}
	if err := RequireRecentReauth(sess, s.clock()); err != nil {
		writePasswordReauthRequired(w)
		return
	}
	raw, err := s.change(r.Context(), sess, req.Password, s.clientIPString(r))
	if err != nil {
		writePasswordError(w, err)
		return
	}
	SetSessionCookie(w, raw)
	writeNoContent(w)
}
