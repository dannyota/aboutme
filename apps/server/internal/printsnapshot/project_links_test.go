package printsnapshot

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"

	schema "danny.vn/aboutme/packages/schema/gen/go"

	"danny.vn/aboutme/apps/server/internal/resume"
)

const (
	linkedFirstURL  = "https://aboutme.example/ada-lovelace/projects/atlas"
	linkedSecondURL = "https://aboutme.example/ada-lovelace/projects/beacon"
)

func linkedOwnerEnvelope(t *testing.T) Envelope {
	t.Helper()
	name, lng, hidden := "Ada", "en", true
	title := func(value string) *string { return &value }
	source := resume.Resume{ID: uuid.MustParse(testResumeID), Revision: 3, Lng: &lng, Doc: schema.Resume{
		SchemaVersion:   schema.CurrentVersion,
		PersonalDetails: schema.PersonalDetails{FullName: &name},
		Content: map[string]schema.Section{
			"profile": schema.NewProfileSection(nil, nil, []schema.ProfileEntry{{ID: "profile-entry", Text: title("Builds things.")}}),
			"projects": schema.NewProjectSection(nil, nil, []schema.ProjectEntry{
				{ID: "first", Title: title("Atlas"), Link: title("https://atlas.example")},
				{ID: "second", Title: title("Beacon")},
				{ID: "hidden", Title: title("Hidden"), IsHidden: &hidden},
			}),
			"unlisted": schema.NewProjectSection(nil, nil, []schema.ProjectEntry{{ID: "unlisted-entry", Title: title("Unlisted")}}),
		},
		Customization: schema.Customization{Layout: schema.Layout{Sections: schema.Sections{Main: []string{"profile", "projects"}}}},
	}}
	envelope, err := FromOwner(source, nil, "")
	if err != nil {
		t.Fatalf("FromOwner: %v", err)
	}
	return envelope
}

func withLinks(envelope Envelope, version int, links map[string]string) Envelope {
	envelope.Version = version
	envelope.ProjectLinks = links
	return envelope
}

func TestMarshalVersionTwoAddsOnlyProjectLinks(t *testing.T) {
	plain := linkedOwnerEnvelope(t)
	v1, err := Marshal(plain)
	if err != nil {
		t.Fatalf("Marshal version 1: %v", err)
	}
	if bytes.Contains(v1, []byte("projectLinks")) {
		t.Fatalf("version 1 carries projectLinks: %s", v1)
	}
	emptyMap, err := Marshal(withLinks(plain, 1, map[string]string{}))
	if err != nil || !bytes.Equal(emptyMap, v1) {
		t.Fatalf("version 1 with an empty map = %s, %v; want the version-1 bytes", emptyMap, err)
	}

	v2, err := Marshal(withLinks(plain, 2, map[string]string{"second": linkedSecondURL, "first": linkedFirstURL}))
	if err != nil {
		t.Fatalf("Marshal version 2: %v", err)
	}
	wantTail := `,"projectLinks":{"first":"` + linkedFirstURL + `","second":"` + linkedSecondURL + `"}}`
	if !bytes.HasSuffix(v2, []byte(wantTail)) {
		t.Fatalf("version 2 does not end with the sorted map: %s", v2)
	}
	wantV2 := strings.Replace(string(v1[:len(v1)-1]), `{"version":1,`, `{"version":2,`, 1) + wantTail
	if string(v2) != wantV2 {
		t.Fatalf("version 2 differs from version 1 beyond version and projectLinks:\n got %s\nwant %s", v2, wantV2)
	}
}

func TestMarshalRefusesProjectLinksThatBreakTheVersionRule(t *testing.T) {
	plain := linkedOwnerEnvelope(t)
	valid := map[string]string{"first": linkedFirstURL}
	if _, err := Marshal(withLinks(plain, 2, valid)); err != nil {
		t.Fatalf("valid version 2 refused: %v", err)
	}
	link := func(entryID, url string) map[string]string {
		return map[string]string{"first": linkedFirstURL, entryID: url}
	}
	tests := []struct {
		name     string
		envelope Envelope
	}{
		{"version 2 without a map", withLinks(plain, 2, nil)},
		{"version 2 with an empty map", withLinks(plain, 2, map[string]string{})},
		{"version 1 with a map", withLinks(plain, 1, valid)},
		{"version 3 with a map", withLinks(plain, 3, valid)},
		{"unknown entry", withLinks(plain, 2, link("missing", linkedSecondURL))},
		{"empty entry id", withLinks(plain, 2, link("", linkedSecondURL))},
		{"hidden entry", withLinks(plain, 2, link("hidden", linkedSecondURL))},
		{"entry of another section type", withLinks(plain, 2, link("profile-entry", linkedSecondURL))},
		{"entry of a section outside the layout", withLinks(plain, 2, link("unlisted-entry", linkedSecondURL))},
		{"ftp value", withLinks(plain, 2, link("second", "ftp://aboutme.example/ada-lovelace/projects/beacon"))},
		{"upper-case scheme", withLinks(plain, 2, link("second", "HTTPS://aboutme.example/ada-lovelace/projects/beacon"))},
		{"relative value", withLinks(plain, 2, link("second", "/ada-lovelace/projects/beacon"))},
		{"empty value", withLinks(plain, 2, link("second", ""))},
		{"no host", withLinks(plain, 2, link("second", "https:///ada-lovelace/projects/beacon"))},
		{"user info", withLinks(plain, 2, link("second", "https://user@aboutme.example/ada-lovelace/projects/beacon"))},
		{"query", withLinks(plain, 2, link("second", linkedSecondURL+"?x=1"))},
		{"fragment", withLinks(plain, 2, link("second", linkedSecondURL+"#top"))},
		{"space", withLinks(plain, 2, link("second", "https://aboutme.example/ada-lovelace/projects/be acon"))},
		{"javascript", withLinks(plain, 2, link("second", "javascript:alert(1)"))},
		{"arbitrary http path", withLinks(plain, 2, link("second", "http://outside.example/arbitrary"))},
		{"root path", withLinks(plain, 2, link("second", "https://aboutme.example/"))},
		{"no path", withLinks(plain, 2, link("second", "https://aboutme.example"))},
		{"project list path", withLinks(plain, 2, link("second", "https://aboutme.example/ada-lovelace/projects"))},
		{"empty project slug", withLinks(plain, 2, link("second", "https://aboutme.example/ada-lovelace/projects/"))},
		{"trailing slash", withLinks(plain, 2, link("second", linkedSecondURL+"/"))},
		{"extra segment", withLinks(plain, 2, link("second", linkedSecondURL+"/extra"))},
		{"other middle segment", withLinks(plain, 2, link("second", "https://aboutme.example/ada-lovelace/project/beacon"))},
		{"invalid resume slug", withLinks(plain, 2, link("second", "https://aboutme.example/ada/projects/beacon"))},
		{"reserved resume slug", withLinks(plain, 2, link("second", "https://aboutme.example/login/projects/beacon"))},
		{"invalid project slug", withLinks(plain, 2, link("second", "https://aboutme.example/ada-lovelace/projects/Beacon"))},
		{"percent-encoded path", withLinks(plain, 2, link("second", "https://aboutme.example/ada%2Dlovelace/projects/beacon"))},
		{"dot segment", withLinks(plain, 2, link("second", "https://aboutme.example/ada-lovelace/projects/../beacon"))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if encoded, err := Marshal(test.envelope); err == nil {
				t.Fatalf("Marshal accepted %s: %s", test.name, encoded)
			}
		})
	}
}

func TestMarshalAcceptsSidebarProjectEntries(t *testing.T) {
	envelope := linkedOwnerEnvelope(t)
	envelope.Document.Customization.Layout.Sections.Sidebar = []string{"unlisted"}
	if _, err := Marshal(withLinks(envelope, 2, map[string]string{"unlisted-entry": linkedSecondURL})); err != nil {
		t.Fatalf("sidebar project entry refused: %v", err)
	}
}

func TestSnapshotsStillFreezeVersionOne(t *testing.T) {
	envelope := linkedOwnerEnvelope(t)
	if envelope.Version != 1 || envelope.ProjectLinks != nil {
		t.Fatalf("FromOwner version %d, links %v", envelope.Version, envelope.ProjectLinks)
	}
}
