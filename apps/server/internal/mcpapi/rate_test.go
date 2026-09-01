package mcpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func testMCPRateConfig() RateConfig {
	return RateConfig{
		TokenRequests:     120,
		TokenWindow:       time.Minute,
		UserRequests:      240,
		UserWindow:        time.Minute,
		ConcurrentPerUser: 4,
		MaxKeys:           10_000,
		Clock:             func() time.Time { return time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC) },
	}
}

func ratePrincipal(userID, tokenID string) Principal {
	return Principal{UserID: uuid.MustParse(userID), TokenID: uuid.MustParse(tokenID), GrantID: uuid.New()}
}

func TestRatePolicies_ExactTokenAndUserBudgets(t *testing.T) {
	policies, err := NewRatePolicies(testMCPRateConfig())
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	user := "018f5b6a-9a3e-7c21-8b1e-000000000040"
	first := ratePrincipal(user, "018f5b6a-9a3e-7c21-8b1e-000000000041")
	second := ratePrincipal(user, "018f5b6a-9a3e-7c21-8b1e-000000000042")

	for i := 1; i <= 120; i++ {
		if allowed, _ := policies.AdmitTool(first); !allowed {
			t.Fatalf("first token call %d denied within 120/minute", i)
		}
	}
	if allowed, retry := policies.AdmitTool(first); allowed || retry != 1 {
		t.Fatalf("first token limit+1 = (%t,%d), want (false,1)", allowed, retry)
	}
	for i := 1; i <= 120; i++ {
		if allowed, _ := policies.AdmitTool(second); !allowed {
			t.Fatalf("second token call %d denied before user reached 240/minute", i)
		}
	}
	third := ratePrincipal(user, "018f5b6a-9a3e-7c21-8b1e-000000000043")
	if allowed, retry := policies.AdmitTool(third); allowed || retry != 1 {
		t.Fatalf("user limit+1 through a third token = (%t,%d), want (false,1)", allowed, retry)
	}
}

func TestRatePolicies_BoundedTokenOverflowCannotBypassUserLimit(t *testing.T) {
	cfg := testMCPRateConfig()
	cfg.TokenRequests = 1
	cfg.UserRequests = 2
	cfg.MaxKeys = 1
	policies, err := NewRatePolicies(cfg)
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	user := "018f5b6a-9a3e-7c21-8b1e-000000000050"
	first := ratePrincipal(user, "018f5b6a-9a3e-7c21-8b1e-000000000051")
	overflowA := ratePrincipal(user, "018f5b6a-9a3e-7c21-8b1e-000000000052")
	overflowB := ratePrincipal(user, "018f5b6a-9a3e-7c21-8b1e-000000000053")

	if allowed, _ := policies.AdmitTool(first); !allowed {
		t.Fatal("tracked token was denied")
	}
	if allowed, _ := policies.AdmitTool(overflowA); !allowed {
		t.Fatal("first overflow token was denied")
	}
	if allowed, _ := policies.AdmitTool(overflowB); allowed {
		t.Fatal("a second overflow token bypassed the shared overflow or per-user ceiling")
	}
}

func TestRatePolicies_FourConcurrentRequestsAndExactRelease(t *testing.T) {
	policies, err := NewRatePolicies(testMCPRateConfig())
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	principal := ratePrincipal(
		"018f5b6a-9a3e-7c21-8b1e-000000000060",
		"018f5b6a-9a3e-7c21-8b1e-000000000061",
	)

	releases := make([]func(), 0, 4)
	for i := 1; i <= 4; i++ {
		release, allowed := policies.AcquireRequest(principal.UserID)
		if !allowed {
			t.Fatalf("concurrent request %d denied", i)
		}
		releases = append(releases, release)
	}
	if _, allowed := policies.AcquireRequest(principal.UserID); allowed {
		t.Fatal("fifth concurrent request was admitted")
	}
	releases[0]()
	if _, allowed := policies.AcquireRequest(principal.UserID); !allowed {
		t.Fatal("one completion did not release exactly one slot")
	}
	releases[0]()
	if _, allowed := policies.AcquireRequest(principal.UserID); allowed {
		t.Fatal("calling one release twice released a second slot")
	}
}

func TestRatePolicies_RequestGuardReleasesOnPanicAndCancellation(t *testing.T) {
	policies, err := NewRatePolicies(testMCPRateConfig())
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	principal := ratePrincipal(
		"018f5b6a-9a3e-7c21-8b1e-000000000070",
		"018f5b6a-9a3e-7c21-8b1e-000000000071",
	)

	func() {
		defer func() { _ = recover() }()
		policies.ServeRequest(principal, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil),
			http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("sentinel") }))
	}()
	for i := 0; i < 4; i++ {
		release, allowed := policies.AcquireRequest(principal.UserID)
		if !allowed {
			t.Fatalf("panic leaked semaphore slot before admission %d", i+1)
		}
		release()
	}

	ctx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/mcp", nil).WithContext(ctx)
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		policies.ServeRequest(principal, httptest.NewRecorder(), request,
			http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				close(started)
				<-r.Context().Done()
			}))
	}()
	<-started
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled request did not return")
	}
	for i := 0; i < 4; i++ {
		release, allowed := policies.AcquireRequest(principal.UserID)
		if !allowed {
			t.Fatalf("cancellation leaked semaphore slot before admission %d", i+1)
		}
		release()
	}
}

func TestRatePolicies_FifthConcurrentRequestReturnsClosedError(t *testing.T) {
	policies, err := NewRatePolicies(testMCPRateConfig())
	if err != nil {
		t.Fatalf("NewRatePolicies: %v", err)
	}
	principal := ratePrincipal(
		"018f5b6a-9a3e-7c21-8b1e-000000000080",
		"018f5b6a-9a3e-7c21-8b1e-000000000081",
	)
	release := make(chan struct{})
	started := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			policies.ServeRequest(principal, httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/mcp", nil),
				http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					started <- struct{}{}
					<-release
				}))
		}()
	}
	for i := 0; i < 4; i++ {
		<-started
	}

	recorder := httptest.NewRecorder()
	policies.ServeRequest(principal, recorder, httptest.NewRequest(http.MethodPost, "/mcp", nil), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("fifth request reached the inner handler")
	}))
	if recorder.Code != http.StatusTooManyRequests || recorder.Header().Get("Retry-After") != "1" || recorder.Body.String() != `{"error":"rate_limited"}` {
		t.Fatalf("fifth response = %d Retry-After %q body %q", recorder.Code, recorder.Header().Get("Retry-After"), recorder.Body.String())
	}
	close(release)
	wg.Wait()
}
