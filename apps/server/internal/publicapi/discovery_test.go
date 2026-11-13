package publicapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/store"
)

func TestDiscoveryHandlerRetriesOneGenerationMismatch(t *testing.T) {
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
	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/sitemap.xml", nil))
	want, err := publicformat.Sitemap(origin, []string{"zeta", "ada"})
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || w.Body.String() != string(want) || strings.Contains(w.Body.String(), "stale") {
		t.Fatalf("response = %d %q", w.Code, w.Body.String())
	}
	if store.calls != 2 {
		t.Fatalf("snapshot calls = %d, want 2", store.calls)
	}
}

func TestDiscoveryHandlerFailsClosedAfterSecondGenerationMismatch(t *testing.T) {
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
	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/sitemap.xml", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if store.calls != 2 {
		t.Fatalf("snapshot calls = %d, want 2", store.calls)
	}
}

func TestDiscoveryHandlerClosedAdmissionServesNoDiscoveryBytes(t *testing.T) {
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
	if closeErr := transition.Close(context.Background(), time.Now().Add(time.Second)); closeErr != nil {
		t.Fatal(closeErr)
	}
	handler, err := NewLLMSHandler(DiscoveryDependencies{Store: &formatDiscoveryStore{snapshots: []formatDiscoverySnapshot{{generation: 1, slugs: []string{"ada"}}}}, Coordinator: coordinator, PublicOrigin: origin})
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/llms.txt", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", w.Code)
	}
	if err := transition.Rollback(); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoveryHandlerServesTheSnapshotItRead(t *testing.T) {
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		handler func(DiscoveryDependencies) (http.Handler, error)
		encode  discoveryEncoder
		path    string
	}{
		{name: "sitemap", handler: NewSitemapHandler, encode: publicformat.Sitemap, path: "/sitemap.xml"},
		{name: "llms", handler: NewLLMSHandler, encode: publicformat.LLMS, path: "/llms.txt"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
			if err != nil {
				t.Fatal(err)
			}
			cache, err := publiccache.New(4, time.Minute, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			before, after := []string{"ada", "deleted"}, []string{"ada"}
			discovery := &changingDiscoveryStore{generation: 1, slugs: before}
			discovery.afterRead = func() { discovery.slugs = after }
			handler, err := test.handler(DiscoveryDependencies{Store: discovery, Coordinator: coordinator, Cache: cache, PublicOrigin: origin, AppDigest: "sha256:app"})
			if err != nil {
				t.Fatal(err)
			}
			for i, slugs := range [][]string{before, after} {
				want, err := test.encode(origin, slugs)
				if err != nil {
					t.Fatal(err)
				}
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, test.path, nil))
				if w.Code != http.StatusOK || w.Body.String() != string(want) {
					t.Fatalf("request %d = %d %q, want the encoding of %q", i+1, w.Code, w.Body.String(), slugs)
				}
			}
		})
	}
}

type changingDiscoveryStore struct {
	generation int64
	slugs      []string
	afterRead  func()
}

func (s *changingDiscoveryStore) GetPublicDiscoverySnapshot(context.Context) (store.GetPublicDiscoverySnapshotRow, error) {
	row := store.GetPublicDiscoverySnapshotRow{DiscoveryGeneration: s.generation, Slugs: append([]string{}, s.slugs...)}
	if afterRead := s.afterRead; afterRead != nil {
		s.afterRead = nil
		afterRead()
	}
	return row, nil
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
