package githubevidence

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
)

type shutdownStub struct {
	mu          sync.Mutex
	tokenDelay  time.Duration
	exchanges   int
	attempts    map[string]int
	issued      []string
	started     chan struct{}
	startedOnce sync.Once
}

func newShutdownStub(tokenDelay time.Duration) *shutdownStub {
	return &shutdownStub{tokenDelay: tokenDelay, attempts: map[string]int{}, started: make(chan struct{})}
}

func (s *shutdownStub) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()
	switch req.Method {
	case http.MethodPost:
		s.mu.Lock()
		s.exchanges++
		s.mu.Unlock()
		s.startedOnce.Do(func() { close(s.started) })
		if s.tokenDelay == 0 {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		select {
		case <-time.After(s.tokenDelay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		token := sentinelToken + "-" + uuid.NewString()
		s.mu.Lock()
		s.issued = append(s.issued, token)
		s.mu.Unlock()
		return jsonResponse(http.StatusOK, `{"access_token":"`+token+`","token_type":"bearer","scope":""}`), nil
	case http.MethodDelete:
		var body struct {
			AccessToken string `json:"access_token"`
		}
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &body); err != nil {
			return nil, err
		}
		s.mu.Lock()
		s.attempts[body.AccessToken]++
		s.mu.Unlock()
		<-ctx.Done()
		return nil, ctx.Err()
	default:
		return nil, fmt.Errorf("unexpected %s %s", req.Method, req.URL)
	}
}

func (s *shutdownStub) snapshot() (int, map[string]int, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	attempts := map[string]int{}
	for token, n := range s.attempts {
		attempts[token] = n
	}
	return s.exchanges, attempts, append([]string(nil), s.issued...)
}

func newShutdownService(stub *shutdownStub) *Service {
	s := &Service{logger: slog.New(slog.DiscardHandler), now: time.Now, callbackURL: testOrigin + CallbackPath}
	s.client = newTestClient("", stub, time.Now)
	s.registry = newRegistry(sessionTTL, time.Now, s.revokeToken)
	s.client.hold = s.registry.hold
	return s
}

func startExchange(s *Service) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		token, issued, err := s.issueToken(context.Background(), uuid.New(), "code", "verifier")
		if issued && err != nil {
			s.registry.release(token)
		}
	}()
	return done
}

func signal(s *Service) {
	ctx, cancel := context.WithCancel(context.Background())
	closed := s.Run(ctx)
	cancel()
	<-closed
}

func TestShutdown_CancelsAnExchangeInFlightAndLeavesNothingHeld(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stub := newShutdownStub(0)
		s := newShutdownService(stub)
		exchange := startExchange(s)
		<-stub.started
		signalAt := time.Now()
		signal(s)
		synctest.Wait()
		select {
		case <-exchange:
		default:
			t.Error("the code exchange was still running after the signal")
		}
		if err := s.Shutdown(context.Background()); err != nil || time.Since(signalAt) != 0 {
			t.Fatalf("Shutdown() = %v after %v, want at once", err, time.Since(signalAt))
		}
		_, attempts, issued := stub.snapshot()
		if len(issued) != 0 || len(attempts) != 0 || len(s.registry.pending) != 0 || len(s.registry.sessions) != 0 {
			t.Fatalf("after shutdown: issued %d, revocation attempts %v, held %d", len(issued), attempts, len(s.registry.pending))
		}
		<-exchange
	})
}

func TestShutdown_RevokesEveryHeldTokenOnceWithinTheBound(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stub := newShutdownStub(4 * time.Second)
		s := newShutdownService(stub)
		account := uuid.New()
		if _, ok := s.registry.install(account, s.registry.hold("live-token"), 1, 1); !ok {
			t.Fatal("install refused")
		}
		s.registry.hold("pending-token")
		exchange := startExchange(s)
		<-stub.started
		signalAt := time.Now()
		signal(s)
		shutdownErr := s.Shutdown(context.Background())
		if elapsed := time.Since(signalAt); elapsed > shutdownBound {
			t.Fatalf("Shutdown() = %v returned %v after the signal, want at most %v", shutdownErr, elapsed, shutdownBound)
		}
		<-exchange
		synctest.Wait()
		_, attempts, issued := stub.snapshot()
		want := map[string]int{"live-token": 1, "pending-token": 1}
		if len(issued) != 0 || fmt.Sprint(attempts) != fmt.Sprint(want) {
			t.Fatalf("revocation attempts = %v with %d tokens issued after the signal, want exactly %v", attempts, len(issued), want)
		}
	})
}

func TestShutdown_ReturnsWithinTheBoundOfTheSignalWhateverTheDrain(t *testing.T) {
	for _, drain := range []time.Duration{0, 3 * time.Second, 7 * time.Second, 10 * time.Second} {
		t.Run(drain.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				stub := newShutdownStub(4 * time.Second)
				s := newShutdownService(stub)
				s.registry.hold("pending-token")
				exchange := startExchange(s)
				<-stub.started
				signalAt := time.Now()
				signal(s)
				time.Sleep(drain)
				calledAt := time.Now()
				shutdownErr := s.Shutdown(context.Background())
				limit := max(shutdownBound, drain)
				if elapsed := time.Since(signalAt); elapsed > limit {
					t.Fatalf("Shutdown() = %v returned %v after the signal (drain %v), want at most %v", shutdownErr, elapsed, drain, limit)
				}
				if drain >= shutdownBound && time.Since(calledAt) != 0 {
					t.Fatalf("after a %v drain Shutdown waited %v, want it to return at once", drain, time.Since(calledAt))
				}
				<-exchange
				synctest.Wait()
			})
		})
	}
}

func TestShutdown_CallbackAfterTheSignalDoesNoExchange(t *testing.T) {
	h := newHarness(t)
	pending := h.start(h.raw, h.session)
	code := "code-" + uuid.NewString()
	h.gh.expect(code, pending.challenge)
	signal(h.svc)
	rec := h.callback(pending, "code="+code+"&state="+url.QueryEscape(pending.state), h.raw)
	requireOutcome(t, rec, outcomeFailed)
	if _, _, exchanges, _ := h.gh.snapshot(); exchanges != 0 {
		t.Fatalf("a callback after the signal made %d code exchanges, want 0", exchanges)
	}
	if !strings.Contains(h.logs.String(), `"reason":"unavailable"`) {
		t.Fatalf("logs = %s, want the unavailable reason", h.logs.String())
	}
}
