// These tests drive the real view-purpose start and callback handlers
// against a live resume fixture, proving sign in to view never touches an
// account, session, or CSRF token (docs/design/viewer-analytics/
// sign-in-to-view.md "Sign-in flow"; AC-VIEW-004, AC-VIEW-005, AC-VIEW-006).
package auth_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/auth/oidctest"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/viewpass"
)

// viewFixture is one live resume with a fresh owner, for the view purpose's
// start and callback tests.
type viewFixture struct {
	ownerID uuid.UUID
	id      uuid.UUID
	slug    string
	rev     int64
}

// newViewFixture creates a fresh user and a live resume with sign in to
// view set to signInToView.
func newViewFixture(t *testing.T, q *store.Queries, signInToView bool) viewFixture {
	t.Helper()
	ctx := context.Background()

	owner, err := q.CreateUser(ctx, store.CreateUserParams{Email: uniqueEmail(t), Name: "Owner"})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	created, err := q.CreateResume(ctx, store.CreateResumeParams{
		UserID: owner.ID, Title: "Test Resume", SchemaVersion: 1,
		PersonalDetails: []byte("{}"), Content: []byte("{}"), Customization: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("CreateResume() error = %v", err)
	}
	slug := "view-fixture-" + uuid.NewString()[:8]
	published, err := q.PublishResumeCAS(ctx, store.PublishResumeCASParams{
		ID: created.ID, UserID: owner.ID, ExpectedRevision: created.Revision,
		Slug: &slug, Live: true, DownloadEnabled: true, SignInToView: signInToView, UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("PublishResumeCAS() error = %v", err)
	}
	return viewFixture{ownerID: owner.ID, id: published.ID, slug: slug, rev: published.Revision}
}

// republish re-applies PublishResumeCAS against the fixture's current
// revision, so a test can change live or signInToView mid-flow and continue
// using the same fixture value.
func (f *viewFixture) republish(t *testing.T, q *store.Queries, live, signInToView bool) {
	t.Helper()
	slug := f.slug
	updated, err := q.PublishResumeCAS(context.Background(), store.PublishResumeCASParams{
		ID: f.id, UserID: f.ownerID, ExpectedRevision: f.rev,
		Slug: &slug, Live: live, DownloadEnabled: true, SignInToView: signInToView, UpdatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("PublishResumeCAS() republish error = %v", err)
	}
	f.rev = updated.Revision
}

func viewStartPath(base, slug string) string {
	return base + "?purpose=view&slug=" + url.QueryEscape(slug)
}

// TestViewStart_MalformedSlug_BadRequest covers Google and LinkedIn.
func TestViewStart_MalformedSlug_BadRequest(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL), withViewPassKey())

	for _, path := range []string{
		viewStartPath(auth.GoogleStartPath, "a"),
		viewStartPath(auth.GoogleStartPath, ""),
		viewStartPath(auth.LinkedInStartPath, "a"),
	} {
		resp := doGet(t, handler, path) //nolint:bodyclose // doGet closes the body itself before returning.
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("GET %s status = %d, want 400", path, resp.StatusCode)
		}
		if extractCookie(resp, auth.OAuthTxCookieName) != nil {
			t.Errorf("GET %s set a transaction cookie on a malformed slug, want none", path)
		}
	}
}

// TestViewStart_GitHub_BadRequest proves GitHub, which the gate never
// offers, refuses purpose=view instead of falling back to login.
func TestViewStart_GitHub_BadRequest(t *testing.T) {
	t.Parallel()
	handler, _ := newTestService(t, withGitHubEndpoint("http://127.0.0.1:1"), withViewPassKey())

	f := viewFixture{slug: "ignored-slug"}
	resp := doGet(t, handler, viewStartPath(auth.GitHubStartPath, f.slug)) //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("GET %s status = %d, want 400", auth.GitHubStartPath, resp.StatusCode)
	}
	if extractCookie(resp, auth.OAuthTxCookieName) != nil {
		t.Error("GitHub purpose=view set a transaction cookie, want none")
	}
}

// TestViewStart_NotLiveOrSwitchOff_RedirectsWithNoTransaction covers both a
// missing slug and a live resume with the switch off.
func TestViewStart_NotLiveOrSwitchOff_RedirectsWithNoTransaction(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withViewPassKey())

	unknownSlug := "no-such-resume-" + uuid.NewString()[:8]
	resp := doGet(t, handler, viewStartPath(auth.GoogleStartPath, unknownSlug)) //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("unknown slug status = %d, want 302", resp.StatusCode)
	}
	assertRedirectPath(t, resp.Header.Get("Location"), "/"+unknownSlug)

	off := newViewFixture(t, q, false)
	resp2 := doGet(t, handler, viewStartPath(auth.GoogleStartPath, off.slug)) //nolint:bodyclose // doGet closes the body itself before returning.
	if resp2.StatusCode != http.StatusFound {
		t.Fatalf("switch-off status = %d, want 302", resp2.StatusCode)
	}
	assertRedirectPath(t, resp2.Header.Get("Location"), "/"+off.slug)
	if extractCookie(resp2, auth.OAuthTxCookieName) != nil {
		t.Error("view start on a switch-off resume set a transaction cookie, want none")
	}
}

// TestViewStart_ScopeIsOpenIDOnly proves both providers request exactly
// scope=openid for the view purpose (AC-VIEW-004).
func TestViewStart_ScopeIsOpenIDOnly(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	for _, path := range []string{
		viewStartPath(auth.GoogleStartPath, f.slug),
		viewStartPath(auth.LinkedInStartPath, f.slug),
	} {
		resp := doGet(t, handler, path) //nolint:bodyclose // doGet closes the body itself before returning.
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("GET %s status = %d, want 302", path, resp.StatusCode)
		}
		scope := mustQueryParam(t, resp.Header.Get("Location"), "scope")
		if scope != "openid" {
			t.Errorf("GET %s authorize scope = %q, want exactly %q", path, scope, "openid")
		}
		if extractCookie(resp, auth.OAuthTxCookieName) == nil {
			t.Errorf("GET %s did not set the transaction cookie", path)
		}
	}
}

// doViewCallback drives the Google or LinkedIn callback for a view-purpose
// transaction.
func doViewCallback(t *testing.T, handler http.Handler, callbackPath, code, state string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	path := callbackPath + "?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	return doGet(t, handler, path, cookies...) //nolint:bodyclose // doGet closes the body itself before returning.
}

// assertNoPassCookie fails if resp set a __Host-view-pass cookie.
func assertNoPassCookie(t *testing.T, resp *http.Response) {
	t.Helper()
	if c := extractCookie(resp, viewpass.CookieName); c != nil {
		t.Errorf("response set a %s cookie (value=%q), want none", viewpass.CookieName, c.Value)
	}
}

// assertValidPassCookie fails unless resp carries a pass valid for
// resumeID at epoch.
func assertValidPassCookie(t *testing.T, resp *http.Response, key []byte, resumeID uuid.UUID, epoch int32) {
	t.Helper()
	c := extractCookie(resp, viewpass.CookieName)
	if c == nil {
		t.Fatalf("response did not set a %s cookie", viewpass.CookieName)
	}
	if !c.Secure || !c.HttpOnly {
		t.Errorf("%s cookie Secure=%v HttpOnly=%v, want both true", viewpass.CookieName, c.Secure, c.HttpOnly)
	}
	if !viewpass.Valid(c.Value, key, resumeID, epoch, time.Now()) {
		t.Errorf("%s cookie is not a valid pass for resume %s at epoch %d", viewpass.CookieName, resumeID, epoch)
	}
}

// viewPassKeyBytes decodes the fixed test VIEW_PASS_KEY.
func viewPassKeyBytes(t *testing.T) []byte {
	t.Helper()
	key, err := base64.RawURLEncoding.Strict().DecodeString(testViewPassKey)
	if err != nil {
		t.Fatalf("decode testViewPassKey: %v", err)
	}
	return key
}

// TestGoogleViewCallback_LiveSwitchOn_SetsPassAndNeverTouchesAccounts is the
// core no-account, no-session proof for a subject with no existing account.
func TestGoogleViewCallback_LiveSwitchOn_SetsPassAndNeverTouchesAccounts(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	start := doGet(t, handler, viewStartPath(auth.GoogleStartPath, f.slug)) //nolint:bodyclose // doGet closes the body itself before returning.
	txCookie := extractCookie(start, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatal("view start did not set the transaction cookie")
	}
	state := mustQueryParam(t, start.Header.Get("Location"), "state")
	nonce := mustQueryParam(t, start.Header.Get("Location"), "nonce")

	subject := uniqueSubject(t)
	p.RegisterCode("code-view", oidctest.Claims{Subject: subject, Nonce: nonce})

	resp := doViewCallback(t, handler, auth.GoogleCallbackPath, "code-view", state, txCookie) //nolint:bodyclose // doViewCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	assertRedirectPath(t, resp.Header.Get("Location"), "/"+f.slug)
	if c := extractCookie(resp, auth.SessionCookieName); c != nil {
		t.Errorf("view callback set a session cookie (value=%q), want none", c.Value)
	}
	if c := extractCookie(resp, pendingCookieName); c != nil {
		t.Errorf("view callback set a pending-authentication cookie (value=%q), want none", c.Value)
	}
	tx := extractCookie(resp, auth.OAuthTxCookieName)
	if tx == nil || tx.MaxAge >= 0 {
		t.Errorf("view callback did not clear the transaction cookie: %#v", tx)
	}
	assertValidPassCookie(t, resp, viewPassKeyBytes(t), f.id, 1)
	assertNoIdentity(t, q, subject)
}

// TestLinkedInViewCallback_NoNonce_SetsPassAndNeverTouchesAccounts proves the
// view purpose accepts LinkedIn's nonce-less token exactly like login, but
// still never resolves an account (AC-VIEW-004, AC-VIEW-005).
func TestLinkedInViewCallback_NoNonce_SetsPassAndNeverTouchesAccounts(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	start := doGet(t, handler, viewStartPath(auth.LinkedInStartPath, f.slug)) //nolint:bodyclose // doGet closes the body itself before returning.
	txCookie := extractCookie(start, auth.OAuthTxCookieName)
	state := mustQueryParam(t, start.Header.Get("Location"), "state")

	subject := uniqueLinkedInSubject(t)
	p.RegisterCode("code-view-li", oidctest.Claims{Subject: subject}) // no Nonce: LinkedIn's observed shape.

	resp := doViewCallback(t, handler, auth.LinkedInCallbackPath, "code-view-li", state, txCookie) //nolint:bodyclose // doViewCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	assertRedirectPath(t, resp.Header.Get("Location"), "/"+f.slug)
	if c := extractCookie(resp, auth.SessionCookieName); c != nil {
		t.Errorf("view callback set a session cookie (value=%q), want none", c.Value)
	}
	assertValidPassCookie(t, resp, viewPassKeyBytes(t), f.id, 1)
	assertNoLinkedInIdentity(t, q, subject)
}

// TestGoogleViewCallback_SubjectAlreadyHasAccount_StillNoAccountAccess
// proves a subject that already owns an account still gets no session and
// causes no read of that account (AC-VIEW-006).
func TestGoogleViewCallback_SubjectAlreadyHasAccount_StillNoAccountAccess(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	// An existing account under a distinct provider identity.
	subject := uniqueSubject(t)
	email := uniqueEmail(t)
	existingUser, err := q.CreateUser(context.Background(), store.CreateUserParams{Email: email, Name: "Existing"})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	if _, createErr := q.CreateIdentity(context.Background(), store.CreateIdentityParams{
		UserID: existingUser.ID, Provider: string(auth.ProviderGoogle), ProviderUserID: subject,
	}); createErr != nil {
		t.Fatalf("CreateIdentity() error = %v", createErr)
	}
	before, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
		Provider: string(auth.ProviderGoogle), ProviderUserID: subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject() before error = %v", err)
	}

	start := doGet(t, handler, viewStartPath(auth.GoogleStartPath, f.slug)) //nolint:bodyclose // doGet closes the body itself before returning.
	txCookie := extractCookie(start, auth.OAuthTxCookieName)
	state := mustQueryParam(t, start.Header.Get("Location"), "state")
	nonce := mustQueryParam(t, start.Header.Get("Location"), "nonce")
	p.RegisterCode("code-existing", oidctest.Claims{Subject: subject, Nonce: nonce})

	resp := doViewCallback(t, handler, auth.GoogleCallbackPath, "code-existing", state, txCookie) //nolint:bodyclose // doViewCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	if c := extractCookie(resp, auth.SessionCookieName); c != nil {
		t.Errorf("view callback signed in the existing account (session cookie value=%q), want none", c.Value)
	}
	assertValidPassCookie(t, resp, viewPassKeyBytes(t), f.id, 1)

	after, err := q.GetIdentityByProviderSubject(context.Background(), store.GetIdentityByProviderSubjectParams{
		Provider: string(auth.ProviderGoogle), ProviderUserID: subject,
	})
	if err != nil {
		t.Fatalf("GetIdentityByProviderSubject() after error = %v", err)
	}
	if after.UserID != before.UserID || after.CreatedAt != before.CreatedAt {
		t.Errorf("existing identity row changed: before=%+v after=%+v, want unchanged", before, after)
	}
}

// TestGoogleViewCallback_SwitchWentOff_RedirectsWithNoPass covers the switch
// turning off between start and callback.
func TestGoogleViewCallback_SwitchWentOff_RedirectsWithNoPass(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	start := doGet(t, handler, viewStartPath(auth.GoogleStartPath, f.slug)) //nolint:bodyclose // doGet closes the body itself before returning.
	txCookie := extractCookie(start, auth.OAuthTxCookieName)
	state := mustQueryParam(t, start.Header.Get("Location"), "state")
	nonce := mustQueryParam(t, start.Header.Get("Location"), "nonce")

	f.republish(t, q, true, false) // still live, switch off.

	p.RegisterCode("code-switch-off", oidctest.Claims{Subject: uniqueSubject(t), Nonce: nonce})
	resp := doViewCallback(t, handler, auth.GoogleCallbackPath, "code-switch-off", state, txCookie) //nolint:bodyclose // doViewCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	assertRedirectPath(t, resp.Header.Get("Location"), "/"+f.slug)
	assertNoPassCookie(t, resp)
}

// TestGoogleViewCallback_Unpublished_RedirectsHome covers the resume going
// not-live between start and callback.
func TestGoogleViewCallback_Unpublished_RedirectsHome(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	start := doGet(t, handler, viewStartPath(auth.GoogleStartPath, f.slug)) //nolint:bodyclose // doGet closes the body itself before returning.
	txCookie := extractCookie(start, auth.OAuthTxCookieName)
	state := mustQueryParam(t, start.Header.Get("Location"), "state")
	nonce := mustQueryParam(t, start.Header.Get("Location"), "nonce")

	f.republish(t, q, false, true) // unpublished.

	p.RegisterCode("code-unpublished", oidctest.Claims{Subject: uniqueSubject(t), Nonce: nonce})
	resp := doViewCallback(t, handler, auth.GoogleCallbackPath, "code-unpublished", state, txCookie) //nolint:bodyclose // doViewCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	assertRedirectPath(t, resp.Header.Get("Location"), "/")
	assertNoPassCookie(t, resp)
}

// TestGoogleViewCallback_SlugChanged_RedirectsToCurrentSlug proves the
// callback redirects to the resume's current slug, not the one requested at
// start, and still sets a pass (a pass names the resume ID, not the slug).
func TestGoogleViewCallback_SlugChanged_RedirectsToCurrentSlug(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	start := doGet(t, handler, viewStartPath(auth.GoogleStartPath, f.slug)) //nolint:bodyclose // doGet closes the body itself before returning.
	txCookie := extractCookie(start, auth.OAuthTxCookieName)
	state := mustQueryParam(t, start.Header.Get("Location"), "state")
	nonce := mustQueryParam(t, start.Header.Get("Location"), "nonce")

	newSlug := "view-fixture-renamed-" + uuid.NewString()[:8]
	f.slug = newSlug
	f.republish(t, q, true, true)

	p.RegisterCode("code-slug-changed", oidctest.Claims{Subject: uniqueSubject(t), Nonce: nonce})
	resp := doViewCallback(t, handler, auth.GoogleCallbackPath, "code-slug-changed", state, txCookie) //nolint:bodyclose // doViewCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	assertRedirectPath(t, resp.Header.Get("Location"), "/"+newSlug)
	assertValidPassCookie(t, resp, viewPassKeyBytes(t), f.id, 1)
}

// TestGoogleViewCallback_StateMismatch_RedirectsToGateFailed proves a
// failure once the transaction is read redirects to the gate, not the login
// page (AC-VIEW-005; design "Sign-in flow").
func TestGoogleViewCallback_StateMismatch_RedirectsToGateFailed(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	start := doGet(t, handler, viewStartPath(auth.GoogleStartPath, f.slug)) //nolint:bodyclose // doGet closes the body itself before returning.
	txCookie := extractCookie(start, auth.OAuthTxCookieName)

	resp := doViewCallback(t, handler, auth.GoogleCallbackPath, "irrelevant-code", "not-the-real-state", txCookie) //nolint:bodyclose // doViewCallback -> doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect Location: %v", err)
	}
	if loc.Path != "/"+f.slug {
		t.Errorf("redirect path = %q, want %q", loc.Path, "/"+f.slug)
	}
	if got := loc.Query().Get("signin"); got != "failed" {
		t.Errorf("signin query = %q, want %q", got, "failed")
	}
	assertNoPassCookie(t, resp)
}

// TestGoogleViewCallback_ConsentDenied_RedirectsToGateCancelled covers the
// cancel outcome's distinct signin value.
func TestGoogleViewCallback_ConsentDenied_RedirectsToGateCancelled(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	start := doGet(t, handler, viewStartPath(auth.GoogleStartPath, f.slug)) //nolint:bodyclose // doGet closes the body itself before returning.
	txCookie := extractCookie(start, auth.OAuthTxCookieName)
	state := mustQueryParam(t, start.Header.Get("Location"), "state")

	path := auth.GoogleCallbackPath + "?error=access_denied&state=" + url.QueryEscape(state)
	resp := doGet(t, handler, path, txCookie) //nolint:bodyclose // doGet closes the body itself before returning.
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("callback status = %d, want 302", resp.StatusCode)
	}
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatalf("parse redirect Location: %v", err)
	}
	if loc.Path != "/"+f.slug || loc.Query().Get("signin") != "cancelled" { //nolint:misspell // Exact wire value uses double-L "cancelled".
		t.Errorf("redirect = %q, want path %q with signin=cancelled", resp.Header.Get("Location"), "/"+f.slug) //nolint:misspell // Exact wire value uses double-L "cancelled".
	}
}

// TestViewCallback_MissingTransactionCookie_GoesToLoginPage proves a
// failure before the transaction is read names no resume and behaves like
// any other purpose's missing-cookie failure.
func TestViewCallback_MissingTransactionCookie_GoesToLoginPage(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, withGoogleIssuer(p.URL), withViewPassKey())

	resp := doGet(t, handler, auth.GoogleCallbackPath+"?code=x&state=y") //nolint:bodyclose // doGet closes the body itself before returning.
	assertRejected(t, resp)                                              // asserts the /login redirect path.
}
