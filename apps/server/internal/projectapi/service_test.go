package projectapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/api"
	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/resumeapi"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

const origin = "https://project-api.example.test"
const projectBody = `{"slug":"example","defaultLanguage":"en","text":{"en":{"name":"Example","description":"<p onclick=\"bad()\">Work<script>bad()</script></p>"}}}`

type harness struct {
	pool        *store.Pool
	q           *store.Queries
	svc         *Service
	mux         *http.ServeMux
	user        store.User
	session     store.Session
	token       string
	coordinator *publicstate.Coordinator
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	pool, err := store.NewPool(t.Context(), testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	q := store.New(pool)
	user, err := q.CreateUser(t.Context(), store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Project API test"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", user.ID); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	sessions := auth.NewSessionManager(q)
	token, session, err := sessions.Issue(t.Context(), user.ID, "", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	pass := func(h http.Handler) http.Handler { return h }
	public, err := q.GetPublicState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: public.DiscoveryGeneration})
	if err != nil {
		t.Fatal(err)
	}
	projector := docmigrate.NewIdentityProjector()
	idempotency := resume.NewIdempotencyStore(pool)
	resumes := resumeapi.New(resume.NewStore(pool, projector), idempotency, projector, nil, resumeapi.Options{
		SessionManager: sessions, PublicOrigin: origin, Coordinator: coordinator, RecoveryPool: pool,
	})
	svc := New(pool, idempotency, Options{Sessions: sessions, PublicOrigin: origin, ReadLimit: pass, WriteLimit: pass, AllowSlug: func(uuid.UUID, time.Time) bool { return true }, Links: resumes})
	mux := http.NewServeMux()
	svc.RegisterRoutes(mux)
	return &harness{pool: pool, q: q, svc: svc, mux: mux, user: user, session: session, token: token, coordinator: coordinator}
}
func (h *harness) request(t *testing.T, method, path, body, key, match string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:3000"
	r.AddCookie(h.cookie())
	r.Header.Set("Origin", origin)
	r.Header.Set(auth.CSRFHeaderName, h.csrf())
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	if match != "" {
		r.Header.Set("If-Match", match)
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, r)
	return rec
}
func (h *harness) cookie() *http.Cookie { return &http.Cookie{Name: "__Host-session", Value: h.token} }
func (h *harness) csrf() string {
	return base64.RawURLEncoding.EncodeToString(h.session.CSRFSecret)
}
func (h *harness) create(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	r := h.request(t, "POST", "/api/v1/projects", projectBody, uuid.NewString(), "")
	if r.Code != 201 {
		t.Fatalf("create status=%d body=%s", r.Code, r.Body.String())
	}
	return r
}
func idOf(t *testing.T, r *httptest.ResponseRecorder) string {
	t.Helper()
	var e struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &e); err != nil || e.Data.ID == "" {
		t.Fatalf("create output=%s error=%v", r.Body.String(), err)
	}
	return e.Data.ID
}
func assertCode(t *testing.T, r *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	var e struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if r.Code != status || e.Error.Code != code {
		t.Fatalf("status=%d code=%s want %d %s", r.Code, e.Error.Code, status, code)
	}
}

func TestProjects_Ownership(t *testing.T) {
	owner := newHarness(t)
	other := newHarness(t)
	created := owner.create(t)
	id := idOf(t, created)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		body := ""
		key := ""
		match := ""
		if method != "GET" {
			key = uuid.NewString()
			match = `"r1"`
		}
		if method == "PUT" {
			body = projectBody
		}
		foreign := other.request(t, method, "/api/v1/projects/"+id, body, key, match)
		missing := other.request(t, method, "/api/v1/projects/"+uuid.NewString(), body, uuid.NewString(), match)
		assertCode(t, foreign, 404, "project_not_found")
		if foreign.Code != missing.Code || !bytes.Equal(foreign.Body.Bytes(), missing.Body.Bytes()) {
			t.Fatal("foreign and missing differ")
		}
	}
	list := other.request(t, "GET", "/api/v1/projects", "", "", "")
	if list.Code != 200 || strings.Contains(list.Body.String(), id) {
		t.Fatalf("foreign project listed: %s", list.Body.String())
	}
	read := owner.request(t, "GET", "/api/v1/projects/"+id, "", "", "")
	if read.Code != 200 || read.Header().Get("ETag") != `"r1"` || !strings.Contains(read.Body.String(), `"linkedResumeIds":[]`) {
		t.Fatalf("read=%d %s", read.Code, read.Body.String())
	}
	var stored struct {
		Data struct {
			Text map[string]struct {
				Description string `json:"description"`
			} `json:"text"`
		} `json:"data"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &stored); err != nil {
		t.Fatal(err)
	}
	if got := stored.Data.Text["en"].Description; got != "<p>Work</p>" {
		t.Fatalf("description=%q", got)
	}
}

func TestProjects_IdempotentReplayAndPreconditions(t *testing.T) {
	h := newHarness(t)
	key := uuid.NewString()
	first := h.request(t, "POST", "/api/v1/projects", projectBody, key, "")
	if first.Code != 201 {
		t.Fatal(first.Body.String())
	}
	again := h.request(t, "POST", "/api/v1/projects", projectBody, key, "")
	if !bytes.Equal(first.Body.Bytes(), again.Body.Bytes()) || again.Code != 201 {
		t.Fatal("create replay differs")
	}
	path := "/api/v1/projects/" + idOf(t, first)
	assertCode(t, h.request(t, "PUT", path, projectBody, uuid.NewString(), `"r2"`), 412, "revision_mismatch")
	key = uuid.NewString()
	first = h.request(t, "PUT", path, projectBody, key, `"r1"`)
	again = h.request(t, "PUT", path, projectBody, key, `"r1"`)
	if first.Code != 200 || !bytes.Equal(first.Body.Bytes(), again.Body.Bytes()) || again.Header().Get("ETag") != `"r2"` {
		t.Fatalf("replace replay %d %s", first.Code, first.Body.String())
	}
	assertCode(t, h.request(t, "PUT", path, strings.Replace(projectBody, "Example", "Other", 1), key, `"r1"`), 409, "idempotency_key_reuse")
	key = uuid.NewString()
	first = h.request(t, "DELETE", path, "", key, `"r2"`)
	again = h.request(t, "DELETE", path, "", key, `"r2"`)
	if first.Code != 200 || !bytes.Equal(first.Body.Bytes(), again.Body.Bytes()) {
		t.Fatalf("delete replay %d %s", first.Code, first.Body.String())
	}
}

func TestProjects_Admission(t *testing.T) {
	h := newHarness(t)
	id := idOf(t, h.create(t))
	path := "/api/v1/projects/" + id
	assertCode(t, h.request(t, "PUT", path, projectBody, "", `"r1"`), 400, "idempotency_key_required")
	assertCode(t, h.request(t, "PUT", path, projectBody, uuid.NewString(), ""), 428, "precondition_required")
	for _, match := range []string{`W/"r1"`, `"r01"`, `*`, `"r+1"`, `"r1", "r2"`} {
		assertCode(t, h.request(t, "PUT", path, projectBody, uuid.NewString(), match), 400, "precondition_malformed")
	}
	assertCode(t, h.request(t, "POST", "/api/v1/projects", projectBody, uuid.NewString(), `"r1"`), 400, "precondition_not_supported")
	r := httptest.NewRequestWithContext(t.Context(), "GET", path, nil)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, r)
	assertCode(t, rec, 401, "session_required")
	r = httptest.NewRequestWithContext(t.Context(), "PUT", path, strings.NewReader(projectBody))
	r.AddCookie(&http.Cookie{Name: "__Host-session", Value: h.token})
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, r)
	assertCode(t, rec, 403, "csrf_rejected")
	if got := rec.Header().Get("Cache-Control"); got != api.CacheControlNoStore {
		t.Fatalf("cache=%q", got)
	}
}

func TestProjects_SlugLimitOnlyOnClaims(t *testing.T) {
	h := newHarness(t)
	calls := 0
	h.svc.allowSlug = func(uuid.UUID, time.Time) bool { calls++; return calls <= 1 }
	created := h.create(t)
	id := idOf(t, created)
	same := h.request(t, "PUT", "/api/v1/projects/"+id, projectBody, uuid.NewString(), `"r1"`)
	if same.Code != 200 || calls != 1 {
		t.Fatalf("unchanged slug consumed attempt: %d %d", same.Code, calls)
	}
	renamed := strings.Replace(projectBody, `"example"`, `"renamed"`, 1)
	assertCode(t, h.request(t, "PUT", "/api/v1/projects/"+id, renamed, uuid.NewString(), `"r2"`), 429, "rate_limited")
	if calls != 2 {
		t.Fatalf("slug calls=%d", calls)
	}
}

func TestProjects_TransactionReauthRejectsRevokedSession(t *testing.T) {
	h := newHarness(t)
	h.svc.idempotency = &revokingIdempotency{IdempotencyStore: resume.NewIdempotencyStore(h.pool), revoke: func() {
		if err := h.q.RevokeSession(t.Context(), store.RevokeSessionParams{ID: h.session.ID, RevokedAt: timePointer(time.Now())}); err != nil {
			t.Fatal(err)
		}
	}}
	assertCode(t, h.request(t, "POST", "/api/v1/projects", projectBody, uuid.NewString(), ""), 401, "session_required")
	var count int
	if err := h.pool.QueryRow(t.Context(), "SELECT count(*) FROM projects WHERE user_id=$1", h.user.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("persisted count=%d error=%v", count, err)
	}
}
func timePointer(t time.Time) *time.Time { return &t }

type revokingIdempotency struct {
	*resume.IdempotencyStore
	revoke func()
}

func (i *revokingIdempotency) Execute(ctx context.Context, owner uuid.UUID, operation string, key uuid.UUID, fingerprint [32]byte, write func(*store.Queries) (resume.StoredResponse, error)) (resume.ExecuteResult, error) {
	i.revoke()
	return i.IdempotencyStore.Execute(ctx, owner, operation, key, fingerprint, write)
}
