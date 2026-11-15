package githubevidence

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
)

type revocations struct {
	mu     sync.Mutex
	tokens map[string]int
	block  time.Duration
}

func (r *revocations) revoke(token string) {
	if r.block > 0 {
		time.Sleep(r.block)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tokens == nil {
		r.tokens = map[string]int{}
	}
	r.tokens[token]++
}

func (r *revocations) count(token string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.tokens[token]
}

func (r *revocations) total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.tokens {
		n += c
	}
	return n
}

func installed(t *testing.T, reg *registry, account uuid.UUID, token string) {
	t.Helper()
	reserved, ok := reg.reserve(account)
	if !ok {
		t.Fatal("reserve refused")
	}
	if _, ok := reg.install(reserved, newHeldToken(token), 1, 1); !ok {
		t.Fatal("install refused")
	}
}

func TestRegistry_EachEndingRevokesExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(t *testing.T, reg *registry, account uuid.UUID)
	}{
		{"done, sign-out, epoch change or disconnect", func(_ *testing.T, reg *registry, account uuid.UUID) { reg.end(account) }},
		{"stale status", func(_ *testing.T, reg *registry, account uuid.UUID) {
			live, _ := reg.current(account)
			reg.endSession(account, live)
		}},
		{"expiry", func(*testing.T, *registry, uuid.UUID) { time.Sleep(sessionTTL) }},
		{"replacement", func(t *testing.T, reg *registry, account uuid.UUID) { installed(t, reg, account, "replacement") }},
		{"process exit", func(_ *testing.T, reg *registry, _ uuid.UUID) { reg.close() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				revoked := &revocations{}
				reg := newRegistry(sessionTTL, time.Now, revoked.revoke)
				account := uuid.New()
				installed(t, reg, account, sentinelToken)
				synctest.Wait()
				if revoked.total() != 0 {
					t.Fatal("install revoked a token")
				}
				tc.end(t, reg, account)
				reg.end(account)
				reg.close()
				synctest.Wait()
				if n := revoked.count(sentinelToken); n != 1 {
					t.Fatalf("revocations of the ended token = %d, want 1", n)
				}
				if _, live := reg.current(account); live {
					t.Fatal("session still live after ending")
				}
			})
		})
	}
}

func TestRegistry_ExpiryRevokesWithoutARequestAtThirtyMinutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		revoked := &revocations{}
		reg := newRegistry(sessionTTL, time.Now, revoked.revoke)
		account := uuid.New()
		start := time.Now()
		installed(t, reg, account, sentinelToken)
		live, _ := reg.current(account)
		if !live.expiresAt.Equal(start.Add(30 * time.Minute)) {
			t.Fatalf("expiresAt = %v, want start + 30m", live.expiresAt)
		}
		time.Sleep(30*time.Minute - time.Nanosecond)
		synctest.Wait()
		if revoked.total() != 0 {
			t.Fatal("token revoked before 30 minutes")
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if revoked.count(sentinelToken) != 1 {
			t.Fatalf("revocations at 30 minutes = %d, want 1", revoked.count(sentinelToken))
		}
	})
}

func TestRegistry_RacingEndingsSendOneRevocation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		revoked := &revocations{}
		reg := newRegistry(sessionTTL, time.Now, revoked.revoke)
		account := uuid.New()
		installed(t, reg, account, sentinelToken)
		live, _ := reg.current(account)
		time.Sleep(sessionTTL - time.Nanosecond)
		var wg sync.WaitGroup
		for _, end := range []func(){
			func() { reg.end(account) },
			func() { reg.endSession(account, live) },
			func() { reg.expire(account, live) },
			func() { reg.discard(live.token) },
			func() { reg.close() },
			func() { time.Sleep(time.Nanosecond) },
		} {
			wg.Go(end)
		}
		wg.Wait()
		synctest.Wait()
		if n := revoked.count(sentinelToken); n != 1 {
			t.Fatalf("racing endings revoked %d times, want 1", n)
		}
	})
}

func TestRegistry_EndingDuringACallbackRefusesTheInstall(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(reg *registry, account uuid.UUID)
	}{
		{"sign-out", func(reg *registry, account uuid.UUID) { reg.end(account) }},
		{"process exit", func(reg *registry, _ uuid.UUID) { reg.close() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				revoked := &revocations{}
				reg := newRegistry(sessionTTL, time.Now, revoked.revoke)
				account := uuid.New()
				reserved, ok := reg.reserve(account)
				if !ok {
					t.Fatal("reserve refused")
				}
				tc.end(reg, account)
				if _, ok := reg.install(reserved, newHeldToken(sentinelToken), 1, 1); ok {
					t.Fatal("install after an ending succeeded")
				}
				synctest.Wait()
				if revoked.count(sentinelToken) != 1 {
					t.Fatalf("refused install revoked %d times, want 1", revoked.count(sentinelToken))
				}
				if _, live := reg.current(account); live {
					t.Fatal("refused install left a session")
				}
				if _, ok := reg.reserve(account); ok == reg.isClosed() {
					t.Fatalf("reserve after %s = %v, closed = %v", tc.name, ok, reg.isClosed())
				}
			})
		})
	}
}

func TestRegistry_AbandonedCallbackRevokesItsTokenOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		revoked := &revocations{}
		reg := newRegistry(sessionTTL, time.Now, revoked.revoke)
		account := uuid.New()
		reserved, _ := reg.reserve(account)
		token := newHeldToken(sentinelToken)
		reg.abandon(reserved, token)
		reg.discard(token)
		synctest.Wait()
		if revoked.count(sentinelToken) != 1 || len(reg.accounts) != 0 {
			t.Fatalf("abandoned callback revoked %d times, %d accounts left; want 1 and 0", revoked.count(sentinelToken), len(reg.accounts))
		}
		reserved, _ = reg.reserve(account)
		reg.abandon(reserved, nil)
		synctest.Wait()
		if revoked.total() != 1 {
			t.Fatal("abandoning without a token revoked something")
		}
	})
}

func TestRegistry_ShutdownWaitsForRevocations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		revoked := &revocations{block: 4 * time.Second}
		reg := newRegistry(sessionTTL, time.Now, revoked.revoke)
		installed(t, reg, uuid.New(), "first")
		installed(t, reg, uuid.New(), "second")
		svc := &Service{registry: reg}
		start := time.Now()
		if err := svc.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
		if time.Since(start) != 4*time.Second || revoked.count("first") != 1 || revoked.count("second") != 1 {
			t.Fatalf("Shutdown returned after %v with revocations %v", time.Since(start), revoked.tokens)
		}
		if _, ok := reg.reserve(uuid.New()); ok {
			t.Fatal("reserve after shutdown succeeded")
		}
	})
}

func TestService_RevocationFailureLogsOnlyItsCategory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		respond  func(*http.Request) (*http.Response, error)
		category string
	}{
		{"rejected", func(*http.Request) (*http.Response, error) {
			return jsonResponse(422, `{"message":"`+sentinelBody+`","token":"`+sentinelToken+`"}`), nil
		}, "rejected"},
		{"network", func(*http.Request) (*http.Response, error) {
			return nil, errors.New("dial " + sentinelBody + " " + sentinelToken)
		}, "network"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			transport := &countingTransport{respond: tc.respond}
			svc := &Service{
				client: newTestClient("", transport, time.Now),
				logger: slog.New(slog.NewJSONHandler(&logs, nil)),
			}
			svc.revokeToken(sentinelToken)
			if transport.count() != 1 {
				t.Fatalf("revocation attempts = %d, want exactly 1", transport.count())
			}
			line := logs.String()
			if !strings.Contains(line, `"category":"`+tc.category+`"`) {
				t.Fatalf("log = %s, want category %s", line, tc.category)
			}
			for _, secret := range []string{sentinelToken, testClientSecret, sentinelBody, "Basic ", "Authorization", "access_token"} {
				if strings.Contains(line, secret) {
					t.Fatalf("log %s contains %q", line, secret)
				}
			}
		})
	}
	var logs bytes.Buffer
	svc := &Service{client: newTestClient("", &countingTransport{respond: func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusNoContent, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}}, time.Now), logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	svc.revokeToken(sentinelToken)
	if logs.Len() != 0 {
		t.Fatalf("successful revocation logged %s", logs.String())
	}
}
