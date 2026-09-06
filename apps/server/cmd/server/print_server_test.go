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
