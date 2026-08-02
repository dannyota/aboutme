// Package oidctest is an in-process, OIDC-shaped test double for Google
// and LinkedIn: a real ephemeral RSA-2048 key plus real
// discovery/JWKS/token HTTP endpoints, signed with
// github.com/go-jose/go-jose/v4, so github.com/coreos/go-oidc/v3 — the
// same client library the production Google/LinkedIn login code uses —
// runs its actual signature/issuer/audience/expiry verification against
// this server instead of a hand-rolled stub of go-oidc's internals.
//
// This package is test infrastructure only. It is used exclusively from
// _test.go files in later auth tasks; production code must never import
// it.
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

// DefaultClientID is the OAuth2 client id a Provider registers itself
// under unless a test overrides Claims.Audience. It is the "registered
// client id" Claims.Audience's default refers to, and matches
// Provider.ClientID's initial value.
const DefaultClientID = "test-client"

// signingKeyID is the "kid" every Provider advertises in its JWKS and
// stamps onto tokens it signs with its own key. A Claims.SigningKey
// override still gets this kid: go-oidc matches keys by kid-or-fallback
// and then verifies the signature cryptographically, so a mismatched key
// under the real kid still fails signature verification exactly as it
// would against a live provider whose kid a client can't predict — this
// is what makes the bad-signature test meaningful rather than trivially
// "key not found".
const signingKeyID = "oidctest-key"

// Claims controls the content of the ID token minted for a registered
// authorization code. Every field defaults to a production-shaped value
// so a test only sets what it's deliberately varying; overriding a field
// is how later tasks provoke a specific OIDC rejection (issuer mismatch,
// wrong audience, bad signature, expired token).
type Claims struct {
	// Subject is the "sub" claim: the provider's stable user identifier.
	Subject string

	// Email is the "email" claim. Empty omits the claim entirely —
	// LinkedIn's optional-email case.
	Email string

	// EmailVerified is the "email_verified" claim. nil omits the claim
	// entirely — LinkedIn's optional case. Never treat a nil
	// EmailVerified as true.
	EmailVerified *bool

	// Nonce is the "nonce" claim. Empty omits the claim; go-oidc does
	// not validate it automatically, so a caller that cares must compare
	// it itself.
	Nonce string

	// Audience is the "aud" claim. Empty defaults to the Provider's
	// ClientID. Set explicitly to test a wrong-audience rejection.
	Audience string

	// Issuer is the "iss" claim. Empty defaults to the Provider's own
	// URL. Set explicitly to test an issuer-mismatch rejection.
	Issuer string

	// ExpiresAt is the "exp" claim. Zero defaults to now+1h. Set in the
	// past to test an expired-token rejection.
	ExpiresAt time.Time

	// SigningKey signs the ID token. nil defaults to the Provider's own
	// key (whose public half is published at /jwks.json). Set a
	// different key to test a bad-signature rejection.
	SigningKey *rsa.PrivateKey

	// CodeChallenge is the PKCE (RFC 7636) S256 code_challenge a real
	// authorization server would have remembered from the /authorize
	// request that issued this code. Empty (the default, and every
	// pre-existing test in this package) means PKCE is not enforced for
	// this code -- /token accepts an exchange with no code_verifier at
	// all, exactly like a provider a client never sent code_challenge to.
	// Set it (e.g. to oauth2.S256ChallengeFromVerifier(verifier)) to prove
	// a client's PKCE send: /token then requires the exchange's
	// code_verifier to hash (S256) to this exact value, rejecting a
	// missing or mismatched one with invalid_grant.
	CodeChallenge string
}

// Provider is an in-process, OIDC-shaped HTTP server: discovery, JWKS, and
// token endpoints backed by a real ephemeral RSA key and real go-jose
// signing, so go-oidc's client code verifies its responses exactly as it
// would a live provider's.
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

// LastTokenRedirectURI returns the "redirect_uri" form value the most
// recent /token exchange sent (RFC 6749 §4.1.3), and whether any exchange
// has happened yet at all. It exists so a test can prove WHICH redirect
// URI a client's token exchange actually used -- e.g. that it came from a
// stored OAuth transaction's own RedirectURI rather than being rebuilt
// from the caller's current configuration (internal/auth's
// TestGoogleCallback_UsesStoredRedirectURI_NotCurrentPublicOrigin) --
// since this mock, like a real authorization server would with a
// registered exact-match redirect_uri, otherwise has no way to surface
// which value a client sent.
func (p *Provider) LastTokenRedirectURI() (redirectURI string, seen bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastTokenRedirectURI, p.lastTokenRedirectSeen
}

// NewProvider starts an in-process OIDC test server and registers
// t.Cleanup to shut it down, so no server outlives the test that created
// it.
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

// RegisterCode makes the /token endpoint return an id_token/access_token
// minted from claims for exactly one exchange of code. A second exchange
// of the same code fails, matching a real authorization server's
// single-use authorization codes.
func (p *Provider) RegisterCode(code string, claims Claims) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.codes[code] = claims
}

// takeCode consumes and returns the claims registered for code, and
// whether code was registered at all. A code is usable exactly once: the
// second call for the same code reports ok == false.
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

// BlockDiscoveryForTest makes the discovery endpoint
// (/.well-known/openid-configuration) block, once entered, until release
// is called -- entered is closed the instant a request actually reaches
// the handler, so a caller can deterministically know a discovery request
// is in flight (blocked inside the network round trip) without a
// sleep-based race. It exists specifically so a test can prove a lazy
// OIDC-provider cache does NOT hold its own mutex for the duration of the
// discovery HTTP call (internal/auth's provider-discovery lazy-init):
// while this Provider's discovery response is deliberately withheld, the
// test can assert the caller's cache mutex is still free to acquire.
// release is idempotent (safe to call more than once, e.g. once
// explicitly mid-test and again via t.Cleanup as a safety net). Must be
// called before the request that would trigger discovery; a Provider
// supports at most one blocked discovery registration at a time (a second
// call replaces the first).
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
		// Safe to call from the server's goroutine: Errorf, unlike
		// Fatalf, may be called from a goroutine other than the one
		// running the test (testing.T docs).
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

// writeTokenError writes a token-endpoint error response as JSON with the
// given status and RFC 6749 §5.2 error code, so
// golang.org/x/oauth2's RetrieveError.ErrorCode actually populates from it.
// A bare http.Error's "text/plain" Content-Type makes oauth2 parse the
// body as an x-www-form-urlencoded query string instead of JSON (see
// golang.org/x/oauth2/internal.doTokenRoundTrip's content-type switch),
// silently losing the error code -- this is what makes a caller's
// errors.As(err, &oauth2.RetrieveError{}).ErrorCode check meaningful
// against this mock.
func (p *Provider) writeTokenError(w http.ResponseWriter, status int, errorCode string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(tokenErrorResponse{Error: errorCode}); err != nil {
		p.t.Errorf("oidctest: encoding token error response: %v", err)
	}
}

// accessTokenExpiresInSeconds is the OAuth2 access token's advertised
// lifetime, deliberately fixed and independent of the ID token's own "exp"
// claim: RFC 6749's access-token expires_in and OIDC's id_token exp are
// two different tokens' lifetimes, set by the same server but not required
// to agree, and a real IdP never derives one from the other. Deriving
// expires_in from Claims.ExpiresAt would give an expired-ID-token test a
// negative expires_in that no real IdP emits — a shape production code
// could reject at the oauth2-token layer before ID-token verification ever
// runs, making an expired-ID-token test pass for the wrong reason.
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

	// PKCE (RFC 7636): a code registered with a CodeChallenge requires a
	// matching code_verifier on this exchange -- see Claims.CodeChallenge.
	// Checked after takeCode (which already consumed the single-use code)
	// so a PKCE-failed exchange still burns the code, matching a real
	// authorization server's behavior for any failed exchange attempt.
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

// signIDToken fills in claims' defaults (issuer, audience, expiry,
// signing key — each per Claims' documented default) and returns a
// signed, compact-serialized JWT.
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
