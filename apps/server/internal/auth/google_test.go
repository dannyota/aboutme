package auth_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/auth/oidctest"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/store"
)

func ptrTrue() *bool {
	b := true
	return &b
}

func TestGoogleCallback_NewUser_CreatesUserAndSession(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)

	startResp := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet (handlers_test.go) closes the body itself before returning; the linter cannot see through the wrapper.
	if startResp.StatusCode != 302 {
		t.Fatalf("GET %s status = %d, want 302", auth.GoogleStartPath, startResp.StatusCode)
	}
	loc, err := url.Parse(startResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse start redirect Location: %v", err)
	}
	state := loc.Query().Get("state")
	if state == "" {
		t.Fatal("start redirect Location missing state param")
	}
	codeChallenge := loc.Query().Get("code_challenge")
	if codeChallenge == "" {
		t.Fatal("start redirect Location missing code_challenge param (PKCE)")
	}
	if method := loc.Query().Get("code_challenge_method"); method != "S256" {
		t.Errorf("code_challenge_method = %q, want %q", method, "S256")
	}
	nonce := loc.Query().Get("nonce")
	if nonce == "" {
		t.Fatal("start redirect Location missing nonce param")
	}
	txCookie := extractCookie(startResp, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatal("start response missing __Host-oauth-tx cookie")
	}

	p.RegisterCode("code-new-user", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		CodeChallenge: codeChallenge,
		Nonce:         nonce,
	})

	cbResp := doGet(t, handler, auth.GoogleCallbackPath+"?code=code-new-user&state="+state, txCookie) //nolint:bodyclose // doGet closes the body itself before returning.
	if cbResp.StatusCode != 302 {
		t.Fatalf("GET callback status = %d, want 302", cbResp.StatusCode)
	}
	if got := cbResp.Header.Get("Location"); got != testPublicOrigin+"/app/resumes" {
		t.Errorf("successful callback Location = %q, want %q", got, testPublicOrigin+"/app/resumes")
	}

	sessionCookie := extractCookie(cbResp, auth.SessionCookieName)
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("callback response missing a non-empty __Host-session cookie")
	}
	if !sessionCookie.Secure || !sessionCookie.HttpOnly {
		t.Errorf("__Host-session cookie Secure=%v HttpOnly=%v, want both true", sessionCookie.Secure, sessionCookie.HttpOnly)
	}

	clearedTxCookie := extractCookie(cbResp, auth.OAuthTxCookieName)
	if clearedTxCookie == nil {
		t.Fatal("callback response missing a __Host-oauth-tx Set-Cookie header clearing it")
	}
	if clearedTxCookie.MaxAge >= 0 {
		t.Errorf("callback __Host-oauth-tx MaxAge = %d, want negative (cleared)", clearedTxCookie.MaxAge)
	}

	ctx := context.Background()
	usr, err := q.GetUserByEmail(ctx, email)
	if err != nil {
		t.Fatalf("GetUserByEmail(%q) error = %v, want a created user row", email, err)
	}
	if usr.Email != email {
		t.Errorf("user.Email = %q, want %q", usr.Email, email)
	}
	wantName, _, _ := strings.Cut(email, "@")
	if usr.Name != wantName {
		t.Errorf("user.Name = %q, want %q (email local part, never the full email)", usr.Name, wantName)
	}

	identity, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       "google",
		ProviderUserID: subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject(google, %q) error = %v, want a created identity row", subject, err)
	}
	if identity.UserID != usr.ID {
		t.Errorf("identity.UserID = %v, want %v (the same created user)", identity.UserID, usr.ID)
	}
}

func TestGoogleCallback_LoginNextRoundTrip(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))
	const next = "/oauth/authorize?client_id=018f5b6a-9a3e-7c21-8b1e-000000000001&scope=resumes%3Aread"

	startResp := doGet(t, handler, auth.GoogleStartPath+"?next="+url.QueryEscape(next)) //nolint:bodyclose // doGet closes the body itself before returning.
	if startResp.StatusCode != http.StatusFound {
		t.Fatalf("GET start status = %d, want %d", startResp.StatusCode, http.StatusFound)
	}
	loc, err := url.Parse(startResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse start redirect Location: %v", err)
	}
	txCookie := extractCookie(startResp, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatal("start response missing __Host-oauth-tx cookie")
	}

	const code = "code-login-next-round-trip"
	p.RegisterCode(code, oidctest.Claims{
		Subject:       uniqueSubject(t),
		Email:         uniqueEmail(t),
		EmailVerified: ptrTrue(),
		CodeChallenge: loc.Query().Get("code_challenge"),
		Nonce:         loc.Query().Get("nonce"),
	})
	cbResp := doGet(t, handler, auth.GoogleCallbackPath+"?code="+code+"&state="+loc.Query().Get("state"), txCookie) //nolint:bodyclose // doGet closes the body itself before returning.
	if cbResp.StatusCode != http.StatusFound {
		t.Fatalf("GET callback status = %d, want %d", cbResp.StatusCode, http.StatusFound)
	}
	if got, want := cbResp.Header.Get("Location"), testPublicOrigin+next; got != want {
		t.Errorf("callback Location = %q, want %q", got, want)
	}
}

func TestGoogleCallback_ExistingIdentity_ReusesUser_NoDuplicateRow(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)

	login := func(code string) (userID string) {
		startResp := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
		loc, err := url.Parse(startResp.Header.Get("Location"))
		if err != nil {
			t.Fatalf("parse start redirect Location: %v", err)
		}
		state := loc.Query().Get("state")
		codeChallenge := loc.Query().Get("code_challenge")
		nonce := loc.Query().Get("nonce")
		txCookie := extractCookie(startResp, auth.OAuthTxCookieName)

		p.RegisterCode(code, oidctest.Claims{
			Subject:       subject,
			Email:         email,
			EmailVerified: ptrTrue(),
			CodeChallenge: codeChallenge,
			Nonce:         nonce,
		})

		cbResp := doGet(t, handler, auth.GoogleCallbackPath+"?code="+code+"&state="+state, txCookie) //nolint:bodyclose // doGet closes the body itself before returning.
		if cbResp.StatusCode != 302 {
			t.Fatalf("callback(%s) status = %d, want 302", code, cbResp.StatusCode)
		}
		if extractCookie(cbResp, auth.SessionCookieName) == nil {
			t.Fatalf("callback(%s) missing __Host-session cookie", code)
		}

		usr, err := q.GetUserByEmail(context.Background(), email)
		if err != nil {
			t.Fatalf("GetUserByEmail(%q) error = %v", email, err)
		}
		return usr.ID.String()
	}

	first := login("code-repeat-1")
	second := login("code-repeat-2")
	if first != second {
		t.Errorf("second login user ID = %s, want %s (same user, not a new one)", second, first)
	}

	inspector := newRowInspectorPool(t)
	var identityCount int
	if err := inspector.QueryRow(context.Background(),
		`SELECT count(*) FROM identities WHERE provider = 'google' AND provider_user_id = $1`, subject,
	).Scan(&identityCount); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if identityCount != 1 {
		t.Errorf("identities row count for (google, %q) = %d, want exactly 1", subject, identityCount)
	}
}

func TestGoogleCallback_NewUser_NameFallsBackToEmailLocalPart_NotFullEmail(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	wantName, _, _ := strings.Cut(email, "@")

	startResp := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	loc, err := url.Parse(startResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse start redirect Location: %v", err)
	}
	state := loc.Query().Get("state")
	codeChallenge := loc.Query().Get("code_challenge")
	nonce := loc.Query().Get("nonce")
	txCookie := extractCookie(startResp, auth.OAuthTxCookieName)

	p.RegisterCode("code-name-fallback", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		CodeChallenge: codeChallenge,
		Nonce:         nonce,
	})

	cbResp := doGet(t, handler, auth.GoogleCallbackPath+"?code=code-name-fallback&state="+state, txCookie) //nolint:bodyclose // doGet closes the body itself before returning.
	if cbResp.StatusCode != 302 {
		t.Fatalf("GET callback status = %d, want 302", cbResp.StatusCode)
	}

	usr, err := q.GetUserByEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("GetUserByEmail(%q) error = %v, want a created user row", email, err)
	}
	if usr.Name != wantName {
		t.Errorf("user.Name = %q, want %q (email local part only)", usr.Name, wantName)
	}
	if usr.Name == email {
		t.Errorf("user.Name = %q, want it to NOT equal the full email %q", usr.Name, email)
	}
}

func newServiceWithOrigin(t *testing.T, pool *store.Pool, issuer, origin string) http.Handler {
	t.Helper()

	cfg := config.Config{
		PublicOrigin:       origin,
		ProviderLogin:      config.ProviderLogin{Google: true, GitHub: true, LinkedIn: true},
		GoogleClientID:     oidctest.DefaultClientID,
		GoogleClientSecret: "test-google-client-secret",
	}
	svc, err := auth.NewServiceForTest(testLogger(), cfg, pool, issuer, "", "")
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	return api.New(testLogger(), noopPinger{}, api.Options{}, nil, svc.RegisterRoutes)
}

func TestGoogleCallback_UsesStoredRedirectURI_NotCurrentPublicOrigin(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	pool := newTestPool(t)

	const (
		beginOrigin    = "https://begin.aboutme.example"
		callbackOrigin = "https://callback.aboutme.example"
	)
	beginHandler := newServiceWithOrigin(t, pool, p.URL, beginOrigin)
	callbackHandler := newServiceWithOrigin(t, pool, p.URL, callbackOrigin)

	startResp := doGet(t, beginHandler, auth.GoogleStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	if startResp.StatusCode != http.StatusFound {
		t.Fatalf("GET %s status = %d, want %d", auth.GoogleStartPath, startResp.StatusCode, http.StatusFound)
	}
	loc, err := url.Parse(startResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse start redirect Location: %v", err)
	}
	state := loc.Query().Get("state")
	nonce := loc.Query().Get("nonce")
	txCookie := extractCookie(startResp, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatal("start response missing __Host-oauth-tx cookie")
	}

	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	p.RegisterCode("code-redirect-uri-origin-change", oidctest.Claims{
		Subject:       subject,
		Email:         email,
		EmailVerified: ptrTrue(),
		Nonce:         nonce,
	})

	cbResp := doGet(t, callbackHandler, auth.GoogleCallbackPath+"?code=code-redirect-uri-origin-change&state="+state, txCookie) //nolint:bodyclose // doGet closes the body itself before returning.
	if cbResp.StatusCode != http.StatusFound {
		t.Fatalf("GET callback status = %d, want %d", cbResp.StatusCode, http.StatusFound)
	}
	if extractCookie(cbResp, auth.SessionCookieName) == nil {
		t.Fatal("callback did not authenticate -- want a successful login despite the origin change")
	}

	gotRedirectURI, seen := p.LastTokenRedirectURI()
	if !seen {
		t.Fatal("token endpoint was never exchanged")
	}
	wantRedirectURI := beginOrigin + auth.GoogleCallbackPath
	if gotRedirectURI != wantRedirectURI {
		t.Errorf("token exchange redirect_uri = %q, want %q (the transaction's own stored RedirectURI from Begin, not callbackOrigin=%q's current config)",
			gotRedirectURI, wantRedirectURI, callbackOrigin)
	}
}

func TestGoogleStart_AuthorizeURL_RedirectURIMatchesPublicOriginAndCallbackPath(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL))

	startResp := doGet(t, handler, auth.GoogleStartPath) //nolint:bodyclose // doGet closes the body itself before returning.
	if startResp.StatusCode != http.StatusFound {
		t.Fatalf("GET %s status = %d, want %d", auth.GoogleStartPath, startResp.StatusCode, http.StatusFound)
	}
	loc, err := url.Parse(startResp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse start redirect Location: %v", err)
	}

	want := testPublicOrigin + auth.GoogleCallbackPath
	if got := loc.Query().Get("redirect_uri"); got != want {
		t.Errorf("authorize URL redirect_uri = %q, want %q (PublicOrigin + GoogleCallbackPath)", got, want)
	}
}
