package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

var errReauthorizationDisabled = errors.New("reauthorization_disabled")

var (
	errAuthorizationShape     = fmt.Errorf("%w: authorization request shape", errInvalidCallback)
	errAuthorizationRedirect  = fmt.Errorf("%w: authorization redirect", errInvalidCallback)
	errAuthorizationScope     = fmt.Errorf("%w: authorization scope", errInvalidCallback)
	errAuthorizationResource  = fmt.Errorf("%w: authorization resource", errInvalidCallback)
	errAuthorizationChallenge = fmt.Errorf("%w: authorization challenge", errInvalidCallback)
)

type oauthGate struct {
	delegate auth.OAuthHandler
	mu       sync.Mutex
	used     bool
	first    error
}

func (g *oauthGate) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	return g.delegate.TokenSource(ctx)
}

func (g *oauthGate) Authorize(ctx context.Context, req *http.Request, response *http.Response) error {
	g.mu.Lock()
	if g.used {
		first := g.first
		g.mu.Unlock()
		return errors.Join(errReauthorizationDisabled, first)
	}
	g.used = true
	g.mu.Unlock()
	err := g.delegate.Authorize(ctx, req, response)
	g.mu.Lock()
	g.first = err
	g.mu.Unlock()
	return err
}

func newAuthorizationHandler(client *http.Client, redirectURI, issuer string, browser browserHandoff, listener loopbackListener) (auth.OAuthHandler, error) {
	metadata := &oauthex.ClientRegistrationMetadata{
		RedirectURIs:            []string{redirectURI},
		TokenEndpointAuthMethod: "none",
		ClientName:              "aboutme MCP owner workflow",
	}
	callbackFetcher := newCallbackFetcher(redirectURI, issuer, browser, listener)
	handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		DynamicClientRegistrationConfig: &auth.DynamicClientRegistrationConfig{Metadata: metadata},
		RedirectURL:                     redirectURI,
		AuthorizationCodeFetcher:        callbackFetcher,
		Client:                          client,
	})
	if err != nil {
		return nil, err
	}
	return &oauthGate{delegate: handler}, nil
}

func newCallbackFetcher(redirectURI, issuer string, browser browserHandoff, listener loopbackListener) auth.AuthorizationCodeFetcher {
	return func(ctx context.Context, args *auth.AuthorizationArgs) (result *auth.AuthorizationResult, err error) {
		openURL, state, err := validateAuthorizationURL(args, redirectURI, issuer)
		if err != nil {
			return nil, err
		}
		waitContext, cancel := context.WithTimeout(ctx, browserTimeout)
		defer cancel()
		receiver := startCallback(listener, state, issuer)
		defer func() {
			if closeErr := receiver.close(ctx); closeErr != nil && err == nil {
				result = nil
				err = closeErr
			}
		}()
		if err := browser.Open(waitContext, openURL); err != nil {
			return nil, err
		}
		return receiver.wait(waitContext)
	}
}

// RFC 6749 section 3.3 gives scope order no meaning, and the MCP Go SDK's authutil.UnionScopes returns any order at random.
const workflowScope = "resumes:read resumes:write projects:read projects:write"

func canonicalWorkflowScope(raw string) bool {
	want := strings.Fields(workflowScope)
	fields := strings.Split(raw, " ")
	if len(fields) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		if !slices.Contains(want, field) || seen[field] {
			return false
		}
		seen[field] = true
	}
	return true
}

func validateAuthorizationURL(args *auth.AuthorizationArgs, redirectURI, issuer string) (string, string, error) {
	if args == nil {
		return "", "", errAuthorizationShape
	}
	origin, err := parseOrigin(issuer)
	if err != nil {
		return "", "", errAuthorizationShape
	}
	authorizationURL, err := url.Parse(args.URL)
	if err != nil || authorizationURL.Scheme != origin.Scheme || authorizationURL.Host != origin.Host || authorizationURL.User != nil || authorizationURL.Path != "/oauth/authorize" || authorizationURL.RawPath != "" || authorizationURL.Fragment != "" {
		return "", "", errAuthorizationShape
	}
	values, err := url.ParseQuery(authorizationURL.RawQuery)
	if err != nil || len(values) != 8 {
		return "", "", errAuthorizationShape
	}
	for _, key := range []string{"response_type", "client_id", "redirect_uri", "scope", "state", "code_challenge", "code_challenge_method", "resource"} {
		if len(values[key]) != 1 {
			return "", "", errAuthorizationShape
		}
	}
	if values.Get("redirect_uri") != redirectURI {
		return "", "", errAuthorizationRedirect
	}
	scope := values.Get("scope")
	if !canonicalWorkflowScope(scope) {
		return "", "", errAuthorizationScope
	}
	resource := issuer + mcpEndpointPath
	if values.Get("resource") != resource || !canonicalResourceParameter(authorizationURL.RawQuery, resource) {
		return "", "", errAuthorizationResource
	}
	state := values.Get("state")
	if values.Get("response_type") != "code" || values.Get("client_id") == "" || state == "" ||
		values.Get("code_challenge_method") != "S256" || !isS256Challenge(values.Get("code_challenge")) {
		return "", "", errAuthorizationChallenge
	}
	if scope != workflowScope {
		values.Set("scope", workflowScope)
		authorizationURL.RawQuery = values.Encode()
	}
	return authorizationURL.String(), state, nil
}

func canonicalResourceParameter(rawQuery, resource string) bool {
	want := "resource=" + url.QueryEscape(resource)
	for _, field := range strings.Split(rawQuery, "&") {
		if strings.HasPrefix(field, "resource=") {
			return field == want
		}
	}
	return false
}

func isS256Challenge(raw string) bool {
	if len(raw) != 43 {
		return false
	}
	decoded, err := base64.RawURLEncoding.Strict().DecodeString(raw)
	return err == nil && len(decoded) == 32
}
