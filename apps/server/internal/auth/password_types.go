package auth

// Password authentication HTTP surface (Phase PA). This file owns the route
// paths, the closed request/response vocabulary, the password-only strict JSON
// chain, registration-name normalization, and every wire response the seven
// password operations emit. The D6/D9 numeric budgets and exact bytes live in
// docs/plans/phase-pa/decisions.md and docs/api/openapi.yaml.

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/password"
)

// Password route paths. The mux serves every API route under /api/v1; these
// match the T02 OpenAPI operations (servers.url = .../api/v1).
const (
	PasswordRegisterPath = "/api/v1/auth/password/register"
	PasswordVerifyPath   = "/api/v1/auth/password/verify"
	PasswordLoginPath    = "/api/v1/auth/password/login"
	PasswordForgotPath   = "/api/v1/auth/password/forgot"
	PasswordResetPath    = "/api/v1/auth/password/reset"
	PasswordReauthPath   = "/api/v1/auth/password/reauth"
	PasswordMePath       = "/api/v1/me/password"
)

// passwordMaxBodyBytes is the exact per-route request-body cap (D2/D9). It is
// tighter than the router's default 256 KiB limit and is enforced by the
// password-only strict JSON chain before any policy or storage work.
const passwordMaxBodyBytes = 4096

// Registration and reset token lifetimes (D3), kept exact against migration
// 00008's expiry CHECK constraints.
const (
	passwordRegistrationTTL = 24 * time.Hour
	passwordResetTokenTTL   = 30 * time.Minute
	passwordNotificationTTL = 24 * time.Hour
)

// Closed wire error codes. Only these strings ever leave the password routes.
const (
	passwordCodeRequestInvalid            = "request_invalid"
	passwordCodeCredentialTokenInvalid    = "credential_token_invalid"
	passwordCodeAuthenticationFailed      = "authentication_failed"
	passwordCodeReauthFailed              = "reauth_failed"
	passwordCodeAuthenticationRequired    = "authentication_required"
	passwordCodeReauthRequired            = "reauth_required"
	passwordCodeMediaTypeUnsupported      = "media_type_unsupported"
	passwordCodeBodyTooLarge              = "body_too_large"
	passwordCodePasswordInvalid           = "password_invalid"
	passwordCodeRateLimited               = "rate_limited"
	passwordCodeAuthenticationUnavailable = "authentication_unavailable"
)

// Closed wire messages paired with the codes above.
const (
	passwordMsgRequestInvalid            = "request is malformed"
	passwordMsgCredentialTokenInvalid    = "credential token is invalid"
	passwordMsgAuthenticationFailed      = "authentication failed"
	passwordMsgReauthFailed              = "reauthentication failed"
	passwordMsgAuthenticationRequired    = "authentication required"
	passwordMsgReauthRequired            = "recent reauthentication required"
	passwordMsgMediaTypeUnsupported      = "Content-Type must be application/json"
	passwordMsgBodyTooLarge              = "request body is too large"
	passwordMsgPasswordInvalid           = "password does not meet policy"
	passwordMsgRateLimited               = "too many requests; retry later"
	passwordMsgAuthenticationUnavailable = "authentication is temporarily unavailable"
)

// Closed service sentinels. Handlers map only these to wire responses; raw
// dependency errors never cross the boundary. Password policy and hash errors
// arrive as the password package's own sentinels and are mapped in the service.
var (
	errPasswordEmailInvalid   = errors.New("auth: password email invalid")
	errPasswordNameInvalid    = errors.New("auth: password name invalid")
	errPasswordTokenShape     = errors.New("auth: password token shape invalid")
	errPasswordTokenInvalid   = errors.New("auth: password token invalid")
	errPasswordAuthFailed     = errors.New("auth: password authentication failed")
	errPasswordReauthFailed   = errors.New("auth: password reauthentication failed")
	errPasswordReauthRequired = errors.New("auth: password reauthentication required")
	errPasswordRateLimited    = errors.New("auth: password rate limited")
	errPasswordUnavailable    = errors.New("auth: password unavailable")
)

// passwordPolicyIssueForError maps the password package's closed policy errors
// to the D9 details.issue value. ok is false for any error that is not a policy
// issue.
func passwordPolicyIssueForError(err error) (issue string, ok bool) {
	switch {
	case errors.Is(err, password.ErrPasswordLength):
		return "length", true
	case errors.Is(err, password.ErrPasswordCommon):
		return "common", true
	case errors.Is(err, password.ErrPasswordBreached):
		return "breached", true
	default:
		return "", false
	}
}

// Request bodies. Each password request is one closed JSON object of string
// fields (D2: strict JSON admits exactly one object, rejects duplicate and
// unknown fields and trailing bytes, and checks scalar types before work).

type passwordRegisterRequest struct {
	Name     string
	Email    string
	Password string
}

type passwordVerifyRequest struct {
	Token string
}

type passwordLoginRequest struct {
	Email    string
	Password string
}

type passwordForgotRequest struct {
	Email string
}

type passwordResetRequest struct {
	Token    string
	Password string
}

type passwordReauthRequest struct {
	Password string
}

type passwordSetRequest struct {
	Password string
}

// decodeStrictStringObject decodes body as exactly one JSON object whose fields
// are all JSON strings, copying each into the string target it is mapped to in
// fields. It rejects a non-object top level, duplicate keys, unknown keys,
// non-string (or null) scalar values, and trailing data. A missing key leaves
// its target at the zero value; the caller enforces required-ness.
func decodeStrictStringObject(body []byte, fields map[string]*string) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()

	tok, err := dec.Token()
	if err != nil {
		return errPasswordStrictJSON
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return errPasswordStrictJSON
	}

	seen := make(map[string]bool, len(fields))
	for dec.More() {
		keyTok, keyErr := dec.Token()
		if keyErr != nil {
			return errPasswordStrictJSON
		}
		key, ok := keyTok.(string)
		if !ok {
			return errPasswordStrictJSON
		}
		if seen[key] {
			return errPasswordStrictJSON
		}
		seen[key] = true

		target, ok := fields[key]
		if !ok {
			return errPasswordStrictJSON
		}

		var raw json.RawMessage
		if decodeErr := dec.Decode(&raw); decodeErr != nil {
			return errPasswordStrictJSON
		}
		// A JSON string always begins with '"'; null, numbers, booleans,
		// objects, and arrays do not, so this rejects every wrong scalar shape.
		if len(raw) == 0 || raw[0] != '"' {
			return errPasswordStrictJSON
		}
		if unmarshalErr := json.Unmarshal(raw, target); unmarshalErr != nil {
			return errPasswordStrictJSON
		}
	}

	tok, err = dec.Token()
	if err != nil {
		return errPasswordStrictJSON
	}
	if d, ok := tok.(json.Delim); !ok || d != '}' {
		return errPasswordStrictJSON
	}
	if _, err := dec.Token(); err != io.EOF {
		return errPasswordStrictJSON
	}
	return nil
}

// errPasswordStrictJSON is the closed strict-JSON failure. It maps to 400
// request_invalid; it never carries the offending bytes or a parse detail.
var errPasswordStrictJSON = errors.New("auth: password body is not strict JSON")

// readPasswordBody enforces the 4,096-byte cap independently of the router's
// default body limit. It returns the raw body on success, or the error mapped
// from a too-large body (413) or a read failure (400).
func readPasswordBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	limited := http.MaxBytesReader(w, r.Body, passwordMaxBodyBytes)
	body, err := io.ReadAll(limited)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writePasswordBodyTooLarge(w)
			return nil, errPasswordBodyTooLarge
		}
		writePasswordRequestInvalid(w)
		return nil, errPasswordStrictJSON
	}
	return body, nil
}

// errPasswordBodyTooLarge is the closed sentinel returned by readPasswordBody
// when the body exceeds passwordMaxBodyBytes.
var errPasswordBodyTooLarge = errors.New("auth: password body too large")

// normalizeRegistrationName applies the D1 name bounds: at most 400 raw UTF-8
// bytes, 1–100 Unicode code points after NFC, and no control characters. It is
// not trimmed, collapsed, or case-folded.
func normalizeRegistrationName(raw string) (string, error) {
	if len(raw) > 400 {
		return "", errPasswordNameInvalid
	}
	normalized := norm.NFC.String(raw)
	if n := utf8.RuneCountInString(normalized); n < 1 || n > 100 {
		return "", errPasswordNameInvalid
	}
	if len(normalized) > 400 {
		return "", errPasswordNameInvalid
	}
	for _, r := range normalized {
		if unicode.IsControl(r) {
			return "", errPasswordNameInvalid
		}
	}
	return normalized, nil
}

// ---- response writers ----

// writePasswordError maps a password service sentinel (or password-package
// policy error) to its exact wire response. Unknown errors fall through to the
// opaque 503; they must never reach here in production.
func writePasswordError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errPasswordEmailInvalid),
		errors.Is(err, errPasswordNameInvalid),
		errors.Is(err, errPasswordTokenShape),
		errors.Is(err, errPasswordStrictJSON):
		writePasswordRequestInvalid(w)
	case errors.Is(err, errPasswordTokenInvalid):
		writePasswordTokenInvalid(w)
	case errors.Is(err, errPasswordAuthFailed):
		writePasswordAuthenticationFailed(w)
	case errors.Is(err, errPasswordReauthFailed):
		writePasswordReauthFailed(w)
	case errors.Is(err, errPasswordReauthRequired):
		writePasswordReauthRequired(w)
	case errors.Is(err, errPasswordRateLimited):
		var rl *passwordRateLimitedError
		if errors.As(err, &rl) {
			writePasswordRateLimited(w, rl.retryAfterSeconds)
		} else {
			writePasswordRateLimited(w, 1)
		}
	case errors.Is(err, password.ErrPasswordLength),
		errors.Is(err, password.ErrPasswordCommon),
		errors.Is(err, password.ErrPasswordBreached):
		if issue, ok := passwordPolicyIssueForError(err); ok {
			writePasswordPolicy(w, issue)
		} else {
			writePasswordUnavailable(w)
		}
	default:
		writePasswordUnavailable(w)
	}
}

func writePasswordRequestInvalid(w http.ResponseWriter) {
	api.WriteError(w, http.StatusBadRequest, passwordCodeRequestInvalid, passwordMsgRequestInvalid)
}

func writePasswordTokenInvalid(w http.ResponseWriter) {
	api.WriteError(w, http.StatusBadRequest, passwordCodeCredentialTokenInvalid, passwordMsgCredentialTokenInvalid)
}

func writePasswordAuthenticationFailed(w http.ResponseWriter) {
	api.WriteError(w, http.StatusUnauthorized, passwordCodeAuthenticationFailed, passwordMsgAuthenticationFailed)
}

func writePasswordReauthFailed(w http.ResponseWriter) {
	api.WriteError(w, http.StatusUnauthorized, passwordCodeReauthFailed, passwordMsgReauthFailed)
}

func writePasswordAuthenticationRequired(w http.ResponseWriter) {
	ClearSessionCookie(w)
	api.WriteError(w, http.StatusUnauthorized, passwordCodeAuthenticationRequired, passwordMsgAuthenticationRequired)
}

func writePasswordReauthRequired(w http.ResponseWriter) {
	api.WriteError(w, http.StatusForbidden, passwordCodeReauthRequired, passwordMsgReauthRequired)
}

func writePasswordMediaTypeUnsupported(w http.ResponseWriter) {
	api.WriteError(w, http.StatusUnsupportedMediaType, passwordCodeMediaTypeUnsupported, passwordMsgMediaTypeUnsupported)
}

func writePasswordBodyTooLarge(w http.ResponseWriter) {
	api.WriteError(w, http.StatusRequestEntityTooLarge, passwordCodeBodyTooLarge, passwordMsgBodyTooLarge)
}

func writePasswordRateLimited(w http.ResponseWriter, retryAfterSeconds int) {
	if retryAfterSeconds < 1 {
		retryAfterSeconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))
	api.WriteError(w, http.StatusTooManyRequests, passwordCodeRateLimited, passwordMsgRateLimited)
}

func writePasswordUnavailable(w http.ResponseWriter) {
	api.WriteError(w, http.StatusServiceUnavailable, passwordCodeAuthenticationUnavailable, passwordMsgAuthenticationUnavailable)
}

// passwordPolicyErrorEnvelope writes the 422 password_invalid rejection with the
// closed details.issue (length|common|breached), never the rejected value. The
// field order is fixed by the struct so the body bytes are deterministic.
type passwordPolicyErrorEnvelope struct {
	Error passwordPolicyErrorBody `json:"error"`
}

type passwordPolicyErrorBody struct {
	Code    string                    `json:"code"`
	Message string                    `json:"message"`
	Details passwordPolicyErrorDetail `json:"details"`
}

type passwordPolicyErrorDetail struct {
	Issue string `json:"issue"`
}

func writePasswordPolicy(w http.ResponseWriter, issue string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	if err := json.NewEncoder(w).Encode(passwordPolicyErrorEnvelope{
		Error: passwordPolicyErrorBody{
			Code:    passwordCodePasswordInvalid,
			Message: passwordMsgPasswordInvalid,
			Details: passwordPolicyErrorDetail{Issue: issue},
		},
	}); err != nil {
		// Status and headers are already sent; the client sees a truncated body.
		api.WriteError(w, http.StatusInternalServerError, "internal_error", "an internal error occurred")
	}
}
