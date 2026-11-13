// These tests drive the view-purpose start and callback for a project page:
// the start accepts only the closed page grammar, and every redirect returns
// to that page under the resume's current slug
// (docs/design/projects-contract.md "Render envelope").
package auth_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/auth/oidctest"
)

func viewStartPagePath(base, slug, rawPage string) string {
	return viewStartPath(base, slug) + "&page=" + rawPage
}

// requireExactRedirect fails unless Location is exactly the test origin plus
// path and query.
func requireExactRedirect(t *testing.T, resp *http.Response, pathAndQuery string) {
	t.Helper()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("status = %d, want 302", resp.StatusCode)
	}
	if got, want := resp.Header.Get("Location"), testPublicOrigin+pathAndQuery; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
	}
}

func TestViewStart_MalformedPage_BadRequest(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	for _, rawPage := range []string{
		"", "https%3A%2F%2Fevil.example", "//evil.example", "projects%2Fnote-g", `projects\note-g`,
		"projects/../note-g", "projects/note-g/extra", "Projects", "projects/a", "projects/note-g&page=projects",
	} {
		for _, base := range []string{auth.GoogleStartPath, auth.LinkedInStartPath} {
			path := viewStartPagePath(base, f.slug, rawPage)
			resp := doGet(t, handler, path) //nolint:bodyclose // doGet closes the body itself before returning.
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("GET %s status = %d, want 400", path, resp.StatusCode)
			}
			if extractCookie(resp, auth.OAuthTxCookieName) != nil {
				t.Errorf("GET %s set a transaction cookie, want none", path)
			}
		}
	}
}

func TestViewStart_PageWithoutTheSwitch_RedirectsToThePage(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withViewPassKey())
	off := newViewFixture(t, q, false)

	resp := doGet(t, handler, viewStartPagePath(auth.GoogleStartPath, off.slug, "projects/note-g")) //nolint:bodyclose // doGet closes the body itself before returning.
	requireExactRedirect(t, resp, "/"+off.slug+"/projects/note-g")
	if extractCookie(resp, auth.OAuthTxCookieName) != nil {
		t.Error("view start on a switch-off resume set a transaction cookie, want none")
	}
}

// TestGoogleViewCallback_ProjectPage_ReturnsUnderTheCurrentSlug proves the
// callback returns to the project page under the resume's renamed slug and
// sets the pass.
func TestGoogleViewCallback_ProjectPage_ReturnsUnderTheCurrentSlug(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	start := doGet(t, handler, viewStartPagePath(auth.GoogleStartPath, f.slug, "projects/note-g")) //nolint:bodyclose // doGet closes the body itself before returning.
	txCookie := extractCookie(start, auth.OAuthTxCookieName)
	if txCookie == nil {
		t.Fatal("view start did not set the transaction cookie")
	}
	state := mustQueryParam(t, start.Header.Get("Location"), "state")
	nonce := mustQueryParam(t, start.Header.Get("Location"), "nonce")

	f.slug = "view-fixture-renamed-" + uuid.NewString()[:8]
	f.republish(t, q, true, true)

	p.RegisterCode("code-project-page", oidctest.Claims{Subject: uniqueSubject(t), Nonce: nonce})
	resp := doViewCallback(t, handler, auth.GoogleCallbackPath, "code-project-page", state, txCookie) //nolint:bodyclose // doViewCallback -> doGet closes the body itself before returning.
	requireExactRedirect(t, resp, "/"+f.slug+"/projects/note-g")
	assertValidPassCookie(t, resp, viewPassKeyBytes(t), f.id, 1)
}

func TestLinkedInViewCallback_ProjectList_ReturnsToTheList(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withLinkedInIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	start := doGet(t, handler, viewStartPagePath(auth.LinkedInStartPath, f.slug, "projects")) //nolint:bodyclose // doGet closes the body itself before returning.
	txCookie := extractCookie(start, auth.OAuthTxCookieName)
	state := mustQueryParam(t, start.Header.Get("Location"), "state")

	p.RegisterCode("code-project-list", oidctest.Claims{Subject: uniqueLinkedInSubject(t)})
	resp := doViewCallback(t, handler, auth.LinkedInCallbackPath, "code-project-list", state, txCookie) //nolint:bodyclose // doViewCallback -> doGet closes the body itself before returning.
	requireExactRedirect(t, resp, "/"+f.slug+"/projects")
	assertValidPassCookie(t, resp, viewPassKeyBytes(t), f.id, 1)
}

// TestGoogleViewCallback_ProjectPage_FailureAndCancelReturnToThePage covers
// both closed failure messages on a project page.
func TestGoogleViewCallback_ProjectPage_FailureAndCancelReturnToThePage(t *testing.T) {
	t.Parallel()
	p := oidctest.NewProvider(t)
	handler, q := newTestService(t, withGoogleIssuer(p.URL), withViewPassKey())
	f := newViewFixture(t, q, true)

	start := doGet(t, handler, viewStartPagePath(auth.GoogleStartPath, f.slug, "projects/note-g"))                                                       //nolint:bodyclose // doGet closes the body itself before returning.
	failed := doViewCallback(t, handler, auth.GoogleCallbackPath, "irrelevant-code", "not-the-real-state", extractCookie(start, auth.OAuthTxCookieName)) //nolint:bodyclose // doViewCallback -> doGet closes the body itself before returning.
	requireExactRedirect(t, failed, "/"+f.slug+"/projects/note-g?signin=failed")
	assertNoPassCookie(t, failed)

	start = doGet(t, handler, viewStartPagePath(auth.GoogleStartPath, f.slug, "projects")) //nolint:bodyclose // doGet closes the body itself before returning.
	state := mustQueryParam(t, start.Header.Get("Location"), "state")
	path := auth.GoogleCallbackPath + "?error=access_denied&state=" + url.QueryEscape(state)
	canceled := doGet(t, handler, path, extractCookie(start, auth.OAuthTxCookieName)) //nolint:bodyclose // doGet closes the body itself before returning.
	requireExactRedirect(t, canceled, "/"+f.slug+"/projects?signin=cancelled")        //nolint:misspell // Exact wire value uses double-L "cancelled".
}
