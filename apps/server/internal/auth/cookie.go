package auth

import (
	"errors"
	"net/http"
)

// OAuthTxCookieName carries the raw OAuth transaction handle between the
// authorization start and callback. Its __Host- prefix requires Secure,
// Path=/, and no Domain attribute.
const OAuthTxCookieName = "__Host-oauth-tx"

// oauthTxCookieMaxAge keeps the cookie lifetime aligned with the database row.
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

// ClearOAuthTxCookie deletes the transaction cookie. Callback handlers must
// call it on every exit path so a consumed or invalid handle does not linger.
func ClearOAuthTxCookie(w http.ResponseWriter) {
	http.SetCookie(w, oauthTxCookie("", -1))
}

// oauthTxCookie keeps the attributes used to set and clear the cookie identical.
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
