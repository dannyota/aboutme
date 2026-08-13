package uatmock

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net/http"
	"net/url"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

const (
	signingKeyID               = "uat-google-key"
	accessTokenLifetimeSeconds = 300
	minPKCEVerifierBytes       = 43
	maxPKCEVerifierBytes       = 128
)

type codeBinding struct {
	clientID      string
	redirectURL   string
	codeChallenge string
	nonce         string
}

type discoveryDocument struct {
	Issuer                            string   `json:"issuer"`
	AuthorizationEndpoint             string   `json:"authorization_endpoint"`
	TokenEndpoint                     string   `json:"token_endpoint"`
	JWKSURI                           string   `json:"jwks_uri"`
	ResponseTypesSupported            []string `json:"response_types_supported"`
	SubjectTypesSupported             []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported  []string `json:"id_token_signing_alg_values_supported"`
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported"`
}

type tokenResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

type oauthError struct {
	Error string `json:"error"`
}

var authorizeTemplate = template.Must(template.New("authorize").Parse(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Local Google sign-in</title></head>
<body>
<main>
  <h1>Choose a local Google account</h1>
  <form method="post" action="` + authorizePath + `">
    {{range .Fields}}<input type="hidden" name="{{.Name}}" value="{{.Value}}">{{end}}
    <fieldset><legend>Google account</legend><label><input type="radio" name="account" value="uat-google-001" checked> Development User — developer@example.invalid</label></fieldset>
    <button type="submit">Continue with Google</button>
  </form>
</main>
</body>
</html>`))

type formField struct {
	Name  string
	Value string
}

func (s *Service) serveDiscovery(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	s.writeJSON(w, http.StatusOK, discoveryDocument{
		Issuer:                            s.cfg.IssuerURL,
		AuthorizationEndpoint:             s.cfg.PublicOrigin + authorizePath,
		TokenEndpoint:                     s.cfg.IssuerURL + "/token",
		JWKSURI:                           s.cfg.IssuerURL + "/jwks.json",
		ResponseTypesSupported:            []string{"code"},
		SubjectTypesSupported:             []string{"public"},
		IDTokenSigningAlgValuesSupported:  []string{string(jose.RS256)},
		TokenEndpointAuthMethodsSupported: []string{"client_secret_post"},
	})
}

func (s *Service) serveJWKS(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	s.writeJSON(w, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       s.key.Public(),
		KeyID:     signingKeyID,
		Algorithm: string(jose.RS256),
		Use:       "sig",
	}}})
}

func (s *Service) serveAuthorize(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		values := r.URL.Query()
		if _, ok := s.authorizeBinding(values); !ok {
			http.Error(w, "invalid authorization request", http.StatusBadRequest)
			return
		}
		fields := make([]formField, 0, len(values))
		for _, name := range []string{"client_id", "redirect_uri", "response_type", "scope", "state", "nonce", "code_challenge", "code_challenge_method"} {
			fields = append(fields, formField{Name: name, Value: values.Get(name)})
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := authorizeTemplate.Execute(w, struct{ Fields []formField }{Fields: fields}); err != nil {
			http.Error(w, "authorization page unavailable", http.StatusInternalServerError)
		}
	case http.MethodPost:
		if !requireFormContentType(w, r) || !s.parseForm(w, r) {
			return
		}
		binding, ok := s.authorizeBinding(r.PostForm)
		if !ok || !oneBoundedValue(r.PostForm, "account") || r.PostForm.Get("account") != googleSubject {
			http.Error(w, "invalid authorization request", http.StatusBadRequest)
			return
		}
		code, err := s.storeCode(binding)
		if err != nil {
			http.Error(w, "authorization unavailable", http.StatusInternalServerError)
			return
		}
		redirect, _ := url.Parse(s.cfg.RedirectURL)
		query := redirect.Query()
		query.Set("code", code)
		query.Set("state", r.PostForm.Get("state"))
		redirect.RawQuery = query.Encode()
		http.Redirect(w, r, redirect.String(), http.StatusFound)
	default:
		w.Header().Set("Allow", http.MethodGet+", "+http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Service) authorizeBinding(values url.Values) (codeBinding, bool) {
	required := []string{"client_id", "redirect_uri", "response_type", "scope", "state", "nonce", "code_challenge", "code_challenge_method"}
	for _, name := range required {
		if !oneBoundedValue(values, name) {
			return codeBinding{}, false
		}
	}
	challenge := values.Get("code_challenge")
	if values.Get("client_id") != s.cfg.ClientID || values.Get("redirect_uri") != s.cfg.RedirectURL || values.Get("response_type") != "code" || values.Get("scope") != "openid profile email" || values.Get("code_challenge_method") != "S256" || !validPKCEChallenge(challenge) {
		return codeBinding{}, false
	}
	return codeBinding{
		clientID:      values.Get("client_id"),
		redirectURL:   values.Get("redirect_uri"),
		codeChallenge: challenge,
		nonce:         values.Get("nonce"),
	}, true
}

func validPKCEChallenge(value string) bool {
	if len(value) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func oneBoundedValue(values url.Values, name string) bool {
	items, ok := values[name]
	return ok && len(items) == 1 && items[0] != "" && len(items[0]) <= maxFieldBytes
}

func (s *Service) storeCode(binding codeBinding) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for range 4 {
		raw := make([]byte, 32)
		if _, err := io.ReadFull(s.cfg.Random, raw); err != nil {
			return "", fmt.Errorf("read code randomness: %w", err)
		}
		code := base64.RawURLEncoding.EncodeToString(raw)
		if _, exists := s.codes[code]; !exists {
			s.codes[code] = binding
			return code, nil
		}
	}
	return "", fmt.Errorf("code collision")
}

func (s *Service) takeCode(code string) (codeBinding, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	binding, ok := s.codes[code]
	if ok {
		delete(s.codes, code)
	}
	return binding, ok
}

func (s *Service) serveToken(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) || !requireFormContentType(w, r) || !s.parseForm(w, r) {
		return
	}
	for _, name := range []string{"grant_type", "code", "redirect_uri", "client_id", "client_secret", "code_verifier"} {
		if !oneBoundedValue(r.PostForm, name) {
			s.writeTokenError(w, "invalid_request")
			return
		}
	}
	if r.PostForm.Get("grant_type") != "authorization_code" {
		s.writeTokenError(w, "invalid_request")
		return
	}
	binding, ok := s.takeCode(r.PostForm.Get("code"))
	if !ok {
		s.writeTokenError(w, "invalid_grant")
		return
	}
	verifier := r.PostForm.Get("code_verifier")
	if !validPKCEVerifier(verifier) {
		s.writeTokenError(w, "invalid_grant")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("client_secret")), []byte(s.cfg.ClientSecret)) != 1 ||
		r.PostForm.Get("client_id") != binding.clientID || r.PostForm.Get("redirect_uri") != binding.redirectURL ||
		oauth2.S256ChallengeFromVerifier(verifier) != binding.codeChallenge {
		s.writeTokenError(w, "invalid_grant")
		return
	}

	idToken, err := s.signIDToken(binding.nonce)
	if err != nil {
		http.Error(w, "token unavailable", http.StatusInternalServerError)
		return
	}
	s.writeJSON(w, http.StatusOK, tokenResponse{
		IDToken:     idToken,
		AccessToken: "uat-google-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   accessTokenLifetimeSeconds,
	})
}

func validPKCEVerifier(value string) bool {
	if len(value) < minPKCEVerifierBytes || len(value) > maxPKCEVerifierBytes {
		return false
	}
	for i := range len(value) {
		char := value[i]
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '.' || char == '_' || char == '~' {
			continue
		}
		return false
	}
	return true
}

func (s *Service) signIDToken(nonce string) (string, error) {
	now := s.cfg.Now()
	payload, err := json.Marshal(map[string]any{
		"iss":            s.cfg.IssuerURL,
		"aud":            s.cfg.ClientID,
		"sub":            googleSubject,
		"email":          googleEmail,
		"email_verified": true,
		"name":           googleName,
		"nonce":          nonce,
		"iat":            now.Unix(),
		"exp":            now.Add(time.Duration(accessTokenLifetimeSeconds) * time.Second).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: s.key}, &jose.SignerOptions{ExtraHeaders: map[jose.HeaderKey]any{jose.HeaderKey("kid"): signingKeyID}})
	if err != nil {
		return "", fmt.Errorf("create signer: %w", err)
	}
	signed, err := signer.Sign(payload)
	if err != nil {
		return "", fmt.Errorf("sign claims: %w", err)
	}
	compact, err := signed.CompactSerialize()
	if err != nil {
		return "", fmt.Errorf("serialize token: %w", err)
	}
	return compact, nil
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	return false
}

func requireFormContentType(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/x-www-form-urlencoded" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return false
	}
	return true
}

func (s *Service) parseForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBytes)
	if err := r.ParseForm(); err != nil {
		s.writeTokenError(w, "invalid_request")
		return false
	}
	return true
}

func (s *Service) writeTokenError(w http.ResponseWriter, errorCode string) {
	s.writeJSON(w, http.StatusBadRequest, oauthError{Error: errorCode})
}

func (s *Service) writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
