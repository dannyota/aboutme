// Package oidctest serves real discovery, JWKS, and signed-token endpoints so
// production go-oidc verification runs unchanged. Production code must not
// import this test infrastructure.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"golang.org/x/oauth2"
)

// DefaultClientID is the provider and audience default.
const DefaultClientID = "test-client"

// signingKeyID remains fixed for signing-key overrides so bad-signature tests
// reach cryptographic verification instead of failing key lookup.
const signingKeyID = "oidctest-key"

// Claims defaults to a valid token; tests set only the property they vary.
type Claims struct {
	// Subject is the stable provider identifier.
	Subject string

	// Empty Email omits the claim.
	Email string

	// Nil EmailVerified omits the claim and never means true.
	EmailVerified *bool

	// Empty Nonce omits the claim; go-oidc does not validate it.
	Nonce string

	// Empty Audience defaults to Provider.ClientID.
	Audience string

	// Empty Issuer defaults to Provider.URL.
	Issuer string

	// Zero ExpiresAt defaults to one hour from now.
	ExpiresAt time.Time

	// Nil SigningKey uses the key published by the provider.
	SigningKey *rsa.PrivateKey

	// Non-empty CodeChallenge requires a matching S256 verifier and rejects a
	// missing or mismatched verifier with invalid_grant.
	CodeChallenge string
}

// Provider is an in-process OIDC server backed by an ephemeral RSA key.
type Provider struct {
	// URL is the server's own base URL. It is also the discovery issuer
	// and the default for Claims.Issuer.
	URL string

	// ClientID is the OAuth2 client id this Provider is registered
	// under. Claims.Audience defaults to this value.
	ClientID string

	t   *testing.T
	key *rsa.PrivateKey

	mu                    sync.Mutex
	codes                 map[string]Claims
	discoveryBlock        func() // see BlockDiscoveryForTest; nil means discovery never blocks
	lastTokenRedirectURI  string
	lastTokenRedirectSeen bool
}

// LastTokenRedirectURI exposes the last token-exchange redirect URI so tests can
// prove it came from the stored transaction.
func (p *Provider) LastTokenRedirectURI() (redirectURI string, seen bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastTokenRedirectURI, p.lastTokenRedirectSeen
}

// NewProvider starts a server and registers its cleanup with t.
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
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", p.serveDiscovery)
	mux.HandleFunc("/jwks.json", p.serveJWKS)
	mux.HandleFunc("/token", p.serveToken)

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	p.URL = server.URL
	return p
}

// RegisterCode registers one single-use authorization code.
func (p *Provider) RegisterCode(code string, claims Claims) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.codes[code] = claims
}

// takeCode atomically consumes a registered code.
func (p *Provider) takeCode(code string) (Claims, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	claims, ok := p.codes[code]
	if ok {
		delete(p.codes, code)
	}
	return claims, ok
}

// discoveryDocument is the subset of OpenID Connect discovery metadata
// go-oidc's Provider parses (see providerJSON in the go-oidc source).
type discoveryDocument struct {
	Issuer                           string   `json:"issuer"`
	AuthorizationEndpoint            string   `json:"authorization_endpoint"`
	TokenEndpoint                    string   `json:"token_endpoint"`
	JWKSURI                          string   `json:"jwks_uri"`
	ResponseTypesSupported           []string `json:"response_types_supported"`
	SubjectTypesSupported            []string `json:"subject_types_supported"`
	IDTokenSigningAlgValuesSupported []string `json:"id_token_signing_alg_values_supported"`
}

// BlockDiscoveryForTest exposes when discovery enters and blocks it until the
// idempotent release. This supports deterministic lock tests without sleeps.
// Call it before discovery; a later registration replaces the earlier one.
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
		// Errorf is safe from the server goroutine; Fatalf is not.
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

// tokenResponse is the token endpoint's success body: the subset of RFC
// 6749 §5.1 fields go-oidc/oauth2 consumes.
type tokenResponse struct {
	IDToken     string `json:"id_token"`
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

// tokenErrorResponse is the token endpoint's RFC 6749 §5.2 error body:
// {"error": "..."}.
type tokenErrorResponse struct {
	Error string `json:"error"`
}

// writeTokenError uses JSON so oauth2 populates RetrieveError.ErrorCode;
// text/plain would be parsed as form data.
func (p *Provider) writeTokenError(w http.ResponseWriter, status int, errorCode string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(tokenErrorResponse{Error: errorCode}); err != nil {
		p.t.Errorf("oidctest: encoding token error response: %v", err)
	}
}

// The access-token lifetime stays independent of the ID token expiry so expired
// ID-token tests reach OIDC verification.
const accessTokenExpiresInSeconds = 3600

func (p *Provider) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		p.writeTokenError(w, http.StatusBadRequest, "invalid_request")
		return
	}

	p.mu.Lock()
	p.lastTokenRedirectURI = r.PostFormValue("redirect_uri")
	p.lastTokenRedirectSeen = true
	p.mu.Unlock()

	code := r.PostFormValue("code")
	claims, ok := p.takeCode(code)
	if !ok {
		p.writeTokenError(w, http.StatusBadRequest, "invalid_grant")
		return
	}

	// Consume the code before PKCE validation so a failed exchange still burns it.
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

// signIDToken applies defaults and returns a compact signed JWT.
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
	if claims.EmailVerified != nil {
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
