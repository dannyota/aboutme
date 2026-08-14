package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestLoadConfigAcceptsOnlyNativeHTTPSHarnessValues(t *testing.T) {
	t.Parallel()

	valid := map[string]string{
		"LISTEN_HOST":          "127.0.0.1",
		"PORT":                 "20442",
		"PUBLIC_ORIGIN":        "https://localhost:20443",
		"GOOGLE_CLIENT_ID":     "aboutme-local-google",
		"GOOGLE_CLIENT_SECRET": "not-a-secret-local-google",
	}
	tests := []struct {
		name  string
		field string
		value string
	}{
		{name: "host", field: "LISTEN_HOST", value: "0.0.0.0"},
		{name: "port", field: "PORT", value: "8080"},
		{name: "origin", field: "PUBLIC_ORIGIN", value: "https://localhost"},
		{name: "client id", field: "GOOGLE_CLIENT_ID", value: "real-looking"},
		{name: "client secret", field: "GOOGLE_CLIENT_SECRET", value: "real-looking"},
	}

	getenv := func(values map[string]string) func(string) string {
		return func(key string) string { return values[key] }
	}
	cfg, err := loadConfig(getenv(valid))
	if err != nil {
		t.Fatalf("loadConfig(valid): %v", err)
	}
	if cfg.IssuerURL != "http://127.0.0.1:20442/google" {
		t.Fatalf("IssuerURL = %q", cfg.IssuerURL)
	}
	if cfg.RedirectURL != "https://localhost:20443/api/v1/auth/google/callback" {
		t.Fatalf("RedirectURL = %q", cfg.RedirectURL)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := make(map[string]string, len(valid))
			for key, value := range valid {
				values[key] = value
			}
			values[tt.field] = tt.value
			if _, err := loadConfig(getenv(values)); err == nil {
				t.Fatal("loadConfig() error = nil")
			}
		})
	}
}

func TestServeDrainsInFlightRequest(t *testing.T) {
	t.Parallel()

	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusNoContent)
	})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, ln, handler) }()
	requestDone := make(chan error, 1)
	go func() {
		req, requestErr := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+ln.Addr().String(), nil)
		if requestErr != nil {
			requestDone <- requestErr
			return
		}
		resp, requestErr := http.DefaultClient.Do(req)
		if requestErr == nil {
			requestErr = resp.Body.Close()
		}
		requestDone <- requestErr
	}()

	select {
	case <-started:
	case requestErr := <-requestDone:
		t.Fatalf("request before handler started: %v", requestErr)
	case <-time.After(5 * time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case <-done:
		t.Fatal("serve returned before request drained")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("serve did not stop")
	}
	if requestErr := <-requestDone; requestErr != nil {
		t.Errorf("request: %v", requestErr)
	}
}
