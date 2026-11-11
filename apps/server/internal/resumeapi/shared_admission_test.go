package resumeapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

func TestSharedAdmissionSpendsTheResumeRouteBudget(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		requests int
		shared   func(*Service) api.Middleware
		route    func(routeChains) api.Middleware
	}{
		{name: "read", requests: resumeReadRequests, shared: (*Service).ReadAdmission, route: func(c routeChains) api.Middleware { return c.read }},
		{name: "write", requests: resumeWriteRequests, shared: (*Service).WriteAdmission, route: func(c routeChains) api.Middleware { return c.write }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			clock := testutil.NewClockAtEpoch()
			service := &Service{clock: clock.Now}
			ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
			account := uuid.NewString()
			send := func(handler http.Handler) int {
				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
				req = req.WithContext(api.WithAccountID(req.Context(), account))
				req.RemoteAddr = "203.0.113.10:1234"
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, req)
				return recorder.Code
			}
			first, second := test.shared(service)(ok), test.shared(service)(ok)
			for attempt := 1; attempt < test.requests; attempt++ {
				if got := send(first); got != http.StatusNoContent {
					t.Fatalf("attempt %d status = %d, want 204", attempt, got)
				}
			}
			if got := send(second); got != http.StatusNoContent {
				t.Fatalf("last admitted attempt status = %d, want 204", got)
			}
			if got := send(test.route(service.sharedRouteChains())(ok)); got != http.StatusTooManyRequests {
				t.Fatalf("resume route after shared budget status = %d, want 429", got)
			}
		})
	}
}

func TestAllowSlugClaimSpendsThePublishSlugBudget(t *testing.T) {
	t.Parallel()

	service := New(nil, nil, nil, nil, Options{})
	account := uuid.New()
	now := time.Date(2035, 1, 1, 0, 0, 0, 0, time.UTC)
	for attempt := 1; attempt <= slugAttemptLimit; attempt++ {
		if !service.AllowSlugClaim(account, now) {
			t.Fatalf("claim %d was denied, want admitted", attempt)
		}
	}
	if admitChangedSlugAttempt(service.slugAttempts, account, now, publishPrepared{ChangedSlug: true}) {
		t.Fatal("publish slug change was admitted after the shared budget was spent")
	}
	if !service.AllowSlugClaim(uuid.New(), now) {
		t.Fatal("another account's claim was denied")
	}
}
