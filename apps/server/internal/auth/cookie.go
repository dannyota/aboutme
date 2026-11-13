package auth

import (
	"errors"
	"net/http"
)

const OAuthTxCookieName = "__Host-oauth-tx"

var oauthTxCookieMaxAge = int(oauthTxTTL.Seconds())

var ErrOAuthTxCookieMissing = errors.New("auth: oauth transaction cookie missing")

func SetOAuthTxCookie(w http.ResponseWriter, handle string) {
	http.SetCookie(w, oauthTxCookie(handle, oauthTxCookieMaxAge))
}

func ReadOAuthTxCookie(r *http.Request) (string, error) {
	c, err := r.Cookie(OAuthTxCookieName)
	if err != nil {
		return "", ErrOAuthTxCookieMissing
	}
	return c.Value, nil
}

func ClearOAuthTxCookie(w http.ResponseWriter) {
	http.SetCookie(w, oauthTxCookie("", -1))
}

func oauthTxCookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     OAuthTxCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		Secure:   true,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
}
