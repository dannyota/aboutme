package main

import (
	"context"
	"errors"
	"net/http"
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
	state, err := validateAuthorizationURL(&auth.AuthorizationArgs{URL: raw}, redirectURI, issuer)
	if err != nil || state != "state" {
		t.Fatalf("validateAuthorizationURL = %q, %v", state, err)
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
			if _, err := validateAuthorizationURL(&auth.AuthorizationArgs{URL: tc.raw}, redirectURI, issuer); !errors.Is(err, errInvalidCallback) {
				t.Fatalf("validateAuthorizationURL error = %v, want %v", err, errInvalidCallback)
			}
		})
	}
}
