package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/oauth2"
)

var (
	errRuntime = errors.New("runtime_unavailable")
	// errTLSRoots names a trust store that could not be built, and errConnect
	// carries the SDK discovery, registration, or authorization failure so the
	// one-word report can name the stage.
	errTLSRoots = fmt.Errorf("%w: tls roots", errRuntime)
	// errTLSOverride names a production run whose environment names an
	// SSL_CERT_FILE or SSL_CERT_DIR override. x509.SystemCertPool honors
	// both, so an inherited local-mode override (or one set by any other
	// caller) would make production silently trust a non-system root instead
	// of refusing outright.
	errTLSOverride = fmt.Errorf("%w: tls override", errRuntime)
	errConnect     = fmt.Errorf("%w: connect", errWorkflowBlocked)
	// errCallbackBind names a loopback listener that could not bind.
	errCallbackBind = fmt.Errorf("%w: callback listener", errConnect)
)

const (
	mcpEndpointPath   = "/mcp"
	tokenEndpointPath = "/oauth/token"
)

// workflowDeps is the narrow runtime boundary. Tests inject every field;
// newRuntimeDeps wires the official SDK, the one-shot OAuth gate, and the
// restricted HTTP client.
type workflowDeps struct {
	Connect       func(context.Context) (sdkToolClient, error)
	Observations  serverObservations
	Tokens        func(context.Context) (accessToken, refreshToken string, err error)
	Revocation    revocationTransport
	WaitCandidate func(context.Context, privateArtifacts) error
	NewKey        func() (string, error)
	LocalNow      func() time.Time
}

// serverObservations exposes same-origin server facts seen by the restricted
// transport. Marks order observations without retaining request content.
type serverObservations interface {
	mark() uint64
	authenticatedDateSince(mark uint64) (time.Time, bool)
	tokenDates() (first, latest time.Time, ok bool)
	deniedSince(mark uint64, accessDigest string) (time.Time, bool)
	reauthorizationRefusedSince(mark uint64) bool
}

type dateEvent struct {
	seq  uint64
	date time.Time
}

// serverObserver records only sequence numbers, response dates, statuses, and
// a SHA-256 of the bearer used on /mcp. It never stores tokens or bodies.
type serverObserver struct {
	base http.RoundTripper

	mu            sync.Mutex
	seq           uint64
	authenticated dateEvent
	denied        dateEvent
	deniedDigest  string
	firstToken    time.Time
	latestToken   time.Time
	refusedSeq    uint64
}

func (o *serverObserver) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := o.base.RoundTrip(request)
	if err != nil || response == nil {
		return response, err
	}
	date, dated := responseDate(response)
	bearer, hasBearer := strings.CutPrefix(request.Header.Get("Authorization"), "Bearer ")
	o.mu.Lock()
	defer o.mu.Unlock()
	o.seq++
	switch {
	case request.URL.Path == mcpEndpointPath && hasBearer && dated && response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices:
		o.authenticated = dateEvent{seq: o.seq, date: date}
	case request.URL.Path == mcpEndpointPath && hasBearer && response.StatusCode == http.StatusUnauthorized:
		o.denied = dateEvent{seq: o.seq, date: date}
		o.deniedDigest = digest([]byte(bearer))
	case request.URL.Path == tokenEndpointPath && request.Method == http.MethodPost && dated && response.StatusCode == http.StatusOK:
		if o.firstToken.IsZero() {
			o.firstToken = date
		}
		o.latestToken = date
	}
	return response, nil
}

func (o *serverObserver) mark() uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.seq
}

func (o *serverObserver) authenticatedDateSince(mark uint64) (time.Time, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.authenticated.date, o.authenticated.seq > mark && !o.authenticated.date.IsZero()
}

func (o *serverObserver) tokenDates() (time.Time, time.Time, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.firstToken, o.latestToken, !o.firstToken.IsZero() && !o.latestToken.Before(o.firstToken)
}

func (o *serverObserver) deniedSince(mark uint64, accessDigest string) (time.Time, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.denied.date, o.denied.seq > mark && !o.denied.date.IsZero() && o.deniedDigest == accessDigest
}

func (o *serverObserver) reauthorizationRefusedSince(mark uint64) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.refusedSeq > mark
}

func (o *serverObserver) recordRefusal() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.refusedSeq = o.seq + 1
	o.seq++
}

// observedOAuth records when the one-shot gate refuses reauthorization. It
// adds no authority of its own.
type observedOAuth struct {
	inner    auth.OAuthHandler
	observer *serverObserver
}

func (h observedOAuth) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	return h.inner.TokenSource(ctx)
}

func (h observedOAuth) Authorize(ctx context.Context, request *http.Request, response *http.Response) error {
	err := h.inner.Authorize(ctx, request, response)
	if errors.Is(err, errReauthorizationDisabled) {
		h.observer.recordRefusal()
		if response != nil && response.Body != nil {
			if closeErr := response.Body.Close(); closeErr != nil {
				return errors.Join(err, errRuntime)
			}
		}
	}
	return err
}

// runtimeToolClient closes the callback listener with the MCP session.
type runtimeToolClient struct {
	session  sdkToolClient
	listener loopbackListener
	once     sync.Once
	err      error
}

func (c *runtimeToolClient) ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error) {
	return c.session.ListTools(ctx, params)
}

func (c *runtimeToolClient) CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error) {
	return c.session.CallTool(ctx, params)
}

func (c *runtimeToolClient) Close() error {
	c.once.Do(func() {
		c.err = c.session.Close()
		if err := closeListener(c.listener); err != nil && c.err == nil {
			c.err = err
		}
	})
	return c.err
}

// finishConnect clears the browser-handoff files only after a successful SDK
// connect. A failed connect must leave browser-result.json in place: the
// launcher still has to read the browser helper's own result word from it
// before the launcher's own cleanup removes the whole browser root. See
// docs/design/mcp-owner-workflow.md#browser-helper-interface.
func finishConnect(browserRoot string, connected bool) error {
	if !connected {
		return nil
	}
	return removeBrowserFiles(browserRoot)
}

// closeListener reports nothing when the callback receiver already closed the
// listener, so cleanup never masks the failure that ended the run.
func closeListener(listener loopbackListener) error {
	if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

// newBaseTransport builds the trust store explicitly, so a local run that
// points SSL_CERT_FILE or SSL_CERT_DIR at the development root fails with a
// named reason instead of an opaque handshake error.
func newBaseTransport() (http.RoundTripper, error) {
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		return nil, errTLSRoots
	}
	transport := &http.Transport{}
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = defaultTransport.Clone()
	}
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	return transport, nil
}

// newRuntimeDeps wires the production and local runtime: the official SDK
// session behind the one-shot OAuth gate, all HTTP through one restricted
// same-origin client, and server time taken only from observed responses.
func newRuntimeDeps(config workflowConfig) (workflowDeps, error) {
	if config.Mode == modeProduction &&
		(os.Getenv("SSL_CERT_FILE") != "" || os.Getenv("SSL_CERT_DIR") != "") {
		return workflowDeps{}, errTLSOverride
	}
	origin, err := parseOrigin(config.Origin)
	if err != nil {
		return workflowDeps{}, errRuntime
	}
	base, err := newBaseTransport()
	if err != nil {
		return workflowDeps{}, err
	}
	observer := &serverObserver{base: base}
	client := newRestrictedHTTPClient(origin, observer)
	var (
		handlerMu sync.Mutex
		handler   auth.OAuthHandler
	)
	connect := func(ctx context.Context) (sdkToolClient, error) {
		listener, listenErr := newLoopbackListener(ctx)
		if listenErr != nil {
			return nil, errors.Join(errCallbackBind, listenErr)
		}
		redirectURI := "http://" + listener.Addr().String() + "/oauth/callback"
		browser := newBrowserCoordinator(config.BrowserRoot, config.Mode, config.Origin)
		gate, gateErr := newAuthorizationHandler(client, redirectURI, config.Origin, browser, listener)
		if gateErr != nil {
			return nil, errors.Join(errConnect, gateErr, closeListener(listener))
		}
		observed := observedOAuth{inner: gate, observer: observer}
		session, connectErr := connectSDKClient(ctx, config.Origin, client, observed)
		cleanupErr := finishConnect(config.BrowserRoot, connectErr == nil)
		if connectErr != nil {
			return nil, errors.Join(errConnect, connectErr, closeListener(listener))
		}
		if cleanupErr != nil {
			return nil, errors.Join(errConnect, cleanupErr, session.Close(), closeListener(listener))
		}
		handlerMu.Lock()
		handler = observed
		handlerMu.Unlock()
		return &runtimeToolClient{session: session, listener: listener}, nil
	}
	tokens := func(ctx context.Context) (string, string, error) {
		handlerMu.Lock()
		current := handler
		handlerMu.Unlock()
		if current == nil {
			return "", "", errRuntime
		}
		source, sourceErr := current.TokenSource(ctx)
		if sourceErr != nil || source == nil {
			return "", "", errRuntime
		}
		token, tokenErr := source.Token()
		if tokenErr != nil || token == nil {
			return "", "", errRuntime
		}
		return token.AccessToken, token.RefreshToken, nil
	}
	return workflowDeps{
		Connect:       connect,
		Observations:  observer,
		Tokens:        tokens,
		Revocation:    httpRevocation{client: client, origin: config.Origin},
		WaitCandidate: waitForCandidate,
		NewKey: func() (string, error) {
			key, keyErr := uuid.NewRandom()
			if keyErr != nil {
				return "", errRuntime
			}
			return key.String(), nil
		},
		LocalNow: time.Now,
	}, nil
}
