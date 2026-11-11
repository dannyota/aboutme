package app

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"danny.vn/aboutme/apps/server/internal/auth"
	"danny.vn/aboutme/apps/server/internal/config"
	"danny.vn/aboutme/apps/server/internal/store"
	"danny.vn/aboutme/apps/server/internal/testutil"
)

const projectsTestOrigin = "https://projects.example.test"

type sharedAdmissionClient struct {
	mux   *http.ServeMux
	token string
	csrf  string
}

func (c sharedAdmissionClient) send(t *testing.T, method, path, body string) int {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:3000"
	r.AddCookie(&http.Cookie{Name: "__Host-session", Value: c.token})
	if method != http.MethodGet {
		r.Header.Set("Origin", projectsTestOrigin)
		r.Header.Set(auth.CSRFHeaderName, c.csrf)
		r.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	c.mux.ServeHTTP(rec, r)
	return rec.Code
}

// spendUntilLimited sends one request shape until the limiter answers 429
// and requires that earlier requests were admitted.
func (c sharedAdmissionClient) spendUntilLimited(t *testing.T, method, path, body string) {
	t.Helper()
	for attempt := 1; attempt <= 1000; attempt++ {
		if c.send(t, method, path, body) != http.StatusTooManyRequests {
			continue
		}
		if attempt < 100 {
			t.Fatalf("%s %s was limited after %d requests", method, path, attempt)
		}
		return
	}
	t.Fatalf("%s %s was never limited", method, path)
}

func TestNewProjects_SpendsTheResumeServiceBudgets(t *testing.T) {
	pool, err := store.NewPool(t.Context(), testutil.RequireMigratedTestDatabaseURL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	q := store.New(pool)
	user, err := q.CreateUser(t.Context(), store.CreateUserParams{Email: uuid.NewString() + "@example.test", Name: "Shared admission"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, cleanupErr := pool.Exec(context.Background(), "DELETE FROM users WHERE id=$1", user.ID); cleanupErr != nil {
			t.Error(cleanupErr)
		}
	})
	sessions := auth.NewSessionManagerWithPool(pool)
	token, session, err := sessions.Issue(t.Context(), user.ID, "", "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{PublicOrigin: projectsTestOrigin}
	resumes := NewResumes(ResumeDependencies{Config: cfg, Pool: pool, Sessions: sessions, Logger: discardLogger()})
	projects := NewProjects(ProjectDependencies{Config: cfg, Pool: pool, Sessions: sessions, Resumes: resumes})
	mux := http.NewServeMux()
	resumes.RegisterRoutes(mux)
	projects.RegisterRoutes(mux)
	client := sharedAdmissionClient{mux: mux, token: token, csrf: base64.RawURLEncoding.EncodeToString(session.CSRFSecret)}

	for attempt := 1; attempt <= 30; attempt++ {
		if !resumes.AllowSlugClaim(user.ID, time.Now()) {
			t.Fatalf("slug claim %d was denied", attempt)
		}
	}
	created := httptest.NewRecorder()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/projects", strings.NewReader(`{"defaultLanguage":"en","text":{"en":{"name":"Budget"}}}`))
	r.RemoteAddr = "127.0.0.1:3000"
	r.AddCookie(&http.Cookie{Name: "__Host-session", Value: token})
	r.Header.Set("Origin", projectsTestOrigin)
	r.Header.Set(auth.CSRFHeaderName, client.csrf)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Idempotency-Key", uuid.NewString())
	mux.ServeHTTP(created, r)
	if created.Code != http.StatusTooManyRequests || !strings.Contains(created.Body.String(), "too many slug attempts") {
		t.Fatalf("project slug claim after resume slug budget: %d %s", created.Code, created.Body.String())
	}

	client.spendUntilLimited(t, http.MethodGet, "/api/v1/projects/not-a-uuid", "")
	if got := client.send(t, http.MethodGet, "/api/v1/resumes/not-a-uuid", ""); got != http.StatusTooManyRequests {
		t.Fatalf("resume read after project reads status = %d, want 429", got)
	}
	client.spendUntilLimited(t, http.MethodPost, "/api/v1/projects", "{}")
	if got := client.send(t, http.MethodPatch, "/api/v1/resumes/"+uuid.NewString(), `{"title":"x"}`); got != http.StatusTooManyRequests {
		t.Fatalf("resume write after project writes status = %d, want 429", got)
	}
}
