package accountapi

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/dannyota/aboutme/apps/server/internal/directrender"
	"github.com/dannyota/aboutme/apps/server/internal/previewcard"
	"github.com/dannyota/aboutme/apps/server/internal/publicapi"
	"github.com/dannyota/aboutme/apps/server/internal/publiccache"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
	"github.com/dannyota/aboutme/apps/server/internal/publicstate"
	"github.com/dannyota/aboutme/apps/server/internal/renderjob"
	"github.com/dannyota/aboutme/apps/server/internal/resume/docmigrate"
)

// The known card version must pass before deletion and fail afterwards even
// while the card collaborator still has its cached bytes.
type deletionCards struct{}

func (deletionCards) Version(publicresume.Snapshot) (string, error) {
	return "0123456789abcdef", nil
}

func (deletionCards) Stored(context.Context, uuid.UUID) (previewcard.StoredCard, bool, error) {
	return previewcard.StoredCard{Version: "0123456789abcdef", PNG: []byte("synthetic card")}, true, nil
}

func (deletionCards) Build(context.Context, publicresume.Snapshot, renderjob.Priority) ([]byte, error) {
	return nil, errors.New("unexpected render")
}

func TestAccountDeletionRevokesKnownVersionedCard(t *testing.T) {
	f := newOperatorFixture(t)
	if _, err := f.pool.Exec(f.ctx, `UPDATE resumes SET live=true WHERE user_id=$1`, f.user.ID); err != nil {
		t.Fatal(err)
	}
	q := f.queries
	state, err := q.GetPublicState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: state.DiscoveryGeneration})
	if err != nil {
		t.Fatal(err)
	}
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{
		Store: q, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: origin,
	})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(8, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	renderOrigin, err := directrender.ParseRenderOrigin("http://127.0.0.1:20030", "development")
	if err != nil {
		t.Fatal(err)
	}
	routes, err := publicapi.NewService(publicapi.ServiceDependencies{
		Reader: reader, DiscoveryStore: q, Coordinator: coordinator, Cache: cache,
		Renderer: directrender.New(renderOrigin, http.DefaultClient), PublicOrigin: origin,
		AppDigest: "sha256:app", RendererDigest: "sha256:renderer", Cards: deletionCards{}, Clock: time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	read := func(path string) int {
		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		request.RemoteAddr = "192.0.2.40:1234"
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, request)
		return response.Code
	}
	card := "/api/v1/public/resumes/" + f.slugs[0] + "/og/0123456789abcdef.png"
	if code := read(card); code != http.StatusOK {
		t.Fatalf("known card before deletion = %d", code)
	}
	if _, err = f.service.DeleteAccountBySlug(f.ctx, f.slugs[0], f.user.ID); err != nil {
		t.Fatal(err)
	}
	for _, slug := range f.slugs {
		for _, path := range []string{
			"/" + slug, "/" + slug + ".md", "/api/v1/public/resumes/" + slug,
			"/api/v1/public/resumes/" + slug + "/photo", "/api/v1/public/resumes/" + slug + "/pdf",
			"/api/v1/public/resumes/" + slug + "/og.png", "/api/v1/public/resumes/" + slug + "/og/0123456789abcdef.png",
		} {
			if code := read(path); code != http.StatusNotFound {
				t.Errorf("deleted public route %s = %d", path, code)
			}
		}
	}
}
