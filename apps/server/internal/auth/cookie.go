package auth

import (
	"errors"
	"net/http"
)

// OAuthTxCookieName is the __Host- prefixed cookie carrying the raw OAuth
// transaction handle (see transaction.go's TransactionStore.Begin)
// between /authorize and /callback. The __Host- prefix is enforced by
// every browser that implements it: it requires Secure, no Domain
// attribute, and Path=/ -- all set below -- so the cookie can never be
// set by, or sent to, anything other than this exact host over HTTPS.
const OAuthTxCookieName = "__Host-oauth-tx"

// oauthTxCookieMaxAge is oauthTxTTL expressed in the whole seconds
// http.Cookie.MaxAge wants (600), computed from oauthTxTTL rather than
// hardcoded so the cookie and the database row it points at can never
// drift out of sync with each other.
var oauthTxCookieMaxAge = int(oauthTxTTL.Seconds())

// ErrOAuthTxCookieMissing is returned by ReadOAuthTxCookie when the
// request carries no OAuthTxCookieName cookie.
var ErrOAuthTxCookieMissing = errors.New("auth: oauth transaction cookie missing")

// SetOAuthTxCookie sets the __Host-oauth-tx cookie to handle --
// TransactionStore.Begin's returned raw handle, never the hash stored in
// the database.
func SetOAuthTxCookie(w http.ResponseWriter, handle string) {
	http.SetCookie(w, oauthTxCookie(handle, oauthTxCookieMaxAge))
}

// ReadOAuthTxCookie returns the raw handle carried by the
// __Host-oauth-tx cookie, or ErrOAuthTxCookieMissing when the request
// carries none.
func ReadOAuthTxCookie(r *http.Request) (string, error) {
	c, err := r.Cookie(OAuthTxCookieName)
	if err != nil {
		return "", ErrOAuthTxCookieMissing
	}
	return c.Value, nil
}

// ClearOAuthTxCookie deletes the __Host-oauth-tx cookie by re-setting it
// with an empty value and a negative Max-Age, matching every attribute
// SetOAuthTxCookie sets: a browser only overwrites/deletes a cookie when
// its Path (and, for a __Host- prefixed name, the implicit no-Domain and
// Secure) attributes match exactly. Call this on both the success and
// failure paths of /callback so a consumed or dead transaction cookie
// never lingers in the browser.
func ClearOAuthTxCookie(w http.ResponseWriter) {
	http.SetCookie(w, oauthTxCookie("", -1))
}

// oauthTxCookie builds the __Host-oauth-tx cookie shared by
// SetOAuthTxCookie and ClearOAuthTxCookie, so every attribute that must
// match between setting and clearing it (Path, Secure, HttpOnly,
// SameSite) is written in exactly one place.
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
