package directrender

import (
	"encoding/json"
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

func TestPublicRenderRequestHasOnlyTheClosedWireFields(t *testing.T) {
	// This fails if a client can add an ambient render-control field to the worker input.
	body, err := json.Marshal(PublicRenderRequest{
		PublicResume:     publicresume.PublicResume{Slug: "ada"},
		Mode:             PublicRenderMode,
		CanonicalOrigin:  "https://aboutme.example",
		DiscoveryEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	if len(fields) != 4 || string(fields["mode"]) != `"continuous"` || string(fields["canonicalOrigin"]) != `"https://aboutme.example"` || string(fields["discoveryEnabled"]) != "true" || fields["publicResume"] == nil {
		t.Fatalf("closed request = %s", body)
	}
}
