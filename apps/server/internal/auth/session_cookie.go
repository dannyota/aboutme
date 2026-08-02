package auth

import "net/http"

// sessionCookieMaxAge is absoluteTimeout expressed in the whole seconds
// http.Cookie.MaxAge wants, computed from absoluteTimeout (session.go)
// rather than hardcoded so the cookie's own lifetime can never drift out
// of sync with the session's actual absolute expiry.
var sessionCookieMaxAge = int(absoluteTimeout.Seconds())

// SetSessionCookie sets the __Host-session cookie to rawToken --
// SessionManager.Issue's (or a rotation's) returned raw bearer token,
// never the hash stored in the database.
func SetSessionCookie(w http.ResponseWriter, rawToken string) {
	http.SetCookie(w, sessionCookie(rawToken, sessionCookieMaxAge))
}

// ClearSessionCookie deletes the __Host-session cookie by re-setting it
// with an empty value and a negative Max-Age, matching every attribute
// SetSessionCookie sets -- see cookie.go's ClearOAuthTxCookie for the same
// reasoning: a browser only overwrites/deletes a cookie when its Path
// (and, for a __Host- prefixed name, the implicit no-Domain and Secure)
// attributes match exactly.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, sessionCookie("", -1))
}

// sessionCookie builds the __Host-session cookie shared by
// SetSessionCookie and ClearSessionCookie, so every attribute that must
// match between setting and clearing it (Path, Secure, HttpOnly,
// SameSite) is written in exactly one place -- the same single-builder
// pattern cookie.go's oauthTxCookie already uses for __Host-oauth-tx.
func sessionCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}
