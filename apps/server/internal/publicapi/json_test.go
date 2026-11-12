package publicapi

import (
	"encoding/json"
	"reflect"
	"testing"

	"danny.vn/aboutme/apps/server/internal/publicresume"
)

func TestPublicJSONAddsEnvelopeAndOneLF(t *testing.T) {
	response, err := NewPublicJSON(publicresume.PublicResume{Slug: "ada", Revision: "1", Lng: "en"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(response.Body), "{\"data\":{\"slug\":\"ada\",\"revision\":\"1\",\"lng\":\"en\",\"downloadEnabled\":false,\"document\":{\"schemaVersion\":0,\"personalDetails\":{\"fullName\":\"\"},\"content\":{},\"customization\":{\"colors\":{\"background\":\"\",\"primary\":\"\",\"text\":\"\"},\"dateFormat\":\"\",\"font\":{\"baseSizePx\":0,\"family\":\"\"},\"heading\":{\"showRule\":false,\"style\":\"\"},\"layout\":{\"columns\":0,\"sections\":{\"main\":null,\"sidebar\":null}},\"pageFormat\":\"\",\"sectionDisplay\":{\"language\":{\"style\":\"\"},\"skill\":{\"style\":\"\"}},\"spacing\":{\"entryGap\":0,\"lineHeight\":0,\"sectionGap\":0}}},\"projectLinks\":[]}}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}

func TestPublicJSON_ProjectLinksCarryEntrySlugAndURLOnly(t *testing.T) {
	links := []publicresume.ProjectLink{
		{EntryID: "4b0c5f9e-1d53-4c55-8f0f-7c6a3d3f1a10", ProjectSlug: "atlas", URL: "https://aboutme.example/ada/projects/atlas"},
		{EntryID: "9e3c1d2a-7a44-4f1b-9a61-0f3b8b6c2d21", ProjectSlug: "orbit-tool", URL: "https://aboutme.example/ada/projects/orbit-tool"},
	}
	response, err := NewPublicJSON(publicresume.PublicResume{Slug: "ada", Revision: "7", Lng: "en"}, links)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Data struct {
			ProjectLinks []map[string]any `json:"projectLinks"`
		} `json:"data"`
	}
	if err = json.Unmarshal(response.Body, &envelope); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{
		{"entryId": links[0].EntryID, "projectSlug": "atlas", "url": "https://aboutme.example/ada/projects/atlas"},
		{"entryId": links[1].EntryID, "projectSlug": "orbit-tool", "url": "https://aboutme.example/ada/projects/orbit-tool"},
	}
	if !reflect.DeepEqual(envelope.Data.ProjectLinks, want) {
		t.Fatalf("projectLinks = %v, want %v", envelope.Data.ProjectLinks, want)
	}
}
