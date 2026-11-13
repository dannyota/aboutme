package directrender

import (
	"bytes"
	"encoding/json"
	"testing"

	"danny.vn/aboutme/apps/server/internal/previewmeta"
	"danny.vn/aboutme/apps/server/internal/publicresume"
)

func TestPublicRenderRequestHasOnlyTheClosedWireFields(t *testing.T) {
	// This fails if a client can add an ambient render-control field to the worker input.
	body, err := json.Marshal(PublicRenderRequest{
		PublicResume:     publicresume.PublicResume{Slug: "ada"},
		Mode:             PublicRenderMode,
		CanonicalOrigin:  "https://aboutme.example",
		DiscoveryEnabled: true,
		PageTitle:        "Ada — Resume",
		Preview:          previewmeta.Meta{Title: "Ada", Description: "Resume on aboutme.vn", ImageAlt: "Ada"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 7 || string(fields["mode"]) != `"continuous"` || string(fields["canonicalOrigin"]) != `"https://aboutme.example"` || string(fields["discoveryEnabled"]) != "true" || fields["publicResume"] == nil ||
		string(fields["pageTitle"]) != `"Ada — Resume"` || string(fields["faviconHref"]) != `""` ||
		string(fields["preview"]) != `{"title":"Ada","description":"Resume on aboutme.vn","locale":"","imageAlt":"Ada"}` {
		t.Fatalf("closed request = %s", body)
	}
}

// The entry points travel as two fields that are omitted together, so a
// request for a resume without an available project keeps the closed shape
// above (docs/design/projects-contract.md "Entry points on the resume page").
func TestPublicRenderRequestEntryPointsAreTwoOptionalFields(t *testing.T) {
	body, err := json.Marshal(PublicRenderRequest{
		PublicResume: publicresume.PublicResume{Slug: "ada"},
		Mode:         PublicRenderMode,
		ProjectLinks: []EntryProjectLink{
			{EntryID: "e1", URL: "https://aboutme.example/ada/projects/atlas"},
			{EntryID: "e2", URL: "https://aboutme.example/ada/projects/beacon"},
		},
		ProjectsURL: "https://aboutme.example/ada/projects",
	})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if decodeErr := json.Unmarshal(body, &fields); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if len(fields) != 9 ||
		string(fields["projectLinks"]) != `[{"entryId":"e1","url":"https://aboutme.example/ada/projects/atlas"},{"entryId":"e2","url":"https://aboutme.example/ada/projects/beacon"}]` ||
		string(fields["projectsUrl"]) != `"https://aboutme.example/ada/projects"` {
		t.Fatalf("request with entry points = %s", body)
	}

	body, err = json.Marshal(PublicRenderRequest{Mode: PublicRenderMode, ProjectLinks: []EntryProjectLink{}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(body, []byte("projectLinks")) || bytes.Contains(body, []byte("projectsUrl")) {
		t.Fatalf("request without entry points = %s", body)
	}
}

// The card image URL is sent only while stored cards are on, so a renderer
// that predates the card keeps its closed preview object.
func TestPreviewImageURLIsSentOnlyWhenSet(t *testing.T) {
	body, err := json.Marshal(previewmeta.Meta{Title: "Ada", Description: "Resume on aboutme.vn", ImageAlt: "Ada", ImageURL: "https://aboutme.example/api/v1/public/resumes/ada/og/0123456789abcdef.png"})
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"title":"Ada","description":"Resume on aboutme.vn","locale":"","imageAlt":"Ada","imageUrl":"https://aboutme.example/api/v1/public/resumes/ada/og/0123456789abcdef.png"}` {
		t.Fatalf("preview with a card = %s", body)
	}
}
