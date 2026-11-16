package githubevidence

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/store"
)

const raceWait = 15 * time.Second

func (h *harness) liveToken() string {
	live, ok := h.svc.registry.current(h.user.ID)
	if !ok {
		return ""
	}
	value, held := live.token.peek()
	if !held {
		return ""
	}
	return value
}

func (h *harness) requireConsistent(generation int64, revoked ...string) {
	h.t.Helper()
	h.drain()
	row := h.connection(h.user.ID)
	live, ok := h.svc.registry.current(h.user.ID)
	if !row.found || row.githubUserID == nil || row.generation != generation {
		h.t.Fatalf("connection = %+v, want connected at generation %d", row, generation)
	}
	if !ok || live.generation != row.generation {
		h.t.Fatalf("live session = %v (present %v), want one at the stored generation %d", live, ok, row.generation)
	}
	_, got, _, _ := h.gh.snapshot()
	slices.Sort(got)
	want := slices.Clone(revoked)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		h.t.Fatalf("revoked %d tokens, want exactly %d", len(got), len(want))
	}
	if token := h.liveToken(); token == "" || slices.Contains(got, token) {
		h.t.Fatal("the live session holds no token or a revoked one")
	}
}

func (h *harness) onceBeforeToken(run func()) {
	var fired atomic.Bool
	h.gh.set(func(gh *stubGitHub) {
		gh.beforeToken = func() {
			if fired.CompareAndSwap(false, true) {
				run()
			}
		}
	})
}

func (h *harness) waitBlockedBy(pid int32, done <-chan struct{}) bool {
	h.t.Helper()
	deadline := time.Now().Add(raceWait)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return false
		default:
		}
		var n int
		if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM pg_stat_activity WHERE $1::integer = ANY(pg_blocking_pids(pid))`, pid).Scan(&n); err != nil {
			h.t.Errorf("query blocked backends: %v", err)
			return false
		}
		if n > 0 {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.t.Errorf("nothing finished or blocked on backend %d within %s", pid, raceWait)
	return false
}

func TestBoundary_DoneDuringTheExchangeKeepsTheNewConnectionAndSession(t *testing.T) {
	h := newHarness(t)
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	first := h.liveToken()
	h.onceBeforeToken(func() {
		if rec := h.do(h.as(http.MethodPost, SessionEndPath)); rec.Code != http.StatusNoContent {
			t.Errorf("done during the exchange = %d %s", rec.Code, rec.Body.String())
		}
	})
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	h.requireConsistent(2, first)
}

func TestBoundary_RevokingAnotherBrowserSessionDuringTheExchange(t *testing.T) {
	h := newHarness(t)
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	first := h.liveToken()
	_, other, err := h.sessions.Issue(t.Context(), h.user.ID, "evidence-test", "203.0.113.8")
	if err != nil {
		t.Fatal(err)
	}
	h.onceBeforeToken(func() {
		if rec := h.do(h.as(http.MethodDelete, auth.SessionsPath+"/"+other.ID.String())); rec.Code != http.StatusNoContent {
			t.Errorf("revoking another session during the exchange = %d %s", rec.Code, rec.Body.String())
		}
	})
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	h.requireConsistent(2, first)
}

func TestBoundary_ShutdownDuringTheExchangeCommitsNothing(t *testing.T) {
	for _, tc := range []struct {
		name      string
		arrange   func(h *harness)
		delivered bool
	}{
		{"signal before the token response", func(h *harness) {
			h.onceBeforeToken(func() {
				h.svc.registry.close()
				ctx, cancel := context.WithTimeout(context.Background(), raceWait)
				defer cancel()
				if err := h.svc.registry.wait(ctx); err != nil {
					h.t.Errorf("the canceled exchange was still running when the token response was written: %v", err)
				}
			})
		}, false},
		{"signal after the token response", func(h *harness) {
			hold := h.svc.client.hold
			h.svc.client.hold = func(value string) *heldToken {
				h.svc.registry.close()
				return hold(value)
			}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tc.arrange(h)
			requireOutcome(t, h.connect(h.raw, h.session), outcomeFailed)
			h.drain()
			tokens, revoked, _, profiles := h.gh.snapshot()
			if len(tokens) != 1 {
				t.Fatalf("the stub issued %d tokens, want 1", len(tokens))
			}
			want := []string{}
			if tc.delivered {
				want = tokens
			}
			if !slices.Equal(revoked, want) {
				t.Fatalf("revoked %d tokens, want %d: every token the exchange yielded is revoked once and an undelivered one is never sent", len(revoked), len(want))
			}
			if profiles != 0 {
				t.Fatalf("profile fetches after the signal = %d, want 0", profiles)
			}
			if len(h.svc.registry.pending) != 0 || len(h.svc.registry.sessions) != 0 {
				t.Fatal("a token is still held after shutdown")
			}
			if row := h.connection(h.user.ID); row.found {
				t.Fatalf("a callback that failed at shutdown committed a connection: %+v", row)
			}
		})
	}
}

func TestBoundary_ConcurrentCallbacksInstallInGenerationOrder(t *testing.T) {
	h := newHarness(t)
	var fired atomic.Bool
	h.svc.hooks.connectionCommitted = func() {
		if fired.CompareAndSwap(false, true) {
			requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
		}
	}
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	tokens, _, _, _ := h.gh.snapshot()
	if len(tokens) != 2 {
		t.Fatalf("issued %d tokens, want 2", len(tokens))
	}
	h.requireConsistent(2, tokens[0])
	if h.liveToken() != tokens[1] {
		t.Fatal("the live session holds the earlier generation's token")
	}
}

func TestBoundary_StatusSnapshotsConnectionAndSessionTogether(t *testing.T) {
	h := newHarness(t)
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	first := h.liveToken()
	pending := h.start(h.raw, h.session)
	code := "code-" + uuid.NewString()
	h.gh.expect(code, pending.challenge)
	var fired atomic.Bool
	done := make(chan struct{})
	var second *httptest.ResponseRecorder
	h.svc.hooks.statusRead = func(tx pgx.Tx) {
		if !fired.CompareAndSwap(false, true) {
			return
		}
		go func() {
			defer close(done)
			second = h.callback(pending, "code="+code+"&state="+url.QueryEscape(pending.state), h.raw)
		}()
		if tx == nil {
			<-done
			return
		}
		var pid int32
		if err := tx.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			t.Errorf("status backend: %v", err)
			return
		}
		h.waitBlockedBy(pid, done)
	}
	body := h.status(h.raw, h.session)
	<-done
	requireOutcome(t, second, outcomeConnected)
	if body.Connection == nil || body.Session == nil {
		t.Fatalf("status during a callback = %+v, want one coherent connection and session", body)
	}
	h.requireConsistent(2, first)
}

func TestBoundary_RejectedTokenResponsesStillRevokeTheToken(t *testing.T) {
	for _, tc := range []struct {
		name    string
		body    func(token string) string
		revoked bool
		reason  string
	}{
		{"duplicate scope", func(token string) string {
			return `{"access_token":"` + token + `","token_type":"bearer","scope":"","scope":"repo"}`
		}, true, "exchange_failed"},
		{"trailing data", func(token string) string {
			return `{"access_token":"` + token + `","token_type":"bearer","scope":""} {"x":1}`
		}, true, "exchange_failed"},
		{"two different tokens", func(token string) string {
			return `{"access_token":"` + token + `","access_token":"` + sentinelToken + `-other","token_type":"bearer","scope":""}`
		}, false, "ambiguous_token"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.gh.set(func(gh *stubGitHub) { gh.tokenBody = tc.body })
			requireOutcome(t, h.connect(h.raw, h.session), outcomeFailed)
			h.drain()
			tokens, revoked, _, profiles := h.gh.snapshot()
			if profiles != 0 || h.connection(h.user.ID).found {
				t.Fatal("a rejected token response reached the profile fetch or the database")
			}
			if tc.revoked && (len(tokens) != 1 || !slices.Equal(revoked, tokens)) {
				t.Fatalf("revoked %d of %d issued tokens, want the one token revoked once", len(revoked), len(tokens))
			}
			if !tc.revoked && len(revoked) != 0 {
				t.Fatalf("revoked %d tokens from an ambiguous response, want none", len(revoked))
			}
			if !strings.Contains(h.logs.String(), `"reason":"`+tc.reason+`"`) {
				t.Fatalf("logs = %s, want reason %s", h.logs.String(), tc.reason)
			}
		})
	}
}

func TestBoundary_ShutdownRevokesTheTokenOfABlockedCallback(t *testing.T) {
	h := newHarness(t)
	pending := h.start(h.raw, h.session)
	code := "code-" + uuid.NewString()
	h.gh.expect(code, pending.challenge)
	holder, err := h.pool.Begin(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if rollbackErr := holder.Rollback(context.Background()); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			t.Errorf("rollback holder: %v", rollbackErr)
		}
	})
	var pid int32
	if err = holder.QueryRow(t.Context(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if _, err = store.New(h.pool).WithTx(holder).GetUserForUpdate(t.Context(), h.user.ID); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	var rec *httptest.ResponseRecorder
	go func() {
		defer close(done)
		rec = h.callback(pending, "code="+code+"&state="+url.QueryEscape(pending.state), h.raw)
	}()
	if !h.waitBlockedBy(pid, done) {
		t.Fatal("the callback did not block on the account lock")
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownBound)
	defer cancel()
	if err = h.svc.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown() with a blocked callback = %v", err)
	}
	tokens, revoked, _, _ := h.gh.snapshot()
	select {
	case <-done:
		t.Fatal("the callback finished while the account lock was held")
	default:
	}
	if len(tokens) != 1 || !slices.Equal(revoked, tokens) {
		t.Fatalf("after shutdown: issued %d tokens, revoked %d; want the blocked callback's token revoked once", len(tokens), len(revoked))
	}
	if err = holder.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-done
	requireOutcome(t, rec, outcomeFailed)
	h.drain()
	if _, again, _, _ := h.gh.snapshot(); len(again) != 1 {
		t.Fatalf("revocations after the blocked callback finished = %d, want 1", len(again))
	}
	if row := h.connection(h.user.ID); row.found {
		t.Fatalf("the blocked callback committed a connection after shutdown: %+v", row)
	}
}

func staleRequest(t *testing.T, method, target string, sess store.Session) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(auth.ContextWithSession(t.Context(), sess), method, target, nil)
	r.RemoteAddr = "203.0.113.7:4000"
	return r
}

func TestBoundary_MutationsRecheckTheCallersSessionUnderTheLock(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stale func(h *harness)
	}{
		{"session revoked after admission", func(h *harness) {
			if err := h.sessions.Revoke(context.Background(), h.session.ID); err != nil {
				h.t.Fatal(err)
			}
		}},
		{"epoch advanced after admission", func(h *harness) {
			if _, err := h.q.AdvanceUserAuthEpoch(context.Background(), h.user.ID); err != nil {
				h.t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
			first := h.liveToken()
			tc.stale(h)
			for _, route := range []struct {
				name    string
				handler http.HandlerFunc
				request *http.Request
			}{
				{"disconnect", h.svc.handleDisconnect, staleRequest(t, http.MethodDelete, StatusPath+"?keepLinks=true", h.session)},
				{"done", h.svc.handleSessionEnd, staleRequest(t, http.MethodPost, SessionEndPath, h.session)},
			} {
				rec := httptest.NewRecorder()
				route.handler(rec, route.request)
				if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "session_required") {
					t.Errorf("%s with a stale session = %d %s, want 401 session_required", route.name, rec.Code, rec.Body.String())
				}
			}
			row := h.connection(h.user.ID)
			if row.githubUserID == nil || row.generation != 1 {
				t.Fatalf("connection after stale mutations = %+v, want unchanged at generation 1", row)
			}
			if h.liveToken() != first {
				t.Fatal("a stale session ended the evidence session")
			}
			h.drain()
			if _, revoked, _, _ := h.gh.snapshot(); len(revoked) != 0 {
				t.Fatalf("stale mutations revoked %d tokens, want 0", len(revoked))
			}
		})
	}
}
