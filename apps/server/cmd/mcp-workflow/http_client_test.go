package main

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestRestrictedHTTP(t *testing.T) {
	t.Parallel()
	origin, err := url.Parse("https://aboutme.vn")
	if err != nil {
		t.Fatal(err)
	}
	client := newRestrictedHTTPClient(origin, roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Request: req}, nil
	}))
	if client.Jar != nil {
		t.Fatal("client has a cookie jar")
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://aboutme.vn/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.Do(request)
	if err != nil {
		t.Fatalf("same-origin request: %v", err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	crossOriginRequest, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "https://evil.example/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	crossOriginResponse, err := client.Do(crossOriginRequest)
	if crossOriginResponse != nil && crossOriginResponse.Body != nil {
		if closeErr := crossOriginResponse.Body.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	if !errors.Is(err, errCrossOrigin) {
		t.Fatalf("cross-origin error = %v, want %v", err, errCrossOrigin)
	}
	if err := client.CheckRedirect(&http.Request{}, nil); !errors.Is(err, errCrossOrigin) {
		t.Fatalf("redirect error = %v, want %v", err, errCrossOrigin)
	}
}

func TestRestrictedHTTPDisablesAmbientProxy(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://ambient-proxy.invalid:8080")
	origin, err := url.Parse("https://aboutme.vn")
	if err != nil {
		t.Fatal(err)
	}
	client := newRestrictedHTTPClient(origin, nil)
	restricted, ok := client.Transport.(originTransport)
	if !ok {
		t.Fatalf("transport = %T, want originTransport", client.Transport)
	}
	transport, ok := restricted.base.(*http.Transport)
	if !ok {
		t.Fatalf("base transport = %T, want *http.Transport", restricted.base)
	}
	if transport.Proxy != nil {
		t.Fatal("restricted client has a proxy function")
	}
	if transport == http.DefaultTransport {
		t.Fatal("restricted client mutates the default transport")
	}
}
