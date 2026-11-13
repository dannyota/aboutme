package publicapi

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/renderjob"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

type frozenPrintEnvelope struct {
	Version      int               `json:"version"`
	ProjectLinks map[string]string `json:"projectLinks"`
}

func linkedArtifactHandlers(t *testing.T, names map[string]string) (*artifactHandlers, map[string]string, *[]frozenPrintEnvelope) {
	t.Helper()
	slug, lng, fullName := "ada-lovelace", "en", "Ada"
	entries, ids, column := []schema.ProjectEntry{}, map[string]string{}, []map[string]any{}
	for _, projectSlug := range []string{"atlas", "quiet"} {
		title, projectID := projectSlug, uuid.NewString()
		ids[projectSlug] = uuid.NewString()
		entries = append(entries, schema.ProjectEntry{ID: ids[projectSlug], Title: &title, ProjectID: &projectID})
		if name, linked := names[projectSlug]; linked {
			column = append(column, map[string]any{"entryId": ids[projectSlug], "projectSlug": projectSlug,
				"document": map[string]any{"defaultLanguage": "en", "text": map[string]any{"en": map[string]string{"name": name}}}})
		}
	}
	document := schema.Resume{SchemaVersion: schema.CurrentVersion, PersonalDetails: schema.PersonalDetails{FullName: &fullName},
		Content: map[string]schema.Section{"projects": schema.NewProjectSection(nil, nil, entries)}}
	document.Customization.Layout.Sections.Main = []string{"projects"}
	parts := make([]json.RawMessage, 4)
	for index, part := range []any{document.PersonalDetails, document.Content, document.Customization, column} {
		var err error
		if parts[index], err = json.Marshal(part); err != nil {
			t.Fatal(err)
		}
	}
	backing := &publicServiceStore{row: store.Resume{ID: uuid.New(), Slug: &slug, Live: true, DownloadEnabled: true, SEOGeoEnabled: true, Revision: 3, Lng: &lng,
		SchemaVersion: int32(schema.CurrentVersion), PersonalDetails: parts[0], Content: parts[1], Customization: parts[2]}, links: parts[3]}
	coordinator, err := publicstate.NewCoordinator(publicstate.CoordinatorConfig{DiscoveryGeneration: 1})
	if err != nil {
		t.Fatal(err)
	}
	reader, err := publicresume.NewReader(publicresume.ReaderDependencies{Store: backing, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: mustPublicOrigin(t)})
	if err != nil {
		t.Fatal(err)
	}
	frozen := &[]frozenPrintEnvelope{}
	queue := artifactQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		snapshot, prepareErr := request.Prepare(ctx)
		if prepareErr != nil {
			return renderjob.Result{}, prepareErr
		}
		var envelope frozenPrintEnvelope
		if decodeErr := json.Unmarshal(snapshot.Payload, &envelope); decodeErr != nil {
			t.Fatalf("decode frozen payload: %v", decodeErr)
		}
		*frozen = append(*frozen, envelope)
		body := []byte("%PDF-1.7\npublic")
		if request.Format == renderjob.PNG {
			body = []byte("\x89PNG\r\npublic")
		}
		return renderjob.Result{Bytes: body, Digest: sha256.Sum256(body), Revision: snapshot.Revision}, nil
	})
	now := func() time.Time { return time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC) }
	cache, err := publiccache.New(4, time.Minute, now)
	if err != nil {
		t.Fatal(err)
	}
	handlers, err := newArtifactHandlers(ArtifactDependencies{Reader: reader, Cache: cache, Queue: queue, PublicOrigin: mustPublicOrigin(t),
		AppDigest: "sha256:app", RendererDigest: "sha256:renderer", Clock: now})
	if err != nil {
		t.Fatal(err)
	}
	return handlers, ids, frozen
}

func serveArtifact(t *testing.T, handler http.Handler, path string) {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	request.RemoteAddr = "192.0.2.30:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, response.Code, response.Body.String())
	}
}

func TestPublicPDFCarriesTheAvailableProjectLinks(t *testing.T) {
	handlers, ids, frozen := linkedArtifactHandlers(t, map[string]string{"atlas": "Atlas", "quiet": "　"})
	serveArtifact(t, handlers.pdf, "/api/v1/public/resumes/ada-lovelace/pdf")
	want := map[string]string{ids["atlas"]: "https://aboutme.example/ada-lovelace/projects/atlas"}
	if len(*frozen) != 1 || (*frozen)[0].Version != 2 || !reflect.DeepEqual((*frozen)[0].ProjectLinks, want) {
		t.Fatalf("frozen PDF envelopes = %+v, want version 2 with %v", *frozen, want)
	}
	serveArtifact(t, handlers.png, "/api/v1/public/resumes/ada-lovelace/og.png")
	if len(*frozen) != 2 || (*frozen)[1].Version != 1 || (*frozen)[1].ProjectLinks != nil {
		t.Fatalf("frozen og.png envelope = %+v, want version 1 without links", (*frozen)[1:])
	}
}

func TestPublicPDFWithoutAnAvailableProjectStaysVersionOne(t *testing.T) {
	for name, names := range map[string]map[string]string{
		"no link":             {},
		"blank-named project": {"atlas": " "},
	} {
		t.Run(name, func(t *testing.T) {
			handlers, _, frozen := linkedArtifactHandlers(t, names)
			serveArtifact(t, handlers.pdf, "/api/v1/public/resumes/ada-lovelace/pdf")
			if len(*frozen) != 1 || (*frozen)[0].Version != 1 || (*frozen)[0].ProjectLinks != nil {
				t.Fatalf("frozen PDF envelopes = %+v, want version 1 without links", *frozen)
			}
		})
	}
}

func TestPublicArtifactsRequireTheCanonicalOrigin(t *testing.T) {
	_, reader, _, _ := publicServiceReaderWithStore(t)
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newArtifactHandlers(ArtifactDependencies{Reader: reader, Cache: cache, AppDigest: "sha256:app", RendererDigest: "sha256:renderer"}); err == nil {
		t.Fatal("newArtifactHandlers() accepted dependencies without the canonical origin")
	}
}
