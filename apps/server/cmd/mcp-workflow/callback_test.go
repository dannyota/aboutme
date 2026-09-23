package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestCallbackValidation(t *testing.T) {
	t.Parallel()
	valid := url.Values{"code": {"code"}, "state": {"state"}, "iss": {"https://aboutme.vn"}}
	if !validCallback(valid, "state", "https://aboutme.vn") {
		t.Fatal("valid callback rejected")
	}
	for _, values := range []url.Values{
		{"code": {"code"}, "state": {"wrong"}, "iss": {"https://aboutme.vn"}},
		{"code": {"code"}, "state": {"state"}},
		{"code": {"code", "second"}, "state": {"state"}, "iss": {"https://aboutme.vn"}},
		{"code": {"code"}, "state": {"state"}, "iss": {"https://evil.example"}},
	} {
		if validCallback(values, "state", "https://aboutme.vn") {
			t.Fatalf("invalid callback accepted: %v", values)
		}
	}
}

func TestCallback(t *testing.T) {
	listener, err := newLoopbackListener(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	receiver := startCallback(listener, "state", "https://aboutme.vn")
	t.Cleanup(func() {
		if closeErr := receiver.close(context.Background()); closeErr != nil {
			t.Errorf("close callback receiver: %v", closeErr)
		}
	})
	response, err := getCallback(t.Context(), receiver.redirectURI()+"?code=code&state=state&iss=https%3A%2F%2Faboutme.vn")
	if err != nil {
		t.Fatal(err)
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	result, err := receiver.wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if result.Code != "code" || result.State != "state" || result.Iss != "https://aboutme.vn" {
		t.Fatalf("callback = %#v", result)
	}
	replay, err := getCallback(t.Context(), receiver.redirectURI()+"?code=second&state=state&iss=https%3A%2F%2Faboutme.vn")
	if err != nil {
		t.Fatal(err)
	}
	if err := replay.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if replay.StatusCode != http.StatusConflict {
		t.Fatalf("replay status = %d, want %d", replay.StatusCode, http.StatusConflict)
	}
	select {
	case <-receiver.done:
	case <-time.After(time.Second):
		t.Fatal("replayed callback did not close listener")
	}
}

func TestInvalidCallbackFailsClosedAndCloses(t *testing.T) {
	listener, err := newLoopbackListener(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	receiver := startCallback(listener, "state", "https://aboutme.vn")
	response, err := getCallback(t.Context(), receiver.redirectURI()+"?code=code&state=wrong&iss=https%3A%2F%2Faboutme.vn")
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid callback status = %d, want %d", response.StatusCode, http.StatusBadRequest)
	}
	if _, err := receiver.wait(t.Context()); !errors.Is(err, errInvalidCallback) {
		t.Fatalf("wait error = %v, want %v", err, errInvalidCallback)
	}
	select {
	case <-receiver.done:
	case <-time.After(time.Second):
		t.Fatal("invalid callback did not close listener")
	}
}

func getCallback(ctx context.Context, rawURL string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(request)
}
