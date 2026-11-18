package githubevidence

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/accountapi"
	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/authmail"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/media"
	"danny.vn/aboutme/apps/server/internal/password"
	"danny.vn/aboutme/apps/server/internal/project"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/secondfactor"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

type traceEvent struct {
	mark bool
	sql  string
	err  error
}

type traceKey struct{}

type statementTrace struct {
	mu          sync.Mutex
	events      []traceEvent
	afterCommit func()
}

func (tr *statementTrace) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	tr.mu.Lock()
	tr.events = append(tr.events, traceEvent{sql: strings.ToLower(strings.TrimSpace(data.SQL))})
	index := len(tr.events) - 1
	tr.mu.Unlock()
	return context.WithValue(ctx, traceKey{}, index)
}

func (tr *statementTrace) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryEndData) {
	index, ok := ctx.Value(traceKey{}).(int)
	if !ok {
		return
	}
	tr.mu.Lock()
	tr.events[index].err = data.Err
	var hook func()
	if tr.events[index].sql == "commit" && data.Err == nil && tr.afterCommit != nil && index > 0 && tr.events[index-1].mark {
		hook, tr.afterCommit = tr.afterCommit, nil
	}
	tr.mu.Unlock()
	if hook != nil {
		hook()
	}
}

func (tr *statementTrace) mark() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.events = append(tr.events, traceEvent{mark: true})
}

func (tr *statementTrace) reset() {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.events = nil
}

func (tr *statementTrace) marks() int {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	n := 0
	for _, event := range tr.events {
		if event.mark {
			n++
		}
	}
	return n
}

func isWrite(sql string) bool {
	for _, verb := range []string{"insert ", "update ", "delete "} {
		if strings.HasPrefix(sql, verb) || strings.Contains(sql, "\n"+verb) {
			return true
		}
	}
	return false
}

func (tr *statementTrace) executed(prefix string) bool {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	for _, event := range tr.events {
		if strings.HasPrefix(event.sql, prefix) {
			return true
		}
	}
	return false
}

func (tr *statementTrace) violations() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	var found []string
	for i, event := range tr.events {
		if !event.mark {
			continue
		}
		next := -1
		for j := i + 1; j < len(tr.events); j++ {
			if !tr.events[j].mark {
				next = j
				break
			}
		}
		if next < 0 {
			found = append(found, "a notification with no commit after it")
			continue
		}
		if tr.events[next].sql != "commit" {
			found = append(found, fmt.Sprintf("a notification followed by %q instead of commit", firstLine(tr.events[next].sql)))
			continue
		}
		if tr.events[next].err != nil {
			continue
		}
		for j := next + 1; j < len(tr.events); j++ {
			if !tr.events[j].mark && isWrite(tr.events[j].sql) {
				found = append(found, fmt.Sprintf("a write after the notified commit: %q", firstLine(tr.events[j].sql)))
			}
		}
	}
	return found
}

func firstLine(sql string) string {
	line, _, _ := strings.Cut(sql, "\n")
	if len(line) > 80 {
		return line[:80]
	}
	return line
}

type offsetClock struct {
	offset atomic.Int64
}

func (c *offsetClock) now() time.Time {
	return time.Now().Add(time.Duration(c.offset.Load()))
}

func (c *offsetClock) advance(d time.Duration) {
	c.offset.Add(int64(d))
}

type invHarness struct {
	*harness
	trace     *statementTrace
	clock     *offsetClock
	passwords *auth.PasswordService
	factors   *secondfactor.Service
	ring      *secondfactor.TOTPKeyRing
	accounts  *accountapi.Service
	hasher    *password.Hasher
}

func newInvariantHarness(t *testing.T) *invHarness {
	t.Helper()
	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	plain, err := store.NewPool(t.Context(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { plain.Close(context.Background()) })
	trace := &statementTrace{}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.Tracer = trace
	traced, err := pgxpool.NewWithConfig(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	pool := &store.Pool{Pool: traced}
	t.Cleanup(traced.Close)

	clock := &offsetClock{}
	gh := newStubGitHub(t)
	logs := &syncBuffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))
	sessions := auth.NewSessionManagerWithPool(pool)
	svc, err := newService(Options{
		Config:       config.GitHubEvidenceConfig{ClientID: testClientID, ClientSecret: config.Secret(testClientSecret)},
		PublicOrigin: testOrigin, Pool: pool, Sessions: sessions, Logger: logger, Clock: clock.now,
	}, gh.URL, newTransport())
	if err != nil {
		t.Fatal(err)
	}
	svc.registry.observe = trace.mark
	t.Cleanup(func() {
		if shutdownErr := svc.Shutdown(context.Background()); shutdownErr != nil {
			t.Errorf("shutdown: %v", shutdownErr)
		}
	})
	sessions.SetEndListener(svc)
	authService, err := auth.NewService(logger, config.Config{PublicOrigin: testOrigin}, pool)
	if err != nil {
		t.Fatal(err)
	}
	authService.SetSessionEndListener(svc)

	var key [32]byte
	if _, err = rand.Read(key[:]); err != nil {
		t.Fatal(err)
	}
	ring, err := authmail.NewKeyRing("k1", map[string][32]byte{"k1": key}, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	outbox, err := authmail.NewOutbox(ring, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	hasher, err := password.NewHasher(password.HashPolicy{Version: 19, MemoryKiB: 64, Iterations: 1, Parallelism: 1, SaltLen: 16, KeyLen: 32}, rand.Reader, password.NewAdmission())
	if err != nil {
		t.Fatal(err)
	}
	limits, err := auth.NewPasswordRatePolicies(key)
	if err != nil {
		t.Fatal(err)
	}
	passwords, err := auth.NewPasswordService(auth.PasswordServiceOptions{
		Pool: pool, Queries: store.New(pool), Sessions: sessions, Policy: password.NewPolicy(nil, nil), Hasher: hasher,
		Outbox: outbox, Limits: limits, PublicOrigin: testOrigin, Clock: clock.now, Entropy: rand.Reader, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	rp, err := secondfactor.NewRelyingParty(testOrigin)
	if err != nil {
		t.Fatal(err)
	}
	var totpKey [32]byte
	if _, err = rand.Read(totpKey[:]); err != nil {
		t.Fatal(err)
	}
	totpRing, err := secondfactor.NewTOTPKeyRing(base64.RawURLEncoding.EncodeToString(totpKey[:]), "", rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	factors, err := secondfactor.New(secondfactor.Options{
		Pool: pool, Pending: auth.NewPendingAuthenticationManager(pool, logger), Sessions: sessions, Outbox: outbox,
		RelyingParty: rp, EnrollmentEnabled: true, TOTPKeyRing: totpRing, TOTPEnrollmentEnabled: true,
		TOTPIssuer: "evidence.example", Clock: clock.now, Entropy: rand.Reader, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	public, err := store.New(plain).GetPublicState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: public.DiscoveryGeneration})
	if err != nil {
		t.Fatal(err)
	}
	projector := docmigrate.NewIdentityProjector()
	svc.SetLibrary(resumeapi.New(resume.NewStore(pool, projector), resume.NewIdempotencyStore(pool), projector, nil, resumeapi.Options{
		SessionManager: sessions, PublicOrigin: testOrigin, Coordinator: coordinator, RecoveryPool: pool,
	}))
	backend, err := media.NewFS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := accountapi.New(accountapi.Dependencies{
		Pool: pool, Sessions: sessions, Coordinator: coordinator, Media: backend,
		Projector: projector, Logger: logger, PublicOrigin: testOrigin, Now: clock.now,
	})
	if err != nil {
		t.Fatal(err)
	}

	h := &harness{t: t, gh: gh, pool: plain, q: store.New(plain), svc: svc, auth: authService, sessions: sessions, logs: logs,
		handler: api.New(logger, noopPinger{}, api.Options{}, nil, svc.RegisterRoutes, authService.RegisterRoutes, passwords.RegisterRoutes)}
	h.user, h.raw, h.session = h.newAccount()
	return &invHarness{harness: h, trace: trace, clock: clock, passwords: passwords, factors: factors, ring: totpRing, accounts: accounts, hasher: hasher}
}

type callerRun struct {
	ih           *invHarness
	ownerToken   string
	raw          string
	session      store.Session
	other        store.Session
	resetToken   password.Token
	resetID      uuid.UUID
	slug         string
	pending      started
	code         string
	factor       auth.SecondFactorCredential
	passkeyID    uuid.UUID
	enrollmentID string
	totpSecret   string
	project      uuid.UUID
}

func (c *callerRun) serve(method, path, body string) *httptest.ResponseRecorder {
	return c.serveWith(method, path, body, nil)
}

func (c *callerRun) serveWith(method, path, body string, header http.Header) *httptest.ResponseRecorder {
	r := httptest.NewRequestWithContext(context.Background(), method, path, strings.NewReader(body))
	for name, values := range header {
		r.Header[name] = values
	}
	r.RemoteAddr = "203.0.113.7:4000"
	r.Header.Set("Origin", testOrigin)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if c.raw != "" {
		r.AddCookie(sessionCookie(c.raw))
		r.Header.Set(auth.CSRFHeaderName, csrfFor(c.session))
	}
	rec := httptest.NewRecorder()
	c.ih.handler.ServeHTTP(rec, r)
	return rec
}

func (c *callerRun) expireSession() error {
	_, err := c.ih.pool.Exec(context.Background(), `UPDATE sessions SET absolute_expires_at = $2 WHERE id = $1`, c.session.ID, time.Now())
	return err
}

func (c *callerRun) revokeSession() error {
	_, err := c.ih.pool.Exec(context.Background(), `UPDATE sessions SET revoked_at = $2 WHERE id = $1`, c.session.ID, time.Now())
	return err
}

func (c *callerRun) freshSession() (store.Session, error) {
	return store.New(c.ih.pool).GetSessionByID(context.Background(), c.session.ID)
}

func (c *callerRun) enableSecondFactor(proofAge time.Duration) error {
	handle := make([]byte, 32)
	if _, err := rand.Read(handle); err != nil {
		return err
	}
	if _, err := c.ih.pool.Exec(context.Background(), `INSERT INTO second_factor_policies (user_id, webauthn_user_handle, enabled_at) VALUES ($1, $2, now())`, c.ih.user.ID, handle); err != nil {
		return err
	}
	proof := time.Now().Add(-proofAge)
	_, err := c.ih.pool.Exec(context.Background(), `UPDATE sessions SET reauthenticated_at = $2, second_factor_verified_at = $2 WHERE id = $1`, c.session.ID, proof)
	return err
}

func statusAccepted(rec *httptest.ResponseRecorder, want int) error {
	if rec.Code != want {
		return fmt.Errorf("status %d %s", rec.Code, firstLine(rec.Body.String()))
	}
	return nil
}

type invariantSite struct {
	file string
	fn   string
}

func (s invariantSite) String() string {
	return s.file + ":" + s.fn
}

type lockStep struct {
	waits string
	sql   string
	key   func(c *callerRun) any
}

type callerCase struct {
	name       string
	site       invariantSite
	setup      func(c *callerRun) error
	run        func(c *callerRun) error
	invalidate func(c *callerRun) error
	stale      func(c *callerRun) error
	untouched  []string
	lastLocks  []lockStep
	lapse      func(c *callerRun, holder pgx.Tx) error
}

const (
	proofLapse = 16 * time.Minute
	idleLapse  = 31 * 24 * time.Hour
)

func callerSessionID(c *callerRun) any { return c.session.ID }

func otherSessionID(c *callerRun) any { return c.other.ID }

func accountUserID(c *callerRun) any { return c.ih.user.ID }

func sessionLock(waits string, key func(c *callerRun) any) lockStep {
	return lockStep{waits: waits, sql: `SELECT 1 FROM sessions WHERE id = $1 FOR UPDATE`, key: key}
}

func policyLock() lockStep {
	return lockStep{waits: "getsecondfactorpolicyforupdate", sql: `SELECT 1 FROM second_factor_policies WHERE user_id = $1 FOR UPDATE`, key: accountUserID}
}

func passwordCredentialLock() lockStep {
	return lockStep{waits: "getpasswordcredentialforupdate", sql: `SELECT 1 FROM password_credentials WHERE user_id = $1 FOR UPDATE`, key: accountUserID}
}

func connectionLock() lockStep {
	return lockStep{waits: "getgithubconnectionforupdate", sql: `SELECT 1 FROM github_connections WHERE user_id = $1 FOR UPDATE`, key: accountUserID}
}

func advanceClock(d time.Duration) func(*callerRun, pgx.Tx) error {
	return func(c *callerRun, _ pgx.Tx) error {
		c.ih.clock.advance(d)
		return nil
	}
}

func expireHeldSession(key func(c *callerRun) any) func(*callerRun, pgx.Tx) error {
	return func(c *callerRun, holder pgx.Tx) error {
		_, err := holder.Exec(context.Background(), `UPDATE sessions SET absolute_expires_at = $2 WHERE id = $1`, key(c), time.Now())
		return err
	}
}

func documentForDeletion() schema.Resume {
	name := "Ada"
	return schema.Resume{
		SchemaVersion:   int64(schema.CurrentVersion),
		PersonalDetails: schema.PersonalDetails{FullName: &name},
		Content:         map[string]schema.Section{},
		Customization: schema.Customization{
			Font:           schema.Font{Family: schema.Inter, BaseSizePx: 14},
			Colors:         schema.Colors{Primary: "#1a1a1a", Text: "#1a1a1a", Background: "#ffffff"},
			Spacing:        schema.Spacing{SectionGap: 16, EntryGap: 8, LineHeight: 1.4},
			Heading:        schema.Heading{Style: schema.Normal},
			Layout:         schema.Layout{Columns: 1, Sections: schema.Sections{Main: []string{}, Sidebar: []string{}}},
			SectionDisplay: schema.SectionDisplay{Skill: schema.SkillClass{Style: schema.Text}, Language: schema.LanguageClass{Style: schema.Text}},
			PageFormat:     schema.A4, DateFormat: schema.MmYyyy,
		},
	}
}

const newPassword = "a long enough brand new password"

func callerCases() []callerCase {
	var cases []callerCase
	for _, group := range [][]callerCase{signOutCases(), deletionCases(), evidenceCases(), factorCases()} {
		cases = append(cases, group...)
	}
	return cases
}

func signOutCases() []callerCase {
	expire := func(c *callerRun) error { return c.expireSession() }
	return []callerCase{
		{name: "logout", site: invariantSite{"internal/auth/sessions_handlers.go", "Service.handleLogout"}, run: func(c *callerRun) error {
			return statusAccepted(c.serve(http.MethodPost, auth.LogoutPath, ""), http.StatusNoContent)
		}, invalidate: func(c *callerRun) error { return c.revokeSession() },
			lastLocks: []lockStep{sessionLock("getsessionbyidforupdate", callerSessionID)}, lapse: expireHeldSession(callerSessionID)},
		{name: "revoke one", site: invariantSite{"internal/auth/sessions_revoke_http.go", "Service.handleRevokeSession"}, setup: func(c *callerRun) error {
			_, other, err := c.ih.sessions.Issue(context.Background(), c.ih.user.ID, "evidence-test", "203.0.113.9")
			c.other = other
			return err
		}, run: func(c *callerRun) error {
			return statusAccepted(c.serve(http.MethodDelete, auth.SessionsPath+"/"+c.other.ID.String(), ""), http.StatusNoContent)
		}, invalidate: expire,
			lastLocks: []lockStep{sessionLock("getsessionforuserforupdate", otherSessionID)}, lapse: expireHeldSession(otherSessionID)},
		{name: "revoke all", site: invariantSite{"internal/auth/sessions_revoke_http.go", "Service.handleRevokeAllSessions"}, run: func(c *callerRun) error {
			return statusAccepted(c.serve(http.MethodDelete, auth.SessionsPath, ""), http.StatusNoContent)
		}, invalidate: expire,
			lastLocks: []lockStep{sessionLock("getsessionbyidforupdate", callerSessionID)}, lapse: expireHeldSession(callerSessionID)},
		{name: "password change", site: invariantSite{"internal/auth/password_account.go", "PasswordService.change"}, setup: func(c *callerRun) error {
			if err := c.setPassword(); err != nil {
				return err
			}
			return c.enableSecondFactor(0)
		}, run: func(c *callerRun) error {
			return statusAccepted(c.serve(http.MethodPut, auth.PasswordMePath, `{"password":"`+newPassword+`"}`), http.StatusNoContent)
		}, invalidate: expire, lastLocks: []lockStep{policyLock(), passwordCredentialLock()}, lapse: advanceClock(proofLapse)},
		{name: "password reset", site: invariantSite{"internal/auth/password_reset.go", "PasswordService.reset"}, setup: func(c *callerRun) error {
			if err := c.setPassword(); err != nil {
				return err
			}
			token, err := password.NewToken(rand.Reader)
			if err != nil {
				return err
			}
			created := time.Now().Truncate(time.Microsecond)
			row, err := c.ih.q.CreatePasswordResetToken(context.Background(), store.CreatePasswordResetTokenParams{
				UserID: c.ih.user.ID, TokenDigest: token.Digest[:], CreatedAt: created, ExpiresAt: created.Add(30 * time.Minute),
			})
			c.resetToken, c.resetID = token, row.ID
			return err
		}, run: func(c *callerRun) error {
			raw := c.raw
			c.raw = ""
			defer func() { c.raw = raw }()
			return statusAccepted(c.serve(http.MethodPost, auth.PasswordResetPath, `{"token":"`+c.resetToken.Raw+`","password":"`+newPassword+`"}`), http.StatusNoContent)
		}, invalidate: func(c *callerRun) error {
			return expireResetToken(c.ih.pool, c.resetID)
		}, lastLocks: []lockStep{{waits: "getpasswordresettokenforupdate", sql: `SELECT 1 FROM password_reset_tokens WHERE id = $1 FOR UPDATE`, key: func(c *callerRun) any { return c.resetID }}},
			lapse: func(c *callerRun, holder pgx.Tx) error { return expireResetToken(holder, c.resetID) }},
	}
}

type execer interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

func expireResetToken(db execer, id uuid.UUID) error {
	_, err := db.Exec(context.Background(), `UPDATE password_reset_tokens SET created_at = $2::timestamptz - interval '30 minutes', expires_at = $2::timestamptz WHERE id = $1`, id, time.Now())
	return err
}

func deletionCases() []callerCase {
	expire := func(c *callerRun) error { return c.expireSession() }
	deletion := invariantSite{"internal/accountapi/delete_transaction.go", "Service.mutateDeletion"}
	return []callerCase{
		{name: "self deletion", site: deletion, setup: func(c *callerRun) error { return c.enableSecondFactor(0) }, run: func(c *callerRun) error {
			r := httptest.NewRequestWithContext(context.Background(), http.MethodDelete, "https://evidence.example/api/v1/me", nil)
			r.RemoteAddr = "203.0.113.7:4000"
			r.Header.Set("Origin", testOrigin)
			r.Header.Set(auth.CSRFHeaderName, csrfFor(c.session))
			r.AddCookie(sessionCookie(c.raw))
			rec := httptest.NewRecorder()
			c.ih.accounts.DeleteHandler().ServeHTTP(rec, r)
			return statusAccepted(rec, http.StatusNoContent)
		}, invalidate: expire, lastLocks: []lockStep{policyLock()}, lapse: advanceClock(proofLapse)},
		{name: "operator deletion", site: deletion, setup: func(c *callerRun) error {
			created, err := resume.NewStore(c.ih.pool, docmigrate.NewIdentityProjector()).Create(context.Background(), c.ih.user.ID, "Operator", documentForDeletion())
			if err != nil {
				return err
			}
			c.slug = "operator-" + uuid.NewString()[:8]
			_, err = c.ih.pool.Exec(context.Background(), `UPDATE resumes SET slug = $2 WHERE id = $1`, created.ID, c.slug)
			return err
		}, run: func(c *callerRun) error {
			_, err := c.ih.accounts.DeleteAccountBySlug(context.Background(), c.slug, c.ih.user.ID)
			return err
		}, invalidate: func(c *callerRun) error {
			_, err := c.ih.pool.Exec(context.Background(), `UPDATE resumes SET slug = $2 WHERE slug = $1`, c.slug, c.slug+"-moved")
			return err
		}, lastLocks: []lockStep{{waits: "listaccountdeletionresumesetforupdate", sql: `SELECT 1 FROM resumes WHERE user_id = $1 FOR UPDATE`, key: accountUserID}},
			lapse: func(c *callerRun, holder pgx.Tx) error {
				_, err := holder.Exec(context.Background(), `UPDATE resumes SET slug = slug || '-moved', revision = revision + 1 WHERE user_id = $1`, c.ih.user.ID)
				return err
			}},
	}
}

func evidenceCases() []callerCase {
	expire := func(c *callerRun) error { return c.expireSession() }
	routes := "internal/githubevidence/routes.go"
	return []callerCase{
		{name: "evidence status", site: invariantSite{routes, "Service.handleStatus"}, stale: func(c *callerRun) error {
			_, err := c.ih.pool.Exec(context.Background(), `UPDATE github_connections SET generation = generation + 1 WHERE user_id = $1`, c.ih.user.ID)
			return err
		}, run: func(c *callerRun) error {
			return statusAccepted(c.serve(http.MethodGet, StatusPath, ""), http.StatusOK)
		}, invalidate: expire, lastLocks: []lockStep{connectionLock()}, lapse: advanceClock(idleLapse)},
		{name: "evidence done", site: invariantSite{routes, "Service.handleSessionEnd"}, run: func(c *callerRun) error {
			return statusAccepted(c.serve(http.MethodPost, SessionEndPath, ""), http.StatusNoContent)
		}, invalidate: expire, lastLocks: []lockStep{connectionLock()}, lapse: advanceClock(idleLapse)},
		{name: "evidence disconnect", site: invariantSite{"internal/githubevidence/disconnect.go", "Service.authorizeDisconnect"}, run: func(c *callerRun) error {
			key := http.Header{"Idempotency-Key": {uuid.NewString()}}
			return statusAccepted(c.serveWith(http.MethodDelete, StatusPath+disconnectQuery(true, 1), "", key), http.StatusOK)
		}, invalidate: expire, lastLocks: []lockStep{connectionLock()}, lapse: advanceClock(idleLapse)},
		{name: "evidence reader", site: invariantSite{"internal/githubevidence/evidence_session.go", "Service.openReader"}, setup: func(c *callerRun) error {
			created, err := project.NewStore().CreateTx(context.Background(), c.ih.q, c.ih.user.ID, "reader",
				project.Document{DefaultLanguage: "en", Text: map[string]project.Text{"en": {Name: "Reader"}}}, time.Now(), nil)
			c.project = created.ID
			return err
		}, stale: func(c *callerRun) error {
			_, err := c.ih.pool.Exec(context.Background(), `UPDATE github_connections SET generation = generation + 1 WHERE user_id = $1`, c.ih.user.ID)
			return err
		}, run: func(c *callerRun) error {
			_, err := c.ih.svc.Candidates(context.Background(), c.session, c.project, project.EvidenceQuery{Kind: kindRepository, Page: 1})
			if errors.Is(err, project.ErrEvidenceSessionRequired) {
				return nil
			}
			if err == nil {
				return errors.New("listed candidates under a stale generation")
			}
			return err
		}, invalidate: expire, lastLocks: []lockStep{connectionLock()}, lapse: advanceClock(idleLapse)},
		{name: "evidence callback", site: invariantSite{"internal/githubevidence/callback.go", "Service.handleCallback"}, setup: func(c *callerRun) error {
			s, err := c.ih.startE(c.raw, c.session)
			if err != nil {
				return err
			}
			c.pending, c.code = s, "code-"+uuid.NewString()
			c.ih.gh.expect(c.code, s.challenge)
			return nil
		}, run: func(c *callerRun) error {
			rec := c.ih.callback(c.pending, "code="+c.code+"&state="+url.QueryEscape(c.pending.state), c.raw)
			if rec.Header().Get("Location") != testOrigin+"/app/settings/sessions?evidence="+outcomeConnected {
				return fmt.Errorf("callback Location %q", rec.Header().Get("Location"))
			}
			return nil
		}, invalidate: expire, lastLocks: []lockStep{connectionLock()}, lapse: advanceClock(idleLapse)},
	}
}

func (c *callerRun) setPassword() error {
	encoded, err := c.ih.hasher.Hash(context.Background(), "the original long password")
	if err != nil {
		return err
	}
	_, err = c.ih.q.UpsertPasswordCredential(context.Background(), store.UpsertPasswordCredentialParams{
		UserID: c.ih.user.ID, EncodedHash: []byte(encoded), CreatedAt: time.Now(), ChangedAt: time.Now(),
	})
	return err
}

func prepareCaller(t *testing.T, tc callerCase) *callerRun {
	t.Helper()
	ih := newInvariantHarness(t)
	requireOutcome(t, ih.connect(ih.raw, ih.session), outcomeConnected)
	raw, sess, err := ih.sessions.Issue(t.Context(), ih.user.ID, "evidence-caller", "203.0.113.8")
	if err != nil {
		t.Fatal(err)
	}
	c := &callerRun{ih: ih, ownerToken: ih.liveToken(), raw: raw, session: sess}
	if c.ownerToken == "" {
		t.Fatal("no evidence session before the caller runs")
	}
	if tc.setup != nil {
		if err = tc.setup(c); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	ih.drain()
	ih.trace.reset()
	return c
}

func (ih *invHarness) beginHolder(t *testing.T) (pgx.Tx, int32) {
	t.Helper()
	holder, err := ih.pool.Begin(t.Context())
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
	return holder, pid
}

func (ih *invHarness) holdAccount(t *testing.T) (pgx.Tx, int32) {
	t.Helper()
	holder, pid := ih.beginHolder(t)
	if _, err := store.New(ih.pool).WithTx(holder).GetUserForUpdate(t.Context(), ih.user.ID); err != nil {
		t.Fatal(err)
	}
	return holder, pid
}

func (ih *invHarness) waitBlockedOn(pid int32, waits string, done <-chan struct{}) error {
	want := "-- name: " + waits + " "
	deadline := time.Now().Add(raceWait)
	for time.Now().Before(deadline) {
		select {
		case <-done:
			return errors.New("it finished without waiting")
		default:
		}
		rows, err := ih.pool.Query(context.Background(), `SELECT query FROM pg_stat_activity WHERE $1::integer = ANY(pg_blocking_pids(pid))`, pid)
		if err != nil {
			return err
		}
		queries, err := pgx.CollectRows(rows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, query := range queries {
			query = strings.ToLower(strings.TrimSpace(query))
			if !strings.HasPrefix(query, want) {
				return fmt.Errorf("it waited in %q", firstLine(query))
			}
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return fmt.Errorf("nothing waited on backend %d within %s", pid, raceWait)
}

func runStaleUnderTheLock(t *testing.T, tc callerCase) (*callerRun, error) {
	t.Helper()
	c := prepareCaller(t, tc)
	holder, pid := c.ih.holdAccount(t)
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		runErr = tc.run(c)
	}()
	if !c.ih.waitBlockedBy(pid, done) {
		<-done
		t.Fatalf("%s did not wait for the account lock: %v", tc.name, runErr)
	}
	if err := tc.invalidate(c); err != nil {
		t.Fatal(err)
	}
	if err := holder.Rollback(t.Context()); err != nil {
		t.Fatal(err)
	}
	<-done
	return c, runErr
}

func runLapsedAtTheLastLock(t *testing.T, tc callerCase) (*callerRun, error) {
	t.Helper()
	c := prepareCaller(t, tc)
	holders := make([]pgx.Tx, len(tc.lastLocks))
	pids := make([]int32, len(tc.lastLocks))
	for i, step := range tc.lastLocks {
		holders[i], pids[i] = c.ih.beginHolder(t)
		if _, err := holders[i].Exec(t.Context(), step.sql, step.key(c)); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan struct{})
	var runErr error
	go func() {
		defer close(done)
		runErr = tc.run(c)
	}()
	abort := func(format string, args ...any) {
		for _, holder := range holders {
			if err := holder.Rollback(context.Background()); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Errorf("rollback holder: %v", err)
			}
		}
		<-done
		t.Fatalf(format, args...)
	}
	last := len(tc.lastLocks) - 1
	for i, step := range tc.lastLocks {
		if err := c.ih.waitBlockedOn(pids[i], step.waits, done); err != nil {
			abort("%s did not wait for %s: %v (run: %v)", tc.name, step.waits, err, runErr)
		}
		if i == last {
			if err := tc.lapse(c, holders[i]); err != nil {
				abort("lapse: %v", err)
			}
		}
		if err := holders[i].Commit(t.Context()); err != nil {
			abort("release %s: %v", step.waits, err)
		}
	}
	<-done
	return c, runErr
}

func requireNothingChanged(t *testing.T, tc callerCase, c *callerRun) {
	t.Helper()
	for _, statement := range tc.untouched {
		if c.ih.trace.executed(statement) {
			t.Errorf("%s ran %q before rejecting stale authority", tc.name, statement)
		}
	}
	c.ih.drain()
	if token := c.ih.liveToken(); token != c.ownerToken {
		t.Errorf("%s with stale authority changed the evidence session", tc.name)
	}
	if _, revoked, _, _ := c.ih.gh.snapshot(); len(revoked) != 0 && tc.name != "evidence callback" {
		t.Errorf("%s with stale authority revoked %d tokens", tc.name, len(revoked))
	}
}

func TestInvariantI1_AuthorityRevalidatedAfterTheLocks(t *testing.T) {
	for _, tc := range callerCases() {
		t.Run(tc.name, func(t *testing.T) {
			c, err := runStaleUnderTheLock(t, tc)
			if err == nil {
				t.Errorf("%s accepted authority that went stale while it waited for the account lock", tc.name)
			}
			requireNothingChanged(t, tc, c)
		})
	}
}

func TestInvariantI1_ClockReadAfterTheLastLock(t *testing.T) {
	for _, tc := range callerCases() {
		t.Run(tc.name, func(t *testing.T) {
			c, err := runLapsedAtTheLastLock(t, tc)
			if err == nil {
				t.Errorf("%s accepted a proof that lapsed while it waited for %s", tc.name, tc.lastLocks[len(tc.lastLocks)-1].waits)
			}
			if n := c.ih.trace.marks(); n != 0 {
				t.Errorf("%s notified %d times after its proof lapsed", tc.name, n)
			}
			requireNothingChanged(t, tc, c)
		})
	}
}

func TestInvariantI2_NotifiedLastAfterAuthorizationAndSessionChanges(t *testing.T) {
	for _, tc := range callerCases() {
		t.Run(tc.name, func(t *testing.T) {
			c := prepareCaller(t, tc)
			if tc.stale != nil {
				if err := tc.stale(c); err != nil {
					t.Fatal(err)
				}
			}
			if err := tc.run(c); err != nil {
				t.Fatalf("%s rejected a valid caller: %v", tc.name, err)
			}
			if n := c.ih.trace.marks(); n != 1 {
				t.Errorf("%s notified %d times, want 1", tc.name, n)
			}
			for _, violation := range c.ih.trace.violations() {
				t.Errorf("%s: %s", tc.name, violation)
			}
		})
	}
}

func TestInvariantI3_NoRollbackAfterANotification(t *testing.T) {
	for _, tc := range callerCases() {
		t.Run(tc.name, func(t *testing.T) {
			c, err := runStaleUnderTheLock(t, tc)
			if err == nil {
				t.Skipf("%s accepted stale authority; I1 reports it", tc.name)
			}
			if n := c.ih.trace.marks(); n != 0 {
				t.Errorf("%s notified %d times in a transaction that was then rejected", tc.name, n)
			}
			for _, violation := range c.ih.trace.violations() {
				t.Errorf("%s: %s", tc.name, violation)
			}
		})
	}
}

func TestInvariant_LogoutRevokesLineagePartnersBeforeNotifying(t *testing.T) {
	ih := newInvariantHarness(t)
	requireOutcome(t, ih.connect(ih.raw, ih.session), outcomeConnected)
	predecessorRaw, predecessor, err := ih.sessions.Issue(t.Context(), ih.user.ID, "evidence-predecessor", "203.0.113.8")
	if err != nil {
		t.Fatal(err)
	}
	successorRaw, successor, err := ih.sessions.Issue(t.Context(), ih.user.ID, "evidence-successor", "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ih.pool.Exec(t.Context(), `UPDATE sessions SET rotated_from = $2 WHERE id = $1`, successor.ID, predecessor.ID); err != nil {
		t.Fatal(err)
	}
	pending := ih.start(successorRaw, successor)
	code := "code-" + uuid.NewString()
	ih.gh.expect(code, pending.challenge)
	ih.drain()
	ih.trace.reset()
	var partnerOutcome string
	ih.trace.mu.Lock()
	ih.trace.afterCommit = func() {
		rec := ih.callback(pending, "code="+code+"&state="+url.QueryEscape(pending.state), successorRaw)
		partnerOutcome = rec.Header().Get("Location")
	}
	ih.trace.mu.Unlock()
	r := httptest.NewRequestWithContext(context.Background(), http.MethodPost, auth.LogoutPath, nil)
	r.RemoteAddr = "203.0.113.7:4000"
	r.Header.Set("Origin", testOrigin)
	r.Header.Set(auth.CSRFHeaderName, csrfFor(predecessor))
	r.AddCookie(sessionCookie(predecessorRaw))
	rec := httptest.NewRecorder()
	ih.handler.ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout = %d %s", rec.Code, rec.Body.String())
	}
	if partnerOutcome == "" {
		t.Fatal("the partner callback did not run after the logout commit")
	}
	ih.drain()
	if token := ih.liveToken(); token != "" {
		t.Fatalf("after logout the account still holds an evidence session (partner callback: %s)", partnerOutcome)
	}
	tokens, revoked, _, _ := ih.gh.snapshot()
	if len(revoked) != len(tokens) {
		t.Fatalf("issued %d tokens, revoked %d; every token must be revoked after logout", len(tokens), len(revoked))
	}
}
