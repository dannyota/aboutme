package publicapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"danny.vn/aboutme/apps/server/internal/publicstate"
)

// TestServeLeasedSelected_FailsClosedWithTheRouteError proves a cancel hook
// that cannot register leaves exactly the route's own 503: none of the
// selected response's headers or bytes are written.
func TestServeLeasedSelected_FailsClosedWithTheRouteError(t *testing.T) {
	response, err := NewSelectedResponse(http.StatusOK, "text/plain; charset=utf-8", "no-cache, must-revalidate", []byte("resume bytes\n"),
		http.Header{"X-Robots-Tag": {"noindex, noarchive"}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name        string
		unavailable func(http.ResponseWriter, *http.Request, int)
	}{
		{name: "public_json", unavailable: servePublicJSONError},
		{name: "text", unavailable: serveTextError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := httptest.NewRecorder()
			test.unavailable(want, newLeaseTestRequest("/ada"), http.StatusServiceUnavailable)
			got := httptest.NewRecorder()
			serveLeasedSelected(got, newLeaseTestRequest("/ada"), &recordingHooks{err: errors.New("cannot register")}, response, test.unavailable)
			assertOnlyResponse(t, got, want)
		})
	}
}

// TestServeGate_FailsClosedWhenTheHookCannotRegister proves the gate writes
// only the HTML 503, with none of the gate's headers, when its cancel hook
// cannot register, and logs the closed reason.
func TestServeGate_FailsClosedWhenTheHookCannotRegister(t *testing.T) {
	dependencies, _ := leaseTestGateDependencies(t)
	logger, logs := newGateCapturingLogger()
	dependencies.Logger = logger
	snapshot, lease, err := dependencies.Reader.ReadResume(context.Background(), "ada", publicstate.RepresentationHTML)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()

	want := httptest.NewRecorder()
	serveHTMLError(want, newLeaseTestRequest("/ada"), http.StatusServiceUnavailable)
	got := httptest.NewRecorder()
	serveGate(lease.Context(), got, newLeaseTestRequest("/ada"), dependencies, snapshot, "", &recordingHooks{err: errors.New("cannot register")})
	assertOnlyResponse(t, got, want)
	if !strings.Contains(logs.String(), "reason=lease_hook_failed") {
		t.Fatalf("log = %q, want the lease_hook_failed reason", logs.String())
	}
}

func assertOnlyResponse(t *testing.T, got, want *httptest.ResponseRecorder) {
	t.Helper()
	if got.Code != want.Code || got.Body.String() != want.Body.String() {
		t.Fatalf("response = %d %q, want %d %q", got.Code, got.Body.String(), want.Code, want.Body.String())
	}
	if !reflect.DeepEqual(got.Header(), want.Header()) {
		t.Fatalf("headers = %v, want only the error's %v", got.Header(), want.Header())
	}
}
