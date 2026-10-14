package oauthsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dannyota/aboutme/apps/server/internal/store"
)

// The trace retains closed operation labels, never SQL arguments or credentials.
type revokeStorageTrace struct {
	mu       sync.Mutex
	steps    []string
	failStep string
	cancel   context.CancelFunc
}

func (p *revokeStorageTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	step := "unexpected query"
	for _, name := range []string{
		"GetOAuthTokenAuthorityByDigest", "GetOAuthClientForUpdate", "GetUserForUpdate",
		"GetOAuthGrantForUpdate", "RevokeOAuthGrant", "RevokeOAuthTokensForGrant",
	} {
		if strings.Contains(data.SQL, "-- name: "+name+" ") {
			step = name
			break
		}
	}
	if data.SQL == "begin" || data.SQL == "commit" || data.SQL == "rollback" {
		step = data.SQL
	}
	p.mu.Lock()
	p.steps = append(p.steps, step)
	if step == p.failStep && p.cancel != nil {
		p.cancel()
	}
	p.mu.Unlock()
	return ctx
}

func (*revokeStorageTrace) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (p *revokeStorageTrace) snapshot() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.steps)
}

func (p *revokeStorageTrace) reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.steps = nil
}

func traceRevokeStorage(t *testing.T, f *revokeFixture) *revokeStorageTrace {
	t.Helper()
	trace := &revokeStorageTrace{}
	cfg := f.pool.Config()
	cfg.ConnConfig.Tracer = trace
	pool, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	f.s.pool = &store.Pool{Pool: pool}
	t.Cleanup(func() { f.s.pool.Close(context.Background()) })
	return trace
}

func TestRevokeStorageWorkDependsOnlyOnToken(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		t.Run(mode, func(t *testing.T) {
			f := newRevokeFixture(t, mode)
			trace := traceRevokeStorage(t, &f)
			for _, raw := range []string{"synthetic-malformed", f.accessRaw, f.raw} {
				for _, claim := range []string{"", f.publicID, "https://unknown.example/synthetic-sensitive-path"} {
					trace.reset()
					form := url.Values{"token": {raw}}
					if claim != "" {
						form.Set("client_id", claim)
					}
					assertEmptyRevoke(t, postRevoke(f.s, form.Encode(), nil))
					var want []string
					if raw != "synthetic-malformed" {
						want = []string{"begin", "GetOAuthTokenAuthorityByDigest", "GetOAuthClientForUpdate", "GetUserForUpdate", "GetOAuthGrantForUpdate", "RevokeOAuthGrant", "RevokeOAuthTokensForGrant", "commit"}
					}
					if !reflect.DeepEqual(trace.snapshot(), want) {
						t.Fatalf("revocation storage path differs: %v", trace.snapshot())
					}
				}
			}
			for _, kind := range []TokenKind{TokenKindAccess, TokenKindRefresh} {
				unknown, _, err := NewToken(kind, bytes.NewReader(bytes.Repeat([]byte{233}, 32)))
				if err != nil {
					t.Fatal(err)
				}
				trace.reset()
				form := url.Values{"token": {unknown}, "client_id": {"https://unknown.example/metadata"}}
				assertEmptyRevoke(t, postRevoke(f.s, form.Encode(), nil))
				if !reflect.DeepEqual(trace.snapshot(), []string{"begin", "GetOAuthTokenAuthorityByDigest", "commit"}) {
					t.Fatalf("unknown token performed more than its digest lookup transaction: %v", trace.snapshot())
				}
			}
		})
	}
}

type revokeLogWriter struct {
	t     *testing.T
	f     revokeFixture
	lines [][]byte
	fail  bool
}

func (w *revokeLogWriter) Write(line []byte) (int, error) {
	grant, err := w.f.q.GetOAuthGrantForUpdate(context.Background(), w.f.grantID)
	if err != nil || grant.RevokedAt == nil {
		w.t.Error("mismatch event preceded the revocation commit", err)
	}
	w.lines = append(w.lines, bytes.Clone(line))
	if w.fail {
		return 0, errors.New("synthetic telemetry failure")
	}
	return len(line), nil
}

func captureRevokeLogs(t *testing.T, f revokeFixture, fail bool) *revokeLogWriter {
	t.Helper()
	w := &revokeLogWriter{t: t, f: f, fail: fail}
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(w, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return w
}

func TestRevokeRejectedAndUnknownInputsEmitNoMismatchEvent(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		t.Run(mode, func(t *testing.T) {
			f := newRevokeFixture(t, mode)
			logs := captureRevokeLogs(t, f, false)
			unknown, _, err := NewToken(TokenKindAccess, bytes.NewReader(bytes.Repeat([]byte{233}, 32)))
			if err != nil {
				t.Fatal(err)
			}
			for _, tc := range []struct {
				form   url.Values
				status int
			}{
				{url.Values{"token": {"synthetic-malformed"}, "client_id": {"https://unknown.example/synthetic-sensitive-path"}}, http.StatusOK},
				{url.Values{"token": {unknown}, "client_id": {"https://unknown.example/synthetic-sensitive-path"}}, http.StatusOK},
				{url.Values{"token": {f.raw}, "client_secret": {"synthetic-sensitive-value"}}, http.StatusBadRequest},
				{url.Values{"token": {f.raw}, "client_id": {"https://invalid.example:443/synthetic-sensitive-path"}}, http.StatusBadRequest},
				{url.Values{"client_id": {f.publicID}}, http.StatusBadRequest},
			} {
				if postRevoke(f.s, tc.form.Encode(), nil).Code != tc.status {
					t.Fatal("negative telemetry fixture returned the wrong status")
				}
				if len(logs.lines) != 0 {
					t.Fatal("syntax failure or unknown token emitted mismatch telemetry")
				}
			}
		})
	}
}

func TestRevokeMismatchTelemetryContainsOnlyCommittedStoredIDs(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		for _, claim := range []string{"omitted", "matching", "UUID spelling", "internal UUID", "encoded URL", "mismatch"} {
			for _, fail := range []bool{false, true} {
				t.Run(mode+"/"+claim+"/telemetry failure="+strconv.FormatBool(fail), func(t *testing.T) {
					f := newRevokeFixture(t, mode)
					logs := captureRevokeLogs(t, f, fail)
					id := "https://unknown.example/synthetic-sensitive-path"
					switch claim {
					case "matching":
						id = f.publicID
					case "UUID spelling":
						id = "{" + strings.ToUpper(f.clientID.String()) + "}"
					case "internal UUID":
						id = f.clientID.String()
					case "encoded URL":
						id = strings.Replace(revokeClaudeClientID, "/oauth/", "/%6fauth/", 1)
					}
					form := url.Values{"token": {f.raw}, "token_type_hint": {"synthetic-sensitive-hint"}}
					if claim != "omitted" {
						form.Set("client_id", id)
					}
					assertEmptyRevoke(t, postRevoke(f.s, form.Encode(), http.Header{
						"Cookie": {"__Host-session=synthetic-sensitive-cookie"}, "Authorization": {"Bearer synthetic-sensitive-header"},
					}))
					assertGrantHasNoLiveTokens(t, f.refreshFixture)
					mismatch := claim != "omitted" && claim != "matching"
					if mode == "dcr" && (claim == "internal UUID" || claim == "UUID spelling") {
						mismatch = false
					}
					if !mismatch {
						if len(logs.lines) != 0 {
							t.Fatal("matching or omitted claim emitted telemetry")
						}
						return
					}
					if len(logs.lines) != 1 {
						t.Fatal("committed mismatch did not emit exactly one event")
					}
					var record map[string]any
					if err := json.Unmarshal(logs.lines[0], &record); err != nil {
						t.Fatal(err)
					}
					delete(record, "time")
					delete(record, "level")
					token, err := f.q.GetOAuthTokenAuthorityByDigest(t.Context(), f.digest[:])
					if err != nil {
						t.Fatal(err)
					}
					want := map[string]any{"msg": "client_id_mismatch", "client_id": f.clientID.String(), "grant_id": f.grantID.String(), "token_id": token.OAuthToken.ID.String()}
					if !reflect.DeepEqual(record, want) {
						t.Fatal("mismatch telemetry contains fields beyond the closed event and stored IDs")
					}
				})
			}
		}
	}
}

func TestRevokeDatabaseFailuresRollBackAndDoNotLogClaims(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		for _, step := range []string{"begin", "GetOAuthTokenAuthorityByDigest", "GetOAuthClientForUpdate", "GetUserForUpdate", "GetOAuthGrantForUpdate", "RevokeOAuthGrant", "RevokeOAuthTokensForGrant", "commit"} {
			t.Run(mode+"/"+step, func(t *testing.T) {
				f := newRevokeFixture(t, mode)
				logs := captureRevokeLogs(t, f, false)
				trace := traceRevokeStorage(t, &f)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				trace.failStep, trace.cancel = step, cancel
				form := url.Values{"token": {f.raw}, "client_id": {"https://unknown.example/synthetic-sensitive-path"}}
				r := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/revoke", strings.NewReader(form.Encode()))
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				w := httptest.NewRecorder()
				f.s.HandleRevoke(w, r)
				if w.Code != http.StatusInternalServerError || w.Body.String() != `{"error":"server_error","error_description":"The server encountered an error."}` ||
					!reflect.DeepEqual(w.Header(), http.Header{"Content-Type": {"application/json"}}) {
					t.Fatal("database failure did not return the generic server error")
				}
				if !slices.Contains(trace.snapshot(), step) {
					t.Fatal("fault injection did not reach the named database operation")
				}
				if len(logs.lines) != 0 {
					t.Fatal("failed transaction emitted mismatch telemetry")
				}
				grant, err := f.q.GetOAuthGrantForUpdate(t.Context(), f.grantID)
				if err != nil || grant.RevokedAt != nil {
					t.Fatal("failed transaction committed the grant revocation", err)
				}
				var live int
				if err = f.pool.QueryRow(t.Context(), "SELECT count(*) FROM oauth_tokens WHERE grant_id=$1 AND revoked_at IS NULL", f.grantID).Scan(&live); err != nil || live != 4 {
					t.Fatal("failed transaction committed token revocations", err)
				}
				f.assertIsolation(t)
				trace.failStep, trace.cancel = "", nil
				assertEmptyRevoke(t, postRevoke(f.s, form.Encode(), nil))
				assertEmptyRevoke(t, postRevoke(f.s, form.Encode(), nil))
				assertGrantHasNoLiveTokens(t, f.refreshFixture)
			})
		}
	}
}

func TestRevokeUnavailableDatabaseIsNotUnknownTokenSuccess(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		t.Run(mode, func(t *testing.T) {
			f := newRevokeFixture(t, mode)
			logs := captureRevokeLogs(t, f, false)
			failedPool, err := pgxpool.NewWithConfig(t.Context(), f.pool.Config())
			if err != nil {
				t.Fatal(err)
			}
			failedPool.Close()
			f.s.pool = &store.Pool{Pool: failedPool}
			unknown, _, err := NewToken(TokenKindRefresh, bytes.NewReader(bytes.Repeat([]byte{233}, 32)))
			if err != nil {
				t.Fatal(err)
			}
			for _, raw := range []string{f.accessRaw, f.raw, unknown} {
				form := url.Values{"token": {raw}, "client_id": {"https://unknown.example/synthetic-sensitive-path"}}
				w := postRevoke(f.s, form.Encode(), nil)
				if w.Code != http.StatusInternalServerError || w.Body.String() != `{"error":"server_error","error_description":"The server encountered an error."}` ||
					!reflect.DeepEqual(w.Header(), http.Header{"Content-Type": {"application/json"}}) {
					t.Fatal("unavailable database became success or disclosed dependency details")
				}
			}
			malformed := url.Values{"token": {"synthetic-malformed"}, "client_id": {f.publicID}}
			assertEmptyRevoke(t, postRevoke(f.s, malformed.Encode(), nil))
			invalid := url.Values{"token": {f.raw}, "client_id": {"https://invalid.example:443/metadata"}}
			assertSameTokenResponse(t, postRevoke(f.s, "token=", nil), postRevoke(f.s, invalid.Encode(), nil))
			if len(logs.lines) != 0 {
				t.Fatal("unavailable database or rejected input emitted mismatch telemetry")
			}
			grant, err := f.q.GetOAuthGrantForUpdate(t.Context(), f.grantID)
			if err != nil || grant.RevokedAt != nil {
				t.Fatal("unavailable database changed the grant", err)
			}
			f.s.pool = f.pool
			assertEmptyRevoke(t, postRevoke(f.s, url.Values{"token": {f.raw}, "client_id": {f.publicID}}.Encode(), nil))
			assertGrantHasNoLiveTokens(t, f.refreshFixture)
			f.assertIsolation(t)
		})
	}
}

func TestRevokeBypassesExhaustedTokenAndGrantBudgets(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		for _, overflow := range []bool{false, true} {
			t.Run(mode+"/overflow="+strconv.FormatBool(overflow), func(t *testing.T) {
				f := newTokenRefusalFixture(t, mode, "refresh", overflow)
				invalid := f.makeInvalid(t, "wrong client")
				for range testOAuthRateConfig().TokenRequests {
					assertClosedInvalidGrant(t, f.post(t, invalid))
				}
				if f.post(t, invalid).Code != http.StatusTooManyRequests {
					t.Fatal("token-IP setup did not exhaust its budget")
				}
				bucket := f.probe.grants.entries[f.grantID]
				if overflow {
					bucket = &f.probe.grants.overflow
				}
				if bucket == nil || bucket.failures != f.probe.grants.limit {
					t.Fatal("stored-grant setup did not exhaust its budget")
				}
				failures, pending, start := bucket.failures, len(bucket.pending), bucket.windowStart
				lease, highTime, entries := f.probe.grants.nextLease, f.probe.grants.highTime, len(f.probe.grants.entries)
				addressCalls, grantCalls, completions := len(f.probe.addressAdmissions), f.probe.grantAdmissions, f.probe.completions
				form := url.Values{"token": {f.form.Get("refresh_token")}, "client_id": {"https://unknown.example/metadata"}}
				assertEmptyRevoke(t, postRevoke(f.s, form.Encode(), nil))
				if len(f.probe.addressAdmissions) != addressCalls || f.probe.grantAdmissions != grantCalls || f.probe.completions != completions ||
					bucket.failures != failures || len(bucket.pending) != pending || bucket.windowStart != start ||
					f.probe.grants.nextLease != lease || f.probe.grants.highTime != highTime || len(f.probe.grants.entries) != entries {
					t.Fatal("revocation consulted or changed token or grant admission")
				}
				grant, err := f.q.GetOAuthGrantForUpdate(t.Context(), f.grantID)
				if err != nil || grant.RevokedAt == nil {
					t.Fatal("exhausted grant remained live", err)
				}
			})
		}
	}
}

func TestRevokeClaimedClientSerializesWithRefresh(t *testing.T) {
	for _, mode := range []string{"dcr", "cimd on", "cimd off"} {
		for _, refreshFirst := range []bool{false, true} {
			t.Run(mode+"/refresh first="+strconv.FormatBool(refreshFirst), func(t *testing.T) {
				f := newRevokeFixture(t, mode)
				blocker, pid := lockUserForRace(t, f.refreshFixture)
				defer rollbackTestTx(t, blocker)
				revoke := func() <-chan *httptest.ResponseRecorder {
					result := make(chan *httptest.ResponseRecorder, 1)
					go func() {
						ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
						defer cancel()
						form := url.Values{"token": {f.raw}, "client_id": {"https://unknown.example/metadata"}}
						r := httptest.NewRequestWithContext(ctx, http.MethodPost, "https://aboutme.example/oauth/revoke", strings.NewReader(form.Encode()))
						r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
						w := httptest.NewRecorder()
						f.s.HandleRevoke(w, r)
						result <- w
					}()
					return result
				}
				var revocation <-chan *httptest.ResponseRecorder
				var rotation <-chan tokenHandlerResult
				if refreshFirst {
					rotation = runTokenRequest(f.s, "grant_type=refresh_token&refresh_token="+f.raw)
				} else {
					revocation = revoke()
				}
				waitForBlockedTransactions(t, f.pool, pid, 1)
				if refreshFirst {
					revocation = revoke()
				} else {
					rotation = runTokenRequest(f.s, "grant_type=refresh_token&refresh_token="+f.raw)
				}
				waitForBlockedDescendants(t, f.pool, pid, 2)
				if err := blocker.Commit(t.Context()); err != nil {
					t.Fatal(err)
				}
				assertEmptyRevoke(t, awaitRefusalResult(t, revocation))
				result := awaitTokenResult(t, rotation)
				if refreshFirst {
					if result.status != http.StatusOK {
						t.Fatal("refresh that held the lock first did not issue")
					}
				} else if result.status != http.StatusBadRequest || result.body != `{"error":"invalid_grant","error_description":"The request is invalid."}` {
					t.Fatal("refresh following revocation issued a successor")
				}
				assertGrantHasNoLiveTokens(t, f.refreshFixture)
				f.assertIsolation(t)
			})
		}
	}
}
