package auth

import "net/http"

// sessionCookieMaxAge keeps the cookie lifetime aligned with absoluteTimeout.
var sessionCookieMaxAge = int(absoluteTimeout.Seconds())

// SetSessionCookie sets the __Host-session cookie to rawToken --
// SessionManager.Issue's (or a rotation's) returned raw bearer token,
// never the hash stored in the database.
func SetSessionCookie(w http.ResponseWriter, rawToken string) {
	http.SetCookie(w, sessionCookie(rawToken, sessionCookieMaxAge))
}

// ClearSessionCookie deletes the session cookie with matching attributes.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, sessionCookie("", -1))
}

// sessionCookie keeps the attributes used to set and clear the cookie identical.
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
