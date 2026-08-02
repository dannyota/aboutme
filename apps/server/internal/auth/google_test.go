// Package auth_test exercises the Google OIDC login flow end to end
// against oidctest's in-process mock provider (Task 3), through the same
// real golang.org/x/oauth2 + coreos/go-oidc code paths production uses --
// see task-4-brief.md Step 2. The adversarial OIDC verification matrix
// (wrong issuer/audience/signature/nonce/expiry, unverified email) is a
// separate, independently authored suite per the phase's review workflow
// and is not duplicated here.
package auth_test

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/config"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func ptrTrue() *bool {
	b := true
	return &b
}

// TestGoogleCallback_NewUser_CreatesUserAndSession is task-4-brief.md
// Step 2's happy-path test: a first-ever login via Google creates a users
// row and an identities row, issues a real session (__Host-session
// cookie), and clears the __Host-oauth-tx cookie used to carry the
// transaction handle. It drives /start then /callback exactly as a
// browser would (capturing the Set-Cookie and state/code_challenge from
// /start's redirect, then presenting them back to /callback), so the PKCE
// send (oauth2.Config.Exchange with oauth2.VerifierOption) is proven
// through oidctest's own code_challenge validation (ruling 4a/4c), not
// merely assumed.
//
// subject/email use uniqueSubject/uniqueEmail (google_adversarial_test.go,
// same package) rather than fixed literals: this test's own database is
// shared and persists across every run against TEST_DATABASE_URL (no
// per-test reset -- see that file's own doc comment), so a fixed
// "g-sub-1"/"new@example.com" would silently REUSE a row a previous run
// already created instead of exercising the new-user path a second time,
// which is exactly what happened here once resolveGoogleUser's name
// gained a real fallback (fix-round ruling b1): a re-run of this suite
// found an existing identity for the fixed subject and returned its
// already-created user (with the OLD full-email name), so the new name
// assertion below failed against a stale row -- collision-proof
// identifiers are what a "new user" test genuinely needs.
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

	// A real provider signs the nonce it was sent back into the id_token
	// verbatim; oidctest.Claims.Nonce is this test's stand-in for that --
	// registering the exact nonce /start sent is what proves handleGoogle
	// Callback's own nonce comparison (go-oidc does not check it) actually
	// matches a genuine round trip.
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
	// DD-C7: a SUCCESSFUL callback stays pinned to the bare origin ("/"),
	// never "/login" -- that path is reserved for a rejection's ?error=
	// redirect (see handlers_test.go's assertRedirectPath).
	if got := cbResp.Header.Get("Location"); got != testPublicOrigin+"/" {
		t.Errorf("successful callback Location = %q, want %q", got, testPublicOrigin+"/")
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
		t.Fatal("callback response missing a __Host-oauth-tx Set-Cookie header clearing it (ruling 1)")
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
	// Fix-round ruling b1: oidctest.Claims has no Name field at all, so
	// this happy path always exercises resolveGoogleUser's fallback --
	// the LOCAL PART of the email, never the full address (see the
	// dedicated TestGoogleCallback_NewUser_NameFallsBackToEmailLocalPart
	// below for a case where the distinction is even more explicit).
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

// TestGoogleCallback_ExistingIdentity_ReusesUser_NoDuplicateRow is a
// second-login smoke check on top of the happy path above: the same
// (provider, sub) logging in again must authenticate as the SAME user
// (not create a second one) and must not attempt a second identities
// insert (identities_provider_subject_key's UNIQUE constraint would
// surface that as a 500, not silently succeed). subject/email are unique
// per run (see the happy-path test's doc comment for why that matters).
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

// TestGoogleCallback_NewUser_NameFallsBackToEmailLocalPart_NotFullEmail is
// fix-round ruling b1's dedicated regression test: when Google's optional
// "name" claim is absent (as it always is from oidctest, which has no
// Name field), the created user's display name must be the LOCAL PART of
// the email (the part before "@"), never the full email address -- a
// later phase renders this value as a display name, and the full email
// would leak the visitor's address to anyone who can see it.
// subject/email are unique per run (see the happy-path test's doc
// comment for why that matters).
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

// newServiceWithOrigin builds a Service backed by q (shared with the
// caller, unlike newTestService's own fresh newTestQueries each time) at
// a specific PublicOrigin -- for
// TestGoogleCallback_UsesStoredRedirectURI_NotCurrentPublicOrigin's
// two-Service scenario below, where /start and /callback deliberately run
// against different PublicOrigin configurations sharing one database.
func newServiceWithOrigin(t *testing.T, q *store.Queries, issuer, origin string) http.Handler {
	t.Helper()

	cfg := config.Config{
		PublicOrigin:       origin,
		GoogleClientID:     oidctest.DefaultClientID,
		GoogleClientSecret: "test-google-client-secret",
	}
	svc, err := auth.NewServiceForTest(testLogger(), cfg, q, issuer, "")
	if err != nil {
		t.Fatalf("NewServiceForTest() error = %v", err)
	}
	return api.New(testLogger(), noopPinger{}, api.Options{}, svc.RegisterRoutes)
}

// TestGoogleCallback_UsesStoredRedirectURI_NotCurrentPublicOrigin proves
// handleGoogleCallback's token exchange uses the STORED transaction's own
// RedirectURI (set once, at Begin) rather than rebuilding one from this
// Service's own current PublicOrigin config. It simulates PUBLIC_ORIGIN
// changing mid-flight (a real deploy scenario: an origin migration or
// config change landing between when a visitor's browser fetched /start
// and when they complete /callback) with two separate Service instances
// sharing one database: /start runs against beginOrigin, /callback
// against a DIFFERENT callbackOrigin. If the Exchange call rebuilt
// redirect_uri from the CALLBACK Service's own config (the bug this
// hardening fixes), oidctest.Provider.LastTokenRedirectURI would observe
// callbackOrigin's callback URL instead of beginOrigin's -- and a real
// provider, which enforces redirect_uri as an exact match against what it
// issued the code for, would reject the exchange outright.
func TestGoogleCallback_UsesStoredRedirectURI_NotCurrentPublicOrigin(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	q := newTestQueries(t)

	const (
		beginOrigin    = "https://begin.aboutme.example"
		callbackOrigin = "https://callback.aboutme.example"
	)
	beginHandler := newServiceWithOrigin(t, q, p.URL, beginOrigin)
	callbackHandler := newServiceWithOrigin(t, q, p.URL, callbackOrigin)

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

// TestGoogleStart_AuthorizeURL_RedirectURIMatchesPublicOriginAndCallbackPath
// asserts the one authorize-URL query parameter no existing test checked
// directly: redirect_uri. Every other /start assertion (state,
// code_challenge, code_challenge_method, nonce) already has coverage;
// redirect_uri is what a real provider validates as an exact match
// against what it has on file for this client, so a wrong value here
// would silently break every real login while every hermetic test (which
// never validates it) kept passing.
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
