package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"golang.org/x/oauth2"
)

type fakeOAuthHandler struct {
	calls int
	err   error
}

type countingBrowser struct{ calls int }

func (b *countingBrowser) Open(context.Context, string) error {
	b.calls++
	return nil
}

func (h *fakeOAuthHandler) TokenSource(context.Context) (oauth2.TokenSource, error) { return nil, nil }
func (h *fakeOAuthHandler) Authorize(context.Context, *http.Request, *http.Response) error {
	h.calls++
	return h.err
}

func TestOAuthGate(t *testing.T) {
	t.Parallel()
	delegate := &fakeOAuthHandler{}
	gate := &oauthGate{delegate: delegate}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://aboutme.vn/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Authorize(t.Context(), request, &http.Response{}); err != nil {
		t.Fatalf("first Authorize error = %v", err)
	}
	if err := gate.Authorize(t.Context(), request, &http.Response{}); !errors.Is(err, errReauthorizationDisabled) {
		t.Fatalf("second Authorize error = %v, want %v", err, errReauthorizationDisabled)
	}
	if delegate.calls != 1 {
		t.Fatalf("Authorize calls = %d, want 1", delegate.calls)
	}
}

func TestOAuthGateStaysClosedAfterError(t *testing.T) {
	t.Parallel()
	delegate := &fakeOAuthHandler{err: errors.New("token error")}
	gate := &oauthGate{delegate: delegate}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://aboutme.vn/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.Authorize(t.Context(), request, &http.Response{}); err == nil {
		t.Fatal("first Authorize error = nil, want delegate error")
	}
	if err := gate.Authorize(t.Context(), request, &http.Response{}); !errors.Is(err, errReauthorizationDisabled) {
		t.Fatalf("second Authorize error = %v, want %v", err, errReauthorizationDisabled)
	}
}

func TestCallbackFetcherRejectsRemoteAuthorizationURLBeforeBrowserHandoff(t *testing.T) {
	browser := &countingBrowser{}
	fetcher := newCallbackFetcher("http://127.0.0.1:20090/oauth/callback", "https://aboutme.vn", browser, nil)
	for _, raw := range []string{
		"https://evil.example/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn",
		"https://aboutme.vn/other?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn",
		"https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn",
		"https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme%2Evn",
	} {
		if _, err := fetcher(t.Context(), &auth.AuthorizationArgs{URL: raw}); !errors.Is(err, errInvalidCallback) {
			t.Fatalf("fetcher(%q) error = %v, want %v", raw, err, errInvalidCallback)
		}
	}
	if browser.calls != 0 {
		t.Fatalf("browser handoffs = %d, want 0", browser.calls)
	}
}

func TestAuthorizationURLValidationAcceptsSDKShape(t *testing.T) {
	const redirectURI = "http://127.0.0.1:20090/oauth/callback"
	const issuer = "https://aboutme.vn"
	raw := "https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn"
	openURL, state, err := validateAuthorizationURL(&auth.AuthorizationArgs{URL: raw}, redirectURI, issuer)
	if err != nil || state != "state" || openURL != raw {
		t.Fatalf("validateAuthorizationURL = %q, %q, %v", openURL, state, err)
	}
}

// The pinned SDK's step-up scope union collects the requested set through a
// map (internal/authutil.UnionScopes), so it hands the authorization URL
// either scope order at random. Both must be accepted, and the browser must
// always be sent the canonical order the server's own authorize endpoint
// requires (see docs/design/mcp-owner-workflow.md#browser-helper-interface).
func TestAuthorizationURLValidationAcceptsEitherSDKScopeOrder(t *testing.T) {
	const redirectURI = "http://127.0.0.1:20090/oauth/callback"
	const issuer = "https://aboutme.vn"
	const canonical = "https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn"
	reversed := strings.Replace(canonical, "scope=resumes%3Aread+resumes%3Awrite", "scope=resumes%3Awrite+resumes%3Aread", 1)
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"canonical order", canonical},
		{"reversed order", reversed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			openURL, state, err := validateAuthorizationURL(&auth.AuthorizationArgs{URL: tc.raw}, redirectURI, issuer)
			if err != nil || state != "state" {
				t.Fatalf("validateAuthorizationURL = %q, %q, %v", openURL, state, err)
			}
			opened, err := url.Parse(openURL)
			if err != nil {
				t.Fatalf("parse open URL: %v", err)
			}
			if got := opened.Query().Get("scope"); got != "resumes:read resumes:write" {
				t.Fatalf("open URL scope = %q, want the canonical order regardless of input order", got)
			}
		})
	}
}

func TestAuthorizationURLValidationRejectsScopeOutsideTheClosedSet(t *testing.T) {
	const redirectURI = "http://127.0.0.1:20090/oauth/callback"
	const issuer = "https://aboutme.vn"
	const valid = "https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn"
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"missing a scope", strings.Replace(valid, "scope=resumes%3Aread+resumes%3Awrite", "scope=resumes%3Aread", 1)},
		{"repeated scope", strings.Replace(valid, "scope=resumes%3Aread+resumes%3Awrite", "scope=resumes%3Aread+resumes%3Aread", 1)},
		{"unknown scope", strings.Replace(valid, "scope=resumes%3Aread+resumes%3Awrite", "scope=resumes%3Aread+resumes%3Aadmin", 1)},
		{"extra whitespace", strings.Replace(valid, "scope=resumes%3Aread+resumes%3Awrite", "scope=resumes%3Aread++resumes%3Awrite", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := validateAuthorizationURL(&auth.AuthorizationArgs{URL: tc.raw}, redirectURI, issuer); !errors.Is(err, errAuthorizationScope) {
				t.Fatalf("validateAuthorizationURL error = %v, want %v", err, errAuthorizationScope)
			}
		})
	}
}

func TestAuthorizationURLValidationRejectsContractMismatch(t *testing.T) {
	const redirectURI = "http://127.0.0.1:20090/oauth/callback"
	const issuer = "https://aboutme.vn"
	const valid = "https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn"
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"response type", strings.Replace(valid, "response_type=code", "response_type=token", 1)},
		{"redirect URI", strings.Replace(valid, "127.0.0.1%3A20090", "127.0.0.1%3A20091", 1)},
		{"empty state", strings.Replace(valid, "state=state", "state=", 1)},
		{"duplicate state", valid + "&state=second"},
		{"PKCE method", strings.Replace(valid, "code_challenge_method=S256", "code_challenge_method=plain", 1)},
		{"PKCE challenge", strings.Replace(valid, "code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", "code_challenge=short", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := validateAuthorizationURL(&auth.AuthorizationArgs{URL: tc.raw}, redirectURI, issuer); !errors.Is(err, errInvalidCallback) {
				t.Fatalf("validateAuthorizationURL error = %v, want %v", err, errInvalidCallback)
			}
		})
	}
}
