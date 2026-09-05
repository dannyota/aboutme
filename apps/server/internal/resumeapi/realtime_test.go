package resumeapi

import (
	"bufio"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/api"
	"github.com/dannyota/aboutme/apps/server/internal/auth"
	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/publicapi"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/realtime"
	"github.com/dannyota/aboutme/apps/server/internal/realtimeapi"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

func TestRealtimePublicMutationClosesStreamAndRejectsCachedJSON(t *testing.T) {
	for _, operation := range []string{"unpublish", "rename", "delete", "agent-delete"} {
		t.Run(operation, func(t *testing.T) {
			h := newResumeAPITestHarness(t)
			created, err := h.resumes.Create(h.ctx, h.userID, "Realtime mutation", publishCompleteDocument(t))
			if err != nil {
				t.Fatal(err)
			}
			slug := "realtime-" + uuid.NewString()[:8]
			path := apiResumePath + "/" + created.ID.String()
			published := h.mutationRequest(t, http.MethodPost, path+"/publish",
				strings.NewReader(`{"slug":"`+slug+`","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`), 1, uuid.NewString())
			if published.status != http.StatusOK {
				t.Fatalf("publish status = %d: %s", published.status, published.body)
			}
			server := newRealtimePublicServer(t, h)
			client := server.Client()
			client.Timeout = 5 * time.Second
			jsonURL := server.URL + "/api/v1/public/resumes/" + slug
			beforeRequest, err := http.NewRequestWithContext(h.ctx, http.MethodGet, jsonURL, nil)
			if err != nil {
				t.Fatal(err)
			}
			before, err := client.Do(beforeRequest)
			if err != nil {
				t.Fatal(err)
			}
			cached := snapshotHTTPResponse(t, before)
			if cached.status != http.StatusOK || cached.header.Get("ETag") == "" {
				t.Fatalf("public read = %d, want cached 200", cached.status)
			}
			streamRequest, err := http.NewRequestWithContext(h.ctx, http.MethodGet, server.URL+"/api/v1/live/"+slug, nil)
			if err != nil {
				t.Fatal(err)
			}
			stream, err := client.Do(streamRequest)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if closeErr := stream.Body.Close(); closeErr != nil {
					t.Errorf("close stream: %v", closeErr)
				}
			}()
			if stream.StatusCode != http.StatusOK {
				t.Fatalf("stream status = %d", stream.StatusCode)
			}
			reader := bufio.NewReader(stream.Body)
			for _, want := range []string{"event: heartbeat\n", "data: {\"version\":1}\n", "\n"} {
				line, readErr := reader.ReadString('\n')
				if readErr != nil || line != want {
					t.Fatalf("initial stream frame = %q, %v", line, readErr)
				}
			}
			switch operation {
			case "unpublish", "rename":
				body := `{"live":false,"downloadEnabled":false,"seoGeoEnabled":false}`
				if operation == "rename" {
					body = `{"slug":"` + slug + `-new","live":true,"downloadEnabled":false,"seoGeoEnabled":false}`
				}
				result := h.mutationRequest(t, http.MethodPost, path+"/publish", strings.NewReader(body), 2, uuid.NewString())
				if result.status != http.StatusOK {
					t.Fatalf("%s status = %d: %s", operation, result.status, result.body)
				}
			case "delete":
				result := h.mutationRequest(t, http.MethodDelete, path, nil, 2, uuid.NewString())
				if result.status != http.StatusNoContent {
					t.Fatalf("delete status = %d: %s", result.status, result.body)
				}
			case "agent-delete":
				principal := newAgentPrincipalForTest(t, h)
				result := h.service.ExecuteAgent(h.ctx, principal, AgentCall{Operation: AgentDeleteResume, IdempotencyKey: uuid.NewString(), ResumeID: created.ID.String(), Revision: "2"})
				if result.Status != http.StatusNoContent {
					t.Fatalf("agent delete status = %d", result.Status)
				}
			}
			if _, streamErr := reader.ReadByte(); !errors.Is(streamErr, io.EOF) {
				t.Fatalf("successful %s left stream open: %v", operation, streamErr)
			}
			request, err := http.NewRequestWithContext(h.ctx, http.MethodGet, jsonURL, nil)
			if err != nil {
				t.Fatal(err)
			}
			request.Header.Set("If-None-Match", cached.header.Get("ETag"))
			request.AddCookie(h.cookie)
			after, err := client.Do(request)
			if err != nil {
				t.Fatal(err)
			}
			missing := snapshotHTTPResponse(t, after)
			if missing.status != http.StatusNotFound {
				t.Fatalf("%s reused public cache: %d", operation, missing.status)
			}
		})
	}
}

func newRealtimePublicServer(t *testing.T, h *resumeAPITestHarness) *httptest.Server {
	t.Helper()
	origin, err := publicresume.ParsePublicOrigin(resumeAPITestOrigin, "dev")
	if err != nil {
		t.Fatal(err)
	}
	renderOrigin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "dev")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{Store: h.queries, Projector: docmigrate.NewIdentityProjector(), Coordinator: h.service.coordinator, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(8, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	hub, err := realtime.NewHub(realtime.Config{})
	if err != nil {
		t.Fatal(err)
	}
	// This test isolates synchronous revocation from notification delivery.
	// The real DB listener tests prove delivery separately.
	hub.SetAvailable(true)
	streams, err := realtimeapi.New(realtimeapi.Dependencies{Hub: hub, Store: h.queries, Sessions: auth.NewSessionManager(h.queries), Coordinator: h.service.coordinator})
	if err != nil {
		t.Fatal(err)
	}
	public, err := publicapi.NewService(publicapi.ServiceDependencies{Reader: reader, DiscoveryStore: h.queries, Coordinator: h.service.coordinator, Cache: cache, Renderer: directrender.New(renderOrigin, nil), PublicOrigin: origin, AppDigest: "test-app", RendererDigest: "test-renderer", Live: streams.PublicHandler()})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(api.New(slog.New(slog.NewTextHandler(io.Discard, nil)), h.pool, api.Options{}, public))
	t.Cleanup(func() { hub.Close(); server.Close() })
	return server
}
