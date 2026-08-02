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
	"net/url"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
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
func TestGoogleCallback_NewUser_CreatesUserAndSession(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

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
	p.RegisterCode("code-1", oidctest.Claims{
		Subject:       "g-sub-1",
		Email:         "new@example.com",
		EmailVerified: ptrTrue(),
		CodeChallenge: codeChallenge,
		Nonce:         nonce,
	})

	cbResp := doGet(t, handler, auth.GoogleCallbackPath+"?code=code-1&state="+state, txCookie) //nolint:bodyclose // doGet closes the body itself before returning.
	if cbResp.StatusCode != 302 {
		t.Fatalf("GET callback status = %d, want 302", cbResp.StatusCode)
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
	usr, err := q.GetUserByEmail(ctx, "new@example.com")
	if err != nil {
		t.Fatalf("GetUserByEmail(new@example.com) error = %v, want a created user row", err)
	}
	if usr.Email != "new@example.com" {
		t.Errorf("user.Email = %q, want %q", usr.Email, "new@example.com")
	}

	identity, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       "google",
		ProviderUserID: "g-sub-1",
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject(google, g-sub-1) error = %v, want a created identity row", err)
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
// surface that as a 500, not silently succeed).
func TestGoogleCallback_ExistingIdentity_ReusesUser_NoDuplicateRow(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))

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
			Subject:       "g-sub-repeat",
			Email:         "repeat@example.com",
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

		usr, err := q.GetUserByEmail(context.Background(), "repeat@example.com")
		if err != nil {
			t.Fatalf("GetUserByEmail(repeat@example.com) error = %v", err)
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
		`SELECT count(*) FROM identities WHERE provider = 'google' AND provider_user_id = 'g-sub-repeat'`,
	).Scan(&identityCount); err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if identityCount != 1 {
		t.Errorf("identities row count for (google, g-sub-repeat) = %d, want exactly 1", identityCount)
	}
}
