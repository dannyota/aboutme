package publicapi

import (
	"testing"

	"github.com/dannyota/aboutme/apps/server/internal/publicresume"
)

func TestPublicJSONAddsEnvelopeAndOneLF(t *testing.T) {
	response, err := NewPublicJSON(publicresume.PublicResume{Slug: "ada", Revision: "1", Lng: "en"})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(response.Body), "{\"data\":{\"slug\":\"ada\",\"revision\":\"1\",\"lng\":\"en\",\"downloadEnabled\":false,\"document\":{\"schemaVersion\":0,\"personalDetails\":{\"fullName\":\"\"},\"content\":{},\"customization\":{\"colors\":{\"background\":\"\",\"primary\":\"\",\"text\":\"\"},\"dateFormat\":\"\",\"font\":{\"baseSizePx\":0,\"family\":\"\"},\"heading\":{\"showRule\":false,\"style\":\"\"},\"layout\":{\"columns\":0,\"sections\":{\"main\":null,\"sidebar\":null}},\"pageFormat\":\"\",\"sectionDisplay\":{\"language\":{\"style\":\"\"},\"skill\":{\"style\":\"\"}},\"spacing\":{\"entryGap\":0,\"lineHeight\":0,\"sectionGap\":0}}}}}\n"; got != want {
		t.Fatalf("body = %q, want %q", got, want)
	}
}
