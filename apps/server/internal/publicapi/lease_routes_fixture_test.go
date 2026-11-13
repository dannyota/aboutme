package publicapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/directrender"
	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

type leasedRoute struct {
	handler     http.Handler
	path        string
	coordinator *publicstate.Coordinator
	resumeID    uuid.UUID
	cache       *publiccache.Cache
	key         publiccache.Key
}

func (r leasedRoute) plan(class publicstate.TransitionClass) publicstate.Plan {
	if r.resumeID == uuid.Nil {
		return publicstate.Plan{DiscoveryGeneration: int64Pointer(1)}
	}
	return publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: r.resumeID, ExpectedRevision: 1, Class: class}}}
}

type leasedRouteCase struct {
	name   string
	cached bool
	build  func(*testing.T) leasedRoute
}

func leasedRouteCases() []leasedRouteCase {
	return []leasedRouteCase{
		{name: "page_fresh", build: leasedPageRoute},
		{name: "page_cached", cached: true, build: leasedPageRoute},
		{name: "gate", build: leasedGateRoute},
		{name: "json_fresh", build: leasedJSONRoute},
		{name: "json_cached", cached: true, build: leasedJSONRoute},
		{name: "photo_fresh", build: leasedPhotoRoute},
		{name: "photo_cached", cached: true, build: leasedPhotoRoute},
		{name: "markdown_fresh", build: leasedMarkdownRoute},
		{name: "markdown_cached", cached: true, build: leasedMarkdownRoute},
		{name: "sitemap", build: func(t *testing.T) leasedRoute { return leasedDiscoveryRoute(t, NewSitemapHandler, "/sitemap.xml") }},
		{name: "llms", build: func(t *testing.T) leasedRoute { return leasedDiscoveryRoute(t, NewLLMSHandler, "/llms.txt") }},
	}
}

func leasedPageRoute(t *testing.T) leasedRoute {
	t.Helper()
	slug, reader, coordinator := formatTestReader(t, false)
	cache := newLeaseTestCache(t)
	page := validHTMLIn("en", "Ada", "https://aboutme.example/"+slug, "1", "")
	handler, err := NewHTMLHandler(HTMLDependencies{
		Reader: reader, Cache: cache, Renderer: leaseTestRenderer(t, func([]byte) string { return page }),
		PublicOrigin: mustPublicOrigin(t), AppDigest: "sha256:app", RendererDigest: "sha256:renderer",
	})
	if err != nil {
		t.Fatal(err)
	}
	return leasedRoute{handler: handler, path: "/" + slug, coordinator: coordinator, resumeID: formatResumeID, cache: cache, key: publiccache.Key{
		RouteClass: "resume", Representation: publicstate.RepresentationHTML, Variant: "nondiscoverable", ResumeID: formatResumeID,
		Generation: 1, FormatVersion: htmlFormatVersion, AppDigest: "sha256:app", RendererDigest: "sha256:renderer",
	}}
}

func leasedGateRoute(t *testing.T) leasedRoute {
	t.Helper()
	dependencies, coordinator := leaseTestGateDependencies(t)
	handler, err := NewHTMLHandler(dependencies)
	if err != nil {
		t.Fatal(err)
	}
	return leasedRoute{handler: handler, path: "/ada", coordinator: coordinator, resumeID: gateTestResumeID}
}

func leaseTestGateDependencies(t *testing.T) (HTMLDependencies, *publicstate.Coordinator) {
	t.Helper()
	slug, lng := "ada", "en"
	coordinator := newLeaseTestCoordinator(t)
	origin := mustPublicOrigin(t)
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: formatReadStore{row: store.Resume{
			ID: gateTestResumeID, Slug: &slug, Live: true, Revision: 1, Lng: &lng, SchemaVersion: int32(schema.CurrentVersion),
			PersonalDetails: []byte(`{"fullName":"Ada"}`), Content: []byte(`{}`), Customization: []byte(`{}`),
			SignInToView: true, ViewPassEpoch: 1,
		}},
		Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	renderer := leaseTestRenderer(t, func(body []byte) string {
		var envelope directrender.GateRenderRequest
		if decodeErr := json.Unmarshal(body, &envelope); decodeErr != nil {
			t.Errorf("gate envelope: %v", decodeErr)
		}
		return gateValidHTML(envelope, origin)
	})
	return HTMLDependencies{
		Reader: reader, Cache: newLeaseTestCache(t), Renderer: renderer, PublicOrigin: origin,
		AppDigest: "sha256:app", RendererDigest: "sha256:renderer",
		ViewPassKey: gateTestKey, GateProviders: []string{"google", "linkedin"}, JoinInviteTarget: "/register",
	}, coordinator
}

func leasedJSONRoute(t *testing.T) leasedRoute {
	t.Helper()
	slug, reader, coordinator, backing := publicServiceReaderWithStore(t)
	cache := newLeaseTestCache(t)
	return leasedRoute{
		handler: newPublicServiceForReader(t, reader, coordinator, cache), path: "/api/v1/public/resumes/" + slug,
		coordinator: coordinator, resumeID: backing.row.ID, cache: cache, key: publiccache.Key{
			RouteClass: "resume", Representation: publicstate.RepresentationJSON, Variant: "default", ResumeID: backing.row.ID,
			Generation: 1, FormatVersion: jsonFormatVersion, AppDigest: "sha256:app",
		},
	}
}

func leasedPhotoRoute(t *testing.T) leasedRoute {
	t.Helper()
	slug, _, coordinator, backing := publicServiceReaderWithStore(t)
	var personal schema.PersonalDetails
	if err := json.Unmarshal(backing.row.PersonalDetails, &personal); err != nil {
		t.Fatal(err)
	}
	personal.Photo = &schema.Photo{Key: "private/photo.png"}
	encoded, err := json.Marshal(personal)
	if err != nil {
		t.Fatal(err)
	}
	backing.row.PersonalDetails = encoded
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: backing, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator,
		Media: &artifactPhotoBackend{body: []byte("photo bytes"), contentType: "image/png"}, Origin: mustPublicOrigin(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	cache := newLeaseTestCache(t)
	return leasedRoute{
		handler: newPublicServiceForReader(t, reader, coordinator, cache), path: "/api/v1/public/resumes/" + slug + "/photo",
		coordinator: coordinator, resumeID: backing.row.ID, cache: cache, key: publiccache.Key{
			RouteClass: "resume", Representation: publicstate.RepresentationPhoto, Variant: "default", ResumeID: backing.row.ID,
			Generation: 1, FormatVersion: photoFormatVersion, AppDigest: "sha256:app",
		},
	}
}

func leasedMarkdownRoute(t *testing.T) leasedRoute {
	t.Helper()
	slug, reader, coordinator := formatTestReader(t, true)
	cache := newLeaseTestCache(t)
	handler, err := NewMarkdownHandler(MarkdownDependencies{Reader: reader, Cache: cache, AppDigest: "sha256:app"})
	if err != nil {
		t.Fatal(err)
	}
	return leasedRoute{handler: handler, path: "/" + slug + ".md", coordinator: coordinator, resumeID: formatResumeID, cache: cache, key: publiccache.Key{
		RouteClass: "resume", Representation: publicstate.RepresentationMarkdown, Variant: "discoverable", ResumeID: formatResumeID,
		Generation: 1, FormatVersion: publicformat.MarkdownFormatVersion, AppDigest: "sha256:app",
	}}
}

func leasedDiscoveryRoute(t *testing.T, newHandler func(DiscoveryDependencies) (http.Handler, error), path string) leasedRoute {
	t.Helper()
	coordinator := newLeaseTestCoordinator(t)
	handler, err := newHandler(DiscoveryDependencies{
		Store:       &formatDiscoveryStore{snapshots: []formatDiscoverySnapshot{{generation: 1, slugs: []string{"ada"}}}},
		Coordinator: coordinator, PublicOrigin: mustPublicOrigin(t),
	})
	if err != nil {
		t.Fatal(err)
	}
	return leasedRoute{handler: handler, path: path, coordinator: coordinator}
}

func newLeaseTestCoordinator(t *testing.T) *publicstate.Coordinator {
	t.Helper()
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	return coordinator
}

func newLeaseTestCache(t *testing.T) *publiccache.Cache {
	t.Helper()
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return cache
}

func leaseTestRenderer(t *testing.T, page func(body []byte) string) *directrender.Client {
	t.Helper()
	origin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	return directrender.New(origin, &http.Client{Transport: htmlRoundTrip(func(request *http.Request) (*http.Response, error) {
		body, readErr := io.ReadAll(request.Body)
		if closeErr := request.Body.Close(); readErr == nil {
			readErr = closeErr
		}
		if readErr != nil {
			return nil, readErr
		}
		return &http.Response{
			StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}},
			Body: io.NopCloser(bytes.NewBufferString(page(body))),
		}, nil
	})})
}

func newLeaseTestRequest(path string) *http.Request {
	return httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
}

func primeLeasedRoute(t *testing.T, route leasedRoute, cached bool) {
	t.Helper()
	if cached {
		recorder := httptest.NewRecorder()
		route.handler.ServeHTTP(recorder, newLeaseTestRequest(route.path))
		if recorder.Code != http.StatusOK {
			t.Fatalf("priming status = %d, body = %q", recorder.Code, recorder.Body.String())
		}
	}
	if route.cache == nil {
		return
	}
	if _, found := route.cache.Get(route.key); found != cached {
		t.Fatalf("cache holds the route's body = %t, want %t", found, cached)
	}
}

type statusBlockedWriter struct {
	*deadlineBlockedWriter
	status atomic.Int32
}

func newStatusBlockedWriter() *statusBlockedWriter {
	return &statusBlockedWriter{deadlineBlockedWriter: newDeadlineBlockedWriter()}
}

func (w *statusBlockedWriter) WriteHeader(status int) { w.status.Store(int32(status)) }

func serveBlocked(t *testing.T, route leasedRoute, writer *statusBlockedWriter) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		route.handler.ServeHTTP(writer, newLeaseTestRequest(route.path))
	}()
	select {
	case <-writer.writing:
	case <-done:
		t.Fatal("the handler returned without writing a body")
	case <-time.After(5 * time.Second):
		t.Fatal("the handler never wrote the body")
	}
	if status := writer.status.Load(); status != http.StatusOK {
		t.Fatalf("the blocked write has status %d, want 200", status)
	}
	return done
}
