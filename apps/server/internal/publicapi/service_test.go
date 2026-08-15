package publicapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicformat"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
	"github.com/dannyota/aboutme/apps/server/internal/store"
	schema "github.com/dannyota/aboutme/packages/schema/gen/go"
)

func TestPublicServiceDispatchesJSONAndRejectsWrongMethodBeforeLookup(t *testing.T) {
	// This fails if a public JSON method error can reach the database or use
	// the private API error shape.
	slug, reader, coordinator := publicServiceReader(t)
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	origin := mustPublicOrigin(t)
	renderOrigin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(ServiceDependencies{
		Reader: reader, Cache: cache, Coordinator: coordinator, DiscoveryStore: &formatDiscoveryStore{snapshots: []formatDiscoverySnapshot{{generation: 1}}},
		Renderer: directrender.New(renderOrigin, nil), PublicOrigin: origin, AppDigest: "sha256:app", RendererDigest: "sha256:renderer",
	})
	if err != nil {
		t.Fatal(err)
	}

	wrong := httptest.NewRecorder()
	service.ServeHTTP(wrong, httptest.NewRequest(http.MethodPost, "/api/v1/public/resumes/"+slug, nil))
	if got, want := wrong.Code, http.StatusMethodNotAllowed; got != want {
		t.Fatalf("POST status = %d, want %d", got, want)
	}
	if got, want := wrong.Header().Get("Allow"), "GET, HEAD"; got != want {
		t.Fatalf("Allow = %q, want %q", got, want)
	}
	if got, want := wrong.Body.String(), "{\"error\":{\"code\":\"method_not_allowed\",\"message\":\"method is not allowed\"}}\n"; got != want {
		t.Fatalf("POST body = %q, want %q", got, want)
	}

	ok := httptest.NewRecorder()
	service.ServeHTTP(ok, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/public/resumes/"+slug, nil))
	if got, want := ok.Code, http.StatusOK; got != want {
		t.Fatalf("GET status = %d, want %d", got, want)
	}
	if got := ok.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("GET Content-Type = %q", got)
	}
}

func TestPublicJSONCacheSitsAfterLiveAdmission(t *testing.T) {
	// This fails if public JSON skips the generation-keyed cache, or if a
	// stale cache value can answer before the current lease is acquired.
	slug, reader, coordinator, backing := publicServiceReaderWithStore(t)
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	service := newPublicServiceForReader(t, reader, coordinator, cache)

	first := httptest.NewRecorder()
	service.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/"+slug, nil))
	if first.Code != http.StatusOK {
		t.Fatalf("initial status = %d", first.Code)
	}
	key := publiccache.Key{RouteClass: "resume", Representation: publicstate.RepresentationJSON, Variant: "default", ResumeID: backing.row.ID, Generation: 1, FormatVersion: 1, AppDigest: "sha256:app"}
	if _, ok := cache.Get(key); !ok {
		t.Fatal("successful JSON response was not retained under its admitted generation")
	}
	cache.Put(key, publiccache.Value{Status: http.StatusOK, Header: http.Header{"Content-Type": {"application/json; charset=utf-8"}}, Body: []byte("stale")})
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{Resumes: []publicstate.ResumeTarget{{ID: backing.row.ID, ExpectedRevision: 1, Class: publicstate.NonDraining}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := transition.Close(context.Background(), time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	backing.row.Revision = 2
	if err := transition.Commit(publicstate.CommittedState{ResumeRevisions: map[uuid.UUID]int64{backing.row.ID: 2}}); err != nil {
		t.Fatal(err)
	}

	second := httptest.NewRecorder()
	service.ServeHTTP(second, httptest.NewRequest(http.MethodGet, "/api/v1/public/resumes/"+slug, nil))
	if second.Code != http.StatusOK || second.Body.String() == "stale" {
		t.Fatalf("new-generation response = %d %q, want a current body", second.Code, second.Body.String())
	}
	if backing.calls != 2 {
		t.Fatalf("public cache bypassed live admission: reads = %d, want 2", backing.calls)
	}
}

func TestRestartGenerationAndInvalidationFailureCannotRestoreAccess(t *testing.T) {
	// A retained pre-restart artifact stands in for a failed edge invalidation.
	// Startup's committed generation must make it unselectable.
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 41})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	origin := mustPublicOrigin(t)
	cache.Put(publiccache.Key{RouteClass: "discovery", Representation: publicstate.RepresentationLLMS, Variant: "default", Generation: 40, FormatVersion: publicformat.LLMSFormatVersion, AppDigest: "sha256:app"}, publiccache.Value{Status: http.StatusOK, Header: http.Header{}, Body: []byte("stale")})
	handler, err := NewLLMSHandler(DiscoveryDependencies{Store: &formatDiscoveryStore{snapshots: []formatDiscoverySnapshot{{generation: 41, slugs: []string{"current"}}}}, Coordinator: coordinator, Cache: cache, PublicOrigin: origin, AppDigest: "sha256:app"})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/llms.txt", nil))
	if response.Code != http.StatusOK || response.Body.String() == "stale" || !strings.Contains(response.Body.String(), "/current") {
		t.Fatalf("restart response = %d %q, want committed generation body", response.Code, response.Body.String())
	}
}

func TestOriginAdmissionBoundaryAroundCompletedEdgeValidation(t *testing.T) {
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 41})
	if err != nil {
		t.Fatal(err)
	}
	// This lease models an origin validation that completed before the fence
	// closed but whose edge response has not yet finished.
	lease, err := coordinator.AcquireDiscovery(context.Background(), 41, publicstate.RepresentationLLMS)
	if err != nil {
		t.Fatal(err)
	}
	transition, err := coordinator.Begin(context.Background(), publicstate.Plan{DiscoveryGeneration: int64Pointer(41)})
	if err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- transition.Close(context.Background(), time.Now().Add(time.Second)) }()
	select {
	case <-lease.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("close did not cancel the already-admitted origin response")
	}
	if _, err := coordinator.AcquireDiscovery(context.Background(), 41, publicstate.RepresentationLLMS); !errors.Is(err, publicstate.ErrAdmissionClosed) {
		t.Fatalf("post-close admission error = %v, want ErrAdmissionClosed", err)
	}
	lease.Release()
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if err := transition.Commit(publicstate.CommittedState{DiscoveryGeneration: int64Pointer(42)}); err != nil {
		t.Fatal(err)
	}
	current, err := coordinator.AcquireDiscovery(context.Background(), 42, publicstate.RepresentationLLMS)
	if err != nil {
		t.Fatalf("committed-generation admission error = %v", err)
	}
	current.Release()
}

type publicServiceStore struct {
	row   store.Resume
	calls int
}

func (s *publicServiceStore) GetPublicState(context.Context) (store.PublicState, error) {
	return store.PublicState{}, errors.New("unused")
}

func (s *publicServiceStore) GetPublicResumeBySlug(context.Context, string) (store.Resume, error) {
	s.calls++
	return s.row, nil
}
func (s *publicServiceStore) GetPublicResumeByOwner(context.Context, store.GetPublicResumeByOwnerParams) (store.Resume, error) {
	return store.Resume{}, errors.New("unused")
}
func (s *publicServiceStore) ListEligiblePublicSlugs(context.Context) ([]string, error) {
	return nil, nil
}

func publicServiceReader(t *testing.T) (string, *publicresume.Reader, *publicstate.Coordinator) {
	slug, reader, coordinator, _ := publicServiceReaderWithStore(t)
	return slug, reader, coordinator
}

func publicServiceReaderWithStore(t *testing.T) (string, *publicresume.Reader, *publicstate.Coordinator, *publicServiceStore) {
	t.Helper()
	slug, lng, name := "ada-lovelace", "en", "Ada"
	document := schema.Resume{SchemaVersion: schema.CurrentVersion, PersonalDetails: schema.PersonalDetails{FullName: &name}, Content: map[string]schema.Section{}}
	personal, err := json.Marshal(document.PersonalDetails)
	if err != nil {
		t.Fatal(err)
	}
	content, err := json.Marshal(document.Content)
	if err != nil {
		t.Fatal(err)
	}
	customization, err := json.Marshal(document.Customization)
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	origin := mustPublicOrigin(t)
	backing := &publicServiceStore{row: store.Resume{ID: uuid.New(), Slug: &slug, Live: true, SEOGeoEnabled: true, Revision: 1, Lng: &lng, SchemaVersion: int32(schema.CurrentVersion), PersonalDetails: personal, Content: content, Customization: customization}}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store:     backing,
		Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	return slug, reader, coordinator, backing
}

func newPublicServiceForReader(t *testing.T, reader *publicresume.Reader, coordinator *publicstate.Coordinator, cache *publiccache.Cache) *Service {
	t.Helper()
	origin := mustPublicOrigin(t)
	renderOrigin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	service, err := NewService(ServiceDependencies{Reader: reader, Cache: cache, Coordinator: coordinator, DiscoveryStore: &formatDiscoveryStore{snapshots: []formatDiscoverySnapshot{{generation: 1}}}, Renderer: directrender.New(renderOrigin, nil), PublicOrigin: origin, AppDigest: "sha256:app", RendererDigest: "sha256:renderer"})
	if err != nil {
		t.Fatal(err)
	}
	return service
}
