package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

// TestServe_DrainsInFlightRequestBeforeReturning is a regression test for
// graceful shutdown: canceling the context must not cut off a request
// that's already being handled. serve must wait for it to finish (up to
// shutdownTimeout) before returning, exactly like a SIGTERM during a real
// deploy is supposed to drain in-flight work rather than abort it.
func TestServe_DrainsInFlightRequestBeforeReturning(t *testing.T) {
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
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+ln.Addr().String()+"/", nil)
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
	case <-serveDone:
		t.Fatal("serve() returned before the in-flight request completed — shutdown did not drain")
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
