package projectapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
)

const unnamedSlugBody = `{"defaultLanguage":"en","text":{"en":{"name":"Contested work"}}}`

func (h *harness) concurrentCreates(t *testing.T, n int, body string) []*httptest.ResponseRecorder {
	t.Helper()
	out := make([]*httptest.ResponseRecorder, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range out {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			out[i] = h.request(t, "POST", "/api/v1/projects", body, uuid.NewString(), "")
		}()
	}
	close(start)
	wg.Wait()
	return out
}

func (h *harness) projectCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(t.Context(), "SELECT count(*) FROM projects WHERE user_id=$1", h.user.ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestProjects_ConcurrentSlugClaimsCommitOnce(t *testing.T) {
	h := newHarness(t)
	body := `{"slug":"contested","defaultLanguage":"en","text":{"en":{"name":"Contested work"}}}`
	created := 0
	for _, r := range h.concurrentCreates(t, 8, body) {
		if r.Code == 201 {
			created++
			continue
		}
		assertCode(t, r, 409, "project_slug_taken")
	}
	if created != 1 || h.projectCount(t) != 1 {
		t.Fatalf("created=%d stored=%d", created, h.projectCount(t))
	}
}

func TestProjects_ConcurrentProposalsAreDistinct(t *testing.T) {
	h := newHarness(t)
	seen := map[string]bool{}
	for _, r := range h.concurrentCreates(t, 8, unnamedSlugBody) {
		if r.Code != 201 {
			t.Fatalf("status=%d body=%s", r.Code, r.Body.String())
		}
		var e struct {
			Data struct {
				Slug string `json:"slug"`
			} `json:"data"`
		}
		if err := json.Unmarshal(r.Body.Bytes(), &e); err != nil {
			t.Fatal(err)
		}
		if seen[e.Data.Slug] || !strings.HasPrefix(e.Data.Slug, "contested-work") {
			t.Fatalf("slug=%q seen=%v", e.Data.Slug, seen)
		}
		seen[e.Data.Slug] = true
	}
}

func TestProjects_ConcurrentCreatesHoldAccountBound(t *testing.T) {
	h := newHarness(t)
	if _, err := h.pool.Exec(t.Context(), `INSERT INTO projects(user_id,slug,document)
		SELECT $1, 'filled-' || n, '{"defaultLanguage":"en","text":{"en":{"name":"Filled"}}}'
		FROM generate_series(1, 29) AS n`, h.user.ID); err != nil {
		t.Fatal(err)
	}
	created := 0
	for _, r := range h.concurrentCreates(t, 4, unnamedSlugBody) {
		if r.Code == 201 {
			created++
			continue
		}
		assertCode(t, r, 409, "project_limit")
	}
	if created != 1 || h.projectCount(t) != 30 {
		t.Fatalf("created=%d stored=%d", created, h.projectCount(t))
	}
}

func TestProjects_CrossSiteOriginWritesNothingAndKeepsKey(t *testing.T) {
	h := newHarness(t)
	key := uuid.NewString()
	r := httptest.NewRequestWithContext(t.Context(), "POST", "/api/v1/projects", strings.NewReader(projectBody))
	r.RemoteAddr = "127.0.0.1:3000"
	r.AddCookie(h.cookie())
	r.Header.Set("Origin", "https://attacker.example.test")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set(auth.CSRFHeaderName, h.csrf())
	r.Header.Set("Idempotency-Key", key)
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, r)
	assertCode(t, rec, 403, "csrf_rejected")
	if h.projectCount(t) != 0 {
		t.Fatal("cross-site request wrote a project")
	}
	if retry := h.request(t, "POST", "/api/v1/projects", projectBody, key, ""); retry.Code != 201 {
		t.Fatalf("rejected request reserved its key: %d %s", retry.Code, retry.Body.String())
	}
}

func TestProjects_HostileRequestsWriteNothing(t *testing.T) {
	h := newHarness(t)
	path := "/api/v1/projects/" + idOf(t, h.create(t))
	big := `{"defaultLanguage":"en","text":{"en":{"name":"` + strings.Repeat("a", 256*1024) + `"}}}`
	for _, tc := range []struct {
		name, method, path, body, match string
		status                          int
		code                            string
	}{
		{"query on create", "POST", "/api/v1/projects?x=1", unnamedSlugBody, "", 400, "request_invalid"},
		{"duplicate key", "POST", "/api/v1/projects", `{"defaultLanguage":"en","defaultLanguage":"en","text":{"en":{"name":"X"}}}`, "", 400, "request_invalid"},
		{"unknown field", "POST", "/api/v1/projects", `{"id":"` + uuid.NewString() + `","defaultLanguage":"en","text":{"en":{"name":"X"}}}`, "", 400, "request_invalid"},
		{"trailing value", "POST", "/api/v1/projects", unnamedSlugBody + `{}`, "", 400, "request_invalid"},
		{"null slug", "POST", "/api/v1/projects", `{"slug":null,"defaultLanguage":"en","text":{"en":{"name":"X"}}}`, "", 400, "request_invalid"},
		{"oversized body", "POST", "/api/v1/projects", big, "", 413, "body_too_large"},
		{"invalid slug", "POST", "/api/v1/projects", `{"slug":"Bad Slug","defaultLanguage":"en","text":{"en":{"name":"X"}}}`, "", 422, "document_invalid"},
		{"javascript link", "POST", "/api/v1/projects", `{"demoUrl":"javascript:alert(1)","defaultLanguage":"en","text":{"en":{"name":"X"}}}`, "", 422, "document_invalid"},
		{"replace without slug", "PUT", path, unnamedSlugBody, `"r1"`, 422, "document_invalid"},
		{"delete with body", "DELETE", path, `{}`, `"r1"`, 400, "request_invalid"},
		{"malformed id", "PUT", "/api/v1/projects/not-a-uuid", projectBody, `"r1"`, 400, "request_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assertCode(t, h.request(t, tc.method, tc.path, tc.body, uuid.NewString(), tc.match), tc.status, tc.code)
		})
	}
	read := h.request(t, "GET", path, "", "", "")
	if read.Code != 200 || read.Header().Get("ETag") != `"r1"` || h.projectCount(t) != 1 {
		t.Fatalf("hostile requests changed state: %d %s", read.Code, read.Header().Get("ETag"))
	}
	r := httptest.NewRequestWithContext(t.Context(), "GET", path, nil)
	r.AddCookie(h.cookie())
	r.Header.Set("Authorization", "Bearer token")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, r)
	assertCode(t, rec, 400, "request_invalid")
}

func TestProjects_SlugReleaseNeedsRecentReauthentication(t *testing.T) {
	h := newHarness(t)
	path := "/api/v1/projects/" + idOf(t, h.create(t))
	if _, err := h.pool.Exec(t.Context(), "UPDATE sessions SET reauthenticated_at = now() - interval '1 hour' WHERE id=$1", h.session.ID); err != nil {
		t.Fatal(err)
	}
	if r := h.request(t, "PUT", path, projectBody, uuid.NewString(), `"r1"`); r.Code != 200 {
		t.Fatalf("edit without slug release status=%d body=%s", r.Code, r.Body.String())
	}
	renamed := strings.Replace(projectBody, `"example"`, `"renamed"`, 1)
	assertCode(t, h.request(t, "PUT", path, renamed, uuid.NewString(), `"r2"`), 403, "reauth_required")
	assertCode(t, h.request(t, "DELETE", path, "", uuid.NewString(), `"r2"`), 403, "reauth_required")
	var holds int
	if err := h.pool.QueryRow(t.Context(), "SELECT count(*) FROM project_slug_holds WHERE user_id=$1", h.user.ID).Scan(&holds); err != nil || holds != 0 || h.projectCount(t) != 1 {
		t.Fatalf("holds=%d stored=%d error=%v", holds, h.projectCount(t), err)
	}
}
