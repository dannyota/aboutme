package publicapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/publiccache"
	"danny.vn/aboutme/apps/server/internal/publicformat"
	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/publicstate"
	"danny.vn/aboutme/apps/server/internal/resume/docmigrate"
	"danny.vn/aboutme/apps/server/internal/store"
)

func linkedPublicService(t *testing.T) (svc *Service, reader *publicresume.Reader, slug, entryID, projectID string) {
	t.Helper()
	slug, lng, name, title := "ada-lovelace", "en", "Ada", "Atlas"
	entryID, projectID = uuid.NewString(), uuid.NewString()
	document := schema.Resume{SchemaVersion: schema.CurrentVersion, PersonalDetails: schema.PersonalDetails{FullName: &name}, Content: map[string]schema.Section{
		"projects": {SectionType: schema.Project, ProjectEntries: []schema.ProjectEntry{{ID: entryID, Title: &title, ProjectID: &projectID}}},
	}}
	document.Customization.Layout.Sections.Main = []string{"projects"}
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
	backing := &publicServiceStore{
		row: store.Resume{ID: uuid.New(), Slug: &slug, Live: true, SEOGeoEnabled: true, Revision: 1, Lng: &lng, SchemaVersion: int32(schema.CurrentVersion),
			PersonalDetails: personal, Content: content, Customization: customization},
		links: json.RawMessage(`[{"entryId":"` + entryID + `","projectSlug":"atlas-tool","document":{"defaultLanguage":"en","text":{"en":{"name":"Atlas"}}}}]`),
	}
	reader, err = publicresume.NewReader(publicresume.ReaderDependencies{Store: backing, Projector: docmigrate.NewIdentityProjector(), Coordinator: coordinator, Origin: mustPublicOrigin(t)})
	if err != nil {
		t.Fatal(err)
	}
	cache, err := publiccache.New(4, time.Minute, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	return newPublicServiceForReader(t, reader, coordinator, cache), reader, slug, entryID, projectID
}

func servePublic(t *testing.T, svc *Service, path string) []byte {
	t.Helper()
	w := httptest.NewRecorder()
	svc.ServeHTTP(w, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", path, w.Code, w.Body.String())
	}
	return w.Body.Bytes()
}

func TestPublicJSON_CarriesProjectLinksWithoutTheProjectID(t *testing.T) {
	svc, _, slug, entryID, projectID := linkedPublicService(t)
	body := servePublic(t, svc, "/api/v1/public/resumes/"+slug)
	var envelope struct {
		Data struct {
			ProjectLinks []map[string]any `json:"projectLinks"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{{"entryId": entryID, "projectSlug": "atlas-tool", "url": "https://aboutme.example/ada-lovelace/projects/atlas-tool"}}
	if !reflect.DeepEqual(envelope.Data.ProjectLinks, want) {
		t.Fatalf("projectLinks = %v, want %v", envelope.Data.ProjectLinks, want)
	}
	if bytes.Contains(body, []byte(projectID)) || bytes.Contains(body, []byte(`"projectId"`)) {
		t.Fatalf("public JSON carries the project id: %s", body)
	}
}

func TestPublicMarkdownAndJSONLD_GainNoProjectSlugOrURL(t *testing.T) {
	svc, reader, slug, _, projectID := linkedPublicService(t)
	snapshot, lease, err := reader.ReadResume(context.Background(), slug, publicstate.RepresentationHTML)
	if err != nil {
		t.Fatal(err)
	}
	lease.Release()
	if len(snapshot.ProjectLinks) != 1 {
		t.Fatalf("snapshot links = %+v, want the one linked entry", snapshot.ProjectLinks)
	}
	jsonLD, err := publicformat.JSONLD(snapshot.Public, mustPublicOrigin(t), true)
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string][]byte{
		"markdown": servePublic(t, svc, "/"+slug+".md"),
		"json-ld":  jsonLD.JSON,
	} {
		for _, leak := range []string{"atlas-tool", "/projects/", projectID} {
			if bytes.Contains(body, []byte(leak)) {
				t.Fatalf("%s carries %q: %s", name, leak, body)
			}
		}
	}
}
