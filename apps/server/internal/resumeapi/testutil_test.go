package resumeapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/media"
	"github.com/dannyota/aboutme/apps/server/internal/media/mediatest"
	"github.com/dannyota/aboutme/apps/server/internal/resume"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	"github.com/dannyota/aboutme/apps/server/internal/testutil"
)

const resumeAPITestOrigin = "https://resume-api.example.test"

type resumeAPITestHarness struct {
	ctx       context.Context
	pool      *store.Pool
	queries   *store.Queries
	resumes   *resume.Store
	service   *Service
	handler   http.Handler
	server    *httptest.Server
	client    *http.Client
	userID    uuid.UUID
	session   store.Session
	cookie    *http.Cookie
	csrfToken string
}

type testHTTPResponse struct {
	status int
	header http.Header
	body   []byte
}

func snapshotHTTPResponse(t *testing.T, response *http.Response) testHTTPResponse {
	t.Helper()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response: %v", err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatalf("close response: %v", err)
	}
	return testHTTPResponse{status: response.StatusCode, header: response.Header.Clone(), body: raw}
}

// newResumeAPITestHarness is the shared live-database, selected-media, real
// router fixture for this package and the endpoint tasks that follow it.
func newResumeAPITestHarness(t *testing.T) *resumeAPITestHarness {
	t.Helper()

	dsn := testutil.RequireMigratedTestDatabaseURL(t)
	ctx := context.Background()
	pool, err := store.NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { pool.Close(context.Background()) })
	queries := store.New(pool)
	projector := docmigrate.NewIdentityProjector()
	resumeStore := resume.NewStore(pool, projector)
	idempotency := resume.NewIdempotencyStore(pool)
	blobs := newResumeAPITestMediaBackend(ctx, t)
	manager := auth.NewSessionManager(queries)

	var suffix [12]byte
	if _, randErr := rand.Read(suffix[:]); randErr != nil {
		pool.Close(context.Background())
		t.Fatalf("create random user suffix: %v", randErr)
	}
	user, err := queries.CreateUser(ctx, store.CreateUserParams{
		Email: "resumeapi-" + hex.EncodeToString(suffix[:]) + "@example.test",
		Name:  "resume API test",
	})
	if err != nil {
		pool.Close(context.Background())
		t.Fatalf("create test user: %v", err)
	}
	rawSession, session, err := manager.Issue(ctx, user.ID, "resumeapi-test", "127.0.0.1")
	if err != nil {
		pool.Close(context.Background())
		t.Fatalf("issue test session: %v", err)
	}

	service := New(resumeStore, idempotency, projector, blobs, Options{
		Logger:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		SessionManager: manager,
		PublicOrigin:   resumeAPITestOrigin,
	})
	handler := api.New(slog.New(slog.NewTextHandler(io.Discard, nil)), pool, api.Options{}, service.RegisterRoutes)
	server := httptest.NewServer(handler)
	h := &resumeAPITestHarness{
		ctx: ctx, pool: pool, queries: queries, resumes: resumeStore, service: service, handler: handler,
		server: server, client: server.Client(), userID: user.ID,
		session:   session,
		cookie:    &http.Cookie{Name: "__Host-session", Value: rawSession},
		csrfToken: base64.RawURLEncoding.EncodeToString(session.CSRFSecret),
	}
	t.Cleanup(func() {
		server.Close()
	})
	return h
}

func newResumeAPITestMediaBackend(ctx context.Context, t *testing.T) media.Backend {
	t.Helper()
	switch os.Getenv("TEST_MEDIA_BACKEND") {
	case "", "fs":
		backend, err := media.NewFS(t.TempDir())
		if err != nil {
			t.Fatalf("create filesystem media backend: %v", err)
		}
		return backend
	case "s3":
		backend, err := media.NewS3(ctx, mediatest.RequireTestS3(t))
		if err != nil {
			t.Fatalf("create S3 media backend: %v", err)
		}
		return backend
	default:
		t.Fatalf("TEST_MEDIA_BACKEND must be fs or s3")
		return nil
	}
}

func (h *resumeAPITestHarness) request(t *testing.T, method, path string, body io.Reader, authenticated, csrf bool) testHTTPResponse {
	t.Helper()
	req, err := http.NewRequestWithContext(h.ctx, method, h.server.URL+path, body)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if authenticated {
		req.AddCookie(h.cookie)
	}
	if csrf {
		req.Header.Set("Origin", resumeAPITestOrigin)
		req.Header.Set(auth.CSRFHeaderName, h.csrfToken)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("perform request: %v", err)
	}
	return snapshotHTTPResponse(t, resp)
}

func (h *resumeAPITestHarness) createResume(t *testing.T) resume.Resume {
	t.Helper()
	created, err := h.resumes.Create(h.ctx, h.userID, "Test resume", loadMinimalDocument(t))
	if err != nil {
		t.Fatalf("create test resume: %v", err)
	}
	return created
}

func (h *resumeAPITestHarness) snapshotUserTable(t *testing.T, table string) string {
	t.Helper()
	var snapshot string
	// table is supplied only by package-private test constants.
	query := "SELECT coalesce(string_agg(row_text, '|' ORDER BY row_text), '') FROM " +
		"(SELECT value::text AS row_text FROM " + table + " value WHERE user_id = $1) rows"
	if err := h.pool.QueryRow(h.ctx, query, h.userID).Scan(&snapshot); err != nil {
		t.Fatalf("snapshot %s: %v", table, err)
	}
	return snapshot
}

func concreteRoutePath(pattern string) string {
	replacer := strings.NewReplacer(
		"{id}", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e5f",
		"{sectionKey}", "work",
		"{entryId}", "01890f47-7e8a-7b2a-8d70-9a1f2c3d4e60",
	)
	return replacer.Replace(pattern)
}
