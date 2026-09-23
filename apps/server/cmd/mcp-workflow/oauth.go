package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"golang.org/x/oauth2"
)

var errReauthorizationDisabled = errors.New("reauthorization_disabled")

// Authorization-request rejections name the exact contract field that did not
// match, so a failed run reports the field without printing its value. Each
// wraps errInvalidCallback, which remains the closed OAuth failure.
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
	// first keeps the first authorization failure, so a refused second attempt
	// still reports the cause that ended the run.
	first error
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
		state, err := validateAuthorizationURL(args, redirectURI, issuer)
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
		if err := browser.Open(waitContext, args.URL); err != nil {
			return nil, err
		}
		return receiver.wait(waitContext)
	}
}

func validateAuthorizationURL(args *auth.AuthorizationArgs, redirectURI, issuer string) (string, error) {
	if args == nil {
		return "", errAuthorizationShape
	}
	origin, err := parseOrigin(issuer)
	if err != nil {
		return "", errAuthorizationShape
	}
	authorizationURL, err := url.Parse(args.URL)
	if err != nil || authorizationURL.Scheme != origin.Scheme || authorizationURL.Host != origin.Host || authorizationURL.User != nil || authorizationURL.Path != "/oauth/authorize" || authorizationURL.RawPath != "" || authorizationURL.Fragment != "" {
		return "", errAuthorizationShape
	}
	values, err := url.ParseQuery(authorizationURL.RawQuery)
	if err != nil || len(values) != 8 {
		return "", errAuthorizationShape
	}
	for _, key := range []string{"response_type", "client_id", "redirect_uri", "scope", "state", "code_challenge", "code_challenge_method", "resource"} {
		if len(values[key]) != 1 {
			return "", errAuthorizationShape
		}
	}
	if values.Get("redirect_uri") != redirectURI {
		return "", errAuthorizationRedirect
	}
	if values.Get("scope") != "resumes:read resumes:write" {
		return "", errAuthorizationScope
	}
	if values.Get("resource") != issuer || !canonicalResourceParameter(authorizationURL.RawQuery, issuer) {
		return "", errAuthorizationResource
	}
	state := values.Get("state")
	if values.Get("response_type") != "code" || values.Get("client_id") == "" || state == "" ||
		values.Get("code_challenge_method") != "S256" || !isS256Challenge(values.Get("code_challenge")) {
		return "", errAuthorizationChallenge
	}
	return state, nil
}

func canonicalResourceParameter(rawQuery, issuer string) bool {
	want := "resource=" + url.QueryEscape(issuer)
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
