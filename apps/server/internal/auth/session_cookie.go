package auth

import "net/http"

var sessionCookieMaxAge = int(absoluteTimeout.Seconds())

const pendingAuthenticationCookieName = "__Host-auth-pending"

var pendingAuthenticationCookieMaxAge = int(pendingAuthenticationLifetime.Seconds())

func SetSessionCookie(w http.ResponseWriter, rawToken string) {
	http.SetCookie(w, sessionCookie(rawToken, sessionCookieMaxAge))
}

func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, sessionCookie("", -1))
}

func SetPendingAuthenticationCookie(w http.ResponseWriter, rawToken string) {
	http.SetCookie(w, pendingAuthenticationCookie(rawToken, pendingAuthenticationCookieMaxAge))
}

func ClearPendingAuthenticationCookie(w http.ResponseWriter) {
	http.SetCookie(w, pendingAuthenticationCookie("", -1))
}

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

func pendingAuthenticationCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     pendingAuthenticationCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	}
}
