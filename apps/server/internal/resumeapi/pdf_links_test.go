package resumeapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/publicresume"
	"danny.vn/aboutme/apps/server/internal/renderjob"
	"danny.vn/aboutme/apps/server/internal/resume"
)

type ownerPrintEnvelope struct {
	Version      int               `json:"version"`
	ProjectLinks map[string]string `json:"projectLinks"`
}

func linkedOwnerResume(t *testing.T, live bool) (resume.Resume, string) {
	t.Helper()
	document := loadMinimalDocument(t)
	entryID, projectID, title, slug, lng := uuid.NewString(), uuid.NewString(), "Atlas", "ada-lovelace", "en"
	document.Content["owner-projects"] = schema.NewProjectSection(nil, nil, []schema.ProjectEntry{{ID: entryID, Title: &title, ProjectID: &projectID}})
	document.Customization.Layout.Sections.Main = append(document.Customization.Layout.Sections.Main, "owner-projects")
	return resume.Resume{
		ID: uuid.MustParse("20000000-0000-4000-8000-000000000031"), UserID: uuid.MustParse("10000000-0000-4000-8000-000000000001"),
		Slug: &slug, Live: live, Lng: &lng, Revision: 5, Doc: document,
	}, entryID
}

func ownerLinkContext(t *testing.T, entryID, name string) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal([]map[string]any{{"entryId": entryID, "projectSlug": "atlas",
		"document": map[string]any{"defaultLanguage": "en", "text": map[string]any{"en": map[string]string{"name": name}}}}})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func ownerPDFEnvelope(t *testing.T, row resume.Resume, links json.RawMessage) (int, ownerPrintEnvelope) {
	t.Helper()
	origin, err := publicresume.ParsePublicOrigin("https://aboutme.example", "production")
	if err != nil {
		t.Fatal(err)
	}
	var frozen ownerPrintEnvelope
	queue := pdfQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
		snapshot, err := request.Prepare(ctx)
		if err != nil {
			return renderjob.Result{}, err
		}
		if err := json.Unmarshal(snapshot.Payload, &frozen); err != nil {
			t.Fatalf("decode frozen payload: %v", err)
		}
		return renderjob.Result{Bytes: []byte("%PDF-owner"), Revision: snapshot.Revision}, nil
	})
	service := &Service{
		resumes: pdfResumeReader{links: links, get: func(_ context.Context, userID, resumeID uuid.UUID) (resume.Resume, error) {
			if userID != row.UserID || resumeID != row.ID {
				return resume.Resume{}, resume.ErrNotFound
			}
			return row, nil
		}},
		printQueue: queue, canonicalOrigin: origin,
	}
	recorder := httptest.NewRecorder()
	service.handleDownloadResumePDF(recorder, pdfRequest(t, http.MethodGet, row.ID, nil))
	return recorder.Code, frozen
}

func TestOwnerPDFOfALiveResumeCarriesItsProjectLinks(t *testing.T) {
	t.Parallel()

	row, entryID := linkedOwnerResume(t, true)
	status, envelope := ownerPDFEnvelope(t, row, ownerLinkContext(t, entryID, "Atlas"))
	want := map[string]string{entryID: "https://aboutme.example/ada-lovelace/projects/atlas"}
	if status != http.StatusOK || envelope.Version != 2 || !reflect.DeepEqual(envelope.ProjectLinks, want) {
		t.Fatalf("owner PDF = %d %+v, want version 2 with %v", status, envelope, want)
	}
}

func TestOwnerPDFCarriesNoLinksWithoutAnAvailableLivePage(t *testing.T) {
	t.Parallel()

	for name, test := range map[string]struct {
		live bool
		name string
	}{
		"unpublished resume":  {live: false, name: "Atlas"},
		"blank-named project": {live: true, name: " "},
	} {
		t.Run(name, func(t *testing.T) {
			row, entryID := linkedOwnerResume(t, test.live)
			status, envelope := ownerPDFEnvelope(t, row, ownerLinkContext(t, entryID, test.name))
			if status != http.StatusOK || envelope.Version != 1 || envelope.ProjectLinks != nil {
				t.Fatalf("owner PDF = %d %+v, want version 1 without links", status, envelope)
			}
		})
	}
}

func TestOwnerPDFRefusesAMalformedLinkContext(t *testing.T) {
	t.Parallel()

	row, entryID := linkedOwnerResume(t, true)
	bare, err := json.Marshal([]map[string]string{{"entryId": entryID, "projectSlug": "atlas"}})
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := ownerPDFEnvelope(t, row, bare); status != http.StatusServiceUnavailable {
		t.Fatalf("owner PDF with a link but no document = %d, want 503", status)
	}
}

func TestOwnerPDFReadsTheLinkContextOfTheStoredRow(t *testing.T) {
	h := newResumeAPITestHarness(t)
	projectSlug := "atlas-" + uuid.NewString()[:8]
	projectID := insertLegacyProject(t, h, projectSlug, "https://github.com/ada/atlas")
	for name, live := range map[string]bool{"live": true, "unpublished": false} {
		t.Run(name, func(t *testing.T) {
			stored := seedLinkingResume(t, h, projectID, live)
			var frozen ownerPrintEnvelope
			h.service.printQueue = pdfQueueFunc(func(ctx context.Context, request renderjob.Request) (renderjob.Result, error) {
				snapshot, err := request.Prepare(ctx)
				if err != nil {
					return renderjob.Result{}, err
				}
				if err := json.Unmarshal(snapshot.Payload, &frozen); err != nil {
					t.Fatalf("decode frozen payload: %v", err)
				}
				return renderjob.Result{Bytes: []byte("%PDF-owner"), Revision: snapshot.Revision}, nil
			})
			response := h.request(t, http.MethodGet, apiResumePath+"/"+stored.ID.String()+"/pdf", nil, true, false)
			if response.status != http.StatusOK {
				t.Fatalf("owner PDF = %d %s", response.status, response.body)
			}
			if !live {
				if frozen.Version != 1 || frozen.ProjectLinks != nil {
					t.Fatalf("unpublished owner PDF = %+v, want version 1 without links", frozen)
				}
				return
			}
			entryID := ""
			for id := range storedLinks(t, h, stored.ID) {
				entryID = id
			}
			want := map[string]string{entryID: resumeAPITestOrigin + "/" + *stored.Slug + "/projects/" + projectSlug}
			if frozen.Version != 2 || !reflect.DeepEqual(frozen.ProjectLinks, want) {
				t.Fatalf("live owner PDF = %+v, want version 2 with %v", frozen, want)
			}
		})
	}
}
