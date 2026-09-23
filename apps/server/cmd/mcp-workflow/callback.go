package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
)

var errInvalidCallback = errors.New("invalid_callback")

const callbackShutdownTimeout = 5 * time.Second

type callbackReceiver struct {
	listener  loopbackListener
	state     string
	issuer    string
	path      string
	result    chan callbackResult
	server    *http.Server
	done      chan struct{}
	mu        sync.Mutex
	finished  bool
	closeOnce sync.Once
	closeErr  error
}

type callbackResult struct {
	result *auth.AuthorizationResult
	err    error
}

func startCallback(listener loopbackListener, state, issuer string) *callbackReceiver {
	c := &callbackReceiver{
		listener: listener,
		state:    state,
		issuer:   issuer,
		path:     "/oauth/callback",
		result:   make(chan callbackResult, 1),
		done:     make(chan struct{}),
	}
	c.server = &http.Server{Handler: http.HandlerFunc(c.handle)}
	go func() {
		if err := c.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			c.finish(callbackResult{err: err})
		}
		close(c.done)
	}()
	return c
}

func (c *callbackReceiver) redirectURI() string {
	return "http://" + c.listener.Addr().String() + c.path
}

func (c *callbackReceiver) handle(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet || request.URL.Path != c.path || request.Host != c.listener.Addr().String() || !validCallback(request.URL.Query(), c.state, c.issuer) {
		c.finish(callbackResult{err: errInvalidCallback})
		http.Error(w, "invalid callback", http.StatusBadRequest)
		go c.closeAfterCallback(request.Context())
		return
	}
	if !c.finish(callbackResult{result: &auth.AuthorizationResult{Code: request.URL.Query().Get("code"), State: c.state, Iss: c.issuer}}) {
		http.Error(w, "invalid callback", http.StatusConflict)
		go c.closeAfterCallback(request.Context())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (c *callbackReceiver) closeAfterCallback(ctx context.Context) {
	if err := c.close(ctx); err != nil {
		c.finish(callbackResult{err: err})
	}
}

func (c *callbackReceiver) finish(result callbackResult) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return false
	}
	c.finished = true
	c.result <- result
	return true
}

func validCallback(query url.Values, state, issuer string) bool {
	return len(query["code"]) == 1 && query.Get("code") != "" && len(query["state"]) == 1 && query.Get("state") == state && len(query["iss"]) == 1 && query.Get("iss") == issuer && len(query) == 3
}

func (c *callbackReceiver) wait(ctx context.Context) (*auth.AuthorizationResult, error) {
	select {
	case result := <-c.result:
		return result.result, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// close shuts the callback server down. Shutdown keeps its own bound even
// when the caller's context is already canceled.
func (c *callbackReceiver) close(ctx context.Context) error {
	c.closeOnce.Do(func() {
		shutdownContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), callbackShutdownTimeout)
		defer cancel()
		c.closeErr = c.server.Shutdown(shutdownContext)
		if errors.Is(c.closeErr, http.ErrServerClosed) {
			c.closeErr = nil
		}
		if c.closeErr != nil {
			if err := c.server.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				c.closeErr = err
			}
		}
	})
	select {
	case <-c.done:
		return c.closeErr
	case <-time.After(callbackShutdownTimeout):
		return context.DeadlineExceeded
	}
}

func newLoopbackListener(ctx context.Context) (loopbackListener, error) {
	return (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
}
