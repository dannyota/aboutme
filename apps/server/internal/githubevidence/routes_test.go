package githubevidence

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

const testOrigin = "https://evidence.example"

type stubGitHub struct {
	t   *testing.T
	URL string

	mu           sync.Mutex
	code         string
	challenge    string
	scope        *string
	userStatus   int
	userID       int64
	login        string
	tokens       []string
	exchanges    []url.Values
	profiles     []string
	revoked      []string
	revokeStatus int
	beforeToken  func()
	tokenBody    func(token string) string
}

func newStubGitHub(t *testing.T) *stubGitHub {
	t.Helper()
	empty := ""
	gh := &stubGitHub{t: t, scope: &empty, userStatus: http.StatusOK, userID: 583231, login: "octocat", revokeStatus: http.StatusNoContent}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login/oauth/access_token", gh.serveToken)
	mux.HandleFunc("GET /user", gh.serveUser)
	mux.HandleFunc("DELETE /applications/{client}/token", gh.serveRevoke)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	gh.URL = server.URL
	return gh
}

func (gh *stubGitHub) expect(code, challenge string) {
	gh.mu.Lock()
	defer gh.mu.Unlock()
	gh.code, gh.challenge = code, challenge
}

func (gh *stubGitHub) serveToken(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	gh.mu.Lock()
	before := gh.beforeToken
	gh.exchanges = append(gh.exchanges, r.PostForm)
	ok := r.PostForm.Get("code") == gh.code && gh.code != "" &&
		oauth2.S256ChallengeFromVerifier(r.PostForm.Get("code_verifier")) == gh.challenge &&
		r.PostForm.Get("client_id") == testClientID && r.PostForm.Get("client_secret") == testClientSecret
	token := sentinelToken + "-" + uuid.NewString()
	scope, raw := gh.scope, gh.tokenBody
	if ok {
		gh.tokens = append(gh.tokens, token)
	}
	gh.mu.Unlock()
	if before != nil {
		before()
	}
	w.Header().Set("Content-Type", "application/json")
	if !ok {
		gh.write(w, `{"error":"bad_verification_code"}`)
		return
	}
	if raw != nil {
		gh.write(w, raw(token))
		return
	}
	body := map[string]any{"access_token": token, "token_type": "bearer", "expires_in": 28800,
		"refresh_token": sentinelRefresh, "refresh_token_expires_in": 15897600}
	if scope != nil {
		body["scope"] = *scope
	}
	gh.encode(w, body)
}

func (gh *stubGitHub) serveUser(w http.ResponseWriter, r *http.Request) {
	gh.mu.Lock()
	gh.profiles = append(gh.profiles, r.Header.Get("Authorization"))
	status, id, login := gh.userStatus, gh.userID, gh.login
	gh.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	gh.encode(w, map[string]any{"id": id, "login": login, "message": sentinelBody})
}

func (gh *stubGitHub) serveRevoke(w http.ResponseWriter, r *http.Request) {
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gh.t.Errorf("revocation body: %v", err)
	}
	gh.mu.Lock()
	if r.PathValue("client") != testClientID || r.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte(testClientID+":"+testClientSecret)) {
		gh.t.Errorf("revocation without the client's Basic credentials")
	}
	gh.revoked = append(gh.revoked, body.AccessToken)
	status := gh.revokeStatus
	gh.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if status != http.StatusNoContent {
		gh.write(w, `{"message":"`+sentinelBody+`"}`)
	}
}

func (gh *stubGitHub) write(w io.Writer, body string) {
	if _, err := io.WriteString(w, body); err != nil {
		gh.t.Errorf("stub write: %v", err)
	}
}

func (gh *stubGitHub) encode(w io.Writer, body any) {
	if err := json.NewEncoder(w).Encode(body); err != nil {
		gh.t.Errorf("stub encode: %v", err)
	}
}

func (gh *stubGitHub) set(change func(*stubGitHub)) {
	gh.mu.Lock()
	defer gh.mu.Unlock()
	change(gh)
}

func (gh *stubGitHub) snapshot() (tokens, revoked []string, exchanges int, profiles int) {
	gh.mu.Lock()
	defer gh.mu.Unlock()
	return append([]string(nil), gh.tokens...), append([]string(nil), gh.revoked...), len(gh.exchanges), len(gh.profiles)
}

type harness struct {
	t        *testing.T
	gh       *stubGitHub
	pool     *store.Pool
	q        *store.Queries
	svc      *Service
	auth     *auth.Service
	sessions *auth.SessionManager
	handler  http.Handler
	logs     *syncBuffer
	user     store.User
	raw      string
	session  store.Session
	bodiesMu sync.Mutex
	bodies   []string
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool, err := store.NewPool(t.Context(), testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	gh := newStubGitHub(t)
	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	sessions := auth.NewSessionManagerWithPool(pool)
	svc, err := newService(Options{
		Config:       config.GitHubEvidenceConfig{ClientID: testClientID, ClientSecret: config.Secret(testClientSecret)},
		PublicOrigin: testOrigin, Pool: pool, Sessions: sessions, Logger: logger,
	}, gh.URL, newTransport())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if shutdownErr := svc.Shutdown(context.Background()); shutdownErr != nil {
			t.Errorf("shutdown: %v", shutdownErr)
		}
	})
	authService, err := auth.NewService(logger, config.Config{PublicOrigin: testOrigin}, pool)
	if err != nil {
		t.Fatal(err)
	}
	authService.SetSessionEndListener(svc)
	sessions.SetEndListener(svc)
	h := &harness{t: t, gh: gh, pool: pool, q: store.New(pool), svc: svc, auth: authService, sessions: sessions, logs: logs,
		handler: api.New(logger, noopPinger{}, api.Options{}, nil, svc.RegisterRoutes, authService.RegisterRoutes)}
	h.user, h.raw, h.session = h.newAccount()
	return h
}

type noopPinger struct{}

func (noopPinger) Ping(context.Context) error { return nil }

func (h *harness) newAccount() (store.User, string, store.Session) {
	h.t.Helper()
	user, err := h.q.CreateUser(h.t.Context(), store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Evidence test"})
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() {
		if _, cleanupErr := h.pool.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID); cleanupErr != nil {
			h.t.Error(cleanupErr)
		}
	})
	raw, sess, err := h.sessions.Issue(h.t.Context(), user.ID, "evidence-test", "203.0.113.7")
	if err != nil {
		h.t.Fatal(err)
	}
	return user, raw, sess
}

type call struct {
	method, path, csrf, body string
	cookies                  []*http.Cookie
	header                   http.Header
}

func (h *harness) do(c call) *httptest.ResponseRecorder {
	h.t.Helper()
	r := httptest.NewRequestWithContext(h.t.Context(), c.method, c.path, strings.NewReader(c.body))
	r.RemoteAddr = "203.0.113.7:4000"
	r.Header.Set("Origin", testOrigin)
	if c.csrf != "" {
		r.Header.Set(auth.CSRFHeaderName, c.csrf)
	}
	for name, values := range c.header {
		r.Header[name] = values
	}
	for _, cookie := range c.cookies {
		r.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, r)
	h.bodiesMu.Lock()
	h.bodies = append(h.bodies, rec.Body.String(), rec.Header().Get("Location"), strings.Join(rec.Header().Values("Set-Cookie"), ";"))
	h.bodiesMu.Unlock()
	return rec
}

func sessionCookie(raw string) *http.Cookie {
	return &http.Cookie{Name: "__Host-session", Value: raw}
}

func csrfFor(sess store.Session) string {
	return base64.RawURLEncoding.EncodeToString(sess.CSRFSecret)
}

func (h *harness) as(method, path string) call {
	return call{method: method, path: path, csrf: csrfFor(h.session), cookies: []*http.Cookie{sessionCookie(h.raw)}}
}

type started struct {
	state     string
	challenge string
	redirect  string
	link      *url.URL
	txCookie  *http.Cookie
}

func (h *harness) start(raw string, sess store.Session) started {
	h.t.Helper()
	s, err := h.startE(raw, sess)
	if err != nil {
		h.t.Fatal(err)
	}
	return s
}

func (h *harness) startE(raw string, sess store.Session) (started, error) {
	rec := h.do(call{method: http.MethodPost, path: StartPath, csrf: csrfFor(sess), cookies: []*http.Cookie{sessionCookie(raw)}})
	if rec.Code != http.StatusOK {
		return started{}, fmt.Errorf("start status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data struct {
			AuthorizeURL string `json:"authorizeUrl"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		return started{}, err
	}
	link, err := url.Parse(body.Data.AuthorizeURL)
	if err != nil {
		return started{}, err
	}
	var tx *http.Cookie
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == auth.OAuthTxCookieName {
			tx = cookie
		}
	}
	if tx == nil || tx.Value == "" || !tx.Secure || !tx.HttpOnly || tx.SameSite != http.SameSiteLaxMode || tx.Path != "/" || tx.Domain != "" || tx.MaxAge != 600 {
		return started{}, fmt.Errorf("start transaction cookie = %+v", tx)
	}
	return started{state: link.Query().Get("state"), challenge: link.Query().Get("code_challenge"), redirect: link.Query().Get("redirect_uri"), link: link, txCookie: tx}, nil
}

func (h *harness) callback(s started, query string, raw string) *httptest.ResponseRecorder {
	h.t.Helper()
	cookies := []*http.Cookie{}
	if s.txCookie != nil {
		cookies = append(cookies, &http.Cookie{Name: s.txCookie.Name, Value: s.txCookie.Value})
	}
	if raw != "" {
		cookies = append(cookies, sessionCookie(raw))
	}
	return h.do(call{method: http.MethodGet, path: CallbackPath + "?" + query, cookies: cookies})
}

func (h *harness) connect(raw string, sess store.Session) *httptest.ResponseRecorder {
	h.t.Helper()
	rec, err := h.connectE(raw, sess)
	if err != nil {
		h.t.Fatal(err)
	}
	return rec
}

func (h *harness) connectE(raw string, sess store.Session) (*httptest.ResponseRecorder, error) {
	s, err := h.startE(raw, sess)
	if err != nil {
		return nil, err
	}
	code := "code-" + uuid.NewString()
	h.gh.expect(code, s.challenge)
	return h.callback(s, "code="+code+"&state="+url.QueryEscape(s.state), raw), nil
}

func requireOutcome(t *testing.T, rec *httptest.ResponseRecorder, outcome string) {
	t.Helper()
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != testOrigin+"/app/settings/sessions?evidence="+outcome {
		t.Fatalf("callback = %d Location=%q body=%s, want 302 to evidence=%s", rec.Code, rec.Header().Get("Location"), rec.Body.String(), outcome)
	}
	cleared := false
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == auth.OAuthTxCookieName && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("callback did not clear the transaction cookie")
	}
}

func (h *harness) drain() {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := h.svc.registry.wait(ctx); err != nil {
		h.t.Fatalf("revocations did not finish: %v", err)
	}
}

func (h *harness) requireRevokedOnce(tokens ...string) {
	h.t.Helper()
	h.drain()
	_, revoked, _, _ := h.gh.snapshot()
	counts := map[string]int{}
	for _, token := range revoked {
		counts[token]++
	}
	for _, token := range tokens {
		if counts[token] != 1 {
			h.t.Fatalf("token %d revocations = %d, want 1 (all: %d)", len(token), counts[token], len(revoked))
		}
	}
	if len(revoked) != len(tokens) {
		h.t.Fatalf("revocations = %d, want %d", len(revoked), len(tokens))
	}
}

type connectionRow struct {
	githubUserID *int64
	login        *string
	connectedAt  *time.Time
	generation   int64
	found        bool
}

func (h *harness) connection(userID uuid.UUID) connectionRow {
	h.t.Helper()
	var row connectionRow
	err := h.pool.QueryRow(h.t.Context(), `SELECT github_user_id, login, connected_at, generation FROM github_connections WHERE user_id = $1`, userID).
		Scan(&row.githubUserID, &row.login, &row.connectedAt, &row.generation)
	if errors.Is(err, pgx.ErrNoRows) {
		return row
	}
	if err != nil {
		h.t.Fatal(err)
	}
	row.found = true
	return row
}

func (h *harness) status(raw string, sess store.Session) statusBody {
	h.t.Helper()
	rec := h.do(call{method: http.MethodGet, path: StatusPath, cookies: []*http.Cookie{sessionCookie(raw)}, csrf: csrfFor(sess)})
	if rec.Code != http.StatusOK {
		h.t.Fatalf("status = %d %s", rec.Code, rec.Body.String())
	}
	var body struct {
		Data statusBody `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		h.t.Fatal(err)
	}
	return body.Data
}

func TestEvidence_ConnectsWithTheExactCallbackAndNoScope(t *testing.T) {
	h := newHarness(t)
	if body := h.status(h.raw, h.session); body.Connection != nil || body.Session != nil {
		t.Fatalf("status before connecting = %+v, want nothing", body)
	}
	s := h.start(h.raw, h.session)
	want := testOrigin + "/api/v1/github-evidence/callback"
	query := s.link.Query()
	if s.link.Scheme+"://"+s.link.Host != h.gh.URL || s.link.Path != "/login/oauth/authorize" || query.Get("client_id") != testClientID ||
		s.redirect != want || query.Get("code_challenge_method") != "S256" || s.state == "" || s.challenge == "" || query.Has("scope") {
		t.Fatalf("authorize URL = %s", s.link)
	}
	code := "code-" + uuid.NewString()
	h.gh.expect(code, s.challenge)
	rec := h.callback(s, "code="+code+"&state="+url.QueryEscape(s.state), h.raw)
	requireOutcome(t, rec, outcomeConnected)

	h.gh.mu.Lock()
	exchange := h.gh.exchanges[0]
	authorization := h.gh.profiles[0]
	token := h.gh.tokens[0]
	h.gh.mu.Unlock()
	if exchange.Get("redirect_uri") != want || exchange.Has("scope") {
		t.Fatalf("exchange form = %v, want redirect_uri %s and no scope", exchange, want)
	}
	if authorization != "Bearer "+token {
		t.Fatal("profile fetch did not use the exchanged token")
	}
	row := h.connection(h.user.ID)
	if !row.found || *row.githubUserID != 583231 || *row.login != "octocat" || row.generation != 1 || row.connectedAt == nil {
		t.Fatalf("connection row = %+v", row)
	}
	body := h.status(h.raw, h.session)
	if body.Connection == nil || body.Connection.GitHubUserID != 583231 || body.Connection.Login != "octocat" || body.Session == nil {
		t.Fatalf("status after connecting = %+v", body)
	}
	if left := time.Until(body.Session.ExpiresAt); left > 30*time.Minute || left < 29*time.Minute {
		t.Fatalf("session expires in %v, want about 30 minutes", left)
	}
	h.requireRevokedOnce()
	if _, live := h.svc.registry.current(h.user.ID); !live {
		t.Fatal("no live session after connecting")
	}
}

func TestEvidence_CallbackChecksTheTransactionRedirectURI(t *testing.T) {
	h := newHarness(t)
	handle, tx, err := auth.NewTransactionStore(h.q).BeginEvidence(t.Context(), h.user.ID, 0, testOrigin+"/api/v1/auth/github/callback")
	if err != nil {
		t.Fatal(err)
	}
	h.gh.expect("code", oauth2.S256ChallengeFromVerifier(tx.PKCEVerifier))
	rec := h.callback(started{txCookie: &http.Cookie{Name: auth.OAuthTxCookieName, Value: handle}}, "code=code&state="+url.QueryEscape(tx.State), h.raw)
	requireOutcome(t, rec, outcomeFailed)
	if _, _, exchanges, _ := h.gh.snapshot(); exchanges != 0 {
		t.Fatalf("a transaction for another redirect URI reached the exchange %d times", exchanges)
	}
}

func TestEvidence_CallbackFailuresBeforeTheExchange(t *testing.T) {
	h := newHarness(t)
	other, otherRaw, otherSession := h.newAccount()
	for _, tc := range []struct {
		name  string
		query func(started) string
		raw   string
		noTx  bool
	}{
		{"another user's session", func(s started) string { return "code=c&state=" + url.QueryEscape(s.state) }, otherRaw, false},
		{"no session", func(s started) string { return "code=c&state=" + url.QueryEscape(s.state) }, "", false},
		{"state mismatch", func(started) string { return "code=c&state=wrong" }, h.raw, false},
		{"provider error", func(s started) string { return "error=access_denied&state=" + url.QueryEscape(s.state) }, h.raw, false},
		{"code missing", func(s started) string { return "state=" + url.QueryEscape(s.state) }, h.raw, false},
		{"transaction cookie missing", func(s started) string { return "code=c&state=" + url.QueryEscape(s.state) }, h.raw, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := h.start(h.raw, h.session)
			h.gh.expect("c", s.challenge)
			if tc.noTx {
				s.txCookie = nil
			}
			requireOutcome(t, h.callback(s, tc.query(s), tc.raw), outcomeFailed)
		})
	}
	if _, _, exchanges, _ := h.gh.snapshot(); exchanges != 0 {
		t.Fatalf("failed callbacks reached the exchange %d times, want 0", exchanges)
	}
	if h.connection(h.user.ID).found || h.connection(other.ID).found {
		t.Fatal("a failed callback created a connection")
	}
	if body := h.status(otherRaw, otherSession); body.Connection != nil || body.Session != nil {
		t.Fatalf("other account status = %+v", body)
	}
}

func TestEvidence_ReplayedCallbackFails(t *testing.T) {
	h := newHarness(t)
	s := h.start(h.raw, h.session)
	code := "code-" + uuid.NewString()
	h.gh.expect(code, s.challenge)
	query := "code=" + code + "&state=" + url.QueryEscape(s.state)
	requireOutcome(t, h.callback(s, query, h.raw), outcomeConnected)
	requireOutcome(t, h.callback(s, query, h.raw), outcomeFailed)
	if _, _, exchanges, _ := h.gh.snapshot(); exchanges != 1 {
		t.Fatalf("exchanges = %d, want 1", exchanges)
	}
	if row := h.connection(h.user.ID); row.generation != 1 {
		t.Fatalf("generation after a replay = %d, want 1", row.generation)
	}
}

func TestEvidence_FailuresAfterTheExchangeRevokeOnce(t *testing.T) {
	missing := (*string)(nil)
	granted := "read:user"
	for _, tc := range []struct {
		name    string
		prepare func(h *harness, s started)
	}{
		{"callback after a disconnect", func(h *harness, _ started) {
			if rec := h.do(h.as(http.MethodDelete, StatusPath+"?keepLinks=true")); rec.Code != http.StatusNoContent {
				h.t.Fatalf("disconnect = %d %s", rec.Code, rec.Body.String())
			}
		}},
		{"disconnect commits during the exchange", func(h *harness, _ started) {
			h.gh.set(func(gh *stubGitHub) {
				gh.beforeToken = func() {
					if _, err := h.q.ClearGitHubConnection(context.Background(), h.user.ID); err != nil {
						h.t.Error(err)
					}
				}
			})
		}},
		{"scope granted", func(h *harness, _ started) { h.gh.set(func(gh *stubGitHub) { gh.scope = &granted }) }},
		{"scope missing", func(h *harness, _ started) { h.gh.set(func(gh *stubGitHub) { gh.scope = missing }) }},
		{"profile fetch failed", func(h *harness, _ started) { h.gh.set(func(gh *stubGitHub) { gh.userStatus = http.StatusBadGateway }) }},
		{"session revoked during the exchange", func(h *harness, _ started) {
			h.gh.set(func(gh *stubGitHub) {
				gh.beforeToken = func() {
					if err := h.sessions.Revoke(context.Background(), h.session.ID); err != nil {
						h.t.Error(err)
					}
				}
			})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			s := h.start(h.raw, h.session)
			code := "code-" + uuid.NewString()
			h.gh.expect(code, s.challenge)
			tc.prepare(h, s)
			requireOutcome(t, h.callback(s, "code="+code+"&state="+url.QueryEscape(s.state), h.raw), outcomeFailed)
			tokens, _, exchanges, _ := h.gh.snapshot()
			if exchanges != 1 || len(tokens) != 1 {
				t.Fatalf("exchanges = %d tokens = %d, want 1 and 1", exchanges, len(tokens))
			}
			h.requireRevokedOnce(tokens...)
			if row := h.connection(h.user.ID); row.githubUserID != nil {
				t.Fatalf("connection after a failed callback = %+v", row)
			}
			if _, live := h.svc.registry.current(h.user.ID); live {
				t.Fatal("failed callback left a live session")
			}
		})
	}
}

func TestEvidence_SessionEndingsRevokeOnce(t *testing.T) {
	for _, tc := range []struct {
		name string
		end  func(h *harness)
	}{
		{"done", func(h *harness) {
			if rec := h.do(h.as(http.MethodPost, SessionEndPath)); rec.Code != http.StatusNoContent {
				h.t.Fatalf("done = %d %s", rec.Code, rec.Body.String())
			}
			rec := h.do(h.as(http.MethodPost, SessionEndPath))
			if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), sessionRequiredCode) {
				h.t.Fatalf("second done = %d %s, want 409 %s", rec.Code, rec.Body.String(), sessionRequiredCode)
			}
		}},
		{"disconnect", func(h *harness) {
			if rec := h.do(h.as(http.MethodDelete, StatusPath+"?keepLinks=false")); rec.Code != http.StatusNoContent {
				h.t.Fatalf("disconnect = %d %s", rec.Code, rec.Body.String())
			}
			if row := h.connection(h.user.ID); row.githubUserID != nil || row.login != nil || row.connectedAt != nil || row.generation != 2 {
				h.t.Fatalf("connection after disconnect = %+v, want cleared at generation 2", row)
			}
		}},
		{"sign-out", func(h *harness) {
			if rec := h.do(h.as(http.MethodPost, auth.LogoutPath)); rec.Code != http.StatusNoContent {
				h.t.Fatalf("logout = %d %s", rec.Code, rec.Body.String())
			}
		}},
		{"epoch change", func(h *harness) {
			err := pgx.BeginFunc(context.Background(), h.pool, func(tx pgx.Tx) error {
				qtx := h.q.WithTx(tx)
				if _, err := qtx.AdvanceUserAuthEpoch(context.Background(), h.user.ID); err != nil {
					return err
				}
				_, err := h.sessions.ReplaceAfterEpochChangeTx(context.Background(), qtx, h.user.ID, h.session.ID, nil)
				return err
			})
			if err != nil {
				h.t.Fatal(err)
			}
		}},
		{"replacement", func(h *harness) {
			requireOutcome(h.t, h.connect(h.raw, h.session), outcomeConnected)
		}},
		{"process exit", func(h *harness) {
			if err := h.svc.Shutdown(context.Background()); err != nil {
				h.t.Fatal(err)
			}
			if rec := h.do(h.as(http.MethodPost, StartPath)); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), unavailableCode) {
				h.t.Fatalf("start after shutdown = %d %s", rec.Code, rec.Body.String())
			}
		}},
		{"stale epoch seen by status", func(h *harness) {
			if _, err := h.q.AdvanceUserAuthEpoch(context.Background(), h.user.ID); err != nil {
				h.t.Fatal(err)
			}
			raw, sess, err := h.sessions.Issue(context.Background(), h.user.ID, "evidence-test", "203.0.113.7")
			if err != nil {
				h.t.Fatal(err)
			}
			if body := h.status(raw, sess); body.Session != nil {
				h.t.Fatalf("status after an epoch change = %+v, want no session", body)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
			first, _, _, _ := h.gh.snapshot()
			tc.end(h)
			h.requireRevokedOnce(first[0])
			if live, ok := h.svc.registry.current(h.user.ID); ok && live.token != nil {
				if value, held := live.token.peek(); held && value == first[0] {
					t.Fatal("the ended token is still held")
				}
			}
		})
	}
}

func TestEvidence_RacingEndingsSendOneRevocation(t *testing.T) {
	h := newHarness(t)
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	tokens, _, _, _ := h.gh.snapshot()
	live, _ := h.svc.registry.current(h.user.ID)
	var wg sync.WaitGroup
	for range 4 {
		_, other, err := h.sessions.Issue(t.Context(), h.user.ID, "evidence-test", "203.0.113.9")
		if err != nil {
			t.Fatal(err)
		}
		wg.Go(func() { h.svc.registry.endSession(h.user.ID, live) })
		wg.Go(func() {
			r := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, auth.SessionsPath+"/"+other.ID.String(), nil)
			r.Header.Set("Origin", testOrigin)
			r.Header.Set(auth.CSRFHeaderName, csrfFor(h.session))
			r.AddCookie(sessionCookie(h.raw))
			h.handler.ServeHTTP(httptest.NewRecorder(), r)
		})
		wg.Go(func() {
			r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, SessionEndPath, nil)
			r.Header.Set("Origin", testOrigin)
			r.Header.Set(auth.CSRFHeaderName, csrfFor(h.session))
			r.AddCookie(sessionCookie(h.raw))
			h.handler.ServeHTTP(httptest.NewRecorder(), r)
		})
		wg.Go(func() {
			r := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, StatusPath+"?keepLinks=true", nil)
			r.Header.Set("Origin", testOrigin)
			r.Header.Set(auth.CSRFHeaderName, csrfFor(h.session))
			r.AddCookie(sessionCookie(h.raw))
			h.handler.ServeHTTP(httptest.NewRecorder(), r)
		})
	}
	wg.Wait()
	h.requireRevokedOnce(tokens[0])
}

func TestEvidence_StartAndRouteRules(t *testing.T) {
	h := newHarness(t)
	for _, tc := range []struct {
		name   string
		c      call
		status int
		code   string
	}{
		{"GET start", call{method: http.MethodGet, path: StartPath, cookies: []*http.Cookie{sessionCookie(h.raw)}}, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"GET session end", call{method: http.MethodGet, path: SessionEndPath, cookies: []*http.Cookie{sessionCookie(h.raw)}}, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"POST callback", call{method: http.MethodPost, path: CallbackPath}, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"PUT status", call{method: http.MethodPut, path: StatusPath}, http.StatusMethodNotAllowed, "method_not_allowed"},
		{"start without a session", call{method: http.MethodPost, path: StartPath}, http.StatusUnauthorized, "session_required"},
		{"start with a bearer token only", call{method: http.MethodPost, path: StartPath, header: http.Header{"Authorization": {"Bearer " + sentinelToken}}}, http.StatusUnauthorized, "session_required"},
		{"status with a bearer token only", call{method: http.MethodGet, path: StatusPath, header: http.Header{"Authorization": {"Bearer " + sentinelToken}}}, http.StatusUnauthorized, "session_required"},
		{"start without CSRF", call{method: http.MethodPost, path: StartPath, cookies: []*http.Cookie{sessionCookie(h.raw)}}, http.StatusForbidden, "csrf_rejected"},
		{"done without CSRF", call{method: http.MethodPost, path: SessionEndPath, cookies: []*http.Cookie{sessionCookie(h.raw)}}, http.StatusForbidden, "csrf_rejected"},
		{"disconnect without CSRF", call{method: http.MethodDelete, path: StatusPath + "?keepLinks=true", cookies: []*http.Cookie{sessionCookie(h.raw)}}, http.StatusForbidden, "csrf_rejected"},
		{"start with a query", func() call { c := h.as(http.MethodPost, StartPath+"?purpose=link"); return c }(), http.StatusBadRequest, requestInvalidCode},
		{"done without a session", h.as(http.MethodPost, SessionEndPath), http.StatusConflict, sessionRequiredCode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := h.do(tc.c)
			var body struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("%s body %q: %v", tc.name, rec.Body.String(), err)
			}
			if rec.Code != tc.status || body.Error.Code != tc.code {
				t.Fatalf("%s = %d %s, want %d %s", tc.name, rec.Code, rec.Body.String(), tc.status, tc.code)
			}
		})
	}
	var count int
	if err := h.pool.QueryRow(t.Context(), `SELECT count(*) FROM oauth_transactions WHERE linking_user_id = $1`, h.user.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("refused starts created %d transactions, %v", count, err)
	}
}

func TestEvidence_KeepLinksIsARequiredBoolean(t *testing.T) {
	h := newHarness(t)
	for _, query := range []string{"", "?", "?keepLinks", "?keepLinks=", "?keepLinks=yes", "?keepLinks=TRUE", "?keepLinks=1",
		"?keepLinks=true&keepLinks=false", "?keepLinks=true&other=1", "?keeplinks=true"} {
		rec := h.do(h.as(http.MethodDelete, StatusPath+query))
		if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), requestInvalidCode) {
			t.Errorf("DELETE %q = %d %s, want 400 %s", query, rec.Code, rec.Body.String(), requestInvalidCode)
		}
	}
	if h.connection(h.user.ID).found {
		t.Fatal("an invalid disconnect wrote the connection row")
	}
	withBody := h.as(http.MethodDelete, StatusPath+"?keepLinks=true")
	withBody.body = "{}"
	withBody.header = http.Header{"Content-Type": {"application/json"}}
	if rec := h.do(withBody); rec.Code != http.StatusBadRequest {
		t.Errorf("DELETE with a body = %d, want 400", rec.Code)
	}
	for i, query := range []string{"?keepLinks=true", "?keepLinks=false"} {
		if rec := h.do(h.as(http.MethodDelete, StatusPath+query)); rec.Code != http.StatusNoContent {
			t.Fatalf("DELETE %s = %d %s", query, rec.Code, rec.Body.String())
		}
		if row := h.connection(h.user.ID); row.generation != int64(i+1) || row.githubUserID != nil {
			t.Fatalf("after DELETE %s connection = %+v, want generation %d", query, row, i+1)
		}
	}
}

func TestEvidence_EleventhStartInAnHourIsRefused(t *testing.T) {
	h := newHarness(t)
	for range startLimit {
		h.start(h.raw, h.session)
	}
	rec := h.do(h.as(http.MethodPost, StartPath))
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("11th start body %q: %v", rec.Body.String(), err)
	}
	retry, retryErr := strconv.Atoi(rec.Header().Get("Retry-After"))
	if rec.Code != http.StatusTooManyRequests || body.Error.Code != "rate_limited" || body.Error.Message != "too many requests; retry later" ||
		retryErr != nil || retry < 1 || retry > 3600 {
		t.Fatalf("11th start = %d %s Retry-After=%q, want 429 rate_limited with whole seconds", rec.Code, rec.Body.String(), rec.Header().Get("Retry-After"))
	}
	var started int
	if err := h.pool.QueryRow(t.Context(), `SELECT count(*) FROM oauth_transactions WHERE linking_user_id = $1`, h.user.ID).Scan(&started); err != nil || started != startLimit {
		t.Fatalf("transactions after a refused start = %d, %v; want %d", started, err, startLimit)
	}
	_, otherRaw, otherSession := h.newAccount()
	h.start(otherRaw, otherSession)
}

func TestEvidence_NoTokenOrSecretInRoutesLogsOrRows(t *testing.T) {
	h := newHarness(t)
	requireOutcome(t, h.connect(h.raw, h.session), outcomeConnected)
	h.status(h.raw, h.session)
	h.gh.set(func(gh *stubGitHub) {
		gh.scope = nil
		gh.revokeStatus = http.StatusInternalServerError
	})
	requireOutcome(t, h.connect(h.raw, h.session), outcomeFailed)
	h.do(h.as(http.MethodPost, SessionEndPath))
	h.do(h.as(http.MethodDelete, StatusPath+"?keepLinks=true"))
	h.drain()
	tokens, revoked, _, _ := h.gh.snapshot()
	if len(tokens) != 2 || len(revoked) != 2 {
		t.Fatalf("tokens = %d, revocation attempts = %d; want 2 and 2", len(tokens), len(revoked))
	}
	var rows string
	if err := h.pool.QueryRow(t.Context(), `
		SELECT coalesce((SELECT string_agg(row_to_json(c)::text, ' ') FROM github_connections c WHERE user_id = $1), '') ||
		       coalesce((SELECT string_agg(row_to_json(o)::text, ' ') FROM oauth_transactions o WHERE linking_user_id = $1), '')
	`, h.user.ID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	logs := h.logs.String()
	if !strings.Contains(logs, `"category":"rejected"`) || !strings.Contains(logs, `"reason":"scope_rejected"`) {
		t.Fatalf("logs = %s, want the revocation category and the callback reason", logs)
	}
	surfaces := map[string]string{"responses": strings.Join(h.bodies, "\n"), "logs": logs, "rows": rows}
	for name, text := range surfaces {
		for _, secret := range []string{sentinelToken, sentinelRefresh, testClientSecret, sentinelBody, "Basic ", "Bearer "} {
			if strings.Contains(text, secret) {
				t.Fatalf("%s contain %q", name, secret)
			}
		}
	}
}

func TestEvidence_OffAnswers404EverywhereAndSignInIsUnchanged(t *testing.T) {
	pool, err := store.NewPool(t.Context(), testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	signIn := config.Config{PublicOrigin: testOrigin, ProviderLogin: config.ProviderLogin{GitHub: true},
		GitHubClientID: "sign-in-client", GitHubClientSecret: "sign-in-secret"}
	signInStart := func(evidence config.GitHubEvidenceConfig) (int, string) {
		cfg := signIn
		cfg.GitHubEvidence = evidence
		authService, err := auth.NewService(slog.New(slog.DiscardHandler), cfg, pool)
		if err != nil {
			t.Fatal(err)
		}
		svc, err := New(Options{Config: evidence, PublicOrigin: testOrigin, Pool: pool, Sessions: auth.NewSessionManagerWithPool(pool)})
		if err != nil {
			t.Fatal(err)
		}
		handler := api.New(slog.New(slog.DiscardHandler), noopPinger{}, api.Options{}, nil, svc.RegisterRoutes, authService.RegisterRoutes)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, auth.GitHubStartPath, nil))
		location, err := url.Parse(rec.Header().Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		values := location.Query()
		values.Del("state")
		values.Del("code_challenge")
		if !svc.Enabled() {
			for _, path := range []string{StatusPath, StartPath, CallbackPath, SessionEndPath, StatusPath + "/candidates"} {
				for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
					off := httptest.NewRecorder()
					handler.ServeHTTP(off, httptest.NewRequestWithContext(t.Context(), method, path, nil))
					if off.Code != http.StatusNotFound || !strings.Contains(off.Body.String(), unavailableCode) {
						t.Errorf("evidence off: %s %s = %d %s, want 404 %s", method, path, off.Code, off.Body.String(), unavailableCode)
					}
				}
			}
		}
		return rec.Code, location.Host + location.Path + "?" + values.Encode()
	}
	offStatus, off := signInStart(config.GitHubEvidenceConfig{})
	onStatus, on := signInStart(config.GitHubEvidenceConfig{ClientID: testClientID, ClientSecret: config.Secret(testClientSecret)})
	if offStatus != http.StatusFound || onStatus != offStatus || off != on || !strings.Contains(off, "client_id=sign-in-client") || !strings.Contains(off, "scope=user%3Aemail") {
		t.Fatalf("sign-in start off = %d %s, on = %d %s; want the same sign-in redirect", offStatus, off, onStatus, on)
	}
}
