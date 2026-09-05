package realtimeapi

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/realtime"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

type manualTicker struct {
	c chan time.Time
}

func (t *manualTicker) Chan() <-chan time.Time { return t.c }
func (t *manualTicker) Stop()                  {}

type deadlineWriter struct {
	header       http.Header
	deadline     chan time.Time
	writeStarted chan struct{}
	unblock      chan struct{}
	once         sync.Once
}

func newDeadlineWriter() *deadlineWriter {
	return &deadlineWriter{
		header:       make(http.Header),
		deadline:     make(chan time.Time, 4),
		writeStarted: make(chan struct{}),
		unblock:      make(chan struct{}),
	}
}

func (w *deadlineWriter) Header() http.Header { return w.header }
func (w *deadlineWriter) WriteHeader(int)     {}
func (w *deadlineWriter) Flush()              {}
func (w *deadlineWriter) SetWriteDeadline(deadline time.Time) error {
	w.deadline <- deadline
	if deadline.IsZero() {
		w.once.Do(func() { close(w.unblock) })
	}
	return nil
}
func (w *deadlineWriter) Write(body []byte) (int, error) {
	close(w.writeStarted)
	<-w.unblock
	return len(body), nil
}

var (
	testOwner   = uuid.MustParse("11111111-1111-4111-8111-111111111111")
	testResume  = uuid.MustParse("22222222-2222-4222-8222-222222222222")
	testSession = uuid.MustParse("33333333-3333-4333-8333-333333333333")
)

type streamStore struct {
	mu          sync.Mutex
	publicErr   error
	sessionErr  error
	publicCalls int
	row         store.GetPublicRealtimeResumeRow
	session     store.Session
}

func (s *streamStore) GetPublicRealtimeResume(_ context.Context, slug string) (store.GetPublicRealtimeResumeRow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.publicCalls++
	if slug != "test-resume" {
		return store.GetPublicRealtimeResumeRow{}, pgx.ErrNoRows
	}
	return s.row, s.publicErr
}

func (s *streamStore) GetSessionByID(_ context.Context, id uuid.UUID) (store.Session, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if id != testSession {
		return store.Session{}, pgx.ErrNoRows
	}
	return s.session, s.sessionErr
}

func streamFixture(t *testing.T) (*Service, *realtime.Hub, *streamStore, *publicstate.Coordinator) {
	t.Helper()
	hub, err := realtime.NewHub(realtime.Config{AdmitFD: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	hub.SetAvailable(true)
	t.Cleanup(hub.Close)
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)
	data := &streamStore{
		row:     store.GetPublicRealtimeResumeRow{ID: testResume, Revision: 1},
		session: store.Session{ID: testSession, UserID: testOwner, CreatedAt: now, LastSeenAt: now, AbsoluteExpiresAt: now.Add(time.Hour)},
	}
	service, err := New(Dependencies{Hub: hub, Store: data, Sessions: auth.NewSessionManager(nil), Coordinator: coordinator, Clock: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	return service, hub, data, coordinator
}

func openStream(t *testing.T, handler http.Handler, path string) (*http.Response, *bufio.Reader) {
	return openStreamWithCookies(t, handler, path, &http.Cookie{Name: "__Host-session", Value: "invalid-ignored-cookie"})
}

func openStreamWithCookies(t *testing.T, handler http.Handler, path string, cookies ...*http.Cookie) (*http.Response, *bufio.Reader) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 3 * time.Second
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	request.Header.Set("Last-Event-ID", "999999999999999999")
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if closeErr := response.Body.Close(); closeErr != nil {
			t.Errorf("close stream response: %v", closeErr)
		}
	})
	if response.StatusCode != http.StatusOK {
		t.Fatalf("stream status %d", response.StatusCode)
	}
	if got := response.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("Content-Type %q", got)
	}
	if got := response.Header.Get("Cache-Control"); got != "no-store, no-transform" {
		t.Fatalf("cache policy %q", got)
	}
	reader := bufio.NewReader(response.Body)
	if frame := readFrame(t, reader); frame != "event: heartbeat\ndata: {\"version\":1}\n\n" {
		t.Fatalf("first flushed frame = %q", frame)
	}
	return response, reader
}

func readFrame(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	var result strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		result.WriteString(line)
		if line == "\n" {
			return result.String()
		}
	}
}

func TestPublicStreamOmitsOwnerMetadataAndClosesBeforeRevocationCompletes(t *testing.T) {
	service, hub, _, coordinator := streamFixture(t)
	response, reader := openStream(t, service.PublicHandler(), "/api/v1/live/test-resume")
	hub.Publish(realtime.Change{AccountID: testOwner, ResumeID: testResume, Revision: 2})
	frame := readFrame(t, reader)
	if frame != "event: revision\nid: 2\ndata: {\"version\":1,\"revision\":\"2\"}\n\n" {
		t.Fatalf("public frame = %q", frame)
	}
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: testResume, ExpectedRevision: 1, Class: publicstate.Revoking}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := transition.Close(ctx, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if rollbackErr := transition.Rollback(); rollbackErr != nil {
			t.Errorf("rollback transition: %v", rollbackErr)
		}
	})
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("stream remained open after completed drain: %v", err)
	}
	blocked := httptest.NewRecorder()
	service.PublicHandler().ServeHTTP(blocked, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/live/test-resume", nil))
	if blocked.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed public fence status = %d, want %d", blocked.Code, http.StatusServiceUnavailable)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestOwnerStreamRechecksSessionAndKeepsInt64RevisionExact(t *testing.T) {
	service, hub, data, _ := streamFixture(t)
	ownerHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		service.owner(w, r.WithContext(auth.ContextWithSession(r.Context(), data.session)))
	})
	response, reader := openStream(t, ownerHandler, "/api/v1/events")
	defer func() {
		if err := response.Body.Close(); err != nil {
			t.Errorf("close owner stream: %v", err)
		}
	}()
	hub.Publish(realtime.Change{AccountID: testOwner, ResumeID: testResume, Revision: 9007199254740993})
	frame := readFrame(t, reader)
	want := "event: revision\nid: 22222222-2222-4222-8222-222222222222:9007199254740993\ndata: {\"version\":1,\"resume_id\":\"22222222-2222-4222-8222-222222222222\",\"revision\":\"9007199254740993\",\"deleted\":false}\n\n"
	if frame != want {
		t.Fatalf("owner frame = %q", frame)
	}
	data.mu.Lock()
	data.sessionErr = auth.ErrSessionInvalid
	data.mu.Unlock()
	hub.Publish(realtime.Change{AccountID: testOwner, ResumeID: testResume, Revision: 9007199254740994})
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("revoked session received bytes or remained open: %v", err)
	}
}

func TestOwnerStreamRejectsExpiredSessionBeforeFlushingHeaders(t *testing.T) {
	service, _, data, _ := streamFixture(t)
	data.mu.Lock()
	data.session.AbsoluteExpiresAt = service.dependencies.Clock().Add(-time.Second)
	expired := data.session
	data.mu.Unlock()

	response := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/events", nil)
	service.owner(response, request.WithContext(auth.ContextWithSession(request.Context(), expired)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status = %d, want %d", response.Code, http.StatusUnauthorized)
	}
	if strings.Contains(response.Body.String(), "event:") {
		t.Fatalf("expired session received SSE bytes: %q", response.Body.String())
	}
}

func TestPublicStreamRejectsHostilePathsAndMissingLiveState(t *testing.T) {
	service, hub, data, _ := streamFixture(t)
	for _, path := range []string{"/api/v1/live/test-resume/extra", "/api/v1/live/%74est-resume", "/api/v1/live/app", "/api/v1/live/missing-resume", "/api/v1/live/a"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			service.PublicHandler().ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
			if w.Code != http.StatusNotFound {
				t.Fatalf("status = %d", w.Code)
			}
		})
	}
	for _, state := range []struct {
		err    error
		status int
	}{{pgx.ErrNoRows, 404}, {errors.New("database unavailable"), 503}} {
		data.publicErr = state.err
		w := httptest.NewRecorder()
		service.PublicHandler().ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/live/test-resume", nil))
		if w.Code != state.status {
			t.Fatalf("lookup %v status = %d", state.err, w.Code)
		}
	}
	data.publicErr = nil
	hub.SetAvailable(false)
	w := httptest.NewRecorder()
	service.PublicHandler().ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/live/test-resume", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("listener-down status = %d", w.Code)
	}
}

func TestRoutesRejectUnsupportedMethodsAndRequireOwnerSession(t *testing.T) {
	service, _, _, _ := streamFixture(t)
	mux := http.NewServeMux()
	service.RegisterRoutes(mux)
	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodOptions} {
		w := httptest.NewRecorder()
		service.PublicHandler().ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), method, "/api/v1/live/test-resume", nil))
		if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET" {
			t.Fatalf("%s status = %d", method, w.Code)
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/events", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("missing owner session status = %d", w.Code)
	}
}

func TestPublicRouteLimitsRequestChurnBeforeLiveStateLookup(t *testing.T) {
	service, _, data, _ := streamFixture(t)
	handler := service.PublicHandler()
	for requestNumber := 1; requestNumber <= api.DefaultRateLimitRequests+1; requestNumber++ {
		response := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/live/missing-resume", nil)
		handler.ServeHTTP(response, request)
		wantStatus := http.StatusNotFound
		if requestNumber > api.DefaultRateLimitRequests {
			wantStatus = http.StatusTooManyRequests
		}
		if response.Code != wantStatus {
			t.Fatalf("request %d status = %d, want %d", requestNumber, response.Code, wantStatus)
		}
		if got := response.Header().Get("Cache-Control"); got != "no-store, no-transform" {
			t.Fatalf("request %d cache policy = %q", requestNumber, got)
		}
	}
	data.mu.Lock()
	defer data.mu.Unlock()
	if data.publicCalls != api.DefaultRateLimitRequests {
		t.Fatalf("live-state lookups = %d, want %d", data.publicCalls, api.DefaultRateLimitRequests)
	}
}

func TestPublicRouteMapsHubCapacityToTooManyRequests(t *testing.T) {
	service, _, data, coordinator := streamFixture(t)
	limitedHub, err := realtime.NewHub(realtime.Config{MaxConnections: 1, AdmitFD: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	limitedHub.SetAvailable(true)
	t.Cleanup(limitedHub.Close)
	occupied, err := limitedHub.Subscribe(realtime.Scope{ResumeID: uuid.New(), IP: "192.0.2.10"})
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	service, err = New(Dependencies{Hub: limitedHub, Store: data, Sessions: service.dependencies.Sessions, Coordinator: coordinator, Clock: service.dependencies.Clock})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	service.PublicHandler().ServeHTTP(response, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/live/test-resume", nil))
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("capacity response = %d Retry-After %q", response.Code, response.Header().Get("Retry-After"))
	}
}

func TestStreamHeartbeatUsesConfiguredInterval(t *testing.T) {
	service, _, _, _ := streamFixture(t)
	ticker := &manualTicker{c: make(chan time.Time, 1)}
	service.newTicker = func(interval time.Duration) streamTicker {
		if interval != heartbeatInterval {
			t.Fatalf("ticker interval = %s, want %s", interval, heartbeatInterval)
		}
		return ticker
	}

	response, reader := openStream(t, service.PublicHandler(), "/api/v1/live/test-resume")
	ticker.c <- time.Now()
	if frame := readFrame(t, reader); frame != "event: heartbeat\ndata: {\"version\":1}\n\n" {
		t.Fatalf("periodic heartbeat = %q", frame)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStreamSetsDeadlineBeforeAFlushAndStopsAfterCancellation(t *testing.T) {
	service, hub, _, _ := streamFixture(t)
	subscription, err := hub.Subscribe(realtime.Scope{ResumeID: testResume, IP: "127.0.0.1"})
	if err != nil {
		t.Fatal(err)
	}
	defer subscription.Close()

	requestContext, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequestWithContext(requestContext, http.MethodGet, "/api/v1/live/test-resume", nil)
	writer := newDeadlineWriter()
	done := make(chan struct{})
	go func() {
		defer close(done)
		service.stream(requestContext, writer, request, subscription, nil, false)
	}()

	select {
	case <-writer.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("initial heartbeat write did not start")
	}
	deadline := <-writer.deadline
	if remaining := time.Until(deadline); remaining <= 0 || remaining > writeTimeout {
		t.Fatalf("write deadline is %s from now, want (0,%s]", remaining, writeTimeout)
	}
	cancel()
	select {
	case <-done:
		t.Fatal("stream returned while the writer was still blocked")
	case <-time.After(20 * time.Millisecond):
	}
	writer.once.Do(func() { close(writer.unblock) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("stream did not return after blocked write completed")
	}
	if cleared := <-writer.deadline; !cleared.IsZero() {
		t.Fatalf("write deadline after successful flush = %s, want zero", cleared)
	}
}

func TestRealtimeRoutesDeliverCommittedDatabaseChangesThroughRealSessionMiddleware(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, poolErr := store.NewPool(ctx, testutil.RequireMigratedTestDatabaseURL(t))
	if poolErr != nil {
		t.Fatal(poolErr)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	queries := store.New(pool)

	var ownerID, otherID, ownerResumeID, otherResumeID uuid.UUID
	for _, fixture := range []struct {
		name     string
		owner    *uuid.UUID
		resumeID *uuid.UUID
	}{
		{name: "owner", owner: &ownerID, resumeID: &ownerResumeID},
		{name: "other", owner: &otherID, resumeID: &otherResumeID},
	} {
		if queryErr := pool.QueryRow(ctx, `INSERT INTO users (email, name) VALUES ($1, $2) RETURNING id`, uuid.NewString()+"@example.com", fixture.name).Scan(fixture.owner); queryErr != nil {
			t.Fatal(queryErr)
		}
		if queryErr := pool.QueryRow(ctx, `
			INSERT INTO resumes (user_id, title, schema_version, personal_details, content, customization)
			VALUES ($1, $2, 2, '{}'::jsonb, '{}'::jsonb, '{}'::jsonb) RETURNING id
		`, *fixture.owner, fixture.name).Scan(fixture.resumeID); queryErr != nil {
			t.Fatal(queryErr)
		}
	}
	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(context.Background(), `DELETE FROM users WHERE id = ANY($1::uuid[])`, []uuid.UUID{ownerID, otherID}); cleanupErr != nil {
			t.Errorf("delete realtime fixtures: %v", cleanupErr)
		}
	})

	sessionManager := auth.NewSessionManagerWithPool(pool)
	rawSession, issuedSession, issueErr := sessionManager.Issue(ctx, ownerID, "realtime-test", "127.0.0.1")
	if issueErr != nil {
		t.Fatal(issueErr)
	}
	hub, hubErr := realtime.NewHub(realtime.Config{AdmitFD: func() bool { return true }})
	if hubErr != nil {
		t.Fatal(hubErr)
	}
	t.Cleanup(hub.Close)
	listenerContext, stopListener := context.WithCancel(context.Background())
	listenerDone := make(chan error, 1)
	go func() { listenerDone <- realtime.RunListener(listenerContext, pool, hub) }()
	t.Cleanup(func() {
		stopListener()
		if listenerErr := <-listenerDone; listenerErr != nil {
			t.Errorf("RunListener() error = %v", listenerErr)
		}
	})
	waitForListener(t, hub)

	coordinator, coordinatorErr := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if coordinatorErr != nil {
		t.Fatal(coordinatorErr)
	}
	service, serviceErr := New(Dependencies{Hub: hub, Store: queries, Sessions: sessionManager, Coordinator: coordinator})
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	mux := http.NewServeMux()
	service.RegisterRoutes(mux)
	cookieRecorder := httptest.NewRecorder()
	auth.SetSessionCookie(cookieRecorder, rawSession)
	sessionCookie := cookieRecorder.Result().Cookies()[0]
	ownerResponse, reader := openStreamWithCookies(t, mux, "/api/v1/events", sessionCookie)
	defer func() {
		if closeErr := ownerResponse.Body.Close(); closeErr != nil {
			t.Errorf("close database owner stream: %v", closeErr)
		}
	}()

	if _, execErr := pool.Exec(ctx, `UPDATE resumes SET title = 'other changed', revision = revision + 1 WHERE id = $1`, otherResumeID); execErr != nil {
		t.Fatal(execErr)
	}
	if _, err := pool.Exec(ctx, `UPDATE resumes SET title = 'owner changed', revision = revision + 1 WHERE id = $1`, ownerResumeID); err != nil {
		t.Fatal(err)
	}
	want := "event: revision\nid: " + ownerResumeID.String() + ":2\ndata: {\"version\":1,\"resume_id\":\"" + ownerResumeID.String() + "\",\"revision\":\"2\",\"deleted\":false}\n\n"
	if frame := readFrame(t, reader); frame != want {
		t.Fatalf("owner frame after cross-account change = %q, want %q", frame, want)
	}

	if _, err := pool.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1`, issuedSession.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE resumes SET title = 'owner changed again', revision = revision + 1 WHERE id = $1`, ownerResumeID); err != nil {
		t.Fatal(err)
	}
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("revoked database session received bytes or remained open: %v", err)
	}

	slug := "realtime-" + uuid.NewString()[:8]
	if _, err := pool.Exec(ctx, `UPDATE resumes SET slug = $2, live = true, revision = revision + 1 WHERE id = $1`, ownerResumeID, slug); err != nil {
		t.Fatal(err)
	}
	publicResponse, publicReader := openStream(t, service.PublicHandler(), "/api/v1/live/"+slug)
	defer func() {
		if closeErr := publicResponse.Body.Close(); closeErr != nil {
			t.Errorf("close database public stream: %v", closeErr)
		}
	}()
	transition, transitionErr := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: ownerResumeID, ExpectedRevision: 4, Class: publicstate.Revoking}}})
	if transitionErr != nil {
		t.Fatal(transitionErr)
	}
	closeContext, stopClose := context.WithTimeout(context.Background(), time.Second)
	defer stopClose()
	if err := transition.Close(closeContext, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := publicReader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("public stream remained open after revocation drain: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE resumes SET live = false, revision = revision + 1 WHERE id = $1`, ownerResumeID); err != nil {
		if rollbackErr := transition.Rollback(); rollbackErr != nil {
			t.Errorf("rollback failed unpublish transition: %v", rollbackErr)
		}
		t.Fatal(err)
	}
	if err := transition.Commit(publicstate.CommittedState{ResumeRevisions: map[uuid.UUID]int64{ownerResumeID: 5}}); err != nil {
		t.Fatal(err)
	}
	if _, err := queries.GetPublicRealtimeResume(ctx, slug); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("public realtime lookup after committed unpublish = %v, want pgx.ErrNoRows", err)
	}
}

func waitForListener(t *testing.T, hub *realtime.Hub) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		subscription, err := hub.Subscribe(realtime.Scope{ResumeID: uuid.New(), IP: "127.0.0.1"})
		if err == nil {
			subscription.Close()
			return
		}
		if !errors.Is(err, realtime.ErrUnavailable) || time.Now().After(deadline) {
			t.Fatalf("listener did not become available: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
