package auth

// Password HTTP handlers and their route chains. Unauthenticated operations run
// media-type (415) -> body (413) -> strict JSON (400) -> exact Origin (403)
// before any policy, hash, HIBP, or storage work. Authenticated operations run
// the password session (401) -> password CSRF (403/415) -> strict JSON (400)
// chain first. Every handler maps only the closed service sentinels to wire
// responses; raw dependency errors never cross the boundary.

import (
	"errors"
	"net/http"

	"github.com/dannyota/aboutme/apps/server/internal/api"
)

// passwordAcceptedBody is the fixed 202 body shared by register and forgot.
type passwordAcceptedBody struct {
	Accepted bool `json:"accepted"`
}

// requirePasswordSession authenticates the session and stores it in context,
// returning the password 401 (authentication_required) — distinct from the OAuth
// session routes' session_required — and delivering any rotated cookie.
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

// requirePasswordCSRF is RequireCSRF with the password media-type rejection
// (415 media_type_unsupported instead of the OAuth/resume 400). Origin and
// synchronizer-token failures keep the existing 403 csrf_rejected.
func requirePasswordCSRF(allowedOrigin string) api.Middleware {
	return requireCSRF(allowedOrigin, jsonContentTypeAllowed, writePasswordMediaTypeUnsupported, false)
}

// passwordSessionChain authenticates before CSRF enforcement, matching the OAuth
// sessionChain ordering for the two authenticated password routes.
func (s *PasswordService) passwordSessionChain(h http.HandlerFunc) http.HandlerFunc {
	wrapped := s.requirePasswordSession(requirePasswordCSRF(s.publicOrigin)(h))
	return wrapped.ServeHTTP
}

// decodeUnauthenticatedBody runs the unauthenticated password route chain:
// media type, body cap, strict JSON, then exact Origin. On any failure it writes
// the response and returns false.
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

// decodeAuthedBody runs the strict JSON body decode after requirePasswordCSRF
// has already checked media type. On any failure it writes the response and
// returns false.
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
	if !s.decodeUnauthenticatedBody(w, r, map[string]*string{
		"email":    &req.Email,
		"password": &req.Password,
	}) {
		return
	}
	raw, err := s.login(r.Context(), req.Email, req.Password, r.UserAgent(), s.clientIPString(r))
	if err != nil {
		writePasswordError(w, err)
		return
	}
	SetSessionCookie(w, raw)
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
	if err := s.reauth(r.Context(), sess, req.Password, s.clientIPString(r)); err != nil {
		writePasswordError(w, err)
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
