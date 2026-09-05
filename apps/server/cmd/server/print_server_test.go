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

func TestServePairSeparatesPublicAndPrivateRoutesAndJoins(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listen := func() net.Listener {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = listener.Close() })
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
		request, err := http.NewRequest(check.method, "http://"+check.listener.Addr().String()+check.path, nil)
		if err != nil {
			t.Fatal(err)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		_ = response.Body.Close()
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
		connection, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
		if err == nil {
			_ = connection.Close()
			t.Fatal("listener still open")
		}
	}
}
