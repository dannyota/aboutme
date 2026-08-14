package publicapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
)

func TestDiscoveryHandlerRetriesOneGenerationMismatch(t *testing.T) {
	// This fails if the aggregate handler selects a stale cache generation rather than retrying.
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 2})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	store := &formatDiscoveryStore{snapshots: []formatDiscoverySnapshot{{generation: 1, slugs: []string{"stale"}}, {generation: 2, slugs: []string{"zeta", "ada"}}}}
	handler, err := NewSitemapHandler(DiscoveryDependencies{Store: store, Coordinator: coordinator, Cache: cache, PublicOrigin: origin, AppDigest: "sha256:app"})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if w.Code != http.StatusOK || w.Body.String() != "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<urlset xmlns=\"http://www.sitemaps.org/schemas/sitemap/0.9\">\n  <url><loc>https://aboutme.example/ada</loc></url>\n  <url><loc>https://aboutme.example/zeta</loc></url>\n</urlset>\n" {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
	if store.calls != 2 {
		t.Fatalf("snapshot calls = %d, want 2", store.calls)
	}
}

func TestDiscoveryHandlerFailsClosedAfterSecondGenerationMismatch(t *testing.T) {
	// This fails if a second mismatched snapshot is retried or used without admission.
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 3})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	store := &formatDiscoveryStore{snapshots: []formatDiscoverySnapshot{{generation: 1, slugs: []string{"first"}}, {generation: 2, slugs: []string{"second"}}}}
	handler, err := NewSitemapHandler(DiscoveryDependencies{Store: store, Coordinator: coordinator, Cache: cache, PublicOrigin: origin, AppDigest: "sha256:app"})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if store.calls != 2 {
		t.Fatalf("snapshot calls = %d, want 2", store.calls)
	}
}

func TestDiscoveryHandlerClosedAdmissionDoesNotServeCache(t *testing.T) {
	// This fails if global admission happens after cache selection.
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{DiscoveryGeneration: int64Pointer(1)})
	if err != nil {
		t.Fatal(err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	cache.Put(publiccache.Key{RouteClass: "discovery", Representation: publicstate.RepresentationLLMS, Variant: "default", Generation: 1, FormatVersion: publicformat.LLMSFormatVersion, AppDigest: "sha256:app"}, publiccache.Value{Status: http.StatusOK, Header: http.Header{}, Body: []byte("stale")})
	handler, err := NewLLMSHandler(DiscoveryDependencies{Store: &formatDiscoveryStore{snapshots: []formatDiscoverySnapshot{{generation: 1, slugs: []string{"ada"}}}}, Coordinator: coordinator, Cache: cache, PublicOrigin: origin, AppDigest: "sha256:app"})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/llms.txt", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
}

type formatDiscoveryStore struct {
	snapshots []formatDiscoverySnapshot
	calls     int
}

type formatDiscoverySnapshot struct {
	generation int64
	slugs      []string
}

func (s *formatDiscoveryStore) GetPublicDiscoverySnapshot(context.Context) (store.GetPublicDiscoverySnapshotRow, error) {
	index := s.calls
	if index >= len(s.snapshots) {
		index = len(s.snapshots) - 1
	}
	s.calls++
	snapshot := s.snapshots[index]
	return store.GetPublicDiscoverySnapshotRow{DiscoveryGeneration: snapshot.generation, Slugs: append([]string{}, snapshot.slugs...)}, nil
}
func int64Pointer(value int64) *int64 { return &value }
