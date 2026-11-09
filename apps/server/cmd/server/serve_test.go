package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestServePairSeparatesPublicAndPrivateRoutesAndJoins(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	listen := func() net.Listener {
		var listenConfig net.ListenConfig
		listener, err := listenConfig.Listen(ctx, "tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if closeErr := listener.Close(); closeErr != nil && !errors.Is(closeErr, net.ErrClosed) {
				t.Error(closeErr)
			}
		})
		return listener
	}
	publicListener, privateListener := listen(), listen()
	publicMux, privateMux := http.NewServeMux(), http.NewServeMux()
	publicMux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	privateMux.HandleFunc("POST /internal-render/print/redeem", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	var stops atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- servePair(ctx, slog.New(slog.NewTextHandler(io.Discard, nil)), publicListener, publicMux, privateListener, privateMux, func() { stops.Add(1) })
	}()
	client := &http.Client{Timeout: time.Second}
	for _, check := range []struct {
		listener     net.Listener
		method, path string
		want         int
	}{
		{publicListener, "GET", "/healthz", 200},
		{publicListener, "POST", "/internal-render/print/redeem", 404},
		{privateListener, "POST", "/internal-render/print/redeem", 204},
		{privateListener, "GET", "/healthz", 404},
	} {
		request, err := http.NewRequestWithContext(ctx, check.method, "http://"+check.listener.Addr().String()+check.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if response.StatusCode != check.want {
			t.Fatalf("%s status=%d, want%d", check.path, response.StatusCode, check.want)
		}
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("listeners did not join")
	}
	if stops.Load() != 1 {
		t.Fatalf("stop count=%d", stops.Load())
	}
	for _, listener := range []net.Listener{publicListener, privateListener} {
		dialer := net.Dialer{Timeout: time.Second}
		connection, err := dialer.DialContext(t.Context(), "tcp", listener.Addr().String())
		if err == nil {
			if closeErr := connection.Close(); closeErr != nil {
				t.Error(closeErr)
			}
			t.Fatal("listener still open")
		}
	}
}

// TestServe_DrainsInFlightMCPRequestBeforeReturning is a regression test for
// graceful shutdown: canceling the context must not cut off an MCP request
// that's already being handled. serve must wait for it to finish (up to
// shutdownTimeout) before returning, exactly like a SIGTERM during a real
// deploy is supposed to drain in-flight work rather than abort it.
func TestServe_DrainsInFlightMCPRequestBeforeReturning(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}

	started := make(chan struct{})
	release := make(chan struct{})
	var completed atomic.Bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release // held open until the test explicitly releases it
		completed.Store(true)
		w.WriteHeader(http.StatusOK)
	})

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	ctx, cancel := context.WithCancel(context.Background())

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- serve(ctx, logger, ln, handler)
	}()

	reqDone := make(chan error, 1)
	go func() {
		// Deliberately context.Background(), not ctx: this is the client's
		// request context, independent of the server's shutdown-trigger
		// context above. Tying it to ctx would cancel the in-flight request
		// the moment cancel() fires below, defeating the point of this test.
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://"+ln.Addr().String()+"/mcp", nil)
		if err != nil {
			reqDone <- err
			return
		}
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			err = resp.Body.Close()
		}
		reqDone <- err
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("handler never started; request did not reach the server")
	}

	// Trigger shutdown while the handler is blocked mid-request.
	cancel()

	// serve() must not return while the in-flight request is still being
	// held open — give shutdown a moment to (incorrectly) race ahead before
	// we release the handler, so a bug that aborts in-flight work would
	// show up as serveDone firing before we release() below.
	select {
	case err := <-serveDone:
		t.Fatalf("serve() returned before the in-flight request completed; shutdown did not drain: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if completed.Load() {
		t.Fatal("handler completed before being released — test setup is broken")
	}

	close(release)

	select {
	case err := <-serveDone:
		if err != nil {
			t.Fatalf("serve() error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve() did not return after the in-flight request completed")
	}

	if !completed.Load() {
		t.Error("in-flight request never completed")
	}
	if err := <-reqDone; err != nil {
		t.Errorf("client request error: %v", err)
	}
}
