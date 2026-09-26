package directrender

import (
	"encoding/json"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/previewmeta"
	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
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
