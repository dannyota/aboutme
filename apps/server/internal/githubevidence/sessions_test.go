package githubevidence

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
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

func installed(t *testing.T, reg *registry, account uuid.UUID, token string) *liveSession {
	t.Helper()
	session, ok := reg.install(account, reg.hold(token), 1, 1, time.Now())
	if !ok {
		t.Fatal("install refused")
	}
	return session
}

func TestRegistry_EachEndingRevokesExactlyOnce(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(t *testing.T, reg *registry, account uuid.UUID)
	}{
		{"done, sign-out, epoch change or disconnect", func(_ *testing.T, reg *registry, account uuid.UUID) { reg.end(account) }},
		{"stale status or a failed commit", func(_ *testing.T, reg *registry, account uuid.UUID) {
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
				reg := newRegistry(sessionTTL, revoked.revoke)
				account := uuid.New()
				installed(t, reg, account, sentinelToken)
				synctest.Wait()
				if revoked.total() != 0 {
					t.Fatal("install revoked a token")
				}
				tc.end(t, reg, account)
				synctest.Wait()
				if n := revoked.count(sentinelToken); n != 1 {
					t.Fatalf("revocations of the ended token = %d, want 1", n)
				}
				reg.end(account)
				reg.close()
				synctest.Wait()
				if n := revoked.count(sentinelToken); n != 1 {
					t.Fatalf("revocations after further endings = %d, want 1", n)
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
		reg := newRegistry(sessionTTL, revoked.revoke)
		account := uuid.New()
		start := time.Now()
		live := installed(t, reg, account, sentinelToken)
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
		reg := newRegistry(sessionTTL, revoked.revoke)
		account := uuid.New()
		live := installed(t, reg, account, sentinelToken)
		pending := reg.hold("pending-token")
		time.Sleep(sessionTTL - time.Nanosecond)
		var wg sync.WaitGroup
		for _, end := range []func(){
			func() { reg.end(account) },
			func() { reg.endSession(account, live) },
			func() { reg.release(pending) },
			func() { reg.release(pending) },
			func() { reg.close() },
			func() { time.Sleep(time.Nanosecond) },
		} {
			wg.Go(end)
		}
		wg.Wait()
		synctest.Wait()
		if n, p, all := revoked.count(sentinelToken), revoked.count("pending-token"), revoked.total(); n != 1 || p != 1 || all != 2 {
			t.Fatalf("racing endings sent %d revocation requests, %d for the live token and %d for the pending one; want exactly 1 each", all, n, p)
		}
	})
}

func TestRegistry_CloseRevokesPendingTokensAndRefusesTheirInstall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		revoked := &revocations{}
		reg := newRegistry(sessionTTL, revoked.revoke)
		account := uuid.New()
		pending := reg.hold(sentinelToken)
		reg.close()
		synctest.Wait()
		if revoked.count(sentinelToken) != 1 {
			t.Fatalf("close revoked the pending token %d times, want 1", revoked.count(sentinelToken))
		}
		if _, ok := reg.install(account, pending, 1, 1, time.Now()); ok {
			t.Fatal("install of a token claimed at close succeeded")
		}
		late := reg.hold("late-token")
		if _, held := late.peek(); held {
			t.Fatal("a token parsed after close is still held")
		}
		reg.release(pending)
		reg.release(late)
		synctest.Wait()
		if revoked.total() != 2 || revoked.count("late-token") != 1 {
			t.Fatalf("revocations = %v, want each token once", revoked.tokens)
		}
	})
}

func TestRegistry_ReleasedCallbackTokenRevokesOnce(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		revoked := &revocations{}
		reg := newRegistry(sessionTTL, revoked.revoke)
		token := reg.hold(sentinelToken)
		reg.release(token)
		reg.release(token)
		reg.release(nil)
		synctest.Wait()
		if revoked.count(sentinelToken) != 1 || revoked.total() != 1 || len(reg.pending) != 0 {
			t.Fatalf("released token revoked %d times, %d pending left; want 1 and 0", revoked.count(sentinelToken), len(reg.pending))
		}
		if _, ok := reg.install(uuid.New(), token, 1, 1, time.Now()); ok {
			t.Fatal("install of a released token succeeded")
		}
	})
}

func TestRegistry_ShutdownWaitsForRevocations(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		revoked := &revocations{block: 4 * time.Second}
		reg := newRegistry(sessionTTL, revoked.revoke)
		installed(t, reg, uuid.New(), "first")
		reg.hold("pending")
		svc := &Service{registry: reg}
		start := time.Now()
		if err := svc.Shutdown(context.Background()); err != nil {
			t.Fatalf("Shutdown() error = %v", err)
		}
		if elapsed := time.Since(start); elapsed != 4*time.Second {
			t.Fatalf("Shutdown returned after %v, want 4s: the parallel revocations", elapsed)
		}
		for _, token := range []string{"first", "pending"} {
			if revoked.count(token) != 1 {
				t.Fatalf("revocations of %s = %d, want 1", token, revoked.count(token))
			}
		}
	})
}

func TestRegistry_ShutdownWaitIsBounded(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		revoked := &revocations{block: time.Hour}
		reg := newRegistry(sessionTTL, revoked.revoke)
		installed(t, reg, uuid.New(), "stuck")
		svc := &Service{registry: reg}
		start := time.Now()
		if err := svc.Shutdown(context.Background()); !errors.Is(err, context.DeadlineExceeded) || time.Since(start) != shutdownBound {
			t.Fatalf("Shutdown() = %v after %v, want the deadline after %v", err, time.Since(start), shutdownBound)
		}
		time.Sleep(time.Hour)
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
			var record map[string]any
			if err := json.Unmarshal(logs.Bytes(), &record); err != nil || strings.Count(line, "\n") != 1 {
				t.Fatalf("log = %q, want exactly one JSON record: %v", line, err)
			}
			delete(record, "time")
			want := map[string]any{"level": "WARN", "msg": "github evidence: token revocation failed", "category": tc.category}
			if !maps.Equal(record, want) {
				t.Fatalf("log record = %v, want exactly %v", record, want)
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
