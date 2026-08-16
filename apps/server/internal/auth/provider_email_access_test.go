package auth_test

// These tests prove the subject-first D5 rule: a returning subject never
// depends on (or even fetches) the provider email, a new subject requires a
// verified canonical email, and link resolves on subject alone.

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// TestGoogleCallback_ReturningSubject_SkipsEmailRequirement proves a returning
// Google subject logs in even when the id_token's email claim is unverified.
func TestGoogleCallback_ReturningSubject_SkipsEmailRequirement(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	ctx := context.Background()

	userID := createTestUser(t, q)
	subject := uniqueSubject(t)
	if _, err := q.CreateIdentity(ctx, store.CreateIdentityParams{
		UserID:         userID,
		Provider:       string(auth.ProviderGoogle),
		ProviderUserID: subject,
	}); err != nil {
		t.Fatalf("CreateIdentity() error = %v", err)
	}

	txCookie, state, nonce := beginGoogle(t, handler)
	p.RegisterCode("code-returning-unverified-email", oidctest.Claims{
		Subject:       subject,
		Email:         "unverified-returning@example.com",
		EmailVerified: ptrFalse(),
		Nonce:         nonce,
	})

	resp := doCallback(t, handler, "code-returning-unverified-email", state, txCookie) //nolint:bodyclose // doCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got := resp.Header.Get("Location"); got != testPublicOrigin+"/" {
		t.Errorf("callback Location = %q, want %q (a returning subject must log in without re-evaluating email)", got, testPublicOrigin+"/")
	}
	if extractCookie(resp, auth.SessionCookieName) == nil {
		t.Error("callback did not issue a session cookie for a returning subject")
	}
}

// TestLinkedInCallback_ReturningSubject_SkipsEmailRequirement proves a returning
// LinkedIn subject logs in even when the optional email claim is absent.
func TestLinkedInCallback_ReturningSubject_SkipsEmailRequirement(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL))
	ctx := context.Background()

	userID := createTestUser(t, q)
	subject := uniqueLinkedInSubject(t)
	if _, err := q.CreateIdentity(ctx, store.CreateIdentityParams{
		UserID:         userID,
		Provider:       string(auth.ProviderLinkedIn),
		ProviderUserID: subject,
	}); err != nil {
		t.Fatalf("CreateIdentity() error = %v", err)
	}

	txCookie, state, nonce := beginLinkedIn(t, handler)
	p.RegisterCode("code-returning-no-email", oidctest.Claims{
		Subject:       subject,
		Email:         "",
		EmailVerified: nil, // claim absent entirely
		Nonce:         nonce,
	})

	resp := doLinkedInCallback(t, handler, "code-returning-no-email", state, txCookie) //nolint:bodyclose // doLinkedInCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got := resp.Header.Get("Location"); got != testPublicOrigin+"/" {
		t.Errorf("callback Location = %q, want %q (a returning subject must log in without re-evaluating email)", got, testPublicOrigin+"/")
	}
	if extractCookie(resp, auth.SessionCookieName) == nil {
		t.Error("callback did not issue a session cookie for a returning subject")
	}
}

// TestGitHubCallback_ReturningSubject_SkipsEmailsAPI proves a returning GitHub
// subject never calls GET /user/emails.
func TestGitHubCallback_ReturningSubject_SkipsEmailsAPI(t *testing.T) {
	t.Parallel()

	githubID := uniqueGitHubUserID(t)
	gh := newGitHubStub(t,
		withTokenResponse("code-returning-github", "access-token-returning-github"),
		withUser(githubID, "octocat-returning"),
		withEmails([]ghEmail{{Email: "trap@example.com", Primary: true, Verified: true}}),
	)
	handler, q := newTestService(t, withGitHubEndpoint(gh.URL))
	ctx := context.Background()

	providerUserID := strconv.FormatInt(githubID, 10)
	userID := createTestUser(t, q)
	if _, err := q.CreateIdentity(ctx, store.CreateIdentityParams{
		UserID:         userID,
		Provider:       string(auth.ProviderGitHub),
		ProviderUserID: providerUserID,
	}); err != nil {
		t.Fatalf("CreateIdentity() error = %v", err)
	}

	txCookie, state := beginGitHubFlow(t, handler)
	resp := doGitHubCallback(t, handler, "code-returning-github", state, txCookie) //nolint:bodyclose // doGitHubCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got := resp.Header.Get("Location"); got != testPublicOrigin+"/" {
		t.Errorf("callback Location = %q, want %q (a returning subject must log in)", got, testPublicOrigin+"/")
	}
	if extractCookie(resp, auth.SessionCookieName) == nil {
		t.Error("callback did not issue a session cookie for a returning subject")
	}
	if got := gh.emailsRequestCount(); got != 0 {
		t.Errorf("GET /user/emails was called %d times for a returning subject, want 0", got)
	}
}

// TestGoogleCallback_LinkPurpose_IgnoresUnverifiedEmail proves link resolves on
// subject alone and never rejects or reads the (unverified) provider email.
func TestGoogleCallback_LinkPurpose_IgnoresUnverifiedEmail(t *testing.T) {
	t.Parallel()

	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL))
	ctx := context.Background()

	linkingUserID := createTestUser(t, q)
	raw, _ := issueTestSession(t, q, linkingUserID)
	txCookie, tx := beginGoogleTransaction(t, q, auth.PurposeLink, linkingUserID)

	subject := uniqueSubject(t)
	p.RegisterCode("code-link-unverified-email", oidctest.Claims{
		Subject:       subject,
		Email:         "unverified-link@example.com",
		EmailVerified: ptrFalse(),
		Nonce:         tx.Nonce,
	})

	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code=code-link-unverified-email&state="+tx.State, txCookie, sessionRequestCookie(raw)) //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want %d", resp.StatusCode, http.StatusFound)
	}
	if got := resp.Header.Get("Location"); got != testPublicOrigin+wantSettingsSessionsPath {
		t.Errorf("callback Location = %q, want %q (link must succeed by subject)", got, testPublicOrigin+wantSettingsSessionsPath)
	}

	identity, err := q.GetIdentityByProviderSubject(ctx, store.GetIdentityByProviderSubjectParams{
		Provider:       string(auth.ProviderGoogle),
		ProviderUserID: subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject() error = %v, want the linked identity", err)
	}
	if identity.UserID != linkingUserID {
		t.Errorf("identity.UserID = %v, want %v (the linking user)", identity.UserID, linkingUserID)
	}
}
