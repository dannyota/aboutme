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
		"https://evil.example/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite+projects%3Aread+projects%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn%2Fmcp",
		"https://aboutme.vn/other?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite+projects%3Aread+projects%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn%2Fmcp",
		"https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn%2Fmcp",
		"https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite+projects%3Aread+projects%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme%2Evn%2Fmcp",
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
	raw := "https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite+projects%3Aread+projects%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn%2Fmcp"
	openURL, state, err := validateAuthorizationURL(&auth.AuthorizationArgs{URL: raw}, redirectURI, issuer)
	if err != nil || state != "state" || openURL != raw {
		t.Fatalf("validateAuthorizationURL = %q, %q, %v", openURL, state, err)
	}
}

func TestAuthorizationURLValidationAcceptsAnySDKScopeOrder(t *testing.T) {
	const redirectURI = "http://127.0.0.1:20090/oauth/callback"
	const issuer = "https://aboutme.vn"
	const canonical = "https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite+projects%3Aread+projects%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn%2Fmcp"
	reversed := strings.Replace(canonical, "scope=resumes%3Aread+resumes%3Awrite+projects%3Aread+projects%3Awrite",
		"scope=projects%3Awrite+resumes%3Awrite+projects%3Aread+resumes%3Aread", 1)
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
			if got := opened.Query().Get("scope"); got != workflowScope {
				t.Fatalf("open URL scope = %q, want the canonical order regardless of input order", got)
			}
		})
	}
}

func TestAuthorizationURLValidationRejectsScopeOutsideTheClosedSet(t *testing.T) {
	const redirectURI = "http://127.0.0.1:20090/oauth/callback"
	const issuer = "https://aboutme.vn"
	const valid = "https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite+projects%3Aread+projects%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn%2Fmcp"
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"resume scopes only", strings.Replace(valid, "+projects%3Aread+projects%3Awrite", "", 1)},
		{"missing a scope", strings.Replace(valid, "+projects%3Awrite", "", 1)},
		{"repeated scope", strings.Replace(valid, "+projects%3Awrite", "+projects%3Aread", 1)},
		{"unknown scope", strings.Replace(valid, "+projects%3Awrite", "+projects%3Aadmin", 1)},
		{"extra whitespace", strings.Replace(valid, "+projects%3Awrite", "++projects%3Awrite", 1)},
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
	const valid = "https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite+projects%3Aread+projects%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn%2Fmcp"
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

func TestAuthorizationURLValidationRequiresMCPEndpointResource(t *testing.T) {
	const redirectURI = "http://127.0.0.1:20090/oauth/callback"
	const issuer = "https://aboutme.vn"
	const valid = "https://aboutme.vn/oauth/authorize?response_type=code&client_id=client&redirect_uri=http%3A%2F%2F127.0.0.1%3A20090%2Foauth%2Fcallback&scope=resumes%3Aread+resumes%3Awrite+projects%3Aread+projects%3Awrite&state=state&code_challenge=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA&code_challenge_method=S256&resource=https%3A%2F%2Faboutme.vn%2Fmcp"
	const mcpField = "resource=https%3A%2F%2Faboutme.vn%2Fmcp"
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"origin", strings.Replace(valid, mcpField, "resource=https%3A%2F%2Faboutme.vn", 1)},
		{"trailing slash origin", strings.Replace(valid, mcpField, "resource=https%3A%2F%2Faboutme.vn%2F", 1)},
		{"mcp trailing slash", strings.Replace(valid, mcpField, "resource=https%3A%2F%2Faboutme.vn%2Fmcp%2F", 1)},
		{"mcp upper case", strings.Replace(valid, mcpField, "resource=https%3A%2F%2Faboutme.vn%2FMCP", 1)},
		{"raw slash", strings.Replace(valid, mcpField, "resource=https%3A%2F%2Faboutme.vn/mcp", 1)},
		{"lower case hex", strings.Replace(valid, mcpField, "resource=https%3a%2f%2faboutme.vn%2fmcp", 1)},
		{"other host", strings.Replace(valid, mcpField, "resource=https%3A%2F%2Fagent.example%2Fmcp", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := validateAuthorizationURL(&auth.AuthorizationArgs{URL: tc.raw}, redirectURI, issuer); !errors.Is(err, errAuthorizationResource) {
				t.Fatalf("validateAuthorizationURL error = %v, want %v", err, errAuthorizationResource)
			}
		})
	}
}
