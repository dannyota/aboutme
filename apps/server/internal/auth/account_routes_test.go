package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAccountRouteDispatchesInjectedDeleteHandler(t *testing.T) {
	s := &Service{}
	called := false
	s.SetAccountDeleteHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))

	recorder := httptest.NewRecorder()
	s.handleAccount(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodDelete, MePath, nil))
	if recorder.Code != http.StatusNoContent || !called {
		t.Fatalf("DELETE /me = (%d, called=%v), want (204, true)", recorder.Code, called)
	}
}

func TestAccountRouteWithoutDeleteHandlerFailsClosed(t *testing.T) {
	s := &Service{}
	recorder := httptest.NewRecorder()
	s.handleAccount(recorder, httptest.NewRequestWithContext(context.Background(), http.MethodDelete, MePath, nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("DELETE /me = %d, want 503", recorder.Code)
	}
}
