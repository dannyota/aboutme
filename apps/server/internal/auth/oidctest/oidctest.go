package oidctest

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

const DefaultClientID = "test-client"

const signingKeyID = "oidctest-key"

type Claims struct {
	Subject string

	Email string

	EmailVerified *bool

	EmailVerifiedRaw any

	Nonce string

	Audience string

	Issuer string

	ExpiresAt time.Time

	SigningKey *rsa.PrivateKey

	CodeChallenge string
}

type Provider struct {
	URL string

	ClientID string

	t   *testing.T
	key *rsa.PrivateKey

	mu                    sync.Mutex
	codes                 map[string]Claims
	discoveryBlock        func()
	stalls                map[string]*stall
	lastTokenRedirectURI  string
	lastTokenRedirectSeen bool
	lastTokenRequest      TokenRequest
	tokenRequestCount     int
}

type TokenRequest struct {
	Form url.Values

	Authorization string
}

func (p *Provider) LastTokenRequest() (req TokenRequest, count int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastTokenRequest, p.tokenRequestCount
}

func (p *Provider) LastTokenRedirectURI() (redirectURI string, seen bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastTokenRedirectURI, p.lastTokenRedirectSeen
}

func NewProvider(t *testing.T) *Provider {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("oidctest: generating RSA key: %v", err)
	}

	p := &Provider{
		ClientID: DefaultClientID,
		t:        t,
		key:      key,
		codes:    make(map[string]Claims),
		stalls:   make(map[string]*stall),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", p.serveDiscovery)
	mux.HandleFunc("/jwks.json", p.serveJWKS)
	mux.HandleFunc("/token", p.serveToken)

	server := httptest.NewServer(p.serveStalls(mux))
	t.Cleanup(server.Close)

	p.URL = server.URL
	return p
}

func (p *Provider) RegisterCode(code string, claims Claims) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.codes[code] = claims
}

func (p *Provider) takeCode(code string) (Claims, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	claims, ok := p.codes[code]
	if ok {
		delete(p.codes, code)
	}
	return claims, ok
}

type discoveryDocument struct {
	Issuer                           string   `json:"issuer"`
	AuthorizationEndpoint            string   `json:"authorization_endpoint"`
	TokenEndpoint                    string   `json:"token_endpoint"`
	JWKSURI                          string   `json:"jwks_uri"`
	ResponseTypesSupported           []string `json:"response_types_supported"`
	SubjectTypesSupported            []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
}

func (p *Provider) BlockDiscoveryForTest() (entered <-chan struct{}, release func()) {
	enteredCh := make(chan struct{})
	releaseCh := make(chan struct{})
	var enteredOnce, releaseOnce sync.Once

	p.mu.Lock()
	p.discoveryBlock = func() {
		enteredOnce.Do(func() { close(enteredCh) })
		<-releaseCh
	}
	p.mu.Unlock()

	return enteredCh, func() { releaseOnce.Do(func() { close(releaseCh) }) }
}

type stall struct {
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	releaseOnce sync.Once
}

func (p *Provider) StallForTest(path string) (entered <-chan struct{}, release func()) {
	s := &stall{entered: make(chan struct{}), release: make(chan struct{})}
	release = func() { s.releaseOnce.Do(func() { close(s.release) }) }

	p.mu.Lock()
	p.stalls[path] = s
	p.mu.Unlock()
	p.t.Cleanup(release)

	return s.entered, release
}

func (p *Provider) serveStalls(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		s := p.stalls[r.URL.Path]
		p.mu.Unlock()
		if s == nil {
			next.ServeHTTP(w, r)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "oidctest: reading stalled request body", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		s.enteredOnce.Do(func() { close(s.entered) })
		select {
		case <-s.release:
			next.ServeHTTP(w, r)
		case <-r.Context().Done():
		}
	})
}

func (p *Provider) serveDiscovery(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	block := p.discoveryBlock
	p.mu.Unlock()
	if block != nil {
		block()
	}

	doc := discoveryDocument{
		Issuer:                           p.URL,
		AuthorizationEndpoint:            p.URL + "/authorize",
		TokenEndpoint:                    p.URL + "/token",
		JWKSURI:                          p.URL + "/jwks.json",
		ResponseTypesSupported:           []string{"code"},
		SubjectTypesSupported:            []string{"public"},
		IDTokenSigningAlgValuesSupported: []string{string(jose.RS256)},
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(doc); err != nil {
		p.t.Errorf("oidctest: encoding discovery document: %v", err)
	}
}

func (p *Provider) serveJWKS(w http.ResponseWriter, _ *http.Request) {
	set := jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{
			{
				Key:       p.key.Public(),
				KeyID:     signingKeyID,
				Algorithm: string(jose.RS256),
				Use:       "sig",
			},
		},
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(set); err != nil {
		p.t.Errorf("oidctest: encoding jwks: %v", err)
	}
}

type tokenResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

type tokenErrorResponse struct {
	Error string `json:"error"`
}

func (p *Provider) writeTokenError(w http.ResponseWriter, status int, errorCode string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(tokenErrorResponse{Error: errorCode}); err != nil {
		p.t.Errorf("oidctest: encoding token error response: %v", err)
	}
}

const accessTokenExpiresInSeconds = 3600

func (p *Provider) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		p.writeTokenError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	p.mu.Lock()
	p.lastTokenRedirectURI = r.PostFormValue("redirect_uri")
	p.lastTokenRedirectSeen = true
	p.lastTokenRequest = TokenRequest{Form: cloneValues(r.PostForm), Authorization: r.Header.Get("Authorization")}
	p.tokenRequestCount++
	p.mu.Unlock()

	code := r.PostFormValue("code")
	claims, ok := p.takeCode(code)
	if !ok {
		p.writeTokenError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	if claims.CodeChallenge != "" {
		verifier := r.PostFormValue("code_verifier")
		if verifier == "" || oauth2.S256ChallengeFromVerifier(verifier) != claims.CodeChallenge {
			p.writeTokenError(w, http.StatusBadRequest, "invalid_grant")
			return
		}
	}

	idToken, err := p.signIDToken(claims)
	if err != nil {
		http.Error(w, "oidctest: signing id token: "+err.Error(), http.StatusInternalServerError)
		return
	}

	resp := tokenResponse{
		IDToken:     idToken,
		AccessToken: "oidctest-access-token",
		TokenType:   "Bearer",
		ExpiresIn:   accessTokenExpiresInSeconds,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil { //nolint:gosec // access_token here is a fixed placeholder string this mock always returns, not a real credential; the field name is the RFC 6749 §5.1 wire name go-oidc/oauth2 expects, not a leak of a real secret
		p.t.Errorf("oidctest: encoding token response: %v", err)
	}
}

func cloneValues(v url.Values) url.Values {
	out := make(url.Values, len(v))
	for k, vals := range v {
		out[k] = append([]string(nil), vals...)
	}
	return out
}

func (p *Provider) signIDToken(claims Claims) (token string, err error) {
	issuer := claims.Issuer
	if issuer == "" {
		issuer = p.URL
	}
	audience := claims.Audience
	if audience == "" {
		audience = p.ClientID
	}
	expiresAt := claims.ExpiresAt
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(time.Hour)
	}
	signingKey := claims.SigningKey
	if signingKey == nil {
		signingKey = p.key
	}

	payload := map[string]any{
		"iss": issuer,
		"aud": audience,
		"sub": claims.Subject,
		"exp": expiresAt.Unix(),
		"iat": time.Now().Unix(),
	}
	if claims.Email != "" {
		payload["email"] = claims.Email
	}
	if claims.EmailVerifiedRaw != nil {
		payload["email_verified"] = claims.EmailVerifiedRaw
	} else if claims.EmailVerified != nil {
		payload["email_verified"] = *claims.EmailVerified
	}
	if claims.Nonce != "" {
		payload["nonce"] = claims.Nonce
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshaling claims: %w", err)
	}

	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: signingKey},
		&jose.SignerOptions{
			ExtraHeaders: map[jose.HeaderKey]any{
				jose.HeaderKey("kid"): signingKeyID,
			},
		},
	)
	if err != nil {
		return "", fmt.Errorf("creating signer: %w", err)
	}

	sig, err := signer.Sign(raw)
	if err != nil {
		return "", fmt.Errorf("signing claims: %w", err)
	}

	compact, err := sig.CompactSerialize()
	if err != nil {
		return "", fmt.Errorf("serializing jwt: %w", err)
	}

	return compact, nil
}
