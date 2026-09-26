package uatmock

// The LinkedIn mode follows LinkedIn's documented confidential web flow so the
// native HTTPS proof exercises the same requests production sends: no PKCE,
// client credentials only in the token request form body, optional email
// claims, no nonce claim in the ID token, pairwise-looking subjects, and
// LinkedIn's two cancel errors. See docs/design/linkedin-sign-in.md.

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v4"
)

const (
	linkedinDiscoveryPath = "/linkedin/.well-known/openid-configuration"
	linkedinJWKSPath      = "/linkedin/jwks.json"
	linkedinTokenPath     = "/linkedin/token"
	linkedinAuthorizePath = "/__uat/oauth/linkedin/authorize"
	linkedinCallbackPath  = "/api/v1/auth/linkedin/callback"
	linkedinSigningKeyID  = "uat-linkedin-key"
)

// linkedinAccount is one local LinkedIn member. A nil EmailVerified omits the
// claim; an empty Email omits both email claims, as LinkedIn may.
type linkedinAccount struct {
	Subject       string
	Name          string
	Email         string
	EmailVerified *bool
}

// Label is the text beside the account's radio button.
func (a linkedinAccount) Label() string {
	if a.Email == "" {
		return a.Name + " (no email)"
	}
	return a.Name + " (" + a.Email + ")"
}

func verified(v bool) *bool { return &v }

// linkedinAccounts are the local members, the default first. They cover a new
// member, both missing-verification shapes, an email a password account holds,
// and a member for linking.
var linkedinAccounts = []linkedinAccount{
	{Subject: "lnkd-Q7x2mP4tVa", Name: "LinkedIn Verified", Email: "li-verified@example.invalid", EmailVerified: verified(true)},
	{Subject: "lnkd-N3v8cR1sKe", Name: "LinkedIn No Email"},
	{Subject: "lnkd-U5b9hW2yLo", Name: "LinkedIn Unverified", Email: "li-unverified@example.invalid", EmailVerified: verified(false)},
	{Subject: "lnkd-C2k6jT8fMu", Name: "LinkedIn Collision", Email: "li-collision@example.invalid", EmailVerified: verified(true)},
	{Subject: "lnkd-L4r1dZ7gNi", Name: "LinkedIn Link", Email: "li-link@example.invalid", EmailVerified: verified(true)},
}

func linkedinAccountBySubject(subject string) (linkedinAccount, bool) {
	for _, acct := range linkedinAccounts {
		if acct.Subject == subject {
			return acct, true
		}
	}
	return linkedinAccount{}, false
}

// linkedinCancelErrors maps the form's cancel actions to LinkedIn's callback
// error values and descriptions.
var linkedinCancelErrors = map[string][2]string{
	"cancel_login":     {"user_cancelled_login", "The user cancelled LinkedIn login"},        //nolint:misspell // LinkedIn's exact wire values.
	"cancel_authorize": {"user_cancelled_authorize", "The user cancelled the authorization"}, //nolint:misspell // LinkedIn's exact wire values.
}

// linkedinAuthorizeFields are the authorize parameters the server sends, all
// required. LinkedIn accepts nonce but never returns it. PKCE parameters are
// refused rather than ignored.
var linkedinAuthorizeFields = []string{"client_id", "redirect_uri", "response_type", "scope", "state", "nonce"}

// linkedinTokenFields are the documented token request form parameters.
var linkedinTokenFields = []string{"grant_type", "code", "redirect_uri", "client_id", "client_secret"}

type linkedinBinding struct {
	account linkedinAccount
}

// linkedinMock holds the LinkedIn mode's credentials and single-use codes.
type linkedinMock struct {
	svc          *Service
	issuerURL    string
	redirectURL  string
	clientID     string
	clientSecret string

	mu    sync.Mutex
	codes map[string]linkedinBinding
}

var linkedinAuthorizeTemplate = template.Must(template.New("linkedin-authorize").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Local LinkedIn sign-in</title></head>
<body>
<main>
  <h1>Choose a local LinkedIn account</h1>
  <form method="post" action="` + linkedinAuthorizePath + `">
    {{range .Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}
    <fieldset><legend>LinkedIn account</legend>{{range $i, $a := .Accounts}}<label><input type="radio" name="account" value="{{$a.Subject}}"{{if eq $i 0}} checked{{end}}> {{$a.Label}}</label>{{end}}</fieldset>
    <button type="submit" name="action" value="allow">Allow</button>
    <button type="submit" name="action" value="cancel_login">Cancel sign-in</button>
    <button type="submit" name="action" value="cancel_authorize">Cancel authorization</button>
  </form>
</main>
</body>
</html>`))

func newLinkedInMock(svc *Service, cfg Config) *linkedinMock {
	return &linkedinMock{
		svc:          svc,
		issuerURL:    strings.TrimSuffix(cfg.IssuerURL, "/google") + "/linkedin",
		redirectURL:  strings.TrimSuffix(cfg.PublicOrigin, "/") + linkedinCallbackPath,
		clientID:     cfg.LinkedInClientID,
		clientSecret: cfg.LinkedInClientSecret,
		codes:        make(map[string]linkedinBinding),
	}
}

func (l *linkedinMock) register(mux *http.ServeMux) {
	mux.HandleFunc(linkedinDiscoveryPath, l.serveDiscovery)
	mux.HandleFunc(linkedinJWKSPath, l.serveJWKS)
	mux.HandleFunc(linkedinTokenPath, l.serveToken)
	mux.HandleFunc(linkedinAuthorizePath, l.serveAuthorize)
}

func (l *linkedinMock) serveDiscovery(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	l.svc.writeJSON(w, http.StatusOK, discoveryDocument{
		Issuer:                           l.issuerURL,
		AuthorizationEndpoint:            l.svc.cfg.PublicOrigin + linkedinAuthorizePath,
		TokenEndpoint:                    l.issuerURL + "/token",
		JWKSURI:                          l.issuerURL + "/jwks.json",
		ResponseTypesSupported:           []string{"code"},
		SubjectTypesSupported:            []string{"pairwise"},
		IDTokenSigningAlgValuesSupported: []string{string(jose.RS256)},
	})
}

func (l *linkedinMock) serveJWKS(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	l.svc.writeJSON(w, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       l.svc.key.Public(),
		KeyID:     linkedinSigningKeyID,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}}})
}

func (l *linkedinMock) serveAuthorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		values := r.URL.Query()
		if !l.validAuthorize(values) {
			http.Error(w, "invalid authorization request", http.StatusBadRequest)
			return
		}
		fields := make([]formField, 0, len(linkedinAuthorizeFields))
		for _, name := range linkedinAuthorizeFields {
			fields = append(fields, formField{Name: name, Value: values.Get(name)})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := linkedinAuthorizeTemplate.Execute(w, struct {
			Fields   []formField
			Accounts []linkedinAccount
		}{Fields: fields, Accounts: linkedinAccounts}); err != nil {
			http.Error(w, "authorization page unavailable", http.StatusInternalServerError)
		}
	case http.MethodPost:
		if !requireFormContentType(w, r) || !l.svc.parseForm(w, r) {
			return
		}
		form := r.PostForm
		if !l.validAuthorize(form) || !oneBoundedValue(form, "action") {
			http.Error(w, "invalid authorization request", http.StatusBadRequest)
			return
		}
		redirect, err := url.Parse(l.redirectURL)
		if err != nil {
			http.Error(w, "authorization unavailable", http.StatusInternalServerError)
			return
		}
		query := redirect.Query()
		action := form.Get("action")
		if cancel, ok := linkedinCancelErrors[action]; ok {
			query.Set("error", cancel[0])
			query.Set("error_description", cancel[1])
		} else {
			if action != "allow" || !oneBoundedValue(form, "account") {
				http.Error(w, "invalid authorization request", http.StatusBadRequest)
				return
			}
			selected, found := linkedinAccountBySubject(form.Get("account"))
			if !found {
				http.Error(w, "invalid authorization request", http.StatusBadRequest)
				return
			}
			code, storeErr := l.storeCode(linkedinBinding{account: selected})
			if storeErr != nil {
				http.Error(w, "authorization unavailable", http.StatusInternalServerError)
				return
			}
			query.Set("code", code)
		}
		query.Set("state", form.Get("state"))
		redirect.RawQuery = query.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// validAuthorize accepts exactly the documented parameters with this client's
// values, and refuses any PKCE parameter so a proof fails if one is sent.
func (l *linkedinMock) validAuthorize(values url.Values) bool {
	for _, name := range linkedinAuthorizeFields {
		if !oneBoundedValue(values, name) {
			return false
		}
	}
	if values.Has("code_challenge") || values.Has("code_challenge_method") {
		return false
	}
	return values.Get("client_id") == l.clientID && values.Get("redirect_uri") == l.redirectURL &&
		values.Get("response_type") == "code" && values.Get("scope") == "openid profile email"
}

func (l *linkedinMock) storeCode(binding linkedinBinding) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for range 4 {
		code, err := randomCode(l.svc.cfg.Random)
		if err != nil {
			return "", err
		}
		if _, exists := l.codes[code]; !exists {
			l.codes[code] = binding
			return code, nil
		}
	}
	return "", fmt.Errorf("code collision")
}

func (l *linkedinMock) takeCode(code string) (linkedinBinding, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	binding, ok := l.codes[code]
	if ok {
		delete(l.codes, code)
	}
	return binding, ok
}

// serveToken authenticates the client only through form parameters. An
// Authorization header or a code_verifier gets 401 invalid_client before the
// code is read, as a report of LinkedIn's live behavior describes.
func (l *linkedinMock) serveToken(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) || !requireFormContentType(w, r) || !l.svc.parseForm(w, r) {
		return
	}
	if r.Header.Get("Authorization") != "" || r.PostForm.Has("code_verifier") || r.URL.Query().Has("code_verifier") {
		l.svc.writeOAuthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	for _, name := range linkedinTokenFields {
		if !oneBoundedValue(r.PostForm, name) {
			l.svc.writeTokenError(w, "invalid_request")
			return
		}
	}
	if r.PostForm.Get("client_id") != l.clientID ||
		subtle.ConstantTimeCompare([]byte(r.PostForm.Get("client_secret")), []byte(l.clientSecret)) != 1 {
		l.svc.writeOAuthError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		l.svc.writeTokenError(w, "invalid_request")
		return
	}
	binding, ok := l.takeCode(r.PostForm.Get("code"))
	if !ok || r.PostForm.Get("redirect_uri") != l.redirectURL {
		l.svc.writeTokenError(w, "invalid_grant")
		return
	}

	idToken, err := l.signIDToken(binding)
	if err != nil {
		http.Error(w, "token unavailable", http.StatusInternalServerError)
		return
	}
	l.svc.writeJSON(w, http.StatusOK, tokenResponse{
		IDToken:     idToken,
		AccessToken: "uat-linkedin-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   accessTokenLifetimeSeconds,
	})
}

// signIDToken leaves out the nonce claim, as LinkedIn does even when the
// authorize request carried a nonce.
func (l *linkedinMock) signIDToken(binding linkedinBinding) (string, error) {
	now := l.svc.cfg.Now()
	claims := map[string]any{
		"iss":  l.issuerURL,
		"aud":  l.clientID,
		"sub":  binding.account.Subject,
		"name": binding.account.Name,
		"iat":  now.Unix(),
		"exp":  now.Add(time.Duration(accessTokenLifetimeSeconds) * time.Second).Unix(),
	}
	if binding.account.Email != "" {
		claims["email"] = binding.account.Email
		if binding.account.EmailVerified != nil {
			// LinkedIn's ID token sends email_verified as the JSON string
			// "true"/"false", not a JSON boolean.
			claims["email_verified"] = strconv.FormatBool(*binding.account.EmailVerified)
		}
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}
	return l.svc.sign(payload, linkedinSigningKeyID)
}
