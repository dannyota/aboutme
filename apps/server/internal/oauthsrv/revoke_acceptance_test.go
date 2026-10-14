package oauthsrv

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

type revokeLostCommitAcknowledgement struct {
	mu        sync.Mutex
	injected  bool
	committed bool
	commitErr error
	closeErr  error
}

func (p *revokeLostCommitAcknowledgement) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	if data.SQL != "commit" {
		return ctx
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.injected {
		return ctx
	}
	p.injected = true
	// Commit below pgx, then close before pgx's Commit can observe success.
	// This forces the committed side of an unknown transaction outcome.
	results, err := conn.PgConn().Exec(ctx, "commit").ReadAll()
	p.commitErr = err
	p.committed = err == nil && len(results) == 1 && results[0].CommandTag.String() == "COMMIT"
	if p.committed {
		p.closeErr = conn.PgConn().Close(ctx)
	}
	return ctx
}

func (*revokeLostCommitAcknowledgement) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {
}

func (p *revokeLostCommitAcknowledgement) assertCommitted(t *testing.T) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.injected || !p.committed || p.commitErr != nil || p.closeErr != nil {
		t.Fatal("commit acknowledgement fault did not follow a successful PostgreSQL commit", p.commitErr, p.closeErr)
	}
}

func TestRevokeLostCommitAcknowledgementRetryConverges(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		for _, kind := range []TokenKind{TokenKindAccess, TokenKindRefresh} {
			t.Run(mode+"/"+string(kind), func(t *testing.T) {
				f := newRevokeFixture(t, mode)
				logs := captureRevokeLogs(t, f, false)
				fault := &revokeLostCommitAcknowledgement{}
				cfg := f.pool.Config()
				cfg.ConnConfig.Tracer = fault
				pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(pool.Close)
				f.s.pool = &store.Pool{Pool: pool}
				raw := f.raw
				if kind == TokenKindAccess {
					raw = f.accessRaw
				}
				form := url.Values{"token": {raw}, "client_id": {"https://unknown.example/synthetic-sensitive-path"}}
				w := postRevoke(f.s, form.Encode(), nil)
				if w.Code != http.StatusInternalServerError || w.Body.String() != `{"error":"server_error","error_description":"The server encountered an error."}` ||
					!reflect.DeepEqual(w.Header(), http.Header{"Content-Type": {"application/json"}}) {
					t.Fatal("lost commit acknowledgement did not return the generic server error")
				}
				fault.assertCommitted(t)
				if len(logs.snapshot(t)) != 0 {
					t.Fatal("unacknowledged commit emitted mismatch telemetry")
				}
				assertRevokeCommittedState(t, f)
				baseline := postRevoke(&Service{}, "token=synthetic-malformed", nil)
				f.s.clock = func() time.Time { return f.now.Add(time.Minute) }
				for range 2 {
					retry := postRevoke(f.s, form.Encode(), nil)
					assertEmptyRevoke(t, retry)
					assertSameTokenResponse(t, baseline, retry)
					assertRevokeCommittedState(t, f)
					f.assertIsolation(t)
				}
				f.assertCannotIssue(t)
			})
		}
	}
}

func assertRevokeCommittedState(t *testing.T, f revokeFixture) {
	t.Helper()
	assertGrantHasNoLiveTokens(t, f.refreshFixture)
	grant, err := f.q.GetOAuthGrantForUpdate(t.Context(), f.grantID)
	if err != nil || grant.RevokedAt == nil || !grant.RevokedAt.Equal(f.now) {
		t.Fatal("revocation changed the committed grant timestamp", err)
	}
	var total, revoked int
	if err = f.pool.QueryRow(t.Context(), `
		SELECT count(*), count(*) FILTER (WHERE revoked_at=$2)
		FROM oauth_tokens WHERE grant_id=$1
	`, f.grantID, f.now).Scan(&total, &revoked); err != nil || total != 4 || revoked != total {
		t.Fatal("revocation changed the retained token rows or their committed timestamps", err)
	}
}

func runClaimedRevokeRequest(t *testing.T, s *Service, form url.Values) <-chan *httptest.ResponseRecorder {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	result := make(chan *httptest.ResponseRecorder, 1)
	done := make(chan struct{})
	t.Cleanup(func() {
		cancel()
		<-done
	})
	go func() {
		defer close(done)
		defer cancel()
		r := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/revoke", strings.NewReader(form.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		w := httptest.NewRecorder()
		s.HandleRevoke(w, r)
		result <- w
	}()
	return result
}

func TestRevokeConcurrentLogoutAndDuplicateConverge(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		for _, accessFirst := range []bool{false, true} {
			for _, duplicateAccess := range []bool{false, true} {
				t.Run(mode+"/access first="+strconv.FormatBool(accessFirst)+"/duplicate access="+strconv.FormatBool(duplicateAccess), func(t *testing.T) {
					f := newRevokeFixture(t, mode)
					// Keep the blocker and observer out of the three request connections.
					cfg := f.pool.Config()
					cfg.MaxConns = 3
					requestPool, err := pgxpool.NewWithConfig(t.Context(), cfg)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(requestPool.Close)
					f.s.pool = &store.Pool{Pool: requestPool}
					blocker, pid := lockUserForRace(t, f.refreshFixture)
					defer rollbackTestTx(t, blocker)
					refresh := url.Values{"token": {f.raw}, "token_type_hint": {"refresh_token"}, "client_id": {f.publicID}}
					access := url.Values{"token": {f.accessRaw}, "token_type_hint": {"access_token"}, "client_id": {f.publicID}}
					first, second, duplicate := refresh, access, refresh
					if accessFirst {
						first, second = access, refresh
					}
					if duplicateAccess {
						duplicate = access
					}
					results := []<-chan *httptest.ResponseRecorder{runClaimedRevokeRequest(t, f.s, first)}
					waitForBlockedTransactions(t, f.pool, pid, 1)
					results = append(results, runClaimedRevokeRequest(t, f.s, second), runClaimedRevokeRequest(t, f.s, duplicate))
					waitForRevokeLockChain(t, f.pool, pid, 3)
					if err := blocker.Commit(t.Context()); err != nil {
						t.Fatal(err)
					}
					baseline := postRevoke(&Service{}, "token=synthetic-malformed", nil)
					for _, result := range results {
						w := awaitRefusalResult(t, result)
						assertEmptyRevoke(t, w)
						assertSameTokenResponse(t, baseline, w)
					}
					assertRevokeCommittedState(t, f)
					f.assertIsolation(t)
					f.assertCannotIssue(t)
				})
			}
		}
	}
}

// A third request can wait on a tuple lock behind a transaction-id waiter.
// Follow PostgreSQL's blocker graph so all three belong to this fixture.
func waitForRevokeLockChain(t *testing.T, pool *store.Pool, holderPID int32, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked int
		err := pool.QueryRow(ctx, `
			WITH RECURSIVE waits AS (
				SELECT pid, unnest(pg_blocking_pids(pid)) AS holder
				FROM pg_stat_activity WHERE datname=current_database()
			), descendants(pid) AS (
				SELECT pid FROM waits WHERE holder=$1
				UNION
				SELECT waits.pid FROM waits
				JOIN descendants ON waits.holder=descendants.pid
			)
			SELECT count(*) FROM descendants
		`, holderPID).Scan(&blocked)
		if err != nil {
			t.Fatal("observe revocation lock chain", err)
		}
		if blocked >= want {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("observed %d revocation lock descendants, want at least %d", blocked, want)
		case <-ticker.C:
		}
	}
}

func TestRevokeOuterAdmissionRefusalDoesNoStorageWork(t *testing.T) {
	var baseline *httptest.ResponseRecorder
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		for _, state := range []string{"access", "refresh", "expired", "rotated", "revoked", "unknown", "malformed"} {
			t.Run(mode+"/"+state, func(t *testing.T) {
				f := newRevokeFixture(t, mode)
				raw := refusedRevokeToken(t, f, state)
				before, err := f.q.GetOAuthTokenAuthorityByDigest(t.Context(), f.digest[:])
				if err != nil {
					t.Fatal(err)
				}
				trace := traceRevokeStorage(t, &f)
				calls := 0
				mux := http.NewServeMux()
				mux.HandleFunc("/oauth/revoke", func(w http.ResponseWriter, r *http.Request) {
					calls++
					f.s.HandleRevoke(w, r)
				})
				mux.HandleFunc("/synthetic-admission", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
				handler := api.RateLimit(api.RateLimiterConfig{Requests: 1, Window: time.Minute, Clock: f.s.clock})(mux)
				warmup := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "https://aboutme.example/synthetic-admission", nil)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, warmup)
				if w.Code != http.StatusNoContent {
					t.Fatal("outer admission setup did not consume its traffic allowance")
				}
				for _, claim := range []string{f.publicID, "https://unknown.example/metadata", "synthetic-invalid-id"} {
					form := url.Values{"token": {raw}, "client_id": {claim}}
					r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "https://aboutme.example/oauth/revoke", strings.NewReader(form.Encode()))
					r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
					refusal := httptest.NewRecorder()
					handler.ServeHTTP(refusal, r)
					if refusal.Code != http.StatusTooManyRequests || refusal.Header().Get("Retry-After") != "60" {
						t.Fatal("outer admission did not refuse independently of token and claim state")
					}
					if baseline == nil {
						baseline = refusal
					} else {
						assertSameTokenResponse(t, baseline, refusal)
					}
				}
				if calls != 0 || len(trace.snapshot()) != 0 {
					t.Fatal("outer refusal entered the revocation handler or performed storage work")
				}
				after, err := f.q.GetOAuthTokenAuthorityByDigest(t.Context(), f.digest[:])
				if err != nil || !reflect.DeepEqual(before, after) {
					t.Fatal("outer refusal changed stored token or grant authority", err)
				}
				f.assertIsolation(t)
			})
		}
	}
}

func refusedRevokeToken(t *testing.T, f revokeFixture, state string) string {
	t.Helper()
	switch state {
	case "access":
		return f.accessRaw
	case "expired":
		f.s.clock = func() time.Time { return f.now.Add(refreshFamilyTTL + time.Second) }
	case "rotated":
		if _, err := f.s.rotateRefreshToken(t.Context(), f.raw, nil); err != nil {
			t.Fatal(err)
		}
	case "revoked":
		assertEmptyRevoke(t, postRevoke(f.s, url.Values{"token": {f.raw}, "client_id": {f.publicID}}.Encode(), nil))
	case "unknown":
		raw, _, err := NewToken(TokenKindRefresh, bytes.NewReader(bytes.Repeat([]byte{233}, 32)))
		if err != nil {
			t.Fatal(err)
		}
		return raw
	case "malformed":
		return "synthetic-malformed"
	}
	return f.raw
}
