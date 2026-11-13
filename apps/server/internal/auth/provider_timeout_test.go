package auth_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/auth/oidctest"
)

const (
	providerHTTPBudget     = 10 * time.Second
	stalledProviderTimeout = 300 * time.Millisecond
	stalledProviderBound   = 10 * stalledProviderTimeout
	oidcDiscoveryPath      = "/.well-known/openid-configuration"
)

type stalledFlow struct {
	handler  http.Handler
	request  *http.Request
	entered  <-chan struct{}
	release  func()
	wantFail func(*testing.T, *http.Response)
}

type oidcLogin struct {
	issuer       func(string) testServiceOption
	startPath    string
	callbackPath string
	begin        func(*testing.T, http.Handler) (*http.Cookie, string, string)
}

var (
	googleLogin   = oidcLogin{withGoogleIssuer, auth.GoogleStartPath, auth.GoogleCallbackPath, beginGoogle}
	linkedinLogin = oidcLogin{withLinkedInIssuer, auth.LinkedInStartPath, auth.LinkedInCallbackPath, beginLinkedIn}
)

func getRequest(path string, cookies ...*http.Cookie) *http.Request {
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return req
}

func callbackRequest(path, code, state string, cookie *http.Cookie) *http.Request {
	return getRequest(path+"?code="+url.QueryEscape(code)+"&state="+url.QueryEscape(state), cookie)
}

func (l oidcLogin) stallStart(t *testing.T) stalledFlow {
	p := oidctest.NewProvider(t)
	handler, _ := newTestService(t, l.issuer(p.URL))
	entered, release := p.StallForTest(oidcDiscoveryPath)
	return stalledFlow{handler, getRequest(l.startPath), entered, release, assertInternalErrorClosed}
}

func (l oidcLogin) stallCallback(path string, freshInstance bool) func(*testing.T) stalledFlow {
	return func(t *testing.T) stalledFlow {
		p := oidctest.NewProvider(t)
		handler, _ := newTestService(t, l.issuer(p.URL))
		cookie, state, nonce := l.begin(t, handler)
		code := "stalled-" + uuid.NewString()
		p.RegisterCode(code, oidctest.Claims{Subject: uniqueSubject(t), Nonce: nonce})

		wantFail := assertAuthFailedClosed
		if freshInstance {
			handler, _ = newTestService(t, l.issuer(p.URL))
			wantFail = assertInternalErrorClosed
		}
		entered, release := p.StallForTest(path)
		return stalledFlow{handler, callbackRequest(l.callbackPath, code, state, cookie), entered, release, wantFail}
	}
}

func stallGitHubCallback(path string) func(*testing.T) stalledFlow {
	return func(t *testing.T) stalledFlow {
		code, accessToken := "stalled-"+uuid.NewString(), "stalled-"+uuid.NewString()
		gh := newGitHubStub(t,
			withTokenResponse(code, accessToken),
			withUser(uniqueGitHubUserID(t), "stalled"),
			withEmails([]ghEmail{{Email: uniqueEmail(t), Primary: true, Verified: true}}),
		)
		frontURL, entered, release := stallProviderPath(t, gh.URL, path)
		handler, _ := newTestService(t, withGitHubEndpoint(frontURL))
		cookie, state := beginGitHubFlow(t, handler)
		return stalledFlow{handler, callbackRequest(auth.GitHubCallbackPath, code, state, cookie), entered, release, assertAuthFailedClosed}
	}
}

func stallProviderPath(t *testing.T, upstream, path string) (frontURL string, entered <-chan struct{}, release func()) {
	t.Helper()
	target, err := url.Parse(upstream)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	enteredCh, releaseCh := make(chan struct{}), make(chan struct{})
	var enteredOnce, releaseOnce sync.Once
	release = func() { releaseOnce.Do(func() { close(releaseCh) }) }

	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			proxy.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read stalled request body", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		enteredOnce.Do(func() { close(enteredCh) })
		select {
		case <-releaseCh:
			proxy.ServeHTTP(w, r)
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(front.Close)
	t.Cleanup(release)
	return front.URL, enteredCh, release
}

func runStalledFlow(t *testing.T, flow stalledFlow) {
	t.Helper()
	defer flow.release()

	rec := httptest.NewRecorder()
	done := make(chan struct{})
	began := time.Now()
	go func() {
		defer close(done)
		flow.handler.ServeHTTP(rec, flow.request)
	}()

	select {
	case <-done:
	case <-time.After(stalledProviderBound):
		t.Errorf("flow still running %s after it began, with the provider timeout at %s", stalledProviderBound, stalledProviderTimeout)
		flow.release()
		select {
		case <-done:
		case <-time.After(time.Minute):
			t.Fatal("flow still running a minute after the stalled stage was released")
		}
	}
	elapsed := time.Since(began)

	select {
	case <-flow.entered:
	default:
		t.Fatalf("flow ended after %s without reaching the stalled stage", elapsed)
	}

	resp := rec.Result()
	if err := resp.Body.Close(); err != nil {
		t.Errorf("close response body: %v", err)
	}
	flow.wantFail(t, resp)
}

func assertInternalErrorClosed(t *testing.T, resp *http.Response) {
	t.Helper()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
	if loc := resp.Header.Get("Location"); loc != "" {
		t.Errorf("Location = %q, want none", loc)
	}
	if sc := extractCookie(resp, auth.SessionCookieName); sc != nil {
		t.Errorf("response set a %s cookie, want none", auth.SessionCookieName)
	}
	if tx := extractCookie(resp, auth.OAuthTxCookieName); tx == nil || tx.MaxAge >= 0 || tx.Value != "" {
		t.Errorf("%s cookie = %+v, want it cleared", auth.OAuthTxCookieName, tx)
	}
}

func assertAuthFailedClosed(t *testing.T, resp *http.Response) {
	t.Helper()
	if code := assertRejected(t, resp); code != "auth_failed" {
		t.Errorf("error = %q, want auth_failed", code)
	}
}

func TestProviderHTTP_TimeoutCoversEveryStage(t *testing.T) {
	if got := auth.ProviderHTTPTimeoutForTest(); got != providerHTTPBudget {
		t.Fatalf("provider HTTP client timeout = %s, want the %s budget", got, providerHTTPBudget)
	}
	auth.SetProviderHTTPTimeoutForTest(t, stalledProviderTimeout)

	stages := []struct {
		name    string
		prepare func(*testing.T) stalledFlow
	}{
		{"google/discovery at start", googleLogin.stallStart},
		{"google/discovery at callback", googleLogin.stallCallback(oidcDiscoveryPath, true)},
		{"google/token exchange", googleLogin.stallCallback("/token", false)},
		{"google/jwks", googleLogin.stallCallback("/jwks.json", false)},
		{"linkedin/discovery at start", linkedinLogin.stallStart},
		{"linkedin/discovery at callback", linkedinLogin.stallCallback(oidcDiscoveryPath, true)},
		{"linkedin/token exchange", linkedinLogin.stallCallback("/token", false)},
		{"linkedin/jwks", linkedinLogin.stallCallback("/jwks.json", false)},
		{"github/token exchange", stallGitHubCallback("/login/oauth/access_token")},
		{"github/profile read", stallGitHubCallback("/user")},
		{"github/email read", stallGitHubCallback("/user/emails")},
	}
	for _, stage := range stages {
		t.Run(stage.name, func(t *testing.T) {
			runStalledFlow(t, stage.prepare(t))
		})
	}
}
