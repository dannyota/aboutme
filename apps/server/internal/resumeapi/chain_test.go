package resumeapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

func TestCompositeRateLimitKeyRequiresAccountAndClientIP(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	if _, ok := resumeRateLimitKey(req, nil); ok {
		t.Fatal("rate key succeeded without authenticated account context")
	}
	req = req.WithContext(api.WithAccountID(req.Context(), "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f"))
	got, ok := resumeRateLimitKey(req, nil)
	if !ok || got != "acct:01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f|ip:203.0.113.10" {
		t.Fatalf("key = (%q, %v)", got, ok)
	}
}

type failOnReadBody struct{ reads int }

func (b *failOnReadBody) Read([]byte) (int, error) {
	b.reads++
	return 0, errors.New("body was read")
}

func (*failOnReadBody) Close() error { return nil }

type deadlineRecorder struct{ *httptest.ResponseRecorder }

func (*deadlineRecorder) SetReadDeadline(time.Time) error { return nil }

func TestPhotoHeaderRejectionDoesNotReadBody(t *testing.T) {
	h := newResumeAPITestHarness(t)
	body := &failOnReadBody{}
	path := "/api/v1/resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo"
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, nil)
	req.Body = body
	req.ContentLength = -1
	req.TransferEncoding = []string{"chunked"}
	req.AddCookie(h.cookie)
	req.Header.Set("Origin", resumeAPITestOrigin)
	req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body=%s)", rec.Code, rec.Body.Bytes())
	}
	if body.reads != 0 {
		t.Fatalf("body reads = %d, want 0", body.reads)
	}
}

func TestResumeRouteRatePoliciesUseExactIndependentAccountAndIPBudgets(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name     string
		requests int
		selectMW func(routeChains) api.Middleware
	}{
		{name: "read 600 per minute", requests: 600, selectMW: func(chains routeChains) api.Middleware { return chains.read }},
		{name: "write 240 per minute", requests: 240, selectMW: func(chains routeChains) api.Middleware { return chains.write }},
		{name: "upload 20 per hour", requests: 20, selectMW: func(chains routeChains) api.Middleware { return chains.upload }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			clock := testutil.NewClockAtEpoch()
			service := &Service{clock: clock.Now}
			handler := test.selectMW(service.newRouteChains())(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			send := func(accountID, remoteAddr string) int {
				req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
				req = req.WithContext(api.WithAccountID(req.Context(), accountID))
				req.RemoteAddr = remoteAddr
				recorder := httptest.NewRecorder()
				handler.ServeHTTP(recorder, req)
				return recorder.Code
			}
			account := uuid.NewString()
			for attempt := 1; attempt <= test.requests; attempt++ {
				if got := send(account, "203.0.113.10:1234"); got != http.StatusNoContent {
					t.Fatalf("attempt %d status = %d, want 204", attempt, got)
				}
			}
			if got := send(account, "203.0.113.10:1234"); got != http.StatusTooManyRequests {
				t.Fatalf("attempt %d status = %d, want 429", test.requests+1, got)
			}
			if got := send(account, "203.0.113.11:1234"); got != http.StatusNoContent {
				t.Fatalf("same account, distinct IP status = %d, want independent 204", got)
			}
			if got := send(uuid.NewString(), "203.0.113.10:1234"); got != http.StatusNoContent {
				t.Fatalf("distinct account, same IP status = %d, want independent 204", got)
			}
		})
	}
}

func TestPhotoOuterSessionCSRFMediaTypeAndRateFailuresDoNotReadBody(t *testing.T) {
	h := newResumeAPITestHarness(t)
	path := "/api/v1/resumes/01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f/photo"
	for _, test := range []struct {
		name         string
		authenticate bool
		origin       string
		token        string
		contentType  string
		wantStatus   int
	}{
		{name: "session", contentType: "application/json", wantStatus: http.StatusUnauthorized},
		{name: "origin", authenticate: true, origin: "https://foreign.example", token: h.csrfToken, contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "token", authenticate: true, origin: resumeAPITestOrigin, contentType: "application/json", wantStatus: http.StatusForbidden},
		{name: "media type", authenticate: true, origin: resumeAPITestOrigin, token: h.csrfToken, contentType: "application/json", wantStatus: http.StatusUnsupportedMediaType},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := &failOnReadBody{}
			req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, nil)
			req.Body = body
			req.ContentLength = -1
			req.TransferEncoding = []string{"chunked"}
			if test.authenticate {
				req.AddCookie(h.cookie)
			}
			if test.origin != "" {
				req.Header.Set("Origin", test.origin)
			}
			if test.token != "" {
				req.Header.Set(auth.CSRFHeaderName, test.token)
			}
			req.Header.Set("Content-Type", test.contentType)
			recorder := httptest.NewRecorder()
			h.handler.ServeHTTP(recorder, req)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if body.reads != 0 {
				t.Fatalf("body reads = %d, want zero", body.reads)
			}
		})
	}

	for attempt := 1; attempt <= resumeUploadRequests+1; attempt++ {
		body := &failOnReadBody{}
		req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, nil)
		req.Body = body
		req.ContentLength = -1
		req.TransferEncoding = []string{"chunked"}
		req.AddCookie(h.cookie)
		req.Header.Set("Origin", resumeAPITestOrigin)
		req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
		req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
		req.Header.Set("Idempotency-Key", uuid.NewString())
		req.Header.Set("If-Match", `"r1"`)
		recorder := &deadlineRecorder{ResponseRecorder: httptest.NewRecorder()}
		h.handler.ServeHTTP(recorder, req)
		wantStatus := http.StatusBadRequest
		if attempt == resumeUploadRequests+1 {
			wantStatus = http.StatusTooManyRequests
		}
		if recorder.Code != wantStatus {
			t.Fatalf("rate attempt %d status = %d, want %d (body=%s)", attempt, recorder.Code, wantStatus, recorder.Body.String())
		}
		if attempt == resumeUploadRequests+1 && body.reads != 0 {
			t.Fatalf("rate-limited attempt body reads = %d, want zero", body.reads)
		}
		if attempt <= resumeUploadRequests && body.reads == 0 {
			t.Fatalf("admitted attempt %d did not reach the streaming body reader", attempt)
		}
	}
}
