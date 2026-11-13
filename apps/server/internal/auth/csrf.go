package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"mime"
	"net/http"
	"net/url"
	"strings"

	"danny.vn/aboutme/apps/server/internal/api"
)

const CSRFHeaderName = "X-CSRF-Token"

const csrfRejectedCode = "csrf_rejected"

const csrfRejectedMessage = "CSRF validation failed"

func RequireCSRF(allowedOrigin string) api.Middleware {
	return requireCSRF(allowedOrigin, jsonContentTypeAllowed, rejectJSONMediaType, false)
}

func RequireExactJSONCSRF(allowedOrigin string) api.Middleware {
	return requireCSRF(allowedOrigin, exactJSONContentTypeAllowed, rejectExactJSONMediaType, false)
}

func RequireCSRFMultipart(allowedOrigin string) api.Middleware {
	return requireCSRF(allowedOrigin, multipartContentTypeAllowed, rejectMultipartMediaType, true)
}

func requireCSRF(allowedOrigin string, mediaTypeAllowed func(http.Header) bool,
	rejectMediaType func(http.ResponseWriter), requireMediaType bool,
) api.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isMutatingMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			if !originAllowed(r, allowedOrigin) {
				rejectCSRF(w)
				return
			}

			sess, ok := SessionFromContext(r.Context())
			if !ok {
				rejectCSRF(w)
				return
			}

			token, tokenOK := singletonSecurityHeader(r.Header, CSRFHeaderName)
			if !tokenOK || !validCSRFToken(token, sess.CSRFSecret) {
				rejectCSRF(w)
				return
			}

			if (requireMediaType || hasBody(r)) && !mediaTypeAllowed(r.Header) {
				rejectMediaType(w)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func rejectMultipartMediaType(w http.ResponseWriter) {
	api.WriteError(w, http.StatusUnsupportedMediaType, "media_type_unsupported", "multipart/form-data is required")
}

func rejectJSONMediaType(w http.ResponseWriter) {
	api.WriteError(w, http.StatusBadRequest, "request_invalid", "Content-Type must be one application/json value")
}

func rejectExactJSONMediaType(w http.ResponseWriter) {
	api.WriteError(w, http.StatusUnsupportedMediaType, "media_type_unsupported", "Content-Type must be application/json")
}

func rejectCSRF(w http.ResponseWriter) {
	api.WriteError(w, http.StatusForbidden, csrfRejectedCode, csrfRejectedMessage)
}

func isMutatingMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

func originAllowed(r *http.Request, allowedOrigin string) bool {
	origin, originPresent, originOK := optionalSingletonSecurityHeader(r.Header, "Origin")
	if originPresent {
		if !originOK {
			return false
		}
		return origin == allowedOrigin
	}

	referer, refererOK := singletonSecurityHeader(r.Header, "Referer")
	if !refererOK {
		return false
	}
	u, err := url.Parse(referer)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	return u.Scheme+"://"+u.Host == allowedOrigin
}

func singletonSecurityHeader(header http.Header, name string) (string, bool) {
	value, present, ok := optionalSingletonSecurityHeader(header, name)
	return value, present && ok
}

func optionalSingletonSecurityHeader(header http.Header, name string) (string, bool, bool) {
	values := header.Values(name)
	if len(values) == 0 {
		return "", false, true
	}
	if len(values) != 1 || values[0] == "" || strings.Contains(values[0], ",") {
		return "", true, false
	}
	return values[0], true, true
}

func hasBody(r *http.Request) bool {
	return r.ContentLength != 0 || len(r.TransferEncoding) > 0
}

func jsonContentTypeAllowed(header http.Header) bool {
	values := header.Values("Content-Type")
	if len(values) != 1 {
		return false
	}
	ct := values[0]
	if ct == "" {
		return false
	}
	mediaType, params, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		return false
	}
	delete(params, "charset")
	return len(params) == 0
}

func exactJSONContentTypeAllowed(header http.Header) bool {
	values := header.Values("Content-Type")
	return len(values) == 1 && values[0] == "application/json"
}

func multipartContentTypeAllowed(header http.Header) bool {
	values := header.Values("Content-Type")
	if len(values) != 1 {
		return false
	}
	mediaType, params, err := mime.ParseMediaType(values[0])
	if err != nil || mediaType != "multipart/form-data" || len(params) != 1 {
		return false
	}
	return params["boundary"] != ""
}

func validCSRFToken(headerToken string, secret []byte) bool {
	if headerToken == "" || len(secret) == 0 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(headerToken)
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(decoded, secret) == 1
}
