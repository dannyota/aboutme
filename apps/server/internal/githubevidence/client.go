package githubevidence

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"danny.vn/aboutme/apps/server/internal/config"
)

const (
	productionAuthorizeURL = "https://github.com/login/oauth/authorize"
	productionTokenURL     = "https://github.com/login/oauth/access_token"
	productionAPIBaseURL   = "https://api.github.com"
	githubAPIVersion       = "2022-11-28"
	maxLoginLength         = 100
)

var (
	errDestinationRefused = errors.New("githubevidence: destination refused")
	errRedirectRefused    = errors.New("githubevidence: redirect refused")
	errTimeout            = errors.New("githubevidence: request timed out")
	errTransport          = errors.New("githubevidence: request failed")
	errResponseTooLarge   = errors.New("githubevidence: response too large")
	errUpstreamRejected   = errors.New("githubevidence: upstream rejected the request")
	errMalformed          = errors.New("githubevidence: malformed upstream response")
	errScopeRejected      = errors.New("githubevidence: token scope rejected")
	errRequestLimit       = errors.New("githubevidence: request limit reached")
	errTokenGone          = errors.New("githubevidence: token discarded")
	errAmbiguousToken     = fmt.Errorf("%w: more than one access token", errMalformed)
)

type client struct {
	http         *http.Client
	authorizeURL string
	tokenURL     string
	userURL      string
	revokeURL    string
	clientID     string
	clientSecret config.Secret
	requests     *windowLimiter
	now          func() time.Time
	hold         func(value string) *heldToken
}

type githubProfile struct {
	ID    int64
	Login string
}

func newTransport() *http.Transport {
	return &http.Transport{
		Proxy:                  nil,
		DialContext:            (&net.Dialer{Timeout: requestTimeout}).DialContext,
		TLSClientConfig:        &tls.Config{MinVersion: tls.VersionTLS12},
		TLSHandshakeTimeout:    requestTimeout,
		ResponseHeaderTimeout:  requestTimeout,
		MaxResponseHeaderBytes: 64 << 10,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           4,
		IdleConnTimeout:        90 * time.Second,
	}
}

func newClient(clientID string, clientSecret config.Secret, endpoint string, transport http.RoundTripper, requests *windowLimiter, now func() time.Time) *client {
	authorizeURL, tokenURL, apiBase := productionAuthorizeURL, productionTokenURL, productionAPIBaseURL
	if endpoint != "" {
		authorizeURL = endpoint + "/login/oauth/authorize"
		tokenURL = endpoint + "/login/oauth/access_token"
		apiBase = endpoint
	}
	c := &client{
		authorizeURL: authorizeURL,
		tokenURL:     tokenURL,
		userURL:      apiBase + "/user",
		revokeURL:    apiBase + "/applications/" + url.PathEscape(clientID) + "/token",
		clientID:     clientID,
		clientSecret: clientSecret,
		requests:     requests,
		now:          now,
	}
	c.http = &http.Client{
		Transport: guardedTransport{base: transport, allowed: map[string]bool{
			http.MethodPost + " " + c.tokenURL:    true,
			http.MethodGet + " " + c.userURL:      true,
			http.MethodDelete + " " + c.revokeURL: true,
		}},
		Timeout: requestTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errRedirectRefused
		},
	}
	return c
}

type guardedTransport struct {
	base    http.RoundTripper
	allowed map[string]bool
}

func (g guardedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !g.permits(req) {
		if req.Body != nil {
			req.Body.Close() //nolint:errcheck,gosec // net/http requires RoundTrip to close the request body; a close error changes nothing here
		}
		return nil, errDestinationRefused
	}
	return g.base.RoundTrip(req)
}

func (g guardedTransport) permits(req *http.Request) bool {
	u := req.URL
	if u == nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	return g.allowed[req.Method+" "+u.Scheme+"://"+u.Host+u.EscapedPath()]
}

func (c *client) authorizeLink(state, verifier, redirectURI string) string {
	values := url.Values{
		"client_id":             {c.clientID},
		"redirect_uri":          {redirectURI},
		"state":                 {state},
		"code_challenge":        {oauth2.S256ChallengeFromVerifier(verifier)},
		"code_challenge_method": {"S256"},
	}
	return c.authorizeURL + "?" + values.Encode()
}

func (c *client) admit(account uuid.UUID) bool {
	allowed, _ := c.requests.admit(account, c.now())
	return allowed
}

func (c *client) send(ctx context.Context, method, target string, body []byte, prepare func(http.Header)) (int, http.Header, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return 0, nil, nil, errDestinationRefused
	}
	prepare(req.Header)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, classifyTransportError(ctx, err)
	}
	defer resp.Body.Close() //nolint:errcheck // the body is read whole below; a close error changes nothing
	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		clear(payload)
		return 0, nil, nil, classifyTransportError(ctx, err)
	}
	if len(payload) > maxResponseBytes {
		clear(payload)
		return 0, nil, nil, errResponseTooLarge
	}
	return resp.StatusCode, resp.Header, payload, nil
}

func classifyTransportError(ctx context.Context, err error) error {
	var netErr net.Error
	switch {
	case errors.Is(err, errRedirectRefused):
		return errRedirectRefused
	case errors.Is(err, errDestinationRefused):
		return errDestinationRefused
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded),
		errors.As(err, &netErr) && netErr.Timeout():
		return errTimeout
	default:
		return errTransport
	}
}

func (c *client) exchange(ctx context.Context, account uuid.UUID, code, redirectURI, verifier string) (*heldToken, error) {
	if !c.admit(account) {
		return nil, errRequestLimit
	}
	form := url.Values{
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret.Reveal()},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
	}
	encoded := []byte(form.Encode())
	defer clear(encoded)
	status, header, body, err := c.send(ctx, http.MethodPost, c.tokenURL, encoded, func(h http.Header) {
		h.Set("Content-Type", "application/x-www-form-urlencoded")
		h.Set("Accept", "application/json")
	})
	if err != nil {
		return nil, err
	}
	defer clear(body)
	value, ambiguous := accessTokenCandidate(body)
	if ambiguous {
		return nil, errAmbiguousToken
	}
	var token *heldToken
	if value != "" {
		token = c.holdToken(value)
	}
	if status != http.StatusOK || !jsonMediaType(header) {
		return token, errUpstreamRejected
	}
	fields, err := strictObject(body)
	if err != nil {
		return token, errMalformed
	}
	if token == nil {
		return nil, errUpstreamRejected
	}
	if _, failed := fields["error"]; failed {
		return token, errUpstreamRejected
	}
	if kind, isString := jsonString(fields["token_type"]); !isString || !strings.EqualFold(kind, "bearer") {
		return token, errMalformed
	}
	scope, present := fields["scope"]
	if !present {
		return token, errScopeRejected
	}
	if granted, isString := jsonString(scope); !isString || granted != "" {
		return token, errScopeRejected
	}
	return token, nil
}

func (c *client) holdToken(value string) *heldToken {
	if c.hold == nil {
		return newHeldToken(value)
	}
	return c.hold(value)
}

func accessTokenCandidate(body []byte) (string, bool) {
	dec := json.NewDecoder(bytes.NewReader(body))
	if open, err := dec.Token(); err != nil || open != json.Delim('{') {
		return "", false
	}
	var found string
	for dec.More() {
		key, err := dec.Token()
		name, isString := key.(string)
		if err != nil || !isString {
			break
		}
		var raw json.RawMessage
		if decodeErr := dec.Decode(&raw); decodeErr != nil {
			break
		}
		if name != "access_token" {
			continue
		}
		value, isValue := jsonString(raw)
		if !isValue || value == "" {
			continue
		}
		if found != "" && found != value {
			return "", true
		}
		found = value
	}
	return found, false
}

func (c *client) profile(ctx context.Context, account uuid.UUID, token *heldToken) (githubProfile, error) {
	if !c.admit(account) {
		return githubProfile{}, errRequestLimit
	}
	value, ok := token.peek()
	if !ok {
		return githubProfile{}, errTokenGone
	}
	status, header, body, err := c.send(ctx, http.MethodGet, c.userURL, nil, func(h http.Header) {
		h.Set("Accept", "application/vnd.github+json")
		h.Set("X-GitHub-Api-Version", githubAPIVersion)
		h.Set("Authorization", "Bearer "+value)
	})
	if err != nil {
		return githubProfile{}, err
	}
	defer clear(body)
	if status != http.StatusOK || !jsonMediaType(header) {
		return githubProfile{}, errUpstreamRejected
	}
	fields, err := strictObject(body)
	if err != nil {
		return githubProfile{}, errMalformed
	}
	id, err := strconv.ParseInt(string(fields["id"]), 10, 64)
	if err != nil || id <= 0 {
		return githubProfile{}, errMalformed
	}
	login, ok := jsonString(fields["login"])
	if !ok || !validLogin(login) {
		return githubProfile{}, errMalformed
	}
	return githubProfile{ID: id, Login: login}, nil
}

func (c *client) revoke(ctx context.Context, token string) error {
	body, err := json.Marshal(struct { //nolint:gosec // GitHub's token revocation API takes the token in this JSON body
		AccessToken string `json:"access_token"`
	}{AccessToken: token})
	if err != nil {
		return errMalformed
	}
	defer clear(body)
	status, _, response, err := c.send(ctx, http.MethodDelete, c.revokeURL, body, func(h http.Header) {
		h.Set("Accept", "application/vnd.github+json")
		h.Set("Content-Type", "application/json")
		h.Set("X-GitHub-Api-Version", githubAPIVersion)
		h.Set("Authorization", basicAuthorization(c.clientID, c.clientSecret.Reveal()))
	})
	clear(response)
	if err != nil {
		return err
	}
	if status != http.StatusNoContent {
		return errUpstreamRejected
	}
	return nil
}

func basicAuthorization(user, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(user+":"+password))
}

func jsonMediaType(header http.Header) bool {
	mediaType, _, err := mime.ParseMediaType(header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

func strictObject(body []byte) (map[string]json.RawMessage, error) {
	if !json.Valid(body) {
		return nil, errMalformed
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	if open, err := dec.Token(); err != nil || open != json.Delim('{') {
		return nil, errMalformed
	}
	fields := map[string]json.RawMessage{}
	for dec.More() {
		key, err := dec.Token()
		name, isString := key.(string)
		if err != nil || !isString {
			return nil, errMalformed
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, errMalformed
		}
		var raw json.RawMessage
		if decodeErr := dec.Decode(&raw); decodeErr != nil {
			return nil, errMalformed
		}
		fields[name] = raw
	}
	if end, err := dec.Token(); err != nil || end != json.Delim('}') {
		return nil, errMalformed
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errMalformed
	}
	return fields, nil
}

func jsonString(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

func validLogin(login string) bool {
	if login == "" || len([]rune(login)) > maxLoginLength {
		return false
	}
	for _, r := range login {
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == unicode.ReplacementChar {
			return false
		}
	}
	return true
}
